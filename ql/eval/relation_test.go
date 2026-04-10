package eval

import (
	"testing"
)

func TestRelationAddDedup(t *testing.T) {
	r := NewRelation("test", 2)
	t1 := Tuple{IntVal{1}, StrVal{"a"}}
	t2 := Tuple{IntVal{2}, StrVal{"b"}}
	t3 := Tuple{IntVal{1}, StrVal{"a"}} // duplicate of t1

	if !r.Add(t1) {
		t.Fatal("expected Add(t1) to return true (new)")
	}
	if !r.Add(t2) {
		t.Fatal("expected Add(t2) to return true (new)")
	}
	if r.Add(t3) {
		t.Fatal("expected Add(t3) to return false (duplicate)")
	}
	if r.Len() != 2 {
		t.Fatalf("expected 2 tuples, got %d", r.Len())
	}
}

func TestRelationContains(t *testing.T) {
	r := NewRelation("test", 2)
	t1 := Tuple{IntVal{42}, StrVal{"hello"}}
	r.Add(t1)

	if !r.Contains(t1) {
		t.Error("Contains should return true for added tuple")
	}
	if r.Contains(Tuple{IntVal{99}, StrVal{"hello"}}) {
		t.Error("Contains should return false for absent tuple")
	}
}

func TestRelationEmpty(t *testing.T) {
	r := NewRelation("empty", 3)
	if r.Len() != 0 {
		t.Errorf("expected 0, got %d", r.Len())
	}
	if len(r.Tuples()) != 0 {
		t.Error("expected empty tuples slice")
	}
	if r.Contains(Tuple{IntVal{1}, IntVal{2}, IntVal{3}}) {
		t.Error("empty relation should contain nothing")
	}
}

func TestRelationIndexSingleColumn(t *testing.T) {
	r := NewRelation("rel", 3)
	r.Add(Tuple{IntVal{1}, StrVal{"a"}, IntVal{10}})
	r.Add(Tuple{IntVal{1}, StrVal{"b"}, IntVal{20}})
	r.Add(Tuple{IntVal{2}, StrVal{"a"}, IntVal{30}})
	r.Add(Tuple{IntVal{3}, StrVal{"c"}, IntVal{40}})

	idx := r.Index([]int{0}) // index on first column
	hits := idx.Lookup([]Value{IntVal{1}})
	if len(hits) != 2 {
		t.Fatalf("expected 2 hits for key=1, got %d", len(hits))
	}
	// Verify the tuples at those indices are correct.
	tuples := r.Tuples()
	for _, hi := range hits {
		if tuples[hi][0] != (IntVal{1}) {
			t.Errorf("expected first col = 1, got %v", tuples[hi][0])
		}
	}

	hits2 := idx.Lookup([]Value{IntVal{99}})
	if len(hits2) != 0 {
		t.Error("expected 0 hits for absent key")
	}
}

func TestRelationIndexMultiColumn(t *testing.T) {
	r := NewRelation("rel", 3)
	r.Add(Tuple{IntVal{1}, StrVal{"x"}, IntVal{10}})
	r.Add(Tuple{IntVal{1}, StrVal{"x"}, IntVal{20}})
	r.Add(Tuple{IntVal{1}, StrVal{"y"}, IntVal{30}})
	r.Add(Tuple{IntVal{2}, StrVal{"x"}, IntVal{40}})

	// Index on (col0, col1).
	idx := r.Index([]int{0, 1})
	hits := idx.Lookup([]Value{IntVal{1}, StrVal{"x"}})
	if len(hits) != 2 {
		t.Fatalf("expected 2 hits for (1, x), got %d: %v", len(hits), hits)
	}
	hits2 := idx.Lookup([]Value{IntVal{1}, StrVal{"y"}})
	if len(hits2) != 1 {
		t.Fatalf("expected 1 hit for (1, y), got %d", len(hits2))
	}
	hits3 := idx.Lookup([]Value{IntVal{2}, StrVal{"x"}})
	if len(hits3) != 1 {
		t.Fatalf("expected 1 hit for (2, x), got %d", len(hits3))
	}
}

func TestRelationIndexAfterAdd(t *testing.T) {
	// Index is built lazily; verify it is consistent after adding more tuples.
	r := NewRelation("rel", 2)
	r.Add(Tuple{IntVal{1}, StrVal{"a"}})
	idx := r.Index([]int{0}) // build index now
	r.Add(Tuple{IntVal{1}, StrVal{"b"}})
	r.Add(Tuple{IntVal{2}, StrVal{"c"}})

	hits := idx.Lookup([]Value{IntVal{1}})
	if len(hits) != 2 {
		t.Fatalf("expected 2 hits after incremental add, got %d", len(hits))
	}
	hits2 := idx.Lookup([]Value{IntVal{2}})
	if len(hits2) != 1 {
		t.Fatalf("expected 1 hit for key=2, got %d", len(hits2))
	}
}
