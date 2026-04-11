package eval

import "strconv"

// RelKey encodes a relation's identity as (name, arity). The eval engine
// keys all relation maps by RelKey rather than name alone, so that
// different-arity relations sharing a name (e.g., a class characteristic
// predicate `C(this) :- C(this, _, _)` defined alongside a base 3-arity
// schema relation `C`) cannot collide and corrupt joins.
//
// The encoding is "<name>/<arity>" — chosen so the existing
// map[string]*Relation type can keep working without churning every
// internal API. RelKey strings are only ever produced by relKey() and
// only ever consumed by lookups inside this package.
func relKey(name string, arity int) string {
	return name + "/" + strconv.Itoa(arity)
}

// RelsOf builds a map[string]*Relation keyed correctly by (name, arity)
// from a list of *Relation values. This is the canonical way for callers
// (and tests) to construct an input rels map.
func RelsOf(rels ...*Relation) map[string]*Relation {
	m := make(map[string]*Relation, len(rels))
	for _, r := range rels {
		if r == nil {
			continue
		}
		m[relKey(r.Name, r.Arity)] = r
	}
	return m
}

// keyRels rekeys a (possibly name-keyed) map to the canonical (name, arity)
// keying. Each *Relation must have its Name and Arity set correctly.
// This is used by Evaluate to migrate baseRels from a legacy name-keyed
// map to the keyed form before any internal lookups happen.
func keyRels(in map[string]*Relation) map[string]*Relation {
	out := make(map[string]*Relation, len(in))
	for _, r := range in {
		if r == nil {
			continue
		}
		out[relKey(r.Name, r.Arity)] = r
	}
	return out
}
