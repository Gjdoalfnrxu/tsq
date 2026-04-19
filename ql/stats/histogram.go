package stats

import (
	"math/rand"
	"sort"
)

// reservoirSize controls the sample size used to build equi-depth
// histograms. With 64 buckets and a 65536-sample reservoir, each
// bucket is sourced from ~1024 samples — well above the 30-sample
// floor where bucket boundaries become statistically meaningful.
const reservoirSize = 1 << 16 // 65536

// Reservoir is Vitter algorithm-R reservoir sampling for uint64. Used
// to bound histogram-build memory regardless of relation size.
//
// Why reservoir sampling: equi-depth bucket boundaries are quantiles,
// and quantile estimation from a uniform sample of size n has
// confidence-interval width O(1/√n). At n=65536 the p99 estimate is
// good to ~0.4% — more than enough for join cardinality arithmetic.
type Reservoir struct {
	samples []uint64
	seen    int64
	rng     *rand.Rand
}

// NewReservoir returns an empty reservoir. seed=0 is fine for production
// (deterministic across runs is desirable for reproducible plans —
// see plan §8.3 determinism property).
func NewReservoir(seed int64) *Reservoir {
	return &Reservoir{
		samples: make([]uint64, 0, reservoirSize),
		rng:     rand.New(rand.NewSource(seed)),
	}
}

// Add offers v to the reservoir.
func (r *Reservoir) Add(v uint64) {
	r.seen++
	if len(r.samples) < reservoirSize {
		r.samples = append(r.samples, v)
		return
	}
	// Replace samples[j] with probability reservoirSize/seen
	j := r.rng.Int63n(r.seen)
	if j < int64(reservoirSize) {
		r.samples[j] = v
	}
}

// Histogram builds an equi-depth histogram with `buckets` buckets over
// the reservoir. totalRows is the underlying relation's row count, used
// to scale per-bucket Count estimates back to the population.
//
// Returns nil when the reservoir is empty or buckets ≤ 0.
func (r *Reservoir) Histogram(buckets int, totalRows int64) []Bucket {
	if buckets <= 0 || len(r.samples) == 0 {
		return nil
	}
	xs := make([]uint64, len(r.samples))
	copy(xs, r.samples)
	sort.Slice(xs, func(i, j int) bool { return xs[i] < xs[j] })

	n := len(xs)
	if buckets > n {
		buckets = n
	}

	out := make([]Bucket, 0, buckets)
	per := n / buckets
	extra := n % buckets
	idx := 0
	for b := 0; b < buckets; b++ {
		size := per
		if b < extra {
			size++
		}
		end := idx + size - 1
		if end >= n {
			end = n - 1
		}
		// Bucket count is the per-sample size scaled to totalRows.
		// Using totalRows (not r.seen) keeps the buckets summing to
		// totalRows even when the reservoir saw a subset of inserts
		// (it shouldn't in our use, but be defensive).
		bucketCount := int64(size) * totalRows / int64(n)
		out = append(out, Bucket{
			Lo:    xs[idx],
			Hi:    xs[end],
			Count: bucketCount,
		})
		idx = end + 1
		if idx >= n {
			break
		}
	}
	// Distribute any rounding remainder onto the last bucket so the
	// histogram totals to exactly totalRows (callers that sanity-check
	// will find the sum matches).
	if len(out) > 0 {
		var sum int64
		for _, b := range out {
			sum += b.Count
		}
		out[len(out)-1].Count += totalRows - sum
	}
	return out
}
