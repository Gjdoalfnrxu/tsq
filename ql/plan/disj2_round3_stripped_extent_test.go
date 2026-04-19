package plan

import (
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
)

// disj2-round3 — post-#158 surface.
//
// Setup (mirrors the Mastodon production failure described in the
// disj2-round3 hand-off):
//
//   - The desugarer emits an arity-1 class-extent rule
//     `UseStateSetterCall(c) :- CallCalleeSym(c,_), ImportBinding(_,_,_).`
//     marked `ClassExtent: true`.
//   - The materialising estimator hook (`MaterialisingEstimatorHook` /
//     `MakeMaterialisingEstimatorHook`) eagerly evaluates that rule and
//     hands its tuples to the evaluator as if they were a base relation;
//     `EstimateAndPlanWithExtents` then STRIPS the rule from the program
//     before invoking `Plan` / `WithMagicSetAutoOpts`.
//   - Once stripped, `UseStateSetterCall` is no longer an IDB head in the
//     planning program — it's a base-like predicate with a sizeHint.
//   - On a real codebase that hint is > SmallExtentThreshold (5000) AND
//     > LargeArityOneExtentThreshold-coverable but the structural
//     `arity1BaseGroundedIDBs` detector misses it because the rule that
//     would have qualified it has been stripped.
//   - `bodyContextGroundedVars` therefore falls back to `isSmallExtent`
//     for the (now-base) `UseStateSetterCall` literal. With hint > 5000,
//     it returns false; `c` is not marked bound; demand for the synth
//     `_disj_2` collapses; the magic-set rewrite never fires; and at
//     evaluation time `_disj_2`'s 5-atom body cap-hits.
//
// PR #158 fixed this for the case where the class-extent rule remains
// in the planning program (UseStateSetterCall stays as an arity-1 IDB
// with body-only base atoms). PR #158 does NOT fix the case where the
// class extent has been materialised AND stripped — `arity1BaseGroundedIDBs`
// has nothing to match.
//
// This test exercises that gap. It feeds `InferBackwardDemand` and
// `InferRuleBodyDemandBindings` a program in which `UseStateSetterCall`
// has already been stripped (i.e. it appears only as a body atom, never
// as a rule head) and is registered in an explicit
// `classExtentNames` set the planner is supposed to honour.

// progStrippedClassExtentDemandShape is `progArity1ExtentDemandShape`
// minus the `UseStateSetterCall` defining rule. The class extent is
// instead represented as a sizeHint plus membership in the
// classExtentNames set passed to the planner.
func progStrippedClassExtentDemandShape(useStateExtentSize int) (*datalog.Program, map[string]int, map[string]bool) {
	v := func(name string) datalog.Var { return datalog.Var{Name: name} }
	atom := func(pred string, args ...datalog.Term) datalog.Literal {
		return datalog.Literal{Positive: true, Atom: datalog.Atom{Predicate: pred, Args: args}}
	}

	rules := []datalog.Rule{
		// Caller — note: NO UseStateSetterCall defining rule. The class
		// extent has been materialised + stripped by
		// EstimateAndPlanWithExtents.
		{
			Head: datalog.Atom{Predicate: "Caller", Args: []datalog.Term{v("c"), v("line")}},
			Body: []datalog.Literal{
				atom("UseStateSetterCall", v("c")),
				{Positive: true, Atom: datalog.Atom{Predicate: "CallArg", Args: []datalog.Term{v("c"), datalog.IntConst{Value: 0}, v("argFn")}}},
				atom("Function", v("argFn"), v("_a"), v("_b"), v("_d"), v("_e"), v("_f")),
				atom("_disj_2", v("argFn"), v("inner")),
				atom("Call", v("c"), v("callee"), v("_callk")),
				atom("Node", v("callee"), v("_n1"), v("_n2"), v("line"), v("_n3"), v("_n4"), v("_n5")),
			},
		},
		{
			Head: datalog.Atom{Predicate: "_disj_2", Args: []datalog.Term{v("fn"), v("inner")}},
			Body: []datalog.Literal{
				atom("FunctionContains", v("fn"), v("inner")),
			},
		},
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
		"UseStateSetterCall": useStateExtentSize,
		"CallArg":            300_000,
		"Function":           50_000,
		"FunctionContains":   400_000,
		"Call":               300_000,
		"Node":               1_500_000,
	}
	classExtentNames := map[string]bool{
		"UseStateSetterCall": true,
	}
	return prog, hints, classExtentNames
}

