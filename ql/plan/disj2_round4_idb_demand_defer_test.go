package plan

import (
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
)

// disj2-round4 — defer IDB calls until their demand-bound positions are
// runtime-bound.
//
// Surface: PR #161 fixed the inner `_disj_2` cap-hit on
// find_setstate_updater_calls_other_setstate.ql by threading
// materialised class extents through demand inference. Magic-set now
// correctly infers `bindings=map[_disj_2:[0]]`. But the OUTER rule
// `setStateUpdaterCallsOtherSetState` still cap-hits at 20M.
//
// Root cause: even with demand[_disj_2] = [0] correctly inferred, the
// planner still considers the recursive _disj_2 literal as cheap at slot
// 0 (small / default size hint), placing it BEFORE the small grounder
// chain (`UseStateSetterCall(c) → CallArg(c,0,argFn) → _disj_2(argFn,
// inner)`). Without bound `argFn` the recursive call iterates the entire
// star and blows the binding cap at step 2.
//
// Fix: orderJoinsWithDemandAndIDB consults the per-IDB DemandMap when
// scoring each candidate. A positive IDB call whose demand bindings are
// not yet runtime-bound (and whose hint isn't tiny) is penalised with
// SaturatedSizeHint so the planner picks any other reasonable
// alternative first. As soon as a later step grounds the required vars
// the literal regains its true cost and is scheduled in its proper
// place.
//
// This test reproduces the structural shape of the production failure
// and asserts the corrected ordering.
func TestPlan_Disj2Round4_DefersDemandIDBUntilBound(t *testing.T) {
	v := func(name string) datalog.Var { return datalog.Var{Name: name} }
	pos := func(pred string, args ...datalog.Term) datalog.Literal {
		return datalog.Literal{Positive: true, Atom: datalog.Atom{Predicate: pred, Args: args}}
	}

	// Simulate the post-magic-set body of
	// setStateUpdaterCallsOtherSetState. The planner sees:
	//   UseStateSetterCall(c)        — small grounder, hint 7
	//   CallArg(c, 0, argFn)         — point-keyed lookup binding argFn
	//   functionContainsStar(argFn, inner) — recursive IDB,
	//                                  demand[func...Star] = [0]
	//   UseStateSetterCall(inner)    — small grounder, hint 7
	//   Other(c, callee)             — base, large
	body := []datalog.Literal{
		pos("functionContainsStar", v("argFn"), v("inner")),
		pos("UseStateSetterCall", v("inner")),
		pos("UseStateSetterCall", v("c")),
		pos("CallArg", v("c"), datalog.IntConst{Value: 0}, v("argFn")),
		pos("Other", v("c"), v("callee")),
	}
	head := datalog.Atom{
		Predicate: "setStateUpdaterCallsOtherSetState",
		Args:      []datalog.Term{v("c"), v("callee")},
	}

	hints := map[string]int{
		"UseStateSetterCall":   7,
		"CallArg":              50000, // base, but constant arg keeps it cheap per-probe
		"functionContainsStar": 50000, // > SmallExtentThreshold so deferral applies
		"Other":                500000,
	}
	// demand[functionContainsStar] = [0] — col 0 must be bound
	// before scheduling.
	demand := DemandMap{
		"functionContainsStar": {0},
	}

	steps := orderJoinsWithDemandAndIDB(head, body, hints, nil, demand)

	if len(steps) == 0 {
		t.Fatal("no steps produced")
	}
	first := steps[0].Literal.Atom.Predicate
	if first == "functionContainsStar" {
		t.Fatalf("functionContainsStar must NOT be the seed (its demand col 0 / argFn is unbound at slot 0); full order: %v", predicateOrder(steps))
	}
	// The recursive IDB must be scheduled AFTER argFn is bound. argFn
	// is introduced by CallArg(c, 0, argFn) which itself requires c
	// (introduced by UseStateSetterCall(c)). Find positions:
	posOf := func(pred string) int {
		for i, s := range steps {
			if s.Literal.Atom.Predicate == pred {
				return i
			}
		}
		return -1
	}
	pStar := posOf("functionContainsStar")
	pCallArg := posOf("CallArg")
	if pStar == -1 || pCallArg == -1 {
		t.Fatalf("missing expected literals; full order: %v", predicateOrder(steps))
	}
	if pStar < pCallArg {
		t.Fatalf("functionContainsStar at %d must come AFTER CallArg at %d (argFn must be bound first); full order: %v",
			pStar, pCallArg, predicateOrder(steps))
	}
}

