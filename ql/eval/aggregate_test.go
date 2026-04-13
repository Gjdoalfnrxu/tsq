package eval

import (
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

func makeAgg(inputRel, aggVar string, groupByVars []string, fn, resultRelation string) plan.PlannedAggregate {
	groupBy := make([]datalog.Var, len(groupByVars))
	for i, n := range groupByVars {
		groupBy[i] = datalog.Var{Name: n}
	}

	bodyArgs := make([]datalog.Term, len(groupByVars)+1)
	for i, n := range groupByVars {
		bodyArgs[i] = datalog.Var{Name: n}
	}
	bodyArgs[len(groupByVars)] = datalog.Var{Name: aggVar}

	return plan.PlannedAggregate{
		ResultRelation: resultRelation,
		GroupByVars:    groupBy,
		Agg: datalog.Aggregate{
			Func:      fn,
			Var:       aggVar,
			ResultVar: datalog.Var{Name: resultRelation},
			Body: []datalog.Literal{
				{
					Positive: true,
					Atom: datalog.Atom{
						Predicate: inputRel,
						Args:      bodyArgs,
					},
				},
			},
		},
	}
}

func TestAggCount(t *testing.T) {
	rel := makeRelation("R", 2,
		IntVal{1}, IntVal{10},
		IntVal{1}, IntVal{20},
		IntVal{1}, IntVal{30},
		IntVal{2}, IntVal{40},
	)
	rels := RelsOf(rel)
	agg := makeAgg("R", "v", []string{"g"}, "count", "cnt")
	result := Aggregate(agg, rels)
	if result.Len() != 2 {
		t.Fatalf("expected 2 groups, got %d", result.Len())
	}
	counts := map[int64]int64{}
	for _, row := range result.Tuples() {
		counts[row[0].(IntVal).V] = row[1].(IntVal).V
	}
	if counts[1] != 3 {
		t.Errorf("group 1: expected count=3, got %d", counts[1])
	}
	if counts[2] != 1 {
		t.Errorf("group 2: expected count=1, got %d", counts[2])
	}
}

func TestAggCountNoGroup(t *testing.T) {
	rel := makeRelation("R", 1, IntVal{1}, IntVal{2}, IntVal{3})
	rels := RelsOf(rel)
	agg := makeAgg("R", "x", nil, "count", "cnt")
	result := Aggregate(agg, rels)
	if result.Len() != 1 {
		t.Fatalf("expected 1 result, got %d", result.Len())
	}
	cnt := result.Tuples()[0][0].(IntVal).V
	if cnt != 3 {
		t.Errorf("expected count=3, got %d", cnt)
	}
}

func TestAggMin(t *testing.T) {
	rel := makeRelation("R", 2, IntVal{1}, IntVal{50}, IntVal{1}, IntVal{10}, IntVal{1}, IntVal{30})
	rels := RelsOf(rel)
	agg := makeAgg("R", "v", []string{"g"}, "min", "minv")
	result := Aggregate(agg, rels)
	if result.Len() != 1 {
		t.Fatalf("expected 1 group, got %d", result.Len())
	}
	if result.Tuples()[0][1].(IntVal).V != 10 {
		t.Errorf("expected min=10, got %d", result.Tuples()[0][1].(IntVal).V)
	}
}

func TestAggMax(t *testing.T) {
	rel := makeRelation("R", 2, IntVal{1}, IntVal{5}, IntVal{1}, IntVal{100}, IntVal{1}, IntVal{42})
	rels := RelsOf(rel)
	agg := makeAgg("R", "v", []string{"g"}, "max", "maxv")
	result := Aggregate(agg, rels)
	if result.Len() != 1 {
		t.Fatalf("expected 1 group, got %d", result.Len())
	}
	if result.Tuples()[0][1].(IntVal).V != 100 {
		t.Errorf("expected max=100, got %d", result.Tuples()[0][1].(IntVal).V)
	}
}

func TestAggSum(t *testing.T) {
	rel := makeRelation("R", 2, IntVal{1}, IntVal{10}, IntVal{1}, IntVal{20}, IntVal{2}, IntVal{5})
	rels := RelsOf(rel)
	agg := makeAgg("R", "v", []string{"g"}, "sum", "sumv")
	result := Aggregate(agg, rels)
	if result.Len() != 2 {
		t.Fatalf("expected 2 groups, got %d", result.Len())
	}
	sums := map[int64]int64{}
	for _, row := range result.Tuples() {
		sums[row[0].(IntVal).V] = row[1].(IntVal).V
	}
	if sums[1] != 30 {
		t.Errorf("group 1 sum: expected 30, got %d", sums[1])
	}
	if sums[2] != 5 {
		t.Errorf("group 2 sum: expected 5, got %d", sums[2])
	}
}

func TestAggAvg(t *testing.T) {
	rel := makeRelation("R", 2, IntVal{1}, IntVal{10}, IntVal{1}, IntVal{20}, IntVal{1}, IntVal{30})
	rels := RelsOf(rel)
	agg := makeAgg("R", "v", []string{"g"}, "avg", "avgv")
	result := Aggregate(agg, rels)
	if result.Len() != 1 {
		t.Fatalf("expected 1 group, got %d", result.Len())
	}
	if result.Tuples()[0][1].(IntVal).V != 20 {
		t.Errorf("expected avg=20, got %d", result.Tuples()[0][1].(IntVal).V)
	}
}

func TestAggEmptyInput(t *testing.T) {
	rel := NewRelation("R", 2)
	rels := RelsOf(rel)
	agg := makeAgg("R", "v", []string{"g"}, "count", "cnt")
	result := Aggregate(agg, rels)
	if result.Len() != 0 {
		t.Errorf("expected 0 results for empty input, got %d", result.Len())
	}
}

func TestAggStrictcount(t *testing.T) {
	rel := makeRelation("R", 2, IntVal{1}, IntVal{10}, IntVal{1}, IntVal{20}, IntVal{2}, IntVal{30})
	rels := RelsOf(rel)
	agg := makeAgg("R", "v", []string{"g"}, "strictcount", "cnt")
	result := Aggregate(agg, rels)
	if result.Len() != 2 {
		t.Fatalf("expected 2 groups, got %d", result.Len())
	}
	counts := map[int64]int64{}
	for _, row := range result.Tuples() {
		counts[row[0].(IntVal).V] = row[1].(IntVal).V
	}
	if counts[1] != 2 {
		t.Errorf("group 1: expected strictcount=2, got %d", counts[1])
	}
	if counts[2] != 1 {
		t.Errorf("group 2: expected strictcount=1, got %d", counts[2])
	}
}

func TestAggStrictcountEmpty(t *testing.T) {
	rel := NewRelation("R", 2)
	rels := RelsOf(rel)
	agg := makeAgg("R", "v", []string{"g"}, "strictcount", "cnt")
	result := Aggregate(agg, rels)
	if result.Len() != 0 {
		t.Errorf("expected 0 results for empty strictcount, got %d", result.Len())
	}
}

func TestAggStrictsum(t *testing.T) {
	rel := makeRelation("R", 2, IntVal{1}, IntVal{10}, IntVal{1}, IntVal{20})
	rels := RelsOf(rel)
	agg := makeAgg("R", "v", []string{"g"}, "strictsum", "sval")
	result := Aggregate(agg, rels)
	if result.Len() != 1 {
		t.Fatalf("expected 1 group, got %d", result.Len())
	}
	if result.Tuples()[0][1].(IntVal).V != 30 {
		t.Errorf("expected strictsum=30, got %d", result.Tuples()[0][1].(IntVal).V)
	}
}

func TestAggStrictsumEmpty(t *testing.T) {
	rel := NewRelation("R", 2)
	rels := RelsOf(rel)
	agg := makeAgg("R", "v", []string{"g"}, "strictsum", "sval")
	result := Aggregate(agg, rels)
	if result.Len() != 0 {
		t.Errorf("expected 0 results for empty strictsum, got %d", result.Len())
	}
}

func TestAggConcat(t *testing.T) {
	rel := makeRelation("R", 2, IntVal{1}, StrVal{"hello"}, IntVal{1}, StrVal{"world"})
	rels := RelsOf(rel)
	agg := makeAgg("R", "v", []string{"g"}, "concat", "cval")
	result := Aggregate(agg, rels)
	if result.Len() != 1 {
		t.Fatalf("expected 1 group, got %d", result.Len())
	}
	cval := result.Tuples()[0][1].(StrVal).V
	if cval != "helloworld" {
		t.Errorf("expected concat='helloworld', got %q", cval)
	}
}

func TestRankOrdinal(t *testing.T) {
	rel := makeRelation("R", 2, IntVal{1}, IntVal{10}, IntVal{1}, IntVal{20}, IntVal{1}, IntVal{30})
	rels := RelsOf(rel)
	agg := makeAgg("R", "v", []string{"g"}, "rank", "rval")
	result := Aggregate(agg, rels)
	if result.Len() != 3 {
		t.Fatalf("expected 3 tuples (one per value), got %d", result.Len())
	}
	rankSet := map[int64]bool{}
	for _, row := range result.Tuples() {
		if row[0].(IntVal).V != 1 {
			t.Errorf("unexpected group key %d", row[0].(IntVal).V)
		}
		rankSet[row[1].(IntVal).V] = true
	}
	for _, expected := range []int64{1, 2, 3} {
		if !rankSet[expected] {
			t.Errorf("expected rank %d in result, got ranks %v", expected, rankSet)
		}
	}
}

func TestRankEmptyGroup(t *testing.T) {
	rel := NewRelation("R", 2)
	rels := RelsOf(rel)
	agg := makeAgg("R", "v", []string{"g"}, "rank", "rval")
	result := Aggregate(agg, rels)
	if result.Len() != 0 {
		t.Errorf("expected 0 results for empty rank input, got %d", result.Len())
	}
}

func TestComputeRankOrdinal(t *testing.T) {
	vals := []Value{IntVal{10}, IntVal{20}, IntVal{30}}
	ranks := computeRank(vals)
	expected := []int64{1, 2, 3}
	for i, r := range ranks {
		if r != expected[i] {
			t.Errorf("rank[%d]: expected %d, got %d", i, expected[i], r)
		}
	}
}

func TestComputeRankWithTies(t *testing.T) {
	vals := []Value{IntVal{10}, IntVal{20}, IntVal{20}, IntVal{30}}
	ranks := computeRank(vals)
	expected := []int64{1, 2, 3, 4}
	for i, r := range ranks {
		if r != expected[i] {
			t.Errorf("rank[%d]: expected %d, got %d (vals=%v, ranks=%v)", i, expected[i], r, vals, ranks)
		}
	}
}

func TestComputeRankAllTied(t *testing.T) {
	vals := []Value{IntVal{5}, IntVal{5}, IntVal{5}}
	ranks := computeRank(vals)
	expected := []int64{1, 2, 3}
	for i, r := range ranks {
		if r != expected[i] {
			t.Errorf("rank[%d]: expected %d, got %d", i, expected[i], r)
		}
	}
}

func TestAggUniqueSingle(t *testing.T) {
	rel := makeRelation("R", 2, IntVal{1}, IntVal{42}, IntVal{1}, IntVal{42}, IntVal{1}, IntVal{42})
	rels := RelsOf(rel)
	agg := makeAgg("R", "v", []string{"g"}, "unique", "uval")
	result := Aggregate(agg, rels)
	if result.Len() != 1 {
		t.Fatalf("expected 1 group, got %d", result.Len())
	}
	if result.Tuples()[0][1].(IntVal).V != 42 {
		t.Errorf("expected unique=42, got %v", result.Tuples()[0][1])
	}
}

func TestAggUniqueMultiple(t *testing.T) {
	rel := makeRelation("R", 2, IntVal{1}, IntVal{10}, IntVal{1}, IntVal{20})
	rels := RelsOf(rel)
	agg := makeAgg("R", "v", []string{"g"}, "unique", "uval")
	result := Aggregate(agg, rels)
	if result.Len() != 0 {
		t.Errorf("expected 0 results for non-unique values, got %d", result.Len())
	}
}

func TestAggUniqueEmpty(t *testing.T) {
	rel := NewRelation("R", 2)
	rels := RelsOf(rel)
	agg := makeAgg("R", "v", []string{"g"}, "unique", "uval")
	result := Aggregate(agg, rels)
	if result.Len() != 0 {
		t.Errorf("expected 0 results for empty unique, got %d", result.Len())
	}
}

func TestComputeRankStableOrder(t *testing.T) {
	vals := []Value{IntVal{30}, IntVal{20}, IntVal{20}, IntVal{10}}
	ranks := computeRank(vals)
	expected := []int64{4, 2, 3, 1}
	for i, r := range ranks {
		if r != expected[i] {
			t.Errorf("rank[%d]: expected %d, got %d", i, expected[i], r)
		}
	}
}
