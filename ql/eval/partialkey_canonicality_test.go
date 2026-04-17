package eval

import (
	"testing"
)

// TestPartialKeyCanonicality_SameValuesSameKey: same column subset and equal
// values produce equal keys.
func TestPartialKeyCanonicality_SameValuesSameKey(t *testing.T) {
	t1 := Tuple{IntVal{1}, StrVal{"a"}, IntVal{42}}
	t2 := Tuple{IntVal{1}, StrVal{"zzz"}, IntVal{42}} // differs only at col 1
	cols := []int{0, 2}
	if partialKey(t1, cols) != partialKey(t2, cols) {
		t.Fatalf("expected equal partialKey for same values at cols %v: %q vs %q",
			cols, partialKey(t1, cols), partialKey(t2, cols))
	}
}

// TestPartialKeyCanonicality_DifferentValuesDifferentKey: differing values on
// any selected column produce different keys.
func TestPartialKeyCanonicality_DifferentValuesDifferentKey(t *testing.T) {
	t1 := Tuple{IntVal{1}, StrVal{"a"}, IntVal{42}}
	t2 := Tuple{IntVal{2}, StrVal{"a"}, IntVal{42}}
	cols := []int{0, 1, 2}
	if partialKey(t1, cols) == partialKey(t2, cols) {
		t.Fatalf("expected different partialKey for different col-0 values")
	}
}

// TestPartialKeyCanonicality_TypeDistinguished: int 0 and string "0" must
// produce different keys (no cross-type collision).
func TestPartialKeyCanonicality_TypeDistinguished(t *testing.T) {
	t1 := Tuple{IntVal{0}}
	t2 := Tuple{StrVal{"0"}}
	if partialKey(t1, []int{0}) == partialKey(t2, []int{0}) {
		t.Fatalf("int and string with same printed value must have distinct keys")
	}
}

// TestPartialKeyCanonicality_StringSeparatorSafety: ensures the \x00 separator
// is not confused by adjacent values that, when concatenated naively, could
// collide. e.g. ("ab", "c") vs ("a", "bc").
func TestPartialKeyCanonicality_StringSeparatorSafety(t *testing.T) {
	t1 := Tuple{StrVal{"ab"}, StrVal{"c"}}
	t2 := Tuple{StrVal{"a"}, StrVal{"bc"}}
	cols := []int{0, 1}
	if partialKey(t1, cols) == partialKey(t2, cols) {
		t.Fatalf("partialKey collided across separator: %q vs %q",
			partialKey(t1, cols), partialKey(t2, cols))
	}
}

// TestIndexLookupAgreement_SortedCols: when the caller's bound-col list is
// already in sorted ascending order (which is how applyPositive/applyNegative
// build it — by iterating atom.Args in order), Index([sorted]).Lookup(vals)
// agrees with the manual full-equality check on every matching tuple.
//
// This is the canonicality contract that gates change (a) — we may drop the
// post-Lookup re-check ONLY in this regime.
func TestIndexLookupAgreement_SortedCols(t *testing.T) {
	r := NewRelation("R", 3)
	tuples := []Tuple{
		{IntVal{1}, StrVal{"a"}, IntVal{10}},
		{IntVal{1}, StrVal{"b"}, IntVal{10}},
		{IntVal{2}, StrVal{"a"}, IntVal{10}},
		{IntVal{1}, StrVal{"a"}, IntVal{20}},
		{IntVal{1}, StrVal{"a"}, IntVal{10}}, // dup, won't add
	}
	for _, tu := range tuples {
		r.Add(tu)
	}

	// Probe: bound cols 0,2 (sorted), values (1,10) — should match rows 0 and 1.
	cols := []int{0, 2}
	vals := []Value{IntVal{1}, IntVal{10}}
	idx := r.Index(cols)
	got := idx.Lookup(vals)

	// Reference: scan and compare bound cols pointwise.
	var want []int
	for i, tu := range r.Tuples() {
		ok := true
		for j, c := range cols {
			eq, err := Compare("=", tu[c], vals[j])
			if err != nil || !eq {
				ok = false
				break
			}
		}
		if ok {
			want = append(want, i)
		}
	}

	if len(got) != len(want) {
		t.Fatalf("Lookup returned %d rows, reference scan returned %d (got=%v want=%v)",
			len(got), len(want), got, want)
	}
	gotSet := map[int]bool{}
	for _, i := range got {
		gotSet[i] = true
	}
	for _, i := range want {
		if !gotSet[i] {
			t.Fatalf("Lookup missed row %d (vs reference). got=%v want=%v", i, got, want)
		}
	}
}

// TestIndexLookupAgreement_AllArities: exhaustively check 1, 2, and 3 column
// sorted-bound subsets of a 3-arity relation. Every Index().Lookup() result
// must match a reference scan.
func TestIndexLookupAgreement_AllArities(t *testing.T) {
	r := NewRelation("R", 3)
	for i := 0; i < 5; i++ {
		for j := 0; j < 3; j++ {
			for k := 0; k < 4; k++ {
				r.Add(Tuple{IntVal{int64(i)}, StrVal{string(rune('a' + j))}, IntVal{int64(k)}})
			}
		}
	}

	// All sorted column subsets.
	subsets := [][]int{
		{0}, {1}, {2},
		{0, 1}, {0, 2}, {1, 2},
		{0, 1, 2},
	}
	for _, cols := range subsets {
		// Pick a probe value for each column.
		probe := make([]Value, len(cols))
		for i, c := range cols {
			switch c {
			case 0:
				probe[i] = IntVal{2}
			case 1:
				probe[i] = StrVal{"b"}
			case 2:
				probe[i] = IntVal{1}
			}
		}
		idx := r.Index(cols)
		got := idx.Lookup(probe)

		var want []int
		for i, tu := range r.Tuples() {
			ok := true
			for j, c := range cols {
				eq, err := Compare("=", tu[c], probe[j])
				if err != nil || !eq {
					ok = false
					break
				}
			}
			if ok {
				want = append(want, i)
			}
		}

		if len(got) != len(want) {
			t.Fatalf("cols=%v: Lookup=%d rows, scan=%d rows", cols, len(got), len(want))
		}
		gs := map[int]bool{}
		for _, i := range got {
			gs[i] = true
		}
		for _, i := range want {
			if !gs[i] {
				t.Fatalf("cols=%v: Lookup missed row %d", cols, i)
			}
		}
	}
}