// TestInferBackwardDemand_StrippedClassExtentGroundsHeadVar asserts the
// fix: when a class-extent base name is supplied via `classExtentNames`,
// `bodyContextGroundedVars` (and therefore `InferBackwardDemand` and
// `InferRuleBodyDemandBindings`) treats it as a grounder for its single
// arg regardless of size hint. Demand for `_disj_2` should fire and the
// magic-set seed should be emitted.
func TestInferBackwardDemand_StrippedClassExtentGroundsHeadVar(t *testing.T) {
	cases := []struct {
		name       string
		extentSize int
	}{
		// 7000 — above SmallExtentThreshold (5000). Pre-fix the demand
		// drops; post-fix it fires because UseStateSetterCall is in
		// classExtentNames.
		{name: "extent_above_small_threshold", extentSize: 7000},
		// 250_000 — realistic Mastodon shape.
		{name: "extent_realistic_mastodon_shape", extentSize: 250_000},
		// 5_000_000 — larger than LargeArityOneExtentThreshold; should
		// STILL ground because class-extent membership trumps size.
		// Per-tuple iteration of an arity-1 column scan is bounded by
		// the materialised extent the evaluator already holds in RAM.
		{name: "extent_above_large_threshold", extentSize: 5_000_000},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prog, hints, classExtentNames := progStrippedClassExtentDemandShape(tc.extentSize)
			idb := IDBPredicates(prog)

			demand := InferBackwardDemandWithClassExtents(prog, hints, classExtentNames)
			cols, ok := demand["_disj_2"]
			if !ok || len(cols) == 0 {
				t.Fatalf("hint=%d: expected _disj_2 demand to fire (col 0 bound via stripped UseStateSetterCall extent), got demand[_disj_2]=%v exists=%v\n  full demand=%v",
					tc.extentSize, cols, ok, demand)
			}
			if !(len(cols) >= 1 && cols[0] == 0) {
				t.Fatalf("hint=%d: expected _disj_2 demand to include col 0, got cols=%v", tc.extentSize, cols)
			}

			bindings, seeds := InferRuleBodyDemandBindingsWithClassExtents(prog, idb, hints, classExtentNames)
			if len(bindings) == 0 {
				t.Fatalf("hint=%d: expected magic-set bindings for _disj_2; got empty\n  demand=%v\n  seeds=%d",
					tc.extentSize, demand, len(seeds))
			}
			if _, ok := bindings["_disj_2"]; !ok {
				t.Fatalf("hint=%d: expected _disj_2 in bindings; got %v", tc.extentSize, bindings)
			}
			if len(seeds) == 0 {
				t.Fatalf("hint=%d: expected at least one magic seed rule, got 0", tc.extentSize)
			}
		})
	}
}

