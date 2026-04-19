package plan

import (
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
)

// disj2-round2 unit-level regression — see HANDOVER notes in PR.
//
// Hypothesis (verified by static read of backward.go:351, :424-430 and
// the eval.SaturatedSizeHint write in ql/eval/estimate.go:491-494):
//
//   When a class-extent IDB literal that is the SOLE grounding source
//   for a downstream synthesised disjunction's bound var has a size
//   hint above SmallExtentThreshold (5000) — whether because the real
//   extent is in the 5k–500k range OR because MaterialiseClassExtents
//   failed and the trivial-IDB pre-pass wrote SaturatedSizeHint —
//   bodyContextGroundedVars treats it as non-grounding. The all-callers
//   intersect in InferBackwardDemand records cols=[] for the synth
//   `_disj_*` predicate, InferRuleBodyDemandBindings emits no seed,
//   and the magic-set rewrite never fires. The planner then orders
//   the synth-disj's multi-atom body as a free-for-all and cap-hits.
//
// Repro at unit level: progArity1ExtentDemandShape constructs a 3-rule
// program structurally identical to the post-#156 setStateUpdater
// query plan: an arity-1 class-extent IDB head sized just above
// SmallExtentThreshold, a multi-base-atom IDB caller that probes it
// AND `_disj_2`, and `_disj_2` defined as a wide cross-product. With
// the size hint at 5001, the demand chain breaks identically to the
// SaturatedSizeHint=1<<30 case — proof that the bug is gated purely on
// the threshold check, not on saturation per se.

// progArity1ExtentDemandShape models setStateUpdaterCallsOtherSetState
// after PR #156's name-arity shadow fix:
//
//	UseStateSetterCall(c) :- CallCalleeSym(c,_), ImportBinding(_,_,_).  // size > 5000
//	Caller(c, line) :-
//	    UseStateSetterCall(c),                  // <-- only c-grounder
//	    CallArg(c, 0, argFn),                   // base, arity-3 — needs c bound
//	    Function(argFn, _, _, _, _, _),         // base, arity-6
//	    _disj_2(argFn, inner),                  // synth disj
//	    Call(c, callee, _),
//	    Node(callee, _, _, line, _, _, _).
//	_disj_2(fn, inner) :- FunctionContains(fn, inner).
//	_disj_2(fn, inner) :- FunctionContains(fn, mid), FunctionContains(mid, inner).
func progArity1ExtentDemandShape(useStateExtentSize int) (*datalog.Program, map[string]int) {
	v := func(name string) datalog.Var { return datalog.Var{Name: name} }
	atom := func(pred string, args ...datalog.Term) datalog.Literal {
		return datalog.Literal{Positive: true, Atom: datalog.Atom{Predicate: pred, Args: args}}
	}

	rules := []datalog.Rule{
		// Class-extent IDB. Body is base-only so the trivial-IDB
		// pre-pass would attempt it; in real prod runs the pre-pass
		// either materialises it (size ~7k) or hits the cap and writes
		// SaturatedSizeHint. We model the post-pre-pass state by
		// passing the hint directly.
		{
			Head: datalog.Atom{Predicate: "UseStateSetterCall", Args: []datalog.Term{v("c")}},
			Body: []datalog.Literal{
				atom("CallCalleeSym", v("c"), v("_sym")),
				atom("ImportBinding", v("_sym"), v("_mod"), v("_nm")),
			},
		},
		// Caller = setStateUpdaterCallsOtherSetState.
		{
			Head: datalog.Atom{Predicate: "Caller", Args: []datalog.Term{v("c"), v("line")}},
			Body: []datalog.Literal{
				atom("UseStateSetterCall", v("c")),
				// CallArg(c, 0, argFn): the integer-const col 1 means
				// hasConstTerm fires once `c` is bound, grounding argFn.
				{Positive: true, Atom: datalog.Atom{Predicate: "CallArg", Args: []datalog.Term{v("c"), datalog.IntConst{Value: 0}, v("argFn")}}},
				atom("Function", v("argFn"), v("_a"), v("_b"), v("_d"), v("_e"), v("_f")),
				atom("_disj_2", v("argFn"), v("inner")),
				atom("Call", v("c"), v("callee"), v("_callk")),
				atom("Node", v("callee"), v("_n1"), v("_n2"), v("line"), v("_n3"), v("_n4"), v("_n5")),
			},
		},
		// _disj_2 — desugarer-emitted multi-branch synthetic IDB.
		// Branch A: trivial direct-contains.
		{
			Head: datalog.Atom{Predicate: "_disj_2", Args: []datalog.Term{v("fn"), v("inner")}},
			Body: []datalog.Literal{
				atom("FunctionContains", v("fn"), v("inner")),
			},
		},
		// Branch B: 2-hop, the cross-product candidate.
		{
			Head: datalog.Atom{Predicate: "_disj_2", Args: []datalog.Term{v("fn"), v("inner")}},
			Body: []datalog.Literal{
				atom("FunctionContains", v("fn"), v("mid")),
				atom("FunctionContains", v("mid"), v("inner")),
			},
		},
	}

	prog := &datalog.Program{
		Rules: rules,
		Query: &datalog.Query{
			Select: []datalog.Term{v("c"), v("line")},
			Body: []datalog.Literal{
				atom("Caller", v("c"), v("line")),
			},
		},
	}

	hints := map[string]int{
		"UseStateSetterCall": useStateExtentSize, // tunable across the test cases
		"CallCalleeSym":      200_000,
		"ImportBinding":      500,
		"CallArg":            300_000,
		"Function":           50_000,
		"FunctionContains":   400_000,
		"Call":               300_000,
		"Node":               1_500_000,
	}
	return prog, hints
}

