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

// Empty tracker yields nil.
func TestSpaceSaving_Empty(t *testing.T) {
	ss := NewSpaceSaving()
	if got := ss.TopK(5); got != nil {
		t.Fatalf("expected nil from empty, got %+v", got)
	}
}
