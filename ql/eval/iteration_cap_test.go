package eval

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

// divergentTransitiveClosurePlan returns a plan + base relations that compute
// transitive closure over an N-node chain. Reaching the full fixpoint requires
// at least N-1 iterations (semi-naive extends paths by one hop per iteration),
// so it is trivial to construct a query that needs > maxIterations to
// converge — the exact failure mode issue #79 cares about.
//
// The plan deliberately includes both the base rule (Path :- Edge) and the
// recursive rule (Path :- Edge, Path) so it exercises a real semi-naive
// fixpoint, not a trivial single-step query.
func divergentTransitiveClosurePlan(nodes int) (*plan.ExecutionPlan, map[string]*Relation) {
	vals := make([]Value, 0, nodes*2)
	for i := 1; i < nodes; i++ {
		vals = append(vals, IntVal{V: int64(i)}, IntVal{V: int64(i + 1)})
	}
	edge := makeRelation("Edge", 2, vals...)
	baseRels := map[string]*Relation{"Edge": edge}

	ep := &plan.ExecutionPlan{
		Strata: []plan.Stratum{
			{
				Rules: []plan.PlannedRule{
					// Path(x,y) :- Edge(x,y).
					{
						Head: datalog.Atom{Predicate: "Path", Args: []datalog.Term{v("x"), v("y")}},
						JoinOrder: []plan.JoinStep{
							positiveStep("Edge", v("x"), v("y")),
						},
					},
					// Path(x,z) :- Edge(x,y), Path(y,z).
					{
						Head: datalog.Atom{Predicate: "Path", Args: []datalog.Term{v("x"), v("z")}},
						JoinOrder: []plan.JoinStep{
							positiveStep("Edge", v("x"), v("y")),
							positiveStep("Path", v("y"), v("z")),
						},
					},
				},
			},
		},
		Query: &plan.PlannedQuery{
			Select: []datalog.Term{v("a"), v("b")},
			JoinOrder: []plan.JoinStep{
				positiveStep("Path", v("a"), v("b")),
			},
		},
	}
	return ep, baseRels
}

// TestIterationCapErrorsByDefault is the core regression for issue #79.
//
// Before the fix, a query that did not converge in N iterations silently
// returned partial results plus a log warning — indistinguishable from a
// converged answer. This test asserts that the default behaviour is now an
// error wrapping ErrIterationCapExceeded, with a typed *IterationCapError
// carrying actionable diagnostics (rule name, cap, last delta size).
func TestIterationCapErrorsByDefault(t *testing.T) {
	// 8-node chain — full transitive closure needs 7 iterations (semi-naive
	// extends one hop per iteration). With cap=2 the fixpoint cannot finish.
	ep, baseRels := divergentTransitiveClosurePlan(8)

	rs, err := Evaluate(context.Background(), ep, baseRels, WithMaxIterations(2))
	if err == nil {
		t.Fatalf("expected ErrIterationCapExceeded, got nil error and %d rows", len(rs.Rows))
	}
	if !errors.Is(err, ErrIterationCapExceeded) {
		t.Fatalf("expected error wrapping ErrIterationCapExceeded, got: %v", err)
	}
	var ice *IterationCapError
	if !errors.As(err, &ice) {
		t.Fatalf("expected *IterationCapError, got: %T (%v)", err, err)
	}
	if ice.Cap != 2 {
		t.Errorf("expected Cap=2, got %d", ice.Cap)
	}
	if ice.Rule == "" {
		t.Errorf("expected non-empty Rule (head predicate of dominant rule), got empty")
	}
	if ice.Rule != "Path" {
		t.Errorf("expected Rule=%q (the only head in the divergent stratum), got %q", "Path", ice.Rule)
	}
	if ice.LastDeltaSize <= 0 {
		t.Errorf("expected LastDeltaSize > 0 (proves fixpoint was still producing tuples), got %d", ice.LastDeltaSize)
	}
	if !strings.Contains(err.Error(), "Path") {
		t.Errorf("error message should mention rule name 'Path': %q", err.Error())
	}
	if !strings.Contains(err.Error(), "did not converge") {
		t.Errorf("error message should mention non-convergence: %q", err.Error())
	}
}

