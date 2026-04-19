// Disj2-round6 regression: magic-pred anchoring in the join planner.
//
// Scenario (Mastodon `setStateUpdaterCallsOtherSetStateThroughContext`):
// after PR #168 (round-5) lands, the magic-set transform applies cleanly
// for the through-context query. A propagation rule of shape
//
//	magic_<inner>(args) :- magic_<outer>(args), <preceding body lits>.
//
// is generated for `magic__disj_28`. Concretely:
//
//	magic__disj_28(initExpr) :-
//	   magic_contextDestructureBinding(ctxSym, fieldName),
//	   VarDecl(varDecl, _, initExpr, _),
//	   Contains(varDecl, parent),
//	   DestructureField(parent, fieldName, _, paramSym, _).
//
// `magic__disj_28` has head-demand [0] (position 0 = initExpr), so the
// pre-round6 planner pre-binds initExpr in plannerBound. The greedy
// scorer then sees VarDecl as the only literal sharing initExpr at
// slot 0 (boundCount=1) and places it first. After VarDecl, the
// remaining literals — magic_contextDestructureBinding (no overlap),
// DestructureField (no overlap with bound={varDecl,initExpr}), and
// Contains (one overlap, varDecl) — combine into a cap-blowing cross
// product before `magic_contextDestructureBinding` ever filters.
//
// Round-6 fix: any `magic_<pred>` literal in a rule body MUST win the
// next slot regardless of normal cost scoring. Magic predicates are
// the seed-driven demand source — placing them late lets the
// surrounding bases blow the binding cap. With the anchor in place,
// the magic literal is scheduled at slot 0 and the join becomes
// driven by the small magic seed extension.

package plan

import (
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
)

// progRound6PropagationCapHit models the magic__disj_28 propagation
// rule shape. The body literals correspond to the dump observed on
// Mastodon at commit 78d0138.
func progRound6PropagationCapHit() datalog.Rule {
	// magic__disj_28(initExpr) :-
	//   magic_contextDestructureBinding(ctxSym, fieldName),
	//   VarDecl(varDecl, _, initExpr, _),
	//   Contains(varDecl, parent),
	//   DestructureField(parent, fieldName, _, paramSym, _).
	return datalog.Rule{
		Head: datalog.Atom{Predicate: "magic__disj_28", Args: []datalog.Term{
			datalog.Var{Name: "initExpr"},
		}},
		Body: []datalog.Literal{
			{Positive: true, Atom: datalog.Atom{Predicate: "magic_contextDestructureBinding", Args: []datalog.Term{
				datalog.Var{Name: "ctxSym"},
				datalog.Var{Name: "fieldName"},
			}}},
			{Positive: true, Atom: datalog.Atom{Predicate: "VarDecl", Args: []datalog.Term{
				datalog.Var{Name: "varDecl"},
				datalog.Var{Name: "_"},
				datalog.Var{Name: "initExpr"},
				datalog.Var{Name: "_"},
			}}},
			{Positive: true, Atom: datalog.Atom{Predicate: "Contains", Args: []datalog.Term{
				datalog.Var{Name: "varDecl"},
				datalog.Var{Name: "parent"},
			}}},
			{Positive: true, Atom: datalog.Atom{Predicate: "DestructureField", Args: []datalog.Term{
				datalog.Var{Name: "parent"},
				datalog.Var{Name: "fieldName"},
				datalog.Var{Name: "_"},
				datalog.Var{Name: "paramSym"},
				datalog.Var{Name: "_"},
			}}},
		},
	}
}

// TestRound6_MagicAnchor_OrderJoins verifies that the plain orderJoins
// planner schedules the magic literal at slot 0.
func TestRound6_MagicAnchor_OrderJoins(t *testing.T) {
	rule := progRound6PropagationCapHit()
	sizeHints := map[string]int{
		"VarDecl":          5011,
		"Contains":         455468,
		"DestructureField": 2188,
		// magic_* deliberately unhinted — defaultSizeHint applies.
	}
	steps := orderJoins(rule.Body, sizeHints)
	if len(steps) != len(rule.Body) {
		t.Fatalf("len(steps)=%d want %d", len(steps), len(rule.Body))
	}
	if got := steps[0].Literal.Atom.Predicate; got != "magic_contextDestructureBinding" {
		t.Errorf("orderJoins slot 0 = %q want magic_contextDestructureBinding (round-6 anchor)", got)
	}
}

