package stats

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/Gjdoalfnrxu/tsq/extract/db"
	"github.com/Gjdoalfnrxu/tsq/extract/schema"
)

// colAccum is the in-flight accumulator for one column.
type colAccum struct {
	hll       *HLL
	heavy     *SpaceSaving
	reservoir *Reservoir
	nulls     int64
	rows      int64
}

func newColAccum(seed int64) *colAccum {
	return &colAccum{
		hll:       NewHLL(),
		heavy:     NewSpaceSaving(),
		reservoir: NewReservoir(seed),
	}
}

// addInt records one int32/entity-ref cell. If nullable is true, v == 0
// is counted as a null (the sole sentinel for int/entity-ref columns).
// For non-nullable columns NullFrac stays at 0 — real EDB rows can and
// do carry id 0 as a legitimate value (e.g. the first interned file).
func (c *colAccum) addInt(v int32, nullable bool) {
	c.rows++
	if nullable && v == 0 {
		c.nulls++
	}
	uv := uint64(uint32(v))
	c.hll.AddUint64(uv)
	c.heavy.Add(uv)
	c.reservoir.Add(uv)
}

// addStringID records one string cell. If nullable is true, intern id 0
// (the empty-string slot by writer convention) is counted as null.
func (c *colAccum) addStringID(idx uint64, raw []byte, nullable bool) {
	c.rows++
	if nullable && idx == 0 {
		c.nulls++
	}
	c.hll.AddBytes(raw)
	c.heavy.Add(idx)
	c.reservoir.Add(idx)
}

// finalise produces a ColStats from the accumulator.
// pos is the column position; relRows is the relation's row count.
// emitHistogram=false suppresses the equi-depth histogram even when NDV
// crosses NDVHistogramThreshold — used for TypeString columns, whose
// surrogate uint64 ids carry no usable order, so a numeric equi-depth
// histogram would be meaningless to the planner. The planner's
// consumer-side default-selectivity fallback handles the absence.
func (c *colAccum) finalise(pos int, relRows int64, emitHistogram bool) ColStats {
	ndv := c.hll.Estimate()
	cs := ColStats{
		Pos:  pos,
		NDV:  ndv,
		TopK: c.heavy.TopK(TopKLimit),
	}
	if c.rows > 0 {
		cs.NullFrac = float64(c.nulls) / float64(c.rows)
	}
	if emitHistogram && ndv > NDVHistogramThreshold {
		cs.HistBuckets = c.reservoir.Histogram(HistogramBuckets, relRows)
	}
	return cs
}

