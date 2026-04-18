package integration_test

import (
	"context"
	"errors"
	"os"
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
// Originally the `setStateUpdaterCallsFn` predicate
// (testdata/queries/v2/find_setstate_updater_calls_fn.ql, React bridge)
// blew the binding cap because the planner sized the IDB seed
// `isUseStateSetterCall` at the default 1000 tuples instead of its real
// ~7 — leading it to choose a Cartesian-heavy join order led by
// Function × Node × Call.
//
// Issue #121 Phase A.2 ripped the v1 `setStateUpdaterCallsFn` predicate
// out and replaced it with a `BackwardTracker`-based Configuration class
// (`SetStateUpdaterTracker`). The query name on disk is unchanged but
// the rule head shape is no longer a single `setStateUpdaterCallsFn(c, line)`
// rule — instead it desugars into:
//
//   - `BackwardTracker_step` (with subclass-dispatch, body =
//     `functionContainsStar(...) and Call(...)`)
//   - `BackwardTracker_hasFlowTo` (sink-first body)
//   - per-class `BackwardTracker_isSink`, `BackwardTracker_isSource`
//
// This test still extracts the React fixture and runs the v2 query end-to-end
// asserting it completes without binding-cap errors. The previous
// `setStateUpdaterCallsFn`-named-rule-first-join assertion is replaced by a
// looser end-to-end assertion: the query returns at least one row, completes
// under the cap, and the trivial-IDB pre-pass populated `isUseStateSetterCall`
// (the seed predicate) with a real count.
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

	// Plan.
	execPlan, planErrs := plan.Plan(prog, hints)
	if len(planErrs) > 0 {
		t.Fatalf("plan: %v", planErrs)
	}

	// Aggressive binding cap: real fixture intermediate cardinality with the
	// fix in place is < 1k. Pre-fix this rule blew the default 5M cap; we
	// pick 100k as the regression threshold — comfortably above the real
	// number, comfortably below "Cartesian disaster". Also threaded into the
	// pre-pass below so issue #130 (uncapped pre-pass eating RAM before the
	// main eval ever runs) is covered by the same guard.
	const tightCap = 100_000

	// Pre-pass + re-plan (the issue #88 fix). If a future change removes
	// these two calls from cmd/tsq/main.go, the assertion below catches
	// the regression directly: the rule will OOM at step 2.
	baseRels, err := eval.LoadBaseRelations(factDB)
	if err != nil {
		t.Fatalf("load base relations: %v", err)
	}
	updates := eval.EstimateNonRecursiveIDBSizes(prog, baseRels, hints, tightCap)
	if updates["isUseStateSetterCall"] == 0 {
		t.Fatalf("pre-pass failed to size isUseStateSetterCall (the seed predicate); updates=%v", updates)
	}
	for i := range execPlan.Strata {
		plan.RePlanStratum(&execPlan.Strata[i], hints)
	}
	if execPlan.Query != nil {
		plan.RePlanQuery(execPlan.Query, hints)
	}

	// Issue #121 Phase A.2: the v1 `setStateUpdaterCallsFn` rule head no
	// longer exists; the seed-first join assertion is no longer applicable
	// in its original form. The cap-respect + non-empty-result asserts
	// below remain the load-bearing regression guard for issue #88's OOM:
	// if the planner regresses to a Cartesian-heavy order under the new
	// `BackwardTracker_*` shape, the binding-cap error fires here.

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
			t.Fatalf("BUG: v2 setState query (BackwardTracker form) blew the %d-binding cap at step %d (cardinality=%d, rule=%q). The trivial-IDB pre-pass or the magic-set transform regressed — see issue #88 / #121.",
				tightCap, bce.StepIndex, bce.Cardinality, bce.Rule)
		}
		t.Fatalf("evaluate: %v", err)
	}

	// Sanity: the React fixture has at least one Case A match (setCount(prev => helper(prev))).
	if len(rs.Rows) == 0 {
		t.Fatalf("expected at least one v2 setState query match on the fixture, got 0 rows")
	}
	t.Logf("v2 setState query matched %d rows on react-usestate fixture (binding cap %d)", len(rs.Rows), tightCap)
}

// assertSeedFirst checks that the first JoinStep of the named rule's join
// order is on `wantFirstPredicate`. Used as a behavioural assertion that the
// planner is making the seed-selection decision the issue #88 fix targets.
func assertSeedFirst(t *testing.T, ep *plan.ExecutionPlan, ruleHead, wantFirstPredicate string) {
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
			if got != wantFirstPredicate {
				// Print full order for diagnostics.
				var order []string
				for _, st := range r.JoinOrder {
					order = append(order, st.Literal.Atom.Predicate)
				}
				t.Fatalf("rule %s: first literal want %s, got %s. Full order: %v",
					ruleHead, wantFirstPredicate, got, order)
			}
			return
		}
	}
	t.Fatalf("rule %s not found in execution plan", ruleHead)
}
