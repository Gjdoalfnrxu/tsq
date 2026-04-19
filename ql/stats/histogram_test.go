package stats

import "testing"

// Equi-depth: bucket counts should sum to totalRows exactly, and
// boundaries should be monotone non-decreasing.
func TestHistogram_BucketsSumToTotal(t *testing.T) {
	r := NewReservoir(42)
	const n = 100000
	for i := 0; i < n; i++ {
		r.Add(uint64(i))
	}
	h := r.Histogram(64, n)
	if len(h) == 0 {
		t.Fatal("empty histogram")
	}
	var sum int64
	for i, b := range h {
		sum += b.Count
		if b.Lo > b.Hi {
			t.Errorf("bucket[%d]: lo=%d > hi=%d", i, b.Lo, b.Hi)
		}
		if i > 0 && b.Lo < h[i-1].Hi {
			// Buckets may touch but should not interleave.
			t.Errorf("bucket[%d].Lo=%d < bucket[%d].Hi=%d (overlap)", i, b.Lo, i-1, h[i-1].Hi)
		}
	}
	if sum != n {
		t.Fatalf("sum=%d, want %d", sum, n)
	}
}

// Equi-depth: per-bucket counts should be approximately equal for a
// uniform input.
func TestHistogram_EquiDepthBalanced(t *testing.T) {
	r := NewReservoir(7)
	const n = 64000
	for i := 0; i < n; i++ {
		r.Add(uint64(i))
	}
	h := r.Histogram(64, n)
	want := int64(n) / 64
	for i, b := range h {
		// Tolerate ±10% for last-bucket remainder absorption.
		if b.Count < want*9/10 || b.Count > want*11/10 {
			// Allow last bucket some slack since it absorbs remainders.
			if i == len(h)-1 {
				continue
			}
			t.Errorf("bucket[%d].Count=%d, want ≈%d", i, b.Count, want)
		}
	}
}

// Empty reservoir → nil.
func TestHistogram_EmptyReservoir(t *testing.T) {
	r := NewReservoir(0)
	if h := r.Histogram(64, 0); h != nil {
		t.Fatalf("expected nil, got %+v", h)
	}
}
