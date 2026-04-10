package eval

import (
	"context"
	"fmt"
	"sort"
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
	"pgregory.net/rapid"
)

// TestParallelTransitiveClosure verifies that parallel evaluation produces
// the same transitive closure as sequential evaluation.
func TestParallelTransitiveClosure(t *testing.T) {
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
					{
						Head: datalog.Atom{Predicate: "Path", Args: []datalog.Term{v("x"), v("y")}},
						JoinOrder: []plan.JoinStep{
							positiveStep("Edge", v("x"), v("y")),
						},
					},
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

	// Sequential.
	rsSeq, err := Evaluate(context.Background(), ep, baseRels)
	if err != nil {
		t.Fatalf("sequential Evaluate failed: %v", err)
	}

	// Parallel.
	rsPar, err := Evaluate(context.Background(), ep, baseRels, WithParallel())
	if err != nil {
		t.Fatalf("parallel Evaluate failed: %v", err)
	}

	if len(rsSeq.Rows) != len(rsPar.Rows) {
		t.Fatalf("sequential got %d rows, parallel got %d rows",
			len(rsSeq.Rows), len(rsPar.Rows))
	}

	// Compare sorted keys.
	seqKeys := rowsToSortedKeys(rsSeq.Rows)
	parKeys := rowsToSortedKeys(rsPar.Rows)
	for i := range seqKeys {
		if seqKeys[i] != parKeys[i] {
			t.Errorf("mismatch at row %d: %q vs %q", i, seqKeys[i], parKeys[i])
		}
	}
}

// TestParallelMultiHead verifies parallel evaluation with multiple distinct
// head predicates in the same stratum.
func TestParallelMultiHead(t *testing.T) {
	A := makeRelation("A", 2,
		IntVal{1}, IntVal{2},
		IntVal{2}, IntVal{3},
	)
	B := makeRelation("B", 2,
		IntVal{10}, IntVal{20},
		IntVal{20}, IntVal{30},
	)
	baseRels := map[string]*Relation{"A": A, "B": B}

	ep := &plan.ExecutionPlan{
		Strata: []plan.Stratum{
			{
				Rules: []plan.PlannedRule{
					// P(x,y) :- A(x,y).
					{
						Head: datalog.Atom{Predicate: "P", Args: []datalog.Term{v("x"), v("y")}},
						JoinOrder: []plan.JoinStep{
							positiveStep("A", v("x"), v("y")),
						},
					},
					// Q(x,y) :- B(x,y).
					{
						Head: datalog.Atom{Predicate: "Q", Args: []datalog.Term{v("x"), v("y")}},
						JoinOrder: []plan.JoinStep{
							positiveStep("B", v("x"), v("y")),
						},
					},
				},
			},
		},
		Query: &plan.PlannedQuery{
			Select: []datalog.Term{v("a"), v("b")},
			JoinOrder: []plan.JoinStep{
				positiveStep("P", v("a"), v("b")),
			},
		},
	}

	// Sequential.
	rsSeq, err := Evaluate(context.Background(), ep, baseRels)
	if err != nil {
		t.Fatalf("sequential Evaluate failed: %v", err)
	}

	// Parallel.
	rsPar, err := Evaluate(context.Background(), ep, baseRels, WithParallel())
	if err != nil {
		t.Fatalf("parallel Evaluate failed: %v", err)
	}

	if len(rsSeq.Rows) != len(rsPar.Rows) {
		t.Fatalf("sequential got %d rows, parallel got %d rows",
			len(rsSeq.Rows), len(rsPar.Rows))
	}
}

// TestPropertyParallelEqualsSequential is a property test that verifies
// parallel evaluation produces the same results as sequential evaluation
// for randomly generated programs.
func TestPropertyParallelEqualsSequential(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		schema := genSchema(t)
		facts := genFacts(t, schema)
		rules := genRules(t, schema)

		prog := &datalog.Program{Rules: rules}
		execPlan, errs := plan.Plan(prog, nil)
		if len(errs) > 0 {
			t.Skip("invalid program: ", errs[0])
		}

		// Pick a query predicate.
		relNames := make([]string, 0, len(schema))
		for name := range schema {
			relNames = append(relNames, name)
		}
		sort.Strings(relNames)
		queryPred := relNames[rapid.IntRange(0, len(relNames)-1).Draw(t, "queryPred")]
		queryArity := schema[queryPred]

		queryArgs := make([]datalog.Term, queryArity)
		selectArgs := make([]datalog.Term, queryArity)
		for i := 0; i < queryArity; i++ {
			varName := fmt.Sprintf("q%d", i)
			queryArgs[i] = datalog.Var{Name: varName}
			selectArgs[i] = datalog.Var{Name: varName}
		}
		execPlan.Query = &plan.PlannedQuery{
			Select: selectArgs,
			JoinOrder: []plan.JoinStep{
				positiveStep(queryPred, queryArgs...),
			},
		}

		// Sequential.
		seqFacts := copyFacts(facts)
		rsSeq, err := Evaluate(context.Background(), execPlan, seqFacts)
		if err != nil {
			t.Fatalf("sequential: %v", err)
		}

		// Parallel.
		parFacts := copyFacts(facts)
		rsPar, err := Evaluate(context.Background(), execPlan, parFacts, WithParallel())
		if err != nil {
			t.Fatalf("parallel: %v", err)
		}

		seqKeys := rowsToSortedKeys(rsSeq.Rows)
		parKeys := rowsToSortedKeys(rsPar.Rows)

		if len(seqKeys) != len(parKeys) {
			t.Errorf("sequential %d rows vs parallel %d rows\nrules: %s",
				len(seqKeys), len(parKeys), prog.String())
			return
		}
		for i := range seqKeys {
			if seqKeys[i] != parKeys[i] {
				t.Errorf("mismatch at row %d\nseq: %v\npar: %v\nrules: %s",
					i, seqKeys, parKeys, prog.String())
				break
			}
		}
	})
}

func rowsToSortedKeys(rows [][]Value) []string {
	keys := make([]string, len(rows))
	for i, row := range rows {
		keys[i] = tupleKey(Tuple(row))
	}
	sort.Strings(keys)
	return keys
}

func copyFacts(facts map[string]*Relation) map[string]*Relation {
	cp := make(map[string]*Relation, len(facts))
	for k, v := range facts {
		nr := NewRelation(v.Name, v.Arity)
		for _, t := range v.Tuples() {
			nr.Add(t)
		}
		cp[k] = nr
	}
	return cp
}
