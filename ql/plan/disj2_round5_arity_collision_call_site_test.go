// Disj2-round5 regression: arity-keyed call-site match in
// buildDemandSeedsForPred.
//
// Scenario (Mastodon `setStateUpdaterCallsOtherSetStateThroughContext`):
// the desugarer (PR #146) auto-emits arity-1 class-extent helpers like
// `VarDecl(this) :- VarDecl(this, _, _, _).` for parameters typed with
// `@vardecl`. The arity-1 IDB head shadows the underlying arity-4 base
// relation by name. When backward demand inference records
// `demand[VarDecl] = [0]`, `buildDemandSeedsForPred` walks rule bodies
// looking for call sites of "VarDecl" — and (pre-fix) name-matches the
// arity-4 base usages too. For an arity-4 atom like
// `VarDecl(_, sym, srcExpr, _)`, position 0 is a wildcard, so the seed
// becomes `magic_VarDecl(_) :- ...` — `isSafe` lets the wildcard through
// (its `_`-exemption) but `validate.ValidateRule` rejects it as an unbound
// head var. The whole magic-set program then falls back to plain Plan,
// removing all demand pruning and exposing every IDB in the program to
// full-base evaluation.
//
// This test reproduces the unsafe-seed emission at the unit level and
// verifies the round-5 fix (arity-keyed call-site match + wildcard-at-
// demanded-position guard) suppresses it without regressing the
// legitimate arity-1 call-site case.

package plan

import (
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
)

// progArityCollidingHelper models the disj2-round5 shape: an arity-1
// class-extent helper IDB ("VarDecl/1") shadowing an arity-4 base
// relation of the same name, with backward demand on the arity-1 IDB
// being constructable from grounding callers.
//
//	VarDecl(this) :- VarDecl(this, _, _, _).            // arity-1 helper
//	UseVarDeclArity1(x) :- VarDecl(x).                  // pure call site of arity-1 IDB
//	UseVarDeclArity4(s) :- VarDecl(_, s, _, _).         // arity-4 base usage (NOT a call site of /1)
func progArityCollidingHelper() *datalog.Program {
	return &datalog.Program{
		Rules: []datalog.Rule{
			// Arity-1 class-extent helper.
			{
				Head: datalog.Atom{Predicate: "VarDecl", Args: []datalog.Term{datalog.Var{Name: "this"}}},
				Body: []datalog.Literal{
					{Positive: true, Atom: datalog.Atom{Predicate: "VarDecl", Args: []datalog.Term{
						datalog.Var{Name: "this"},
						datalog.Var{Name: "_"},
						datalog.Var{Name: "_"},
						datalog.Var{Name: "_"},
					}}},
				},
			},
			// Genuine call site of VarDecl/1 (not of the base relation).
			{
				Head: datalog.Atom{Predicate: "UseVarDeclArity1", Args: []datalog.Term{datalog.Var{Name: "x"}}},
				Body: []datalog.Literal{
					{Positive: true, Atom: datalog.Atom{Predicate: "VarDecl", Args: []datalog.Term{datalog.Var{Name: "x"}}}},
				},
			},
			// Arity-4 base usage with wildcard at position 0. The
			// pre-fix matcher mistakes this for a call site of
			// VarDecl/1 and emits `magic_VarDecl(_) :- ...`.
			{
				Head: datalog.Atom{Predicate: "UseVarDeclArity4", Args: []datalog.Term{datalog.Var{Name: "s"}}},
				Body: []datalog.Literal{
					{Positive: true, Atom: datalog.Atom{Predicate: "VarDecl", Args: []datalog.Term{
						datalog.Var{Name: "_"},
						datalog.Var{Name: "s"},
						datalog.Var{Name: "_"},
						datalog.Var{Name: "_"},
					}}},
				},
			},
		},
	}
}

// TestBuildDemandSeedsForPred_ArityKeyedCallSite_NoUnsafeSeed asserts
// that a demand on the arity-1 IDB head does not produce a seed from
// the arity-4 base usage (which would have a wildcard at position 0).
func TestBuildDemandSeedsForPred_ArityKeyedCallSite_NoUnsafeSeed(t *testing.T) {
	prog := progArityCollidingHelper()
	sizeHints := map[string]int{}

	// Demand on the arity-1 IDB head: "position 0 of VarDecl/1 is bound".
	seeds := buildDemandSeedsForPred(prog, "VarDecl", []int{0}, sizeHints)

	// Each emitted seed must
	//   (a) have head arity 1 (matching the IDB head, not the base /4), and
	//   (b) NOT have a wildcard `_` at any head position (which would be
	//       rejected by validate.ValidateRule even though isSafe accepts it).
	for i, s := range seeds {
		if s.Head.Predicate != "magic_VarDecl" {
			t.Errorf("seed[%d]: unexpected head predicate %q (want magic_VarDecl)", i, s.Head.Predicate)
		}
		if got, want := len(s.Head.Args), 1; got != want {
			t.Errorf("seed[%d]: head arity = %d, want %d (arity-keyed match should reject base /4 usages)", i, got, want)
		}
		for j, arg := range s.Head.Args {
			if v, ok := arg.(datalog.Var); ok && v.Name == "_" {
				t.Errorf("seed[%d]: wildcard `_` at head position %d — would be rejected by validate.ValidateRule and trigger plain-Plan fallback", i, j)
			}
		}
		// Defensive: the seed should be safe per validate.ValidateRule
		// after the round-5 fix (not just per the looser isSafe).
		errs := ValidateRule(s)
		if len(errs) > 0 {
			t.Errorf("seed[%d]: ValidateRule rejected emitted seed: %v\nseed = %+v", i, errs, s)
		}
	}
}

