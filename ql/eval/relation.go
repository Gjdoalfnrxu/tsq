package eval

import (
	"fmt"
	"strings"
)

// Value is a typed Datalog value.
type Value interface{ evalValue() }

// IntVal is an integer (or entity ref) Datalog value.
type IntVal struct{ V int64 }

// StrVal is a string Datalog value.
type StrVal struct{ V string }

func (IntVal) evalValue() {}
func (StrVal) evalValue() {}

// Tuple is a row of values.
type Tuple []Value

// tupleKey encodes a Tuple as a string suitable for use as a map key.
// Uses \x00 as separator (assumes strings don't contain \x00).
func tupleKey(t Tuple) string {
	if len(t) == 0 {
		return ""
	}
	var b strings.Builder
	for i, v := range t {
		if i > 0 {
			b.WriteByte('\x00')
		}
		switch vv := v.(type) {
		case IntVal:
			fmt.Fprintf(&b, "i%d", vv.V)
		case StrVal:
			b.WriteByte('s')
			b.WriteString(vv.V)
		default:
			b.WriteString("?")
		}
	}
	return b.String()
}

// partialKey encodes specific columns of a Tuple as a map key.
func partialKey(t Tuple, cols []int) string {
	var b strings.Builder
	for i, col := range cols {
		if i > 0 {
			b.WriteByte('\x00')
		}
		if col >= len(t) {
			b.WriteString("nil")
			continue
		}
		switch vv := t[col].(type) {
		case IntVal:
			fmt.Fprintf(&b, "i%d", vv.V)
		case StrVal:
			b.WriteByte('s')
			b.WriteString(vv.V)
		default:
			b.WriteString("?")
		}
	}
	return b.String()
}

// sortedColKey returns a canonical string key for a column list, based on the
// sorted column indices. This ensures Index([0,2]) and Index([2,0]) produce
// the same key, and that Lookup always uses the same ordering as Index build.
func sortedColKey(cols []int) string {
	// Copy and sort so the key is order-independent.
	sorted := make([]int, len(cols))
	copy(sorted, cols)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j] < sorted[j-1]; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	var b strings.Builder
	for i, c := range sorted {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%d", c)
	}
	return b.String()
}

// HashIndex maps a key (values of specific columns) to matching tuple indices.
type HashIndex struct {
	cols  []int
	index map[string][]int // key → list of indices into the parent relation's tuples
}

// Lookup returns the tuple indices (into the owning Relation's Tuples slice)
// matching the given key values on the indexed columns.
// key[i] is the value to match for hi.cols[i].
func (hi *HashIndex) Lookup(key []Value) []int {
	// Build a lookup key from the values directly (not via column re-indexing).
	seqCols := make([]int, len(key))
	for i := range key {
		seqCols[i] = i
	}
	k := partialKey(Tuple(key), seqCols)
	return hi.index[k]
}

// Relation is an in-memory set of tuples with lazy hash indexes.
type Relation struct {
	Name    string
	Arity   int
	tuples  []Tuple
	set     map[string]struct{} // deduplication
	indexes map[string]*HashIndex
}

// NewRelation creates an empty relation.
func NewRelation(name string, arity int) *Relation {
	return &Relation{
		Name:    name,
		Arity:   arity,
		set:     make(map[string]struct{}),
		indexes: make(map[string]*HashIndex),
	}
}

// Add adds a tuple to the relation. Returns true if the tuple was new (actually added).
// Invalidates all indexes.
func (r *Relation) Add(t Tuple) bool {
	k := tupleKey(t)
	if _, exists := r.set[k]; exists {
		return false
	}
	r.set[k] = struct{}{}
	r.tuples = append(r.tuples, t)
	// Update all existing indexes incrementally.
	for _, idx := range r.indexes {
		colKey := partialKey(t, idx.cols)
		idx.index[colKey] = append(idx.index[colKey], len(r.tuples)-1)
	}
	return true
}

// Contains reports whether the relation contains the given tuple.
func (r *Relation) Contains(t Tuple) bool {
	_, ok := r.set[tupleKey(t)]
	return ok
}

// Tuples returns all tuples in the relation.
func (r *Relation) Tuples() []Tuple {
	return r.tuples
}

// Len returns the number of tuples.
func (r *Relation) Len() int {
	return len(r.tuples)
}

// Index returns (building lazily) a HashIndex over the given columns.
// cols is sorted canonically so Index([0,2]) and Index([2,0]) share one index.
func (r *Relation) Index(cols []int) *HashIndex {
	// Canonicalise col order for the map key and index build.
	sorted := make([]int, len(cols))
	copy(sorted, cols)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j] < sorted[j-1]; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	key := sortedColKey(sorted)
	if hi, ok := r.indexes[key]; ok {
		return hi
	}
	hi := &HashIndex{
		cols:  sorted,
		index: make(map[string][]int, len(r.tuples)),
	}
	for i, t := range r.tuples {
		k := partialKey(t, sorted)
		hi.index[k] = append(hi.index[k], i)
	}
	r.indexes[key] = hi
	return hi
}
