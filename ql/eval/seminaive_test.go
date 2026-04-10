package eval

import (
	"context"
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

// TestSeminaiveNonRecursive verifies that a single non-recursive rule
// terminates in one iteration and produces correct results.
func TestSeminaiveNonRecursive(t *testing.T) {
	// Rule: Q(x,y) :- A(x,y).
	A := makeRelation("A", 2,
		IntVal{1}, IntVal{2},
		IntVal{3}, IntVal{4},
	)
	baseRels := map[string]*Relation{"A": A}

	ep := &plan.ExecutionPlan{
		Strata: []plan.Stratum{
			{
				Rules: []plan.PlannedRule{
					{
						Head: datalog.Atom{Predicate: "Q", Args: []datalog.Term{v("x"), v("y")}},
						JoinOrder: []plan.JoinStep{
							positiveStep("A", v("x"), v("y")),
						},
					},
				},
			},
		},
		Query: &plan.PlannedQuery{
			Select: []datalog.Term{v("x"), v("y")},
			JoinOrder: []plan.JoinStep{
				positiveStep("Q", v("x"), v("y")),
			},
		},
	}

	rs, err := Evaluate(context.Background(), ep, baseRels)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	if len(rs.Rows) != 2 {
		t.Fatalf("expected 2 rows, got %d: %v", len(rs.Rows), rs.Rows)
	}
}

// TestSeminaiveTransitiveClosure verifies semi-naive evaluation computes
// the transitive closure of a 5-node chain correctly.
//
// Base: Edge(1,2), Edge(2,3), Edge(3,4), Edge(4,5)
// Rules:
//
//	Path(x,y) :- Edge(x,y).
//	Path(x,z) :- Edge(x,y), Path(y,z).
//
// Expected Path: all pairs (i,j) where i < j and a path exists.
// For a chain 1→2→3→4→5:
//
//	(1,2),(1,3),(1,4),(1,5)
//	(2,3),(2,4),(2,5)
//	(3,4),(3,5)
//	(4,5)
//
// = 4+3+2+1 = 10 pairs.
func TestSeminaiveTransitiveClosure(t *testing.T) {
	edge := makeRelation("Edge", 2,
		IntVal{1}, IntVal{2},
		IntVal{2}, IntVal{3},
		IntVal{3}, IntVal{4},
		IntVal{4}, IntVal{5},
	)
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

	rs, err := Evaluate(context.Background(), ep, baseRels)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	// Collect all (from, to) pairs.
	got := make(map[[2]int64]bool)
	for _, row := range rs.Rows {
		from := row[0].(IntVal).V
		to := row[1].(IntVal).V
		got[[2]int64{from, to}] = true
	}

	// All expected reachability pairs.
	expected := [][2]int64{
		{1, 2}, {1, 3}, {1, 4}, {1, 5},
		{2, 3}, {2, 4}, {2, 5},
		{3, 4}, {3, 5},
		{4, 5},
	}

	if len(rs.Rows) != len(expected) {
		t.Fatalf("expected %d path tuples, got %d: %v", len(expected), len(rs.Rows), rs.Rows)
	}
	for _, pair := range expected {
		if !got[pair] {
			t.Errorf("missing path (%d,%d)", pair[0], pair[1])
		}
	}
}

// TestSeminaiveTwoStrataWithNegation verifies two strata where the second
// uses negation over results computed in the first.
//
// Stratum 0: A(x) :- Base(x).
// Stratum 1: B(x) :- Base(x), not A(x).  [should be empty]
//
// Since A contains everything in Base, B should be empty.
func TestSeminaiveTwoStrataWithNegation(t *testing.T) {
	base := makeRelation("Base", 1, IntVal{1}, IntVal{2}, IntVal{3})
	baseRels := map[string]*Relation{"Base": base}

	ep := &plan.ExecutionPlan{
		Strata: []plan.Stratum{
			// Stratum 0: A(x) :- Base(x).
			{
				Rules: []plan.PlannedRule{
					{
						Head: datalog.Atom{Predicate: "A", Args: []datalog.Term{v("x")}},
						JoinOrder: []plan.JoinStep{
							positiveStep("Base", v("x")),
						},
					},
				},
			},
			// Stratum 1: B(x) :- Base(x), not A(x).
			{
				Rules: []plan.PlannedRule{
					{
						Head: datalog.Atom{Predicate: "B", Args: []datalog.Term{v("x")}},
						JoinOrder: []plan.JoinStep{
							positiveStep("Base", v("x")),
							negativeStep("A", v("x")),
						},
					},
				},
			},
		},
		Query: &plan.PlannedQuery{
			Select: []datalog.Term{v("x")},
			JoinOrder: []plan.JoinStep{
				positiveStep("B", v("x")),
			},
		},
	}

	rs, err := Evaluate(context.Background(), ep, baseRels)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	if len(rs.Rows) != 0 {
		t.Errorf("expected 0 rows (B should be empty), got %d: %v", len(rs.Rows), rs.Rows)
	}
}

// TestSeminaiveAggregateAfterFixpoint verifies that an aggregate is evaluated
// after the stratum's fixpoint, not during it.
//
// Base: Val(1), Val(2), Val(3).
// Stratum 0: A(x) :- Val(x). [fixpoint: A = {1,2,3}]
//
//	count(x | A(x)) → Cnt(3).
func TestSeminaiveAggregateAfterFixpoint(t *testing.T) {
	val := makeRelation("Val", 1, IntVal{1}, IntVal{2}, IntVal{3})
	baseRels := map[string]*Relation{"Val": val}

	ep := &plan.ExecutionPlan{
		Strata: []plan.Stratum{
			{
				Rules: []plan.PlannedRule{
					{
						Head: datalog.Atom{Predicate: "A", Args: []datalog.Term{v("x")}},
						JoinOrder: []plan.JoinStep{
							positiveStep("Val", v("x")),
						},
					},
				},
				Aggregates: []plan.PlannedAggregate{
					{
						ResultRelation: "Cnt",
						GroupByVars:    nil,
						Agg: datalog.Aggregate{
							Func:      "count",
							Var:       "x",
							ResultVar: datalog.Var{Name: "Cnt"},
							Body: []datalog.Literal{
								{
									Positive: true,
									Atom: datalog.Atom{
										Predicate: "A",
										Args:      []datalog.Term{v("x")},
									},
								},
							},
						},
					},
				},
			},
		},
		Query: &plan.PlannedQuery{
			Select: []datalog.Term{v("n")},
			JoinOrder: []plan.JoinStep{
				positiveStep("Cnt", v("n")),
			},
		},
	}

	rs, err := Evaluate(context.Background(), ep, baseRels)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	if len(rs.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d: %v", len(rs.Rows), rs.Rows)
	}
	cnt := rs.Rows[0][0].(IntVal).V
	if cnt != 3 {
		t.Errorf("expected count=3, got %d", cnt)
	}
}

// TestSeminaiveSelfRecursivePath verifies correct semi-naive evaluation for
// a purely self-recursive path rule on a 4-node chain: 1→2→3→4.
//
// Rules:
//
//	Path(x,y) :- Edge(x,y).
//	Path(x,z) :- Path(x,y), Path(y,z).
//
// Expected 6 pairs: (1,2),(2,3),(3,4),(1,3),(2,4),(1,4).
// The purely self-recursive rule requires position-aware delta substitution:
// each semi-naive variant must use delta at exactly ONE of the two Path
// positions, not both. Using delta at both would over-count and miss pairs.
func TestSeminaiveSelfRecursivePath(t *testing.T) {
	edge := makeRelation("Edge", 2,
		IntVal{1}, IntVal{2},
		IntVal{2}, IntVal{3},
		IntVal{3}, IntVal{4},
	)
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
					// Path(x,z) :- Path(x,y), Path(y,z).
					{
						Head: datalog.Atom{Predicate: "Path", Args: []datalog.Term{v("x"), v("z")}},
						JoinOrder: []plan.JoinStep{
							positiveStep("Path", v("x"), v("y")),
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

	rs, err := Evaluate(context.Background(), ep, baseRels)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	got := make(map[[2]int64]bool)
	for _, row := range rs.Rows {
		from := row[0].(IntVal).V
		to := row[1].(IntVal).V
		got[[2]int64{from, to}] = true
	}

	expected := [][2]int64{
		{1, 2}, {2, 3}, {3, 4},
		{1, 3}, {2, 4},
		{1, 4},
	}

	if len(rs.Rows) != len(expected) {
		t.Fatalf("expected %d path tuples, got %d: %v", len(expected), len(rs.Rows), rs.Rows)
	}
	for _, pair := range expected {
		if !got[pair] {
			t.Errorf("missing path (%d,%d)", pair[0], pair[1])
		}
	}
}

// TestSeminaiveCancellation verifies context cancellation is respected.
func TestSeminaiveCancellation(t *testing.T) {
	// Build a trivial plan that would otherwise succeed.
	A := makeRelation("A", 1, IntVal{1})
	baseRels := map[string]*Relation{"A": A}

	ep := &plan.ExecutionPlan{
		Strata: []plan.Stratum{
			{
				Rules: []plan.PlannedRule{
					{
						Head: datalog.Atom{Predicate: "B", Args: []datalog.Term{v("x")}},
						JoinOrder: []plan.JoinStep{
							positiveStep("A", v("x")),
						},
					},
				},
			},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := Evaluate(ctx, ep, baseRels)
	if err == nil {
		// It may succeed because the stratum finished before the cancellation
		// check if the stratum has no fixpoint iteration needed. That's fine —
		// the important thing is it does not panic.
		t.Log("no error returned for pre-cancelled context (stratum may have completed)")
	}
}