// Compute walks every relation in `database`, builds per-column stats,
// then computes JoinStats for the declared JoinPaired set.
//
// edbHash is the SHA-256 of the EDB on disk; the caller is responsible
// for computing it (see HashFile). Compute does not touch the disk.
func Compute(database *db.DB, edbHash [HashSize]byte) (*Schema, error) {
	if database == nil {
		return nil, fmt.Errorf("stats.Compute: nil database")
	}
	s := &Schema{
		FormatVersion: FormatVersion,
		EDBHash:       edbHash,
		// Truncate to second: on-disk encoding is Unix seconds, so
		// keeping nanosecond precision in-memory creates a
		// round-trip mismatch (`s.BuiltAt != Decode(Encode(s)).BuiltAt`).
		BuiltAt: time.Now().UTC().Truncate(time.Second),
		Rels:    make(map[string]*RelStats),
	}

	// Per-relation: walk every tuple, accumulate per column.
	for _, def := range schema.Registry {
		rel := database.Relation(def.Name)
		nrows := rel.Tuples()
		rs := &RelStats{
			Name:     def.Name,
			Arity:    def.Arity(),
			RowCount: int64(nrows),
			Cols:     make([]ColStats, def.Arity()),
		}
		accs := make([]*colAccum, def.Arity())
		for i := range accs {
			// Seed the reservoir per-(rel, col) deterministically so
			// stats are reproducible across runs.
			accs[i] = newColAccum(int64(stableSeed(def.Name, i)))
		}
		for t := 0; t < nrows; t++ {
			for c := 0; c < def.Arity(); c++ {
				nullable := def.Columns[c].Nullable
				switch def.Columns[c].Type {
				case schema.TypeInt32, schema.TypeEntityRef:
					v, err := rel.GetInt(t, c)
					if err != nil {
						return nil, fmt.Errorf("stats: %s[%d].col%d: %w", def.Name, t, c, err)
					}
					accs[c].addInt(v, nullable)
				case schema.TypeString:
					str, err := rel.GetString(database, t, c)
					if err != nil {
						return nil, fmt.Errorf("stats: %s[%d].col%d: %w", def.Name, t, c, err)
					}
					// 64-bit FNV-1a id surrogate. The writer's intern id is
					// not exposed via the public Relation API, so we hash
					// the string ourselves to assign a stable id for
					// TopK/reservoir. 64-bit space keeps collisions
					// negligible at the EDB-string-table ceiling
					// (~50% collision probability only at ≈5 × 10^9
					// distinct strings, vs ~77k for the previous 32-bit
					// surrogate). HLL still uses the bytes directly.
					id := fnv64(str)
					accs[c].addStringID(id, []byte(str), nullable)
				}
			}
		}
		for c := range accs {
			emitHist := def.Columns[c].Type != schema.TypeString
			rs.Cols[c] = accs[c].finalise(c, int64(nrows), emitHist)
		}
		s.Rels[def.Name] = rs
	}

	// JoinStats: empty in v1 — see joinpaired.go for declarations.
	for _, jp := range JoinPaired {
		js, err := computeJoin(database, jp)
		if err != nil {
			// Soft-fail: skip the bad pair, record nothing. JoinStats
			// are advisory; better to ship without than to abort.
			continue
		}
		s.Joins = append(s.Joins, js)
	}

	return s, nil
}

// computeJoin produces the precomputed selectivity for one JoinPaired
// declaration. Two-pass: build per-side HLLs over the keys, then
// inclusion-exclusion for distinct matches; sample for selectivity.
func computeJoin(database *db.DB, jp JoinPair) (JoinStats, error) {
	leftDef, ok := schema.Lookup(jp.LeftRel)
	if !ok {
		return JoinStats{}, fmt.Errorf("unknown rel %q", jp.LeftRel)
	}
	rightDef, ok := schema.Lookup(jp.RightRel)
	if !ok {
		return JoinStats{}, fmt.Errorf("unknown rel %q", jp.RightRel)
	}
	if jp.LeftCol >= leftDef.Arity() || jp.RightCol >= rightDef.Arity() {
		return JoinStats{}, fmt.Errorf("col oob")
	}
	left := database.Relation(jp.LeftRel)
	right := database.Relation(jp.RightRel)
	lrows := left.Tuples()
	rrows := right.Tuples()

	// setCap bounds either-side membership probe set. Big-side FK pairs
	// (Contains, ParamBinding, ...) can be enormous and we only need a
	// representative population to estimate hit rate from the
	// fixed-stride sample on the other side. Cap at 2× probeSize so the
	// probe-vs-set ratio stays informative; HLLs still see every row,
	// so DistinctMatches is exact-up-to-HLL across the full join.
	const probeSize = 1024
	const setCap = 2 * probeSize

	lhll := NewHLL()
	leftSet := make(map[int32]struct{}, min(lrows, setCap))
	for t := 0; t < lrows; t++ {
		v, err := left.GetInt(t, jp.LeftCol)
		if err != nil {
			return JoinStats{}, err
		}
		lhll.AddUint64(uint64(uint32(v)))
		if len(leftSet) < setCap {
			leftSet[v] = struct{}{}
		}
	}
	rhll := NewHLL()
	rightSet := make(map[int32]struct{}, min(rrows, setCap))
	for t := 0; t < rrows; t++ {
		v, err := right.GetInt(t, jp.RightCol)
		if err != nil {
			return JoinStats{}, err
		}
		rhll.AddUint64(uint64(uint32(v)))
		if len(rightSet) < setCap {
			rightSet[v] = struct{}{}
		}
	}

	js := JoinStats{
		LeftRel:         jp.LeftRel,
		LeftCol:         jp.LeftCol,
		RightRel:        jp.RightRel,
		RightCol:        jp.RightCol,
		DistinctMatches: IntersectEstimate(lhll, rhll),
	}

	if lrows == 0 || rrows == 0 {
		return js, nil
	}

	js.LRSelectivity = sampleSelectivity(left, jp.LeftCol, lrows, rightSet, rhll, probeSize)
	js.RLSelectivity = sampleSelectivity(right, jp.RightCol, rrows, leftSet, lhll, probeSize)
	return js, nil
}

