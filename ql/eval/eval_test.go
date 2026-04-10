package eval_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/Gjdoalfnrxu/tsq/extract/db"
	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/eval"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

// TestEvaluatorIntegration builds a small in-memory fact DB, constructs a
// simple query plan, evaluates it, and checks results.
//
// Fact: Node(id=1, file=10, kind="function", startLine=1, startCol=0, endLine=5, endCol=1)
//
//	Node(id=2, file=10, kind="call",     startLine=3, startCol=2, endLine=3, endCol=10)
//
// Query: Find all Node(id, _, kind, _, _, _, _) where kind = "function" → expect id=1.
func TestEvaluatorIntegration(t *testing.T) {
	// Build a fact DB.
	factDB := db.NewDB()
	nodeRel := factDB.Relation("Node")

	// Node(id, file, kind, startLine, startCol, endLine, endCol)
	if err := nodeRel.AddTuple(factDB,
		int32(1), int32(10), "function", int32(1), int32(0), int32(5), int32(1),
	); err != nil {
		t.Fatalf("AddTuple: %v", err)
	}
	if err := nodeRel.AddTuple(factDB,
		int32(2), int32(10), "call", int32(3), int32(2), int32(3), int32(10),
	); err != nil {
		t.Fatalf("AddTuple: %v", err)
	}

	// Serialise and re-read to test the full pipeline.
	var buf bytes.Buffer
	if err := factDB.Encode(&buf); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	data := buf.Bytes()
	readDB, err := db.ReadDB(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("ReadDB: %v", err)
	}

	// Plan: one stratum, one rule: FuncNode(id) :- Node(id, _, "function", _, _, _, _).
	// Then query: select id where FuncNode(id).
	ep := &plan.ExecutionPlan{
		Strata: []plan.Stratum{
			{
				Rules: []plan.PlannedRule{
					{
						Head: datalog.Atom{
							Predicate: "FuncNode",
							Args:      []datalog.Term{datalog.Var{Name: "id"}},
						},
						JoinOrder: []plan.JoinStep{
							{
								Literal: datalog.Literal{
									Positive: true,
									Atom: datalog.Atom{
										Predicate: "Node",
										Args: []datalog.Term{
											datalog.Var{Name: "id"},
											datalog.Wildcard{},
											datalog.StringConst{Value: "function"},
											datalog.Wildcard{},
											datalog.Wildcard{},
											datalog.Wildcard{},
											datalog.Wildcard{},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		Query: &plan.PlannedQuery{
			Select: []datalog.Term{datalog.Var{Name: "id"}},
			JoinOrder: []plan.JoinStep{
				{
					Literal: datalog.Literal{
						Positive: true,
						Atom: datalog.Atom{
							Predicate: "FuncNode",
							Args:      []datalog.Term{datalog.Var{Name: "id"}},
						},
					},
				},
			},
		},
	}

	evaluator := eval.NewEvaluator(ep, readDB)
	rs, err := evaluator.Evaluate(context.Background())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	if len(rs.Rows) != 1 {
		t.Fatalf("expected 1 result row, got %d: %v", len(rs.Rows), rs.Rows)
	}

	// The id column should be 1 (the function node).
	row := rs.Rows[0]
	id, ok := row[0].(eval.IntVal)
	if !ok {
		t.Fatalf("expected IntVal, got %T", row[0])
	}
	if id.V != 1 {
		t.Errorf("expected id=1, got %d", id.V)
	}
}

// TestEvaluatorEmptyDB verifies that evaluation over an empty DB produces
// no results without panicking.
func TestEvaluatorEmptyDB(t *testing.T) {
	factDB := db.NewDB()

	var buf bytes.Buffer
	if err := factDB.Encode(&buf); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	data := buf.Bytes()
	readDB, err := db.ReadDB(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("ReadDB: %v", err)
	}

	ep := &plan.ExecutionPlan{
		Strata: []plan.Stratum{
			{
				Rules: []plan.PlannedRule{
					{
						Head: datalog.Atom{
							Predicate: "Q",
							Args:      []datalog.Term{datalog.Var{Name: "x"}},
						},
						JoinOrder: []plan.JoinStep{
							{
								Literal: datalog.Literal{
									Positive: true,
									Atom: datalog.Atom{
										Predicate: "Node",
										Args:      []datalog.Term{datalog.Var{Name: "x"}},
									},
								},
							},
						},
					},
				},
			},
		},
		Query: &plan.PlannedQuery{
			Select: []datalog.Term{datalog.Var{Name: "x"}},
			JoinOrder: []plan.JoinStep{
				{
					Literal: datalog.Literal{
						Positive: true,
						Atom: datalog.Atom{
							Predicate: "Q",
							Args:      []datalog.Term{datalog.Var{Name: "x"}},
						},
					},
				},
			},
		},
	}

	evaluator := eval.NewEvaluator(ep, readDB)
	rs, err := evaluator.Evaluate(context.Background())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if len(rs.Rows) != 0 {
		t.Errorf("expected 0 rows for empty DB, got %d", len(rs.Rows))
	}
}
