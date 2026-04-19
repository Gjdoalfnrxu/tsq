package stats

import "sort"

// SpaceSaving heavy-hitter tracker (Metwally, Agrawal, Abbadi 2005).
//
// Plan §2.1 calls for a Count-Min Sketch with heavy-hitter readout.
// SpaceSaving has the same one-pass guarantee with a tighter error
// bound and a dramatically simpler implementation. We size k = 8 ×
// TopKLimit = 256 candidates.
//
// What the algorithm actually guarantees:
//   - For each tracked entry e, the true count of e's value lies in
//     [e.count - e.error, e.count]. e.error is the count e inherited
//     from the entry it evicted on first insertion; subsequent
//     increments add only to e.count.
//   - Any value whose true frequency exceeds N/k is guaranteed to be
//     present in the tracker at end-of-stream (where N is the total
//     stream length). Values that never crossed N/k MAY also be
//     present but are spurious — distinguishable at readout because
//     their `count - error` is small (often near zero).
//
// Readout filters by `count - error` (the lower bound of the true
// frequency) so only entries we can defend as heavy survive. Without
// the error field, an adversarial input order — long tail first,
// heavy hitters last — can leave the heavies with tiny counts that
// look indistinguishable from the spurious tail at readout. With the
// error field the heavies surface because their `count - error` is
// large even when their raw count is not.
//
// Memory: 256 × (uint64 key + 2 × int64) ≈ ~6 KB per column in
// flight; nothing on disk beyond the final top-32 itself.
const ssCapacity = 8 * TopKLimit

type ssEntry struct {
	value uint64
	count int64
	// error is the upper bound on the over-count for this entry: the
	// count this slot already had when `value` first occupied it (i.e.
	// the count of the predecessor at eviction time). For entries
	// added before capacity was reached, error == 0.
	err int64
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
	// SpaceSaving invariant: the new entry's count starts at
	// (min_count + 1), and its error bound is min_count — i.e. the
	// new entry "inherited" up to min_count occurrences worth of
	// over-count from the slot it took over. The lower bound on the
	// new entry's true frequency is therefore (count - err) = 1.
	inheritedCount := old.count
	old.value = v
	old.count = inheritedCount + 1
	old.err = inheritedCount
	s.entries[v] = old
}

// TopK returns the top-K entries sorted by descending lower-bound
// count (count - err). Entries with a zero or negative lower bound are
// dropped — they're spurious tail values that happened to occupy a
// slot at end-of-stream and carry no defensible frequency claim.
//
// k is clamped to TopKLimit and to len(s.list). The reported
// TopKEntry.Count is the lower bound (count - err), which is the
// quantity the planner can safely cite as a minimum frequency. The
// raw count would be an over-count by up to err.
func (s *SpaceSaving) TopK(k int) []TopKEntry {
	if k > TopKLimit {
		k = TopKLimit
	}
	if k <= 0 || len(s.list) == 0 {
		return nil
	}
	out := make([]TopKEntry, 0, len(s.list))
	for _, e := range s.list {
		lower := e.count - e.err
		if lower <= 0 {
			continue
		}
		out = append(out, TopKEntry{Value: e.value, Count: lower})
	}
	if len(out) == 0 {
		return nil
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Value < out[j].Value // deterministic tiebreak
	})
	if k > len(out) {
		k = len(out)
	}
	return out[:k]
}
