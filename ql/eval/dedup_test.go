package eval

import (
	"context"
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

// TestResultDeduplication verifies that the evaluator deduplicates query
// result rows. This is a regression test for duplicate rows appearing in
// integration test golden files.
func TestResultDeduplication(t *testing.T) {
	// Setup: two rules that both derive the same tuples for Q.
	// Q(x) :- A(x).
	// Q(x) :- B(x).
	// With A = {1, 2} and B = {1, 3}, Q should contain {1, 2, 3},
	// and the query "select x from Q" should return 3 rows, not 4.
	A := makeRelation("A", 1, IntVal{1}, IntVal{2})
	B := makeRelation("B", 1, IntVal{1}, IntVal{3})
	baseRels := map[string]*Relation{"A": A, "B": B}

	ep := &plan.ExecutionPlan{
		Strata: []plan.Stratum{
			{
				Rules: []plan.PlannedRule{
					{
						Head: datalog.Atom{Predicate: "Q", Args: []datalog.Term{v("x")}},
						JoinOrder: []plan.JoinStep{
							positiveStep("A", v("x")),
						},
					},
					{
						Head: datalog.Atom{Predicate: "Q", Args: []datalog.Term{v("x")}},
						JoinOrder: []plan.JoinStep{
							positiveStep("B", v("x")),
						},
					},
				},
			},
		},
		Query: &plan.PlannedQuery{
			Select: []datalog.Term{v("x")},
			JoinOrder: []plan.JoinStep{
				positiveStep("Q", v("x")),
			},
		},
	}

	rs, err := Evaluate(context.Background(), ep, baseRels)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	if len(rs.Rows) != 3 {
		t.Errorf("expected 3 deduplicated rows, got %d: %v", len(rs.Rows), rs.Rows)
	}
}

// TestResultDeduplicationJoinProduct verifies dedup when a join naturally
// produces duplicate projected rows.
func TestResultDeduplicationJoinProduct(t *testing.T) {
	// R(x, y) with multiple y for same x. Query selects only x.
	// R = {(1,10), (1,20), (2,30)}
	// Query: select x from R(x, _) → should get {1, 2}, not {1, 1, 2}.
	R := makeRelation("R", 2,
		IntVal{1}, IntVal{10},
		IntVal{1}, IntVal{20},
		IntVal{2}, IntVal{30},
	)
	baseRels := map[string]*Relation{"R": R}

	ep := &plan.ExecutionPlan{
		Query: &plan.PlannedQuery{
			Select: []datalog.Term{v("x")},
			JoinOrder: []plan.JoinStep{
				positiveStep("R", v("x"), datalog.Wildcard{}),
			},
		},
	}

	rs, err := Evaluate(context.Background(), ep, baseRels)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	if len(rs.Rows) != 2 {
		t.Errorf("expected 2 deduplicated rows (projecting x only), got %d: %v", len(rs.Rows), rs.Rows)
	}
}

// TestResultDeduplicationEmpty verifies dedup on empty results.
func TestResultDeduplicationEmpty(t *testing.T) {
	R := makeRelation("R", 1)
	baseRels := map[string]*Relation{"R": R}

	ep := &plan.ExecutionPlan{
		Query: &plan.PlannedQuery{
			Select: []datalog.Term{v("x")},
			JoinOrder: []plan.JoinStep{
				positiveStep("R", v("x")),
			},
		},
	}

	rs, err := Evaluate(context.Background(), ep, baseRels)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	if len(rs.Rows) != 0 {
		t.Errorf("expected 0 rows, got %d", len(rs.Rows))
	}
}