// TestInferRuleBodyDemandBindings_Arity1ExtentAboveThresholdBreaksDemand
// is the headline disj2-round2 regression. Three sizings of the SOLE
// demand-grounder (UseStateSetterCall) bracket the SmallExtentThreshold:
//
//	  4_999  →  small → demand fires → bindings non-empty (control: passes today)
//	  5_001  →  above → demand silent → bindings empty (POST-#156 BUG)
//	1<<30   →  saturated → demand silent → bindings empty (same path)
//
// The 5001 case is the proof that the bug is purely a threshold issue,
// independent of MaterialiseClassExtents failure. Saturated cap-hit is
// a sufficient trigger but not a necessary one — any mid-sized class
// extent (5k–50k) reproduces the same outcome on real corpora.
//
// Expected outcome on current main: the >5000 cases FAIL (bug present).
// Expected outcome post-fix: the saturated case still drops (genuine
// huge), but mid-sized arity-1 class extents (e.g. 7k, 50k) should
// continue to ground their head var — the fix is to treat arity-1 IDB
// literals as grounders up to a higher ceiling than the generic
// SmallExtentThreshold, since per-tuple iteration of an arity-1 set is
// cheap regardless.
func TestInferRuleBodyDemandBindings_Arity1ExtentAboveThresholdBreaksDemand(t *testing.T) {
	// Cases marked `expectedPostFix: true` are XFAIL today (skipped via
	// t.Skip with a clear reason) and will become required guards once
	// the planner-policy fix lands. The 4999 control case must always
	// pass — if it ever fails, demand inference broke for the in-spec
	// shape and the regression guard catches it independent of the fix.
	cases := []struct {
		name             string
		extentSize       int
		expectBindings   bool
		expectedPostFix  bool // true → currently failing, intentionally documents the bug
		expectFiringNote string
	}{
		{
			name:             "extent_4999_under_threshold_demand_fires",
			extentSize:       4999,
			expectBindings:   true,
			expectFiringNote: "below SmallExtentThreshold — bodyContextGroundedVars marks c bound, demand cols=[0]",
		},
		{
			name:             "extent_5001_above_threshold_demand_should_fire_post_fix",
			extentSize:       5001,
			expectBindings:   true,
			expectedPostFix:  true,
			expectFiringNote: "1 tuple over threshold should still ground via arity-1 extent (cheap iteration). Currently broken — same demand-empty bug as the saturated case.",
		},
		{
			name:             "extent_50000_realistic_above_threshold_should_fire_post_fix",
			extentSize:       50_000,
			expectBindings:   true,
			expectedPostFix:  true,
			expectFiringNote: "realistic mastodon-shape extent — prod failure mode for setStateUpdater queries. Post-fix should still ground.",
		},
		{
			name:             "extent_saturated_cap_hit_demand_drops_genuine_huge",
			extentSize:       1 << 30, // eval.SaturatedSizeHint
			expectBindings:   false,
			expectFiringNote: "saturated hint = genuinely huge or unmeasurable; correct to drop demand here.",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.expectedPostFix {
				t.Skip("XFAIL today — bug documented; requires arity-1 grounding-threshold fix to pass. " +
					"Note: " + tc.expectFiringNote)
			}
			prog, hints := progArity1ExtentDemandShape(tc.extentSize)
			idb := IDBPredicates(prog)

			bindings, seeds := InferRuleBodyDemandBindings(prog, idb, hints)
			gotFired := len(bindings) > 0

			if gotFired != tc.expectBindings {
				t.Fatalf("UseStateSetterCall hint=%d (threshold=%d): demand bindings fired=%v want=%v\n"+
					"  bindings=%v\n  seeds=%d\n  note: %s\n"+
					"  Failure mechanism: bodyContextGroundedVars (backward.go:351) calls\n"+
					"  isSmallExtent which returns false for sz>SmallExtentThreshold;\n"+
					"  c is therefore not marked bound at the _disj_2(argFn,inner) call site,\n"+
					"  so all-callers intersect collapses to cols=[] for _disj_2.",
					tc.extentSize, SmallExtentThreshold,
					gotFired, tc.expectBindings,
					bindings, len(seeds), tc.expectFiringNote)
			}

			if tc.expectBindings {
				cols, ok := bindings["_disj_2"]
				if !ok {
					t.Fatalf("expected _disj_2 in bindings (control case); got %v", bindings)
				}
				// argFn is at col 0 of _disj_2 — that's what should be demanded.
				if len(cols) == 0 {
					t.Fatalf("expected non-empty cols for _disj_2; got %v", cols)
				}
			}
		})
	}
}
