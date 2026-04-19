package integration_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Gjdoalfnrxu/tsq/bridge"
	extractrules "github.com/Gjdoalfnrxu/tsq/extract/rules"
	"github.com/Gjdoalfnrxu/tsq/extract/schema"
	"github.com/Gjdoalfnrxu/tsq/ql/desugar"
	"github.com/Gjdoalfnrxu/tsq/ql/eval"
	"github.com/Gjdoalfnrxu/tsq/ql/parse"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
	"github.com/Gjdoalfnrxu/tsq/ql/resolve"
)

// TestIssue88_SetStateQueryDoesNotOOM is the regression guard for issue #88.
//
// The setStateUpdaterCallsFn rule (testdata/queries/v2/find_setstate_updater_calls_fn.ql,
// React bridge) historically blew the binding cap at join step 2 because the
// planner sized the IDB seed isUseStateSetterCall at the default 1000 tuples
// instead of its real ~7 — leading it to choose a Cartesian-heavy join order
// led by Function × Node × Call.
//
// Both the seed predicate AND the explody rule co-stratify (same SCC after
// MergeSystemRules), so the prior between-strata refresh in eval.Evaluate
// did NOT fix this. The fix is the trivial-IDB pre-pass
// (eval.EstimateNonRecursiveIDBSizes) wired in cmd/tsq/main.go's
// compileAndEval, which materialises every non-recursive IDB whose body
// uses only base + already-trivial predicates BEFORE the first Plan() call,
// then re-plans every stratum with the real numbers.
//
// This test reproduces the production codepath: extract a small TSX fixture,
// run the same query end-to-end, and assert it completes without binding-cap
// errors and well below an aggressive cardinality budget. If the fix
// regresses (or someone reverts the pre-pass), this test fails immediately
// with a *BindingCapError instead of an OOM-after-an-hour in the field.
func TestIssue88_SetStateQueryDoesNotOOM(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping extraction-heavy integration test in short mode")
	}

	factDB := extractProject(t, "testdata/projects/react-usestate")

	src, err := os.ReadFile("testdata/queries/v2/find_setstate_updater_calls_fn.ql")
	if err != nil {
		t.Fatalf("read query: %v", err)
	}

	bridgeFiles := bridge.LoadBridge()
	importLoader := makeBridgeImportLoader(bridgeFiles)

	// Compile.
	p := parse.NewParser(string(src), "find_setstate_updater_calls_fn.ql")
	mod, err := p.Parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	resolved, err := resolve.Resolve(mod, importLoader)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(resolved.Errors) > 0 {
		t.Fatalf("resolve errors: %v", resolved.Errors)
	}
	prog, dsErrors := desugar.Desugar(resolved)
	if len(dsErrors) > 0 {
		t.Fatalf("desugar: %v", dsErrors)
	}
	prog = extractrules.MergeSystemRules(prog, extractrules.AllSystemRules())

	// Build base size hints from the fact DB.
	hints := make(map[string]int, len(schema.Registry))
	for _, def := range schema.Registry {
		hints[def.Name] = factDB.Relation(def.Name).Tuples()
	}

	// Aggressive binding cap: real fixture intermediate cardinality with the
	// fix in place is < 1k. Pre-fix this rule blew the default 5M cap; we
	// pick 100k as the regression threshold — comfortably above the real
	// number, comfortably below "Cartesian disaster". Also threaded into the
	// pre-pass via EstimateAndPlan so issue #130 (uncapped pre-pass eating
	// RAM before the main eval ever runs) is covered by the same guard.
	const tightCap = 100_000

	// EstimateAndPlan: single estimate-then-plan pass (P1 of planner roadmap).
	// The estimator hook materialises every trivial IDB BEFORE Plan() is
	// called, so the seed cardinality (isUseStateSetterCall ≈ 7) is in
	// sizeHints from the start instead of falling through to default-1000.
	// This replaces the prior "plan → estimate → RePlanStratum / RePlanQuery"
	// two-pass ceremony.
	baseRels, err := eval.LoadBaseRelations(factDB)
	if err != nil {
		t.Fatalf("load base relations: %v", err)
	}
	execPlan, planErrs := plan.EstimateAndPlan(
		prog,
		hints,
		tightCap,
		eval.MakeEstimatorHook(baseRels),
		plan.Plan,
	)
	if len(planErrs) > 0 {
		t.Fatalf("plan: %v", planErrs)
	}
	if hints["UseStateSetterCall"] == 0 {
		t.Fatalf("pre-pass failed to size UseStateSetterCall (the class-extent seed); hints=%v", hints)
	}

	// Assert: the join is anchored against the small class-extent-derived
	// seed. After the class-form rewrite (2026-04-18), the planner picks
	// `UseStateSetterCall_getLine` (size 7, transitively anchored against
	// the materialised `UseStateSetterCall` extent) as the first literal
	// instead of the 12-tuple `Function`/`Call` base relations. The
	// load-bearing property is that the SEED is small (≤ |UseStateSetterCall|)
	// — which `UseStateSetterCall_getLine` satisfies because its own body
	// includes `UseStateSetterCall(this)` and so it has the same
	// cardinality. If the planner stops choosing a seed of this shape, the
	// rule will Cartesian-blow regardless of whether the test happens to
	// fit under the binding cap on this small fixture.
	assertSeedAnchoredOnClassExtent(t, execPlan, "setStateUpdaterCallsFn", "UseStateSetterCall")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	rs, err := eval.Evaluate(
		ctx,
		execPlan,
		baseRels,
		eval.WithMaxBindingsPerRule(tightCap),
		eval.WithSizeHints(hints),
	)
	if err != nil {
		var bce *eval.BindingCapError
		if errors.As(err, &bce) {
			t.Fatalf("BUG: setStateUpdaterCallsFn blew the %d-binding cap at step %d (cardinality=%d, rule=%q). The trivial-IDB pre-pass is broken — see issue #88.",
				tightCap, bce.StepIndex, bce.Cardinality, bce.Rule)
		}
		t.Fatalf("evaluate: %v", err)
	}

	// Sanity: the React fixture has at least one Case A match (setCount(prev => helper(prev))).
	if len(rs.Rows) == 0 {
		t.Fatalf("expected at least one setStateUpdaterCallsFn match on the fixture, got 0 rows")
	}
	t.Logf("setStateUpdaterCallsFn matched %d rows on react-usestate fixture (binding cap %d)", len(rs.Rows), tightCap)
}

