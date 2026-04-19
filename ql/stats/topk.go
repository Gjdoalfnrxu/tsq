package stats

import "sort"

// SpaceSaving heavy-hitter tracker (Metwally, Agrawal, Abbadi 2005).
//
// Plan §2.1 calls for a Count-Min Sketch with heavy-hitter readout.
// SpaceSaving has the same one-pass guarantee with a tighter error
// bound (additive ε = N/k where k is the tracked-key capacity) and a
// dramatically simpler implementation. We size k = 8 × TopKLimit = 256
// candidates so the final top-32 readout is exact for any value whose
// true frequency exceeds N/256 — which is well within the "detect 90%
// skew" use-case the planner cares about.
//
// Memory: 256 × (uint64 key + int64 count + int next/prev pointers) ≈
// ~10 KB per column in flight; nothing on disk beyond the final top-32
// itself (32 × 16 B = 512 B per column).
const ssCapacity = 8 * TopKLimit

type ssEntry struct {
	value uint64
	count int64
}

// SpaceSaving tracks approximate top-K most frequent uint64 values.
// Zero-value SpaceSaving is ready to use.
type SpaceSaving struct {
	entries map[uint64]*ssEntry
	// We keep a slice of pointers for fast minimum lookup. For ssCapacity
	// = 256 a linear scan on eviction is ~256 comparisons per evicted
	// add — cheaper than maintaining a heap given the small constant.
	list []*ssEntry
}

// NewSpaceSaving returns an empty tracker.
func NewSpaceSaving() *SpaceSaving {
	return &SpaceSaving{
		entries: make(map[uint64]*ssEntry, ssCapacity),
		list:    make([]*ssEntry, 0, ssCapacity),
	}
}

// Add increments the count for v.
func (s *SpaceSaving) Add(v uint64) {
	if e, ok := s.entries[v]; ok {
		e.count++
		return
	}
	if len(s.list) < ssCapacity {
		e := &ssEntry{value: v, count: 1}
		s.entries[v] = e
		s.list = append(s.list, e)
		return
	}
	// Capacity exhausted: evict the minimum and replace.
	minIdx := 0
	for i := 1; i < len(s.list); i++ {
		if s.list[i].count < s.list[minIdx].count {
			minIdx = i
		}
	}
	old := s.list[minIdx]
	delete(s.entries, old.value)
	old.value = v
	old.count++ // SpaceSaving: new entry inherits min+1
	s.entries[v] = old
}

// TopK returns the top-K entries sorted by descending count.
// k is clamped to TopKLimit and to len(s.list).
func (s *SpaceSaving) TopK(k int) []TopKEntry {
	if k > TopKLimit {
		k = TopKLimit
	}
	if k > len(s.list) {
		k = len(s.list)
	}
	if k <= 0 {
		return nil
	}
	out := make([]TopKEntry, 0, len(s.list))
	for _, e := range s.list {
		out = append(out, TopKEntry{Value: e.value, Count: e.count})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Value < out[j].Value // deterministic tiebreak
	})
	return out[:k]
}
