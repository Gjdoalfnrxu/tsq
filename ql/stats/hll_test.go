package stats

import (
	"math"
	"testing"
)

// HLL accuracy property: for n distinct values uniformly distributed
// over the hash space, the relative error should be bounded by ~3 ×
// the theoretical std error (1.04/√m ≈ 0.81% at m=16384). 3σ is a
// generous bound that should fail with vanishingly low probability
// even with deterministic inputs (no random seeding in HLL itself).
func TestHLL_AccuracyAcrossScales(t *testing.T) {
	cases := []struct {
		name string
		n    int
	}{
		{"100", 100},
		{"1000", 1000},
		{"10000", 10000},
		{"100000", 100000},
		{"1000000", 1000000},
	}
	const tol = 0.03 // 3% — well above 3σ for our register count
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := NewHLL()
			for i := 0; i < tc.n; i++ {
				h.AddUint64(uint64(i + 1))
			}
			est := h.Estimate()
			rel := math.Abs(float64(est)-float64(tc.n)) / float64(tc.n)
			if rel > tol {
				t.Fatalf("n=%d: estimate=%d, rel-error=%.4f exceeds tol=%.4f", tc.n, est, rel, tol)
			}
			t.Logf("n=%d: est=%d (rel-err=%.4f%%)", tc.n, est, rel*100)
		})
	}
}

// AddBytes path: uses FNV-1a; verify the distinct-string case lands
// within the same accuracy band.
func TestHLL_BytesAccuracy(t *testing.T) {
	const n = 50000
	h := NewHLL()
	for i := 0; i < n; i++ {
		h.AddBytes([]byte("v" + itoa(i)))
	}
	est := h.Estimate()
	rel := math.Abs(float64(est)-float64(n)) / float64(n)
	if rel > 0.03 {
		t.Fatalf("est=%d, rel-error=%.4f exceeds 0.03", est, rel)
	}
}

// HLL must be insensitive to multiplicity (only distinct values count).
func TestHLL_RepeatedAdds(t *testing.T) {
	h := NewHLL()
	for r := 0; r < 100; r++ {
		for i := 0; i < 1000; i++ {
			h.AddUint64(uint64(i + 1))
		}
	}
	est := h.Estimate()
	rel := math.Abs(float64(est)-1000) / 1000
	if rel > 0.05 {
		t.Fatalf("est=%d after 100x repeats: rel-err=%.4f", est, rel)
	}
}

// Merge: union semantics.
func TestHLL_MergeUnion(t *testing.T) {
	a, b := NewHLL(), NewHLL()
	for i := 0; i < 5000; i++ {
		a.AddUint64(uint64(i + 1))
	}
	for i := 3000; i < 8000; i++ {
		b.AddUint64(uint64(i + 1))
	}
	a.Merge(b)
	est := a.Estimate() // expect ≈ 8000 distinct
	rel := math.Abs(float64(est)-8000) / 8000
	if rel > 0.03 {
		t.Fatalf("union est=%d, rel-err=%.4f", est, rel)
	}
}

// IntersectEstimate: inclusion-exclusion sanity.
func TestHLL_IntersectEstimate(t *testing.T) {
	a, b := NewHLL(), NewHLL()
	for i := 0; i < 5000; i++ {
		a.AddUint64(uint64(i + 1))
	}
	for i := 3000; i < 8000; i++ {
		b.AddUint64(uint64(i + 1))
	}
	got := IntersectEstimate(a, b)
	want := int64(2000) // overlap is i in [3000, 5000)
	relErr := math.Abs(float64(got-want)) / float64(want)
	// Inclusion-exclusion compounds the std error: tolerate 15%.
	if relErr > 0.15 {
		t.Fatalf("intersect est=%d, want≈%d, rel-err=%.4f", got, want, relErr)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(b[pos:])
}