// TestRound6_MagicAnchor_OrderJoinsWithDemandAndIDB verifies that the
// demand-aware planner also anchors magic literals — even when
// head-demand pre-binds a var that another body literal happens to
// share. This is the production failure mode: head-demand on
// `initExpr` would otherwise hand slot 0 to VarDecl.
func TestRound6_MagicAnchor_OrderJoinsWithDemandAndIDB(t *testing.T) {
	rule := progRound6PropagationCapHit()
	sizeHints := map[string]int{
		"VarDecl":          5011,
		"Contains":         455468,
		"DestructureField": 2188,
	}
	headDemand := []int{0} // initExpr is demand-bound.
	steps := orderJoinsWithDemandAndIDB(rule.Head, rule.Body, sizeHints, headDemand, nil)
	if len(steps) != len(rule.Body) {
		t.Fatalf("len(steps)=%d want %d", len(steps), len(rule.Body))
	}
	if got := steps[0].Literal.Atom.Predicate; got != "magic_contextDestructureBinding" {
		t.Errorf("orderJoinsWithDemandAndIDB slot 0 = %q want magic_contextDestructureBinding (round-6 anchor); full plan: %s", got, planTrace(steps))
	}
}

// TestRound6_MagicAnchor_BeatsTinySeed verifies the magic-anchor pass
// runs BEFORE the tiny-seed override. A magic literal must win even
// when a hinted-tiny base relation is also present.
func TestRound6_MagicAnchor_BeatsTinySeed(t *testing.T) {
	body := []datalog.Literal{
		// Tiny-seed candidate: hinted ≤ tinySeedThreshold.
		{Positive: true, Atom: datalog.Atom{Predicate: "TinyBase", Args: []datalog.Term{
			datalog.Var{Name: "x"},
		}}},
		// Magic literal — must still anchor at slot 0.
		{Positive: true, Atom: datalog.Atom{Predicate: "magic_Foo", Args: []datalog.Term{
			datalog.Var{Name: "y"},
		}}},
	}
	sizeHints := map[string]int{
		"TinyBase": 5, // qualifies as tiny seed.
	}
	steps := orderJoins(body, sizeHints)
	if got := steps[0].Literal.Atom.Predicate; got != "magic_Foo" {
		t.Errorf("orderJoins slot 0 = %q want magic_Foo (anchor must beat tiny-seed)", got)
	}
}

// TestRound6_MagicAnchor_NoMagicLiteral_DegradesIdentically verifies
// that bodies with no magic literals plan identically to before the
// round-6 patch — the anchor pass is a strict pre-pass that returns
// -1 when no candidate qualifies.
func TestRound6_MagicAnchor_NoMagicLiteral_DegradesIdentically(t *testing.T) {
	body := []datalog.Literal{
		{Positive: true, Atom: datalog.Atom{Predicate: "A", Args: []datalog.Term{datalog.Var{Name: "x"}}}},
		{Positive: true, Atom: datalog.Atom{Predicate: "B", Args: []datalog.Term{datalog.Var{Name: "x"}}}},
	}
	sizeHints := map[string]int{"A": 100, "B": 50}
	steps := orderJoins(body, sizeHints)
	// Smallest size at slot 0 → B.
	if got := steps[0].Literal.Atom.Predicate; got != "B" {
		t.Errorf("orderJoins slot 0 = %q want B (no magic anchor → fall through to scoring)", got)
	}
}

// TestRound6_IsMagicLiteral covers the predicate-name detection helper.
func TestRound6_IsMagicLiteral(t *testing.T) {
	tests := []struct {
		name     string
		lit      datalog.Literal
		wantTrue bool
	}{
		{
			name: "magic_ prefix positive",
			lit: datalog.Literal{Positive: true, Atom: datalog.Atom{
				Predicate: "magic_Foo",
			}},
			wantTrue: true,
		},
		{
			name: "no prefix",
			lit: datalog.Literal{Positive: true, Atom: datalog.Atom{
				Predicate: "Foo",
			}},
			wantTrue: false,
		},
		{
			name: "exact prefix only (length match)",
			lit: datalog.Literal{Positive: true, Atom: datalog.Atom{
				Predicate: "magic_",
			}},
			wantTrue: false, // requires more after the prefix
		},
		{
			name: "negated magic literal",
			lit: datalog.Literal{Positive: false, Atom: datalog.Atom{
				Predicate: "magic_Foo",
			}},
			wantTrue: false, // negation excluded
		},
		{
			name:     "magic-named comparison",
			lit:      datalog.Literal{Cmp: &datalog.Comparison{}},
			wantTrue: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isMagicLiteral(tc.lit)
			if got != tc.wantTrue {
				t.Errorf("isMagicLiteral=%v want %v", got, tc.wantTrue)
			}
		})
	}
}

func planTrace(steps []JoinStep) string {
	out := ""
	for i, s := range steps {
		if i > 0 {
			out += " | "
		}
		out += s.Literal.Atom.Predicate
	}
	return out
}