// assertSeedAnchoredOnClassExtent checks that the named rule's first join
// literal is either the class extent itself OR a predicate whose own body
// is anchored on the class extent (i.e. the predicate name has the
// extent-name as a prefix, by the desugarer's mangling convention
// `ClassName_methodName`). This is the post-P2a class-form behavioural
// assertion: the planner picks a small seed sourced from the
// pre-materialised class extent, instead of starting at a large base
// relation like `Function` or `Call`.
func assertSeedAnchoredOnClassExtent(t *testing.T, ep *plan.ExecutionPlan, ruleHead, classExtent string) {
	t.Helper()
	for _, s := range ep.Strata {
		for _, r := range s.Rules {
			if r.Head.Predicate != ruleHead {
				continue
			}
			if len(r.JoinOrder) == 0 {
				t.Fatalf("rule %s has empty JoinOrder", ruleHead)
			}
			got := r.JoinOrder[0].Literal.Atom.Predicate
			if got == classExtent || strings.HasPrefix(got, classExtent+"_") {
				return
			}
			var order []string
			for _, st := range r.JoinOrder {
				order = append(order, st.Literal.Atom.Predicate)
			}
			t.Fatalf("rule %s: first literal want %s or %s_<method>, got %s. Full order: %v",
				ruleHead, classExtent, classExtent, got, order)
		}
	}
	t.Fatalf("rule %s not found in execution plan", ruleHead)
}
