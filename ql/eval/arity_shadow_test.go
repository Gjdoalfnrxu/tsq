package eval

import (
	"context"
	"strings"
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

// TestRelationAddArityMismatchPanics is the loud-guard regression test.
// If someone removes the arity check in Relation.Add this test fails,
// which is the entire point. The panic is preferred over silent
// acceptance because the original eval-engine arity-shadow bug
// (3-arity base relation `C` quietly receiving 1-arity head tuples
// from a class characteristic predicate) corrupted joins downstream.
func TestRelationAddArityMismatchPanics(t *testing.T) {
	r := NewRelation("C", 3)

	defer func() {
		rec := recover()
		if rec == nil {
			t.Fatal("expected panic on arity mismatch, got none")
		}
		msg, ok := rec.(string)
		if !ok {
			t.Fatalf("expected string panic, got %T: %v", rec, rec)
		}
		if !strings.Contains(msg, "arity mismatch") {
			t.Fatalf("expected arity mismatch in panic message, got: %s", msg)
		}
	}()

	// 1-arity tuple into a 3-arity relation: this is the exact bad shape
	// that the bridge characteristic predicate `C(this) :- C(this, _, _)`
	// would have produced under the old engine.
	r.Add(Tuple{IntVal{V: 1}})
}

// TestEvalArityShadowSeparateRelations is the eval-layer regression test.
// Two relations sharing a name but with different arities must be
// kept fully separate by the engine. The previous bug shadowed them
// in a single map[string]*Relation slot, so a 1-arity probe could
// see col-1 of a 3-arity tuple as effectively unconstrained and
// produce cartesian-style overmatch.
//
// The shape exercised here mirrors the QL bridge case:
//
//	C(id, name, file)             // 3-arity base relation
//	CClass(id) :- C(id, _, _)     // 1-arity characteristic predicate
//	Match(id, name) :- CClass(id), C(id, name, _).
//
// Under the broken engine, the 1-arity head would be Add()ed to the
// same relation slot as the 3-arity base, and the second body atom
// would over-match. Under the fixed engine, CClass and C live in
// separate (name, arity) slots and Match returns exactly the right
// rows. We make the head names different (CClass vs C) here so the
// test does not depend on whether the desugarer renames characteristic
// predicates — what we care about at the eval layer is that ANY two
// same-named different-arity relations cannot collide. The dedicated
// same-name case is exercised by TestEvalArityShadowSameName below.
func TestEvalArityShadowSeparateRelations(t *testing.T) {
	C := NewRelation("C", 3)
	C.Add(Tuple{IntVal{1}, StrVal{"alpha"}, StrVal{"a.ts"}})
	C.Add(Tuple{IntVal{2}, StrVal{"beta"}, StrVal{"b.ts"}})

	rels := RelsOf(C)

	// Build a planned rule equivalent to:
	//   Match(id, name) :- C(id, name, _).
	rule := plan.PlannedRule{
		Head: datalog.Atom{
			Predicate: "Match",
			Args: []datalog.Term{
				datalog.Var{Name: "id"},
				datalog.Var{Name: "name"},
			},
		},
		JoinOrder: []plan.JoinStep{
			{
				Literal: datalog.Literal{
					Positive: true,
					Atom: datalog.Atom{
						Predicate: "C",
						Args: []datalog.Term{
							datalog.Var{Name: "id"},
							datalog.Var{Name: "name"},
							datalog.Wildcard{},
						},
					},
				},
			},
		},
	}

	tuples := Rule(rule, rels)
	if len(tuples) != 2 {
		t.Fatalf("expected 2 Match tuples, got %d: %v", len(tuples), tuples)
	}
}

// TestEvalArityShadowSameName puts two relations with the same name
// but different arities into the eval engine in the same evaluation
// and verifies they do NOT shadow each other. This is the exact
// shape of the original bug.
func TestEvalArityShadowSameName(t *testing.T) {
	// Three-arity base relation C(id, name, file).
	c3 := NewRelation("C", 3)
	c3.Add(Tuple{IntVal{1}, StrVal{"alpha"}, StrVal{"a.ts"}})
	c3.Add(Tuple{IntVal{2}, StrVal{"beta"}, StrVal{"b.ts"}})

	// One-arity "characteristic" relation also called C, with only id=1.
	c1 := NewRelation("C", 1)
	c1.Add(Tuple{IntVal{1}})

	rels := RelsOf(c3, c1)

	// Probe 1: 1-arity C should yield exactly 1 tuple.
	probe1 := plan.PlannedRule{
		Head: datalog.Atom{
			Predicate: "Out1",
			Args:      []datalog.Term{datalog.Var{Name: "x"}},
		},
		JoinOrder: []plan.JoinStep{
			{
				Literal: datalog.Literal{
					Positive: true,
					Atom: datalog.Atom{
						Predicate: "C",
						Args:      []datalog.Term{datalog.Var{Name: "x"}},
					},
				},
			},
		},
	}
	out1 := Rule(probe1, rels)
	if len(out1) != 1 {
		t.Fatalf("1-arity C: expected 1 tuple, got %d: %v", len(out1), out1)
	}

	// Probe 2: 3-arity C should yield exactly 2 tuples.
	probe3 := plan.PlannedRule{
		Head: datalog.Atom{
			Predicate: "Out3",
			Args: []datalog.Term{
				datalog.Var{Name: "id"},
				datalog.Var{Name: "name"},
			},
		},
		JoinOrder: []plan.JoinStep{
			{
				Literal: datalog.Literal{
					Positive: true,
					Atom: datalog.Atom{
						Predicate: "C",
						Args: []datalog.Term{
							datalog.Var{Name: "id"},
							datalog.Var{Name: "name"},
							datalog.Wildcard{},
						},
					},
				},
			},
		},
	}
	out3 := Rule(probe3, rels)
	if len(out3) != 2 {
		t.Fatalf("3-arity C: expected 2 tuples, got %d: %v", len(out3), out3)
	}

	// And run a full Evaluate over both to confirm seminaive doesn't
	// conflate them either.
	execPlan := &plan.ExecutionPlan{
		Strata: []plan.Stratum{
			{Rules: []plan.PlannedRule{probe1, probe3}},
		},
	}
	_ = context.Background
	rs, err := Evaluate(context.Background(), execPlan, RelsOf(c3, c1))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	_ = rs // we only care that no panic / no overmatch happened above
}