// TestEstimateAndPlanWithExtentsCtx_PlumbsClassExtentNamesThrough is
// the end-to-end wiring assertion. It feeds EstimateAndPlanWithExtentsCtx
// a program containing a ClassExtent-tagged arity-1 rule plus a
// downstream synth-disj caller, and a materialising hook that flags
// the class extent as materialised (returning its head name and
// writing a > SmallExtentThreshold size hint).
//
// The custom planFn receives the (stripped) planning program AND the
// materialised-extents set. It then calls WithMagicSetAutoOptsWithClassExtents
// to confirm the magic-set rewrite fires for `_disj_2` — i.e. the
// downstream demand chain is no longer broken by the (stripped) class
// extent's missing defining rule.
//
// Pre-fix (without the disj2-round3 plumbing): the planFn would have
// no way to learn that UseStateSetterCall is a class-extent base; the
// arity-1 structural detector cannot match a stripped rule; the magic
// rewrite drops on the floor; bindings for `_disj_2` are empty.
//
// Post-fix: the materialised-extents set is threaded through and
// bindings include `_disj_2`.
func TestEstimateAndPlanWithExtentsCtx_PlumbsClassExtentNamesThrough(t *testing.T) {
	v := func(name string) datalog.Var { return datalog.Var{Name: name} }
	atom := func(pred string, args ...datalog.Term) datalog.Literal {
		return datalog.Literal{Positive: true, Atom: datalog.Atom{Predicate: pred, Args: args}}
	}

	prog := &datalog.Program{
		Rules: []datalog.Rule{
			// Class-extent rule — will be materialised and stripped.
			{
				Head: datalog.Atom{Predicate: "UseStateSetterCall", Args: []datalog.Term{v("c")}},
				Body: []datalog.Literal{
					atom("CallCalleeSym", v("c"), v("_sym")),
				},
				ClassExtent: true,
			},
			// Caller — references the (to-be-stripped) UseStateSetterCall.
			{
				Head: datalog.Atom{Predicate: "Caller", Args: []datalog.Term{v("c"), v("line")}},
				Body: []datalog.Literal{
					atom("UseStateSetterCall", v("c")),
					{Positive: true, Atom: datalog.Atom{Predicate: "CallArg", Args: []datalog.Term{v("c"), datalog.IntConst{Value: 0}, v("argFn")}}},
					atom("Function", v("argFn"), v("_a"), v("_b"), v("_d"), v("_e"), v("_f")),
					atom("_disj_2", v("argFn"), v("inner")),
					atom("Call", v("c"), v("callee"), v("_callk")),
					atom("Node", v("callee"), v("_n1"), v("_n2"), v("line"), v("_n3"), v("_n4"), v("_n5")),
				},
			},
			{
				Head: datalog.Atom{Predicate: "_disj_2", Args: []datalog.Term{v("fn"), v("inner")}},
				Body: []datalog.Literal{
					atom("FunctionContains", v("fn"), v("inner")),
				},
			},
			{
				Head: datalog.Atom{Predicate: "_disj_2", Args: []datalog.Term{v("fn"), v("inner")}},
				Body: []datalog.Literal{
					atom("FunctionContains", v("fn"), v("mid")),
					atom("FunctionContains", v("mid"), v("inner")),
				},
			},
		},
		Query: &datalog.Query{
			Select: []datalog.Term{v("c"), v("line")},
			Body:   []datalog.Literal{atom("Caller", v("c"), v("line"))},
		},
	}

	hints := map[string]int{
		"CallArg":          300_000,
		"Function":         50_000,
		"FunctionContains": 400_000,
		"Call":             300_000,
		"Node":             1_500_000,
	}

	matExtHook := func(p *datalog.Program, h map[string]int, _ int) map[string]bool {
		// Pretend we materialised the class extent at a size well above
		// SmallExtentThreshold — the disj2-round3 production failure mode.
		h["UseStateSetterCall"] = 250_000
		h["CallCalleeSym"] = 500_000
		return map[string]bool{"UseStateSetterCall": true}
	}

	var sawClassExtents map[string]bool
	var sawProg *datalog.Program
	var sawBindings map[string][]int
	planFn := func(p *datalog.Program, h map[string]int, classExtentNames map[string]bool) (*ExecutionPlan, []error) {
		sawProg = p
		sawClassExtents = classExtentNames
		// Run the magic-set rewrite the way production does and capture
		// the bindings to assert the rewrite fired.
		ep, inf, errs := WithMagicSetAutoOptsWithClassExtents(p, h, MagicSetOptions{Strict: true}, classExtentNames)
		sawBindings = inf.Bindings
		return ep, errs
	}

	_, errs := EstimateAndPlanWithExtentsCtx(prog, hints, 0, nil, matExtHook, planFn)
	if len(errs) > 0 {
		t.Fatalf("plan errors: %v", errs)
	}
	if sawProg == nil {
		t.Fatal("planFn was not invoked")
	}
	// Sanity: the class-extent rule is stripped from the planning program.
	for _, r := range sawProg.Rules {
		if r.Head.Predicate == "UseStateSetterCall" {
			t.Fatalf("UseStateSetterCall rule should have been stripped before reaching planFn")
		}
	}
	// The class-extent name set must have been threaded through.
	if !sawClassExtents["UseStateSetterCall"] {
		t.Fatalf("planFn did not receive UseStateSetterCall in classExtentNames; got %v", sawClassExtents)
	}
	// Headline: with the threading in place, demand for `_disj_2` fires
	// and the magic-set rewrite produces bindings.
	if _, ok := sawBindings["_disj_2"]; !ok {
		t.Fatalf("expected `_disj_2` in magic-set bindings (proves the demand chain through the stripped class extent reaches the rewrite); got bindings=%v", sawBindings)
	}
}