// TestIterationCapPromptness asserts the error fires at exactly iteration N,
// not N+5 or N+50. If the cap check were misplaced or off-by-one, this test
// catches it. We pin the cap at 3 and observe LastDeltaSize stays in the
// range produced by the first 3 iterations of a chain TC.
//
// For an 8-node chain (Edge has 7 tuples), bootstrap produces 7 Path tuples.
// Iteration 1 derives 6 new 2-hop tuples, iteration 2 derives 5 new 3-hop
// tuples, iteration 3 derives 4 new 4-hop tuples. With cap=3 the cap fires
// on the iteration that would have been the 4th — last delta is the
// 3-iteration delta = 4 tuples. We assert delta is small (< 50) — if the
// cap silently let the fixpoint run further, the delta would be wildly
// larger or zero (converged).
func TestIterationCapPromptness(t *testing.T) {
	ep, baseRels := divergentTransitiveClosurePlan(8)

	const cap = 3
	_, err := Evaluate(context.Background(), ep, baseRels, WithMaxIterations(cap))
	if err == nil {
		t.Fatalf("expected error at cap=%d", cap)
	}
	var ice *IterationCapError
	if !errors.As(err, &ice) {
		t.Fatalf("expected *IterationCapError, got: %T", err)
	}
	if ice.Cap != cap {
		t.Errorf("expected Cap=%d, got %d", cap, ice.Cap)
	}
	// Promptness window: total delta must be small. If the cap fired late
	// (e.g. the loop ran 10 extra iterations), the delta would either be 0
	// (converged) or have grown via more chain extensions. A tight upper
	// bound proves the cap fires at the boundary, not after.
	if ice.LastDeltaSize == 0 {
		t.Errorf("LastDeltaSize=0 means the fixpoint converged — cap should not have fired")
	}
	if ice.LastDeltaSize > 20 {
		t.Errorf("LastDeltaSize=%d is too large for cap=%d on an 8-node chain — cap may be firing late", ice.LastDeltaSize, cap)
	}
}

// TestIterationCapAllowPartial verifies WithAllowPartial(true) restores the
// legacy behaviour: cap hit returns partial results with no error. This is
// the explicit opt-in escape hatch named in issue #79.
func TestIterationCapAllowPartial(t *testing.T) {
	ep, baseRels := divergentTransitiveClosurePlan(8)

	// Without allow-partial: errors.
	_, err := Evaluate(context.Background(), ep, baseRels, WithMaxIterations(2))
	if err == nil {
		t.Fatalf("control: expected error without --allow-partial")
	}

	// With allow-partial: returns partial results, no error.
	rs, err := Evaluate(context.Background(), ep, baseRels, WithMaxIterations(2), WithAllowPartial(true))
	if err != nil {
		t.Fatalf("WithAllowPartial: expected nil error, got: %v", err)
	}
	if rs == nil {
		t.Fatal("WithAllowPartial: expected non-nil ResultSet")
	}
	// Partial mode must produce SOME rows (bootstrap + at least one delta
	// iteration), but strictly fewer than the full transitive closure of an
	// 8-node chain (which is 7+6+5+4+3+2+1 = 28 pairs).
	if len(rs.Rows) == 0 {
		t.Errorf("WithAllowPartial: expected partial rows, got 0")
	}
	if len(rs.Rows) >= 28 {
		t.Errorf("WithAllowPartial: expected fewer than 28 rows (full TC), got %d — cap was bypassed entirely", len(rs.Rows))
	}

	// And the unbounded case must produce all 28 pairs — proves the cap-as-
	// error path is NOT triggered on a converging query.
	rsFull, err := Evaluate(context.Background(), ep, baseRels, WithMaxIterations(0))
	if err != nil {
		t.Fatalf("unbounded: unexpected error: %v", err)
	}
	if len(rsFull.Rows) != 28 {
		t.Errorf("unbounded: expected 28 transitive pairs for 8-node chain, got %d", len(rsFull.Rows))
	}
}

