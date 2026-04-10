package eval

import (
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

// makeAgg builds a PlannedAggregate for testing.
// inputRel: name of the source relation in rels.
// aggVar: the variable to aggregate.
// groupByVars: variables forming the group key.
// fn: aggregate function name.
// resultRelation: name of the output relation.
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

// TestAggCount tests count aggregate.
func TestAggCount(t *testing.T) {
	// Relation: (group, value)
	rel := makeRelation("R", 2,
		IntVal{1}, IntVal{10},
		IntVal{1}, IntVal{20},
		IntVal{1}, IntVal{30},
		IntVal{2}, IntVal{40},
	)
	rels := map[string]*Relation{"R": rel}

	agg := makeAgg("R", "v", []string{"g"}, "count", "cnt")
	result := Aggregate(agg, rels)

	if result.Len() != 2 {
		t.Fatalf("expected 2 groups, got %d", result.Len())
	}
	// Find group 1 → count 3, group 2 → count 1.
	counts := map[int64]int64{}
	for _, row := range result.Tuples() {
		groupVal := row[0].(IntVal).V
		cntVal := row[1].(IntVal).V
		counts[groupVal] = cntVal
	}
	if counts[1] != 3 {
		t.Errorf("group 1: expected count=3, got %d", counts[1])
	}
	if counts[2] != 1 {
		t.Errorf("group 2: expected count=1, got %d", counts[2])
	}
}

// TestAggCountNoGroup tests count with no group-by.
func TestAggCountNoGroup(t *testing.T) {
	rel := makeRelation("R", 1, IntVal{1}, IntVal{2}, IntVal{3})
	rels := map[string]*Relation{"R": rel}

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

// TestAggMin tests min aggregate.
func TestAggMin(t *testing.T) {
	rel := makeRelation("R", 2,
		IntVal{1}, IntVal{50},
		IntVal{1}, IntVal{10},
		IntVal{1}, IntVal{30},
	)
	rels := map[string]*Relation{"R": rel}

	agg := makeAgg("R", "v", []string{"g"}, "min", "minv")
	result := Aggregate(agg, rels)

	if result.Len() != 1 {
		t.Fatalf("expected 1 group, got %d", result.Len())
	}
	minVal := result.Tuples()[0][1].(IntVal).V
	if minVal != 10 {
		t.Errorf("expected min=10, got %d", minVal)
	}
}

// TestAggMax tests max aggregate.
func TestAggMax(t *testing.T) {
	rel := makeRelation("R", 2,
		IntVal{1}, IntVal{5},
		IntVal{1}, IntVal{100},
		IntVal{1}, IntVal{42},
	)
	rels := map[string]*Relation{"R": rel}

	agg := makeAgg("R", "v", []string{"g"}, "max", "maxv")
	result := Aggregate(agg, rels)

	if result.Len() != 1 {
		t.Fatalf("expected 1 group, got %d", result.Len())
	}
	maxVal := result.Tuples()[0][1].(IntVal).V
	if maxVal != 100 {
		t.Errorf("expected max=100, got %d", maxVal)
	}
}

// TestAggSum tests sum aggregate.
func TestAggSum(t *testing.T) {
	rel := makeRelation("R", 2,
		IntVal{1}, IntVal{10},
		IntVal{1}, IntVal{20},
		IntVal{2}, IntVal{5},
	)
	rels := map[string]*Relation{"R": rel}

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

// TestAggAvg tests avg aggregate (integer division).
func TestAggAvg(t *testing.T) {
	rel := makeRelation("R", 2,
		IntVal{1}, IntVal{10},
		IntVal{1}, IntVal{20},
		IntVal{1}, IntVal{30},
	)
	rels := map[string]*Relation{"R": rel}

	agg := makeAgg("R", "v", []string{"g"}, "avg", "avgv")
	result := Aggregate(agg, rels)

	if result.Len() != 1 {
		t.Fatalf("expected 1 group, got %d", result.Len())
	}
	avgVal := result.Tuples()[0][1].(IntVal).V
	if avgVal != 20 { // (10+20+30)/3 = 20
		t.Errorf("expected avg=20, got %d", avgVal)
	}
}

// TestAggEmptyInput tests aggregates over empty input.
func TestAggEmptyInput(t *testing.T) {
	rel := NewRelation("R", 2)
	rels := map[string]*Relation{"R": rel}

	agg := makeAgg("R", "v", []string{"g"}, "count", "cnt")
	result := Aggregate(agg, rels)

	// No groups → empty result.
	if result.Len() != 0 {
		t.Errorf("expected 0 results for empty input, got %d", result.Len())
	}
}

// --- Phase 1h: Additional aggregates ---

// TestAggStrictcount tests strictcount — like count but no result for empty set.
func TestAggStrictcount(t *testing.T) {
	rel := makeRelation("R", 2,
		IntVal{1}, IntVal{10},
		IntVal{1}, IntVal{20},
		IntVal{2}, IntVal{30},
	)
	rels := map[string]*Relation{"R": rel}

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

// TestAggStrictcountEmpty tests strictcount returns no result for empty set.
func TestAggStrictcountEmpty(t *testing.T) {
	rel := NewRelation("R", 2)
	rels := map[string]*Relation{"R": rel}

	agg := makeAgg("R", "v", []string{"g"}, "strictcount", "cnt")
	result := Aggregate(agg, rels)

	if result.Len() != 0 {
		t.Errorf("expected 0 results for empty strictcount, got %d", result.Len())
	}
}

// TestAggStrictsum tests strictsum — like sum but no result for empty set.
func TestAggStrictsum(t *testing.T) {
	rel := makeRelation("R", 2,
		IntVal{1}, IntVal{10},
		IntVal{1}, IntVal{20},
	)
	rels := map[string]*Relation{"R": rel}

	agg := makeAgg("R", "v", []string{"g"}, "strictsum", "sval")
	result := Aggregate(agg, rels)

	if result.Len() != 1 {
		t.Fatalf("expected 1 group, got %d", result.Len())
	}
	sval := result.Tuples()[0][1].(IntVal).V
	if sval != 30 {
		t.Errorf("expected strictsum=30, got %d", sval)
	}
}

// TestAggStrictsumEmpty tests strictsum returns no result for empty set.
func TestAggStrictsumEmpty(t *testing.T) {
	rel := NewRelation("R", 2)
	rels := map[string]*Relation{"R": rel}

	agg := makeAgg("R", "v", []string{"g"}, "strictsum", "sval")
	result := Aggregate(agg, rels)

	if result.Len() != 0 {
		t.Errorf("expected 0 results for empty strictsum, got %d", result.Len())
	}
}

// TestAggConcat tests concat aggregate.
func TestAggConcat(t *testing.T) {
	rel := makeRelation("R", 2,
		IntVal{1}, StrVal{"hello"},
		IntVal{1}, StrVal{"world"},
	)
	rels := map[string]*Relation{"R": rel}

	agg := makeAgg("R", "v", []string{"g"}, "concat", "cval")
	result := Aggregate(agg, rels)

	if result.Len() != 1 {
		t.Fatalf("expected 1 group, got %d", result.Len())
	}
	cval := result.Tuples()[0][1].(StrVal).V
	// With default empty separator
	if cval != "helloworld" {
		t.Errorf("expected concat='helloworld', got %q", cval)
	}
}

// TestAggRank tests rank aggregate (v1 approximation).
func TestAggRank(t *testing.T) {
	rel := makeRelation("R", 2,
		IntVal{1}, IntVal{10},
		IntVal{1}, IntVal{20},
		IntVal{1}, IntVal{30},
	)
	rels := map[string]*Relation{"R": rel}

	agg := makeAgg("R", "v", []string{"g"}, "rank", "rval")
	result := Aggregate(agg, rels)

	if result.Len() != 1 {
		t.Fatalf("expected 1 group, got %d", result.Len())
	}
	rval := result.Tuples()[0][1].(IntVal).V
	if rval != 3 {
		t.Errorf("expected rank=3 (v1 approximation = count), got %d", rval)
	}
}
