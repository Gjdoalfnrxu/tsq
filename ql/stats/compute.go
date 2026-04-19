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

func (c *colAccum) addInt(v int32) {
	c.rows++
	if v == 0 {
		c.nulls++
	}
	uv := uint64(uint32(v))
	c.hll.AddUint64(uv)
	c.heavy.Add(uv)
	c.reservoir.Add(uv)
}

func (c *colAccum) addStringID(idx uint32, raw []byte) {
	c.rows++
	if idx == 0 {
		// String table index 0 is the empty string by writer convention.
		c.nulls++
	}
	c.hll.AddBytes(raw)
	c.heavy.Add(uint64(idx))
	c.reservoir.Add(uint64(idx))
}

// finalise produces a ColStats from the accumulator.
// pos is the column position; relRows is the relation's row count.
func (c *colAccum) finalise(pos int, relRows int64) ColStats {
	ndv := c.hll.Estimate()
	cs := ColStats{
		Pos:  pos,
		NDV:  ndv,
		TopK: c.heavy.TopK(TopKLimit),
	}
	if c.rows > 0 {
		cs.NullFrac = float64(c.nulls) / float64(c.rows)
	}
	if ndv > NDVHistogramThreshold {
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
		BuiltAt:       time.Now().UTC(),
		Rels:          make(map[string]*RelStats),
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
				switch def.Columns[c].Type {
				case schema.TypeInt32, schema.TypeEntityRef:
					v, err := rel.GetInt(t, c)
					if err != nil {
						return nil, fmt.Errorf("stats: %s[%d].col%d: %w", def.Name, t, c, err)
					}
					accs[c].addInt(v)
				case schema.TypeString:
					str, err := rel.GetString(database, t, c)
					if err != nil {
						return nil, fmt.Errorf("stats: %s[%d].col%d: %w", def.Name, t, c, err)
					}
					// We need the interned id for TopK/reservoir. Re-use the
					// writer's intern via the public API: AddTuple already
					// populated the column with an index but we don't expose
					// it. Hash the string for HLL; assign a stable id by
					// hashing for top-K (collisions only affect TopK
					// preview, not NDV — tolerable).
					id := fnv32(str)
					accs[c].addStringID(id, []byte(str))
				}
			}
		}
		for c := range accs {
			rs.Cols[c] = accs[c].finalise(c, int64(nrows))
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

	lhll := NewHLL()
	for t := 0; t < lrows; t++ {
		v, err := left.GetInt(t, jp.LeftCol)
		if err != nil {
			return JoinStats{}, err
		}
		lhll.AddUint64(uint64(uint32(v)))
	}
	rhll := NewHLL()
	rightSet := make(map[int32]struct{}, rrows)
	for t := 0; t < rrows; t++ {
		v, err := right.GetInt(t, jp.RightCol)
		if err != nil {
			return JoinStats{}, err
		}
		rhll.AddUint64(uint64(uint32(v)))
		rightSet[v] = struct{}{}
	}

	js := JoinStats{
		LeftRel:         jp.LeftRel,
		LeftCol:         jp.LeftCol,
		RightRel:        jp.RightRel,
		RightCol:        jp.RightCol,
		DistinctMatches: IntersectEstimate(lhll, rhll),
	}

	// Selectivity by sampling: for up to 1024 left rows, count matches
	// against the right set, scale up.
	const probeSize = 1024
	probe := lrows
	if probe > probeSize {
		probe = probeSize
	}
	if probe == 0 || rrows == 0 {
		return js, nil
	}
	step := lrows / probe
	if step < 1 {
		step = 1
	}
	var hits int64
	for t := 0; t < lrows; t += step {
		v, _ := left.GetInt(t, jp.LeftCol)
		if _, ok := rightSet[v]; ok {
			hits++
		}
	}
	probedLeft := int64(0)
	for t := 0; t < lrows; t += step {
		probedLeft++
	}
	if probedLeft == 0 || rrows == 0 {
		return js, nil
	}
	// Selectivity := |L⋈R| / (|L| × |R|)
	// Estimate |L⋈R| from the sample: each probed left row that hits
	// joins with (rrows / distinct_right_keys) right rows on average.
	// For NDV-balanced right keys this collapses to (hits/probed) ×
	// (lrows × rrows / distinct_right_keys), giving:
	// selectivity = hits / (probed × distinct_right_keys).
	distinctRight := rhll.Estimate()
	if distinctRight <= 0 {
		distinctRight = int64(len(rightSet))
	}
	if distinctRight > 0 {
		js.Selectivity = float64(hits) / (float64(probedLeft) * float64(distinctRight))
	}
	return js, nil
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

// fnv32 is a tiny inline FNV-1a 32-bit hash. Used as a string-ID
// surrogate for TopK/reservoir on string columns where we don't have
// the writer's intern table accessible.
func fnv32(s string) uint32 {
	const (
		offset32 = 2166136261
		prime32  = 16777619
	)
	h := uint32(offset32)
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= prime32
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