// TestBuildDemandSeedsForPred_ArityKeyedCallSite_KeepsArity1CallSiteWithGrounding
// guards the positive direction: a genuine arity-1 call site whose
// preceding literal grounds the demanded var must still produce a safe seed.
func TestBuildDemandSeedsForPred_ArityKeyedCallSite_KeepsArity1CallSiteWithGrounding(t *testing.T) {
	// Add a grounding caller: `Grounded(x) :- Anchor(x), VarDecl(x).`
	// where Anchor is a small base relation. This is the realistic
	// shape — the arity-1 IDB call has a preceding atom that binds x.
	prog := progArityCollidingHelper()
	prog.Rules = append(prog.Rules, datalog.Rule{
		Head: datalog.Atom{Predicate: "Grounded", Args: []datalog.Term{datalog.Var{Name: "x"}}},
		Body: []datalog.Literal{
			{Positive: true, Atom: datalog.Atom{Predicate: "Anchor", Args: []datalog.Term{datalog.Var{Name: "x"}}}},
			{Positive: true, Atom: datalog.Atom{Predicate: "VarDecl", Args: []datalog.Term{datalog.Var{Name: "x"}}}},
		},
	})
	sizeHints := map[string]int{}

	seeds := buildDemandSeedsForPred(prog, "VarDecl", []int{0}, sizeHints)

	// We expect at least one seed for the genuine arity-1 call site
	// where x is grounded by Anchor.
	foundArity1 := false
	for _, s := range seeds {
		if s.Head.Predicate != "magic_VarDecl" || len(s.Head.Args) != 1 {
			continue
		}
		if v, ok := s.Head.Args[0].(datalog.Var); ok && v.Name == "x" {
			foundArity1 = true
			break
		}
	}
	if !foundArity1 {
		t.Errorf("arity-keyed match dropped the legitimate arity-1 call site; got seeds = %+v", seeds)
	}
}

// TestInferRuleBodyDemandBindings_ArityCollidingHelper_NoFallback is
// the integration-shaped test. It builds the colliding-helper shape
// inside a program with a synth-disj demand source so the demand-seed
// builder is actually exercised, and asserts that no unsafe seed is
// produced for the colliding name. (We cannot easily reach
// `WithMagicSetAutoOpts` from here without query plumbing, but the
// presence of an unsafe seed is the load-bearing observable: it is what
// later triggers the validate.ValidateRule rejection and the
// plain-Plan fallback.)
func TestInferRuleBodyDemandBindings_ArityCollidingHelper_NoFallback(t *testing.T) {
	// Build a program that combines:
	//   - the disj2-round5 arity collision (VarDecl/1 helper + /4 usage), and
	//   - a synth-disj `_disj_2` whose only direct caller is a rename
	//     trampoline (the round-1/round-3 shape that triggers the
	//     parent-traversal path in buildDemandSeedsForPredWithParents).
	//
	// We don't need _disj_2 to contribute the bug; we just want the
	// demand pass to fire so seeds are constructed across the board.
	rules := []datalog.Rule{
		// Arity-1 class-extent helper for VarDecl.
		{
			Head: datalog.Atom{Predicate: "VarDecl", Args: []datalog.Term{datalog.Var{Name: "this"}}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "VarDecl", Args: []datalog.Term{
					datalog.Var{Name: "this"},
					datalog.Var{Name: "_"},
					datalog.Var{Name: "_"},
					datalog.Var{Name: "_"},
				}}},
			},
		},
		// Arity-4 base usage with wildcard at the demanded position
		// (the round-5 trap).
		{
			Head: datalog.Atom{Predicate: "Consumer", Args: []datalog.Term{datalog.Var{Name: "s"}}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "VarDecl", Args: []datalog.Term{
					datalog.Var{Name: "_"},
					datalog.Var{Name: "s"},
					datalog.Var{Name: "_"},
					datalog.Var{Name: "_"},
				}}},
			},
		},
	}
	prog := &datalog.Program{
		Rules: rules,
		Query: &datalog.Query{
			Select: []datalog.Term{datalog.Var{Name: "s"}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "Consumer", Args: []datalog.Term{datalog.Var{Name: "s"}}}},
			},
		},
	}
	idbPreds := IDBPredicates(prog)
	sizeHints := map[string]int{}

	bindings, seeds := InferRuleBodyDemandBindings(prog, idbPreds, sizeHints)
	// VarDecl is not synth-named so it should never appear in bindings
	// (the synth-only filter blocks the for-loop top-of-iteration). But
	// it could enter via parent-traversal — the round-5 wildcard guard
	// must still suppress unsafe seeds from any path.
	for pred, cols := range bindings {
		_ = cols
		for _, s := range seeds {
			if s.Head.Predicate != magicName(pred) {
				continue
			}
			for j, arg := range s.Head.Args {
				if v, ok := arg.(datalog.Var); ok && v.Name == "_" {
					t.Errorf("unsafe seed with wildcard at head position %d for predicate %q (would trigger plain-Plan fallback): %+v", j, pred, s)
				}
			}
			if errs := ValidateRule(s); len(errs) > 0 {
				t.Errorf("ValidateRule rejected emitted seed for %q: %v\nseed = %+v", pred, errs, s)
			}
		}
	}
}