// TestIterationCapNotTriggeredOnConvergence asserts the cap-as-error path is
// never triggered when a query converges within the cap. We give the
// 8-node chain (needs 7 iterations) a generous cap of 100 and require
// success. This is the regression guard against the iteration check being
// too eager (e.g. firing at iteration N when the fixpoint actually
// converged on the same iteration).
func TestIterationCapNotTriggeredOnConvergence(t *testing.T) {
	ep, baseRels := divergentTransitiveClosurePlan(8)

	rs, err := Evaluate(context.Background(), ep, baseRels, WithMaxIterations(100))
	if err != nil {
		t.Fatalf("converging query erroneously errored: %v", err)
	}
	if len(rs.Rows) != 28 {
		t.Errorf("expected 28 transitive pairs, got %d", len(rs.Rows))
	}

	// And the boundary case: cap = exactly the number of iterations needed.
	// For an 8-node chain TC, semi-naive converges by the 7th delta step.
	// With cap=10 (well above 7) we must succeed, not error.
	rs, err = Evaluate(context.Background(), ep, baseRels, WithMaxIterations(10))
	if err != nil {
		t.Fatalf("converging query at boundary cap=10 erroneously errored: %v", err)
	}
	if len(rs.Rows) != 28 {
		t.Errorf("boundary: expected 28 pairs, got %d", len(rs.Rows))
	}
}

// TestIterationCapParallel runs the same divergent-fixture assertions through
// the parallel evaluator path (WithParallel). The iteration-cap check sits
// in the same loop as the sequential path, but the delta map is built by
// parallelDelta — coverage gap on the parallel path was a previously called-
// out testing failure mode, so this is non-negotiable.
func TestIterationCapParallel(t *testing.T) {
	ep, baseRels := divergentTransitiveClosurePlan(8)

	// Default: errors with parallel on too.
	_, err := Evaluate(context.Background(), ep, baseRels, WithMaxIterations(2), WithParallel())
	if err == nil {
		t.Fatalf("parallel: expected ErrIterationCapExceeded")
	}
	if !errors.Is(err, ErrIterationCapExceeded) {
		t.Fatalf("parallel: expected error wrapping ErrIterationCapExceeded, got: %v", err)
	}
	var ice *IterationCapError
	if !errors.As(err, &ice) {
		t.Fatalf("parallel: expected *IterationCapError, got: %T", err)
	}
	if ice.Cap != 2 {
		t.Errorf("parallel: Cap=%d, want 2", ice.Cap)
	}
	if ice.LastDeltaSize <= 0 {
		t.Errorf("parallel: expected LastDeltaSize > 0, got %d", ice.LastDeltaSize)
	}
	if ice.Rule != "Path" {
		t.Errorf("parallel: expected Rule=Path, got %q", ice.Rule)
	}

	// Allow-partial: parallel + partial returns rows, no error.
	rs, err := Evaluate(context.Background(), ep, baseRels, WithMaxIterations(2), WithParallel(), WithAllowPartial(true))
	if err != nil {
		t.Fatalf("parallel + allow-partial: unexpected error: %v", err)
	}
	if len(rs.Rows) == 0 || len(rs.Rows) >= 28 {
		t.Errorf("parallel + allow-partial: expected partial row count in (0, 28), got %d", len(rs.Rows))
	}

	// Convergence on parallel must succeed without error.
	rsFull, err := Evaluate(context.Background(), ep, baseRels, WithMaxIterations(100), WithParallel())
	if err != nil {
		t.Fatalf("parallel converged-case errored: %v", err)
	}
	if len(rsFull.Rows) != 28 {
		t.Errorf("parallel converged: expected 28 pairs, got %d", len(rsFull.Rows))
	}
}
