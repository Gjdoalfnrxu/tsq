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

// Regression for BLOCKER 2 (PR #175 review): a "ratchet" tail value
// whose raw count exceeds a true heavy's raw count.
//
// Construction:
//  1. Drive every slot's count up to ~K by pouring many distinct
//     singletons (table thrashes, min climbs each pass).
//  2. Insert a small set of true heavies that gain a handful of organic
//     hits. Their raw count is ~K + heavyHits, lower bound = heavyHits.
//  3. Pick one specific tail value V and ratchet it: alternate Add(V)
//     with Add(unique). Each Add(V) inserts V at count = min+1, then
//     Add(unique) evicts V (V is now min) and the new slot gets
//     count = V.count + 1. After N cycles V's raw count climbs to
//     ~K + N, but its lower bound (count - err) stays pinned at 1
//     because every entry it's inherited from carried its full count
//     as error.
//
// Pre-fix code sorts by raw count: the ratcheted V (raw ≈ K+N)
// outranks the heavies (raw ≈ K+heavyHits) when N > heavyHits, and
// the heavies fall out of the top-K. Post-fix code sorts by
// `count - err`: heavies (lower = heavyHits) beat V (lower = 1).
//
// Verified against pre-fix topk.go (commit 11b0574^): swapped that
// file in and this test failed; restored post-fix file and it passes.
func TestSpaceSaving_AdversarialRatchet_HeaviesOutranked(t *testing.T) {
	ss := NewSpaceSaving()

	// Phase 1: fill + thrash. Pour distinct singletons to drive every
	// slot's count up to ~ratchetK (so the bulk min sits well above
	// the heavy organic-hit count we'll add in Phase 2).
	const ratchetK = 8
	const fillChurn = ssCapacity * (ratchetK + 2)
	var nextUnique uint64 = 1_000_000
	for i := 0; i < fillChurn; i++ {
		ss.Add(nextUnique)
		nextUnique++
	}

	// Phase 2: insert true heavies with a small number of organic hits
	// each. They evict a min-slot on first contact (count = min+1)
	// and then count up cleanly; lower bound under post-fix = heavyHits.
	// Heavy IDs are deliberately LARGER than every tail-resident ID
	// (tail uses sequential IDs starting at 1_000_000), so the
	// deterministic value-ascending tiebreak among ties at the same
	// raw count actively works AGAINST the heavies. Pre-fix sort-by-
	// raw-count ties heavies with hundreds of tail residents at
	// count = baseline_min + 5; tiebreak then deterministically
	// puts the smaller-IDed tail residents first, crowding heavies
	// out of the top-K window.
	const heavyHits = 5
	heavies := []uint64{
		9_999_999_001, 9_999_999_002, 9_999_999_003,
		9_999_999_004, 9_999_999_005,
	}
	for i := 0; i < heavyHits; i++ {
		for _, v := range heavies {
			ss.Add(v)
		}
	}

	// Phase 3: ratchet a single tail value V. Each cycle: insert V (it
	// re-enters at min+1), then a unique singleton evicts V (V is now
	// min) and the new slot's count becomes V.count+1 under pre-fix.
	// V's raw count climbs ~1 per cycle while its true frequency stays
	// at 1 per cycle of presence. Post-fix: V's lower bound = 1.
	// (V isn't strictly load-bearing for top-K crowding; the tail
	// residents in Phase 1 already do that. V is included to make the
	// "ratcheting" behaviour the test name advertises observable in
	// the dump if the test fails for a different reason.)
	const ratchetCycles = ssCapacity * 4
	const vID uint64 = 42
	for i := 0; i < ratchetCycles; i++ {
		ss.Add(vID)
		ss.Add(nextUnique)
		nextUnique++
	}

	top := ss.TopK(10)

	// All 5 heavies must be in the top-K. Pre-fix sort-by-raw allows
	// the ratcheted residents to crowd them out.
	heavyHit := map[uint64]bool{}
	for _, e := range top {
		heavyHit[e.Value] = true
	}
	missing := []uint64{}
	for _, v := range heavies {
		if !heavyHit[v] {
			missing = append(missing, v)
		}
	}
	if len(missing) > 0 {
		t.Errorf("heavy hitters %v missing from top-K under ratcheted-tail attack: %+v", missing, top)
	}
}

// Empty tracker yields nil.
func TestSpaceSaving_Empty(t *testing.T) {
	ss := NewSpaceSaving()
	if got := ss.TopK(5); got != nil {
		t.Fatalf("expected nil from empty, got %+v", got)
	}
}
