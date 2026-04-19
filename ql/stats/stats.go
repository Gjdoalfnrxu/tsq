// Package stats implements the EDB statistics sidecar — a per-relation
// summary (HyperLogLog distinct counts, top-K frequents, equi-depth
// histograms, declared join selectivities) written next to a tsq fact
// database and consulted by the planner for cardinality estimation.
//
// File format: see docs/design/stats-sidecar-format.md.
//
// This package is the source-of-truth for §1 and §2 of the Phase B plan
// (docs/design/valueflow-phase-b-plan.md). It is deliberately self-
// contained: no dependency on ql/plan or ql/eval. The planner consumer
// arrives in a follow-on PR ("PR2b"); until then, the sidecar is
// computed and persisted but unused.
package stats

import "time"

// FormatVersion is bumped on any incompatible change to the on-disk
// sidecar layout.
const FormatVersion uint32 = 1

// Magic identifies a tsq stats sidecar file.
const Magic = "TSQS\x00"

// TopKLimit caps the number of top-frequent values per column. Plan §1.2
// rationale: 32 is enough to detect 90% skew without bloating the file.
const TopKLimit = 32

// HistogramBuckets is the equi-depth histogram bucket count. Plan §1.2:
// 64, half of CodeQL's 128, sufficient for our planner's σ arithmetic.
const HistogramBuckets = 64

// NDVHistogramThreshold is the minimum NDV at which a histogram is
// emitted. Below this, the TopK already covers the distribution.
const NDVHistogramThreshold int64 = 256

// HashSize is the EDB content hash length in bytes (SHA-256).
// See docs/design/stats-sidecar-format.md §5 for the BLAKE2b vs SHA-256
// substitution rationale.
const HashSize = 32

// Schema is the top-level sidecar payload.
type Schema struct {
	FormatVersion uint32
	EDBHash       [HashSize]byte
	BuiltAt       time.Time
	Rels          map[string]*RelStats
	Joins         []JoinStats
}

// RelStats holds per-relation summary.
type RelStats struct {
	Name     string
	Arity    int
	RowCount int64
	Cols     []ColStats
}

// ColStats holds per-column summary.
type ColStats struct {
	Pos         int
	NDV         int64
	NullFrac    float64
	TopK        []TopKEntry
	HistBuckets []Bucket
}

// TopKEntry is one (value, count) pair in the top-K most frequent values.
type TopKEntry struct {
	Value uint64
	Count int64
}

// Bucket is one equi-depth histogram bucket. [Lo, Hi] inclusive.
type Bucket struct {
	Lo, Hi uint64
	Count  int64
}

// JoinStats is the precomputed selectivity of a declared FK-like pair.
// Plan §1.1: emitted only for relation/column pairs annotated as
// JoinPaired in the schema.
//
// Selectivity is asymmetric and recorded as two floats:
//
//   - LRSelectivity: probability that a random left-side row matches
//     at least one right-side row on the declared columns. Used for
//     planning a join where the left is the build/outer side.
//   - RLSelectivity: the symmetric quantity for right→left.
//
// A symmetric scalar (the previous shape) was wrong for any planner
// asking "how many right rows survive a probe from this left row?" —
// the answer differs by orders of magnitude on skewed FK pairs (e.g.
// Contains: a child has exactly one parent; a parent has many
// children). Bumping the shape now while no consumer reads JoinStats
// avoids a format-version migration in PR2b.
type JoinStats struct {
	LeftRel         string
	LeftCol         int
	RightRel        string
	RightCol        int
	LRSelectivity   float64
	RLSelectivity   float64
	DistinctMatches int64
}

// Lookup returns the stats for relation name, or nil if absent.
// Cheap nil-safe accessor for the planner's consumer side.
func (s *Schema) Lookup(name string) *RelStats {
	if s == nil {
		return nil
	}
	return s.Rels[name]
}