// sampleSelectivity probes up to probeSize source rows with a fixed
// stride against the targetSet membership set and returns the
// inclusion-exclusion estimate
//
//	selectivity ≈ hits / (probed × distinct_target_keys)
//
// where hits is the number of probed rows whose key is present in
// targetSet, and distinct_target_keys is the HLL distinct-count of the
// target column. When the target set was capped (large-side FK), hits
// is a downward-biased estimator: the planner prefers a conservative
// (under-) estimate of selectivity to an over-estimate.
func sampleSelectivity(
	source interface {
		GetInt(t, c int) (int32, error)
	},
	col, srcRows int,
	targetSet map[int32]struct{},
	targetHLL *HLL,
	probeSize int,
) float64 {
	step := srcRows / probeSize
	if step < 1 {
		step = 1
	}
	var hits int64
	var probed int64
	for t := 0; t < srcRows; t += step {
		v, _ := source.GetInt(t, col)
		probed++
		if _, ok := targetSet[v]; ok {
			hits++
		}
	}
	if probed == 0 {
		return 0
	}
	distinctTarget := targetHLL.Estimate()
	if distinctTarget <= 0 {
		distinctTarget = int64(len(targetSet))
	}
	if distinctTarget <= 0 {
		return 0
	}
	return float64(hits) / (float64(probed) * float64(distinctTarget))
}

// HashFile returns the SHA-256 of the file at path. Caller passes the
// result to Compute as edbHash, and the loader uses it on Load to
// reject stale sidecars (plan §2.3).
func HashFile(path string) ([HashSize]byte, error) {
	var out [HashSize]byte
	f, err := os.Open(path)
	if err != nil {
		return out, fmt.Errorf("stats.HashFile: %w", err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return out, fmt.Errorf("stats.HashFile: %w", err)
	}
	copy(out[:], h.Sum(nil))
	return out, nil
}

// fnv64 is FNV-1a 64-bit. Used as a string-id surrogate for
// TopK/reservoir on string columns where the writer's intern table is
// not exposed via the public Relation API. 64-bit width keeps
// collisions negligible for the EDB string-table sizes we expect (the
// previous 32-bit surrogate hit a 50% collision probability around
// 77,000 distinct strings, which corrupted histogram boundaries on
// even moderate fact databases).
func fnv64(s string) uint64 {
	const (
		offset64 uint64 = 14695981039346656037
		prime64  uint64 = 1099511628211
	)
	h := offset64
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= prime64
	}
	return h
}

// stableSeed produces a deterministic per-(rel, col) seed for reservoir
// sampling, so that stats sidecars are bytewise reproducible across
// runs (plan §8.3 determinism).
func stableSeed(relName string, col int) uint64 {
	h := uint64(14695981039346656037)
	for i := 0; i < len(relName); i++ {
		h ^= uint64(relName[i])
		h *= 1099511628211
	}
	h ^= uint64(col) * 0x9e3779b97f4a7c15
	return h
}