// TestInferBackwardDemand_StrippedClassExtent_ArityGuard pins the
// adversarial-review F1 finding: classExtentNames is keyed by name only
// (per the MaterialisingEstimatorHook contract — class extents are
// always arity-1). If a body literal references a name that happens to
// collide with a class-extent name AT A DIFFERENT ARITY (defensive
// against hand-written predicate name collisions), the relaxation must
// NOT fire — that would over-ground vars in an unrelated wider literal.
//
// Setup: classExtentNames declares "Foo" but the body literal is
// `Foo(a, b, c)` (arity 3). With the arity guard in place, vars
// a/b/c stay unbound and demand for the downstream synth-disj does
// not fire (size hints are the load-bearing signal for arity-N
// literals, as it should be).
func TestInferBackwardDemand_StrippedClassExtent_ArityGuard(t *testing.T) {
	v := func(name string) datalog.Var { return datalog.Var{Name: name} }
	atom := func(pred string, args ...datalog.Term) datalog.Literal {
		return datalog.Literal{Positive: true, Atom: datalog.Atom{Predicate: pred, Args: args}}
	}

	// Body uses `Foo(a, b, c)` — arity 3, NOT the class extent shape.
	// Even though "Foo" is in classExtentNames, the relaxation must not
	// over-ground a/b/c.
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			{
				Head: datalog.Atom{Predicate: "Caller", Args: []datalog.Term{v("a")}},
				Body: []datalog.Literal{
					atom("Foo", v("a"), v("b"), v("c")), // arity-3 collision
					atom("_disj_2", v("b"), v("d")),
				},
			},
			{
				Head: datalog.Atom{Predicate: "_disj_2", Args: []datalog.Term{v("x"), v("y")}},
				Body: []datalog.Literal{atom("Bar", v("x"), v("y"))},
			},
		},
		Query: &datalog.Query{
			Select: []datalog.Term{v("a")},
			Body:   []datalog.Literal{atom("Caller", v("a"))},
		},
	}
	hints := map[string]int{
		"Foo": 250_000,
		"Bar": 100_000,
	}
	classExtentNames := map[string]bool{"Foo": true}

	demand := InferBackwardDemandWithClassExtents(prog, hints, classExtentNames)
	cols := demand["_disj_2"]
	if len(cols) != 0 {
		t.Fatalf("arity guard violated: classExtentNames[\"Foo\"] is keyed by name only, but body uses arity-3 Foo(a,b,c). The relaxation must not ground a/b/c. Got demand[_disj_2]=%v (should be empty since Foo's arity disqualifies it as the documented class-extent shape)", cols)
	}
}

// TestInferBackwardDemand_StrippedClassExtent_NilSetIsBaseline pins the
// no-classExtentNames behaviour: with nil set the existing PR #158
// path runs unchanged. The arity1BaseGroundedIDBs detector cannot match
// (the defining rule is absent) so demand drops for hints > 5000. This
// is the regression-safety check — passing nil must not surface any
// new behaviour.
func TestInferBackwardDemand_StrippedClassExtent_NilSetIsBaseline(t *testing.T) {
	prog, hints, _ := progStrippedClassExtentDemandShape(7000)
	demand := InferBackwardDemandWithClassExtents(prog, hints, nil)
	cols := demand["_disj_2"]
	if len(cols) != 0 {
		t.Fatalf("with nil classExtentNames and hint=7000 the existing baseline behaviour applies: demand[_disj_2] should be empty (PR #158 cannot match a stripped extent's missing rule). Got %v", cols)
	}
}
