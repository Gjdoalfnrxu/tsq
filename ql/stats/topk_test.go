package stats

import "testing"

// SpaceSaving must surface the genuinely-heavy hitters even when many
// low-frequency values are seen too.
func TestSpaceSaving_HeavyHittersSurfaced(t *testing.T) {
	ss := NewSpaceSaving()
	// Heavy hitters: id 1..5 each get 10000 hits.
	for i := 0; i < 10000; i++ {
		for v := uint64(1); v <= 5; v++ {
			ss.Add(v)
		}
	}
	// Long tail: 5000 distinct values each with ~1 hit.
	for v := uint64(1000); v < 6000; v++ {
		ss.Add(v)
	}
	top := ss.TopK(10)
	if len(top) < 5 {
		t.Fatalf("expected ≥5 entries, got %d", len(top))
	}
	gotHeavy := map[uint64]bool{}
	for i := 0; i < 5; i++ {
		gotHeavy[top[i].Value] = true
	}
	for v := uint64(1); v <= 5; v++ {
		if !gotHeavy[v] {
			t.Errorf("heavy hitter %d missing from top-5: %+v", v, top[:5])
		}
	}
}

// TopK clamps to TopKLimit even if asked for more.
func TestSpaceSaving_TopKClamped(t *testing.T) {
	ss := NewSpaceSaving()
	for v := uint64(0); v < 100; v++ {
		for i := 0; i < int(v)+1; i++ {
			ss.Add(v)
		}
	}
	top := ss.TopK(1000) // request more than limit
	if len(top) > TopKLimit {
		t.Fatalf("len(top)=%d exceeds TopKLimit=%d", len(top), TopKLimit)
	}
}

// Regression for BLOCKER 2 (PR #175 review): adversarial input order.
// The standard "heavies first, tail second" test passes regardless of
// whether SpaceSaving tracks per-entry error. Reverse the order — pour
// the long tail in until capacity is exhausted, then add the heavies
// — and the heavies' raw counts are tiny (they took an evicted slot
// after the tail filled the table). Without per-entry error tracking,
// the readout cannot distinguish the heavies from the spurious tail
// values still occupying slots.
//
// With error tracking, each heavy's `count - err` is the lower bound
// on its true frequency, and the heavies surface even with small raw
// counts because the tail residents have `count - err` near zero.
func TestSpaceSaving_AdversarialOrder_HeaviesSurfaceLast(t *testing.T) {
	ss := NewSpaceSaving()

	// Tail first: 4 × ssCapacity distinct singleton values. Capacity
	// fills up and then thrashes: every new tail value evicts an
	// older tail value, leaving the table populated entirely by tail
	// residents at end of phase 1.
	const tailValues = 4 * ssCapacity
	for v := uint64(1000); v < uint64(1000+tailValues); v++ {
		ss.Add(v)
	}

	// Heavies last: ids 1..5, each 50 hits. Because they start by
	// evicting a tail entry, their initial count inherits the
	// minimum (which is small but > 0 after thrashing).
	for i := 0; i < 50; i++ {
		for v := uint64(1); v <= 5; v++ {
			ss.Add(v)
		}
	}

	top := ss.TopK(10)
	if len(top) < 5 {
		t.Fatalf("expected ≥5 entries above the lower-bound threshold, got %d: %+v", len(top), top)
	}
	heavyHit := map[uint64]bool{}
	for _, e := range top[:5] {
		heavyHit[e.Value] = true
	}
	missing := []uint64{}
	for v := uint64(1); v <= 5; v++ {
		if !heavyHit[v] {
			missing = append(missing, v)
		}
	}
	if len(missing) > 0 {
		t.Errorf("heavy hitters %v missing from top-5 under adversarial order: %+v", missing, top[:5])
	}
}

// Empty tracker yields nil.
func TestSpaceSaving_Empty(t *testing.T) {
	ss := NewSpaceSaving()
	if got := ss.TopK(5); got != nil {
		t.Fatalf("expected nil from empty, got %+v", got)
	}
}