// Companion test: when the demand map is empty (or nil), the planner
// preserves its pre-round4 behaviour — no deferral, recursive IDB can
// still be scheduled first if its hint says so. Guards against
// over-correction.
func TestPlan_Disj2Round4_NoDemandPreservesLegacyOrder(t *testing.T) {
	v := func(name string) datalog.Var { return datalog.Var{Name: name} }
	pos := func(pred string, args ...datalog.Term) datalog.Literal {
		return datalog.Literal{Positive: true, Atom: datalog.Atom{Predicate: pred, Args: args}}
	}

	body := []datalog.Literal{
		pos("functionContainsStar", v("argFn"), v("inner")),
		pos("UseStateSetterCall", v("c")),
		pos("CallArg", v("c"), datalog.IntConst{Value: 0}, v("argFn")),
	}
	head := datalog.Atom{Predicate: "R", Args: []datalog.Term{v("c"), v("inner")}}
	hints := map[string]int{
		"UseStateSetterCall":   7,
		"CallArg":              50000,
		"functionContainsStar": 50000,
	}

	// nil demand → no IDB-call deferral
	stepsNil := orderJoinsWithDemandAndIDB(head, body, hints, nil, nil)
	// orderJoinsWithDemand wrapper → byte-identical
	stepsWrap := orderJoinsWithDemand(head, body, hints, nil)
	if len(stepsNil) != len(stepsWrap) {
		t.Fatalf("nil-demand vs wrapper length mismatch: %d vs %d", len(stepsNil), len(stepsWrap))
	}
	for i := range stepsNil {
		if stepsNil[i].Literal.Atom.Predicate != stepsWrap[i].Literal.Atom.Predicate {
			t.Fatalf("nil-demand vs wrapper diverge at step %d: %s vs %s",
				i, stepsNil[i].Literal.Atom.Predicate, stepsWrap[i].Literal.Atom.Predicate)
		}
	}
}

// Companion test: small-hinted IDBs MUST NOT be deferred even when
// their demand cols aren't bound. Otherwise the existing tiny-seed
// heuristic — which depends on small-hinted class extents like
// TaintSink (size 7) winning slot 0 — would regress.
func TestPlan_Disj2Round4_SmallIDBNotDeferredEvenWithUnboundDemand(t *testing.T) {
	v := func(name string) datalog.Var { return datalog.Var{Name: name} }
	pos := func(pred string, args ...datalog.Term) datalog.Literal {
		return datalog.Literal{Positive: true, Atom: datalog.Atom{Predicate: pred, Args: args}}
	}

	body := []datalog.Literal{
		pos("BigBase", v("x"), v("y")),
		pos("TinySink", v("y")), // small IDB, demand[y] not bound
	}
	head := datalog.Atom{Predicate: "Alert", Args: []datalog.Term{v("x"), v("y")}}
	hints := map[string]int{"BigBase": 1_000_000, "TinySink": 7}
	demand := DemandMap{"TinySink": {0}}

	steps := orderJoinsWithDemandAndIDB(head, body, hints, nil, demand)
	if len(steps) == 0 || steps[0].Literal.Atom.Predicate != "TinySink" {
		t.Fatalf("small-hinted IDB must NOT be deferred; expected TinySink first, got order=%v",
			predicateOrder(steps))
	}
}

// Companion test: a recursive self-call (literal whose predicate equals
// the rule's own head) must NEVER be deferred — that would forbid the
// recursive case from being scheduled at all and break fixpoint
// convergence.
func TestPlan_Disj2Round4_RecursiveSelfCallNotDeferred(t *testing.T) {
	v := func(name string) datalog.Var { return datalog.Var{Name: name} }
	pos := func(pred string, args ...datalog.Term) datalog.Literal {
		return datalog.Literal{Positive: true, Atom: datalog.Atom{Predicate: pred, Args: args}}
	}

	// Path(x, z) :- Path(x, y), Edge(y, z).
	body := []datalog.Literal{
		pos("Path", v("x"), v("y")),
		pos("Edge", v("y"), v("z")),
	}
	head := datalog.Atom{Predicate: "Path", Args: []datalog.Term{v("x"), v("z")}}
	hints := map[string]int{"Path": 50000, "Edge": 100000}
	demand := DemandMap{"Path": {0}}

	// Should not panic / should not produce empty plan / should
	// schedule both literals. Order itself is not asserted — the
	// safety property is that the recursive call is not infinitely
	// deferred.
	steps := orderJoinsWithDemandAndIDB(head, body, hints, nil, demand)
	if len(steps) != 2 {
		t.Fatalf("expected 2 steps, got %d (order=%v)", len(steps), predicateOrder(steps))
	}
}
