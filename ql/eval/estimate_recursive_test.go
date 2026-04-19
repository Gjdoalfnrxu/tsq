package eval

import (
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

// fakeLookup mirrors the test helper from ql/plan but lives here to
// avoid a test-time cyclic dependency on ql/plan internals.
type fakeLookup struct {
	rc  map[string]int64
	ndv map[string]map[int]int64
}

func (f fakeLookup) RowCount(rel string) (int64, bool) {
	v, ok := f.rc[rel]
	return v, ok
}

func (f fakeLookup) NDV(rel string, col int) (int64, bool) {
	cols, ok := f.ndv[rel]
	if !ok {
		return 0, false
	}
	v, ok := cols[col]
	return v, ok
}

// progTC builds the textbook transitive-closure program over a base
// `edge` relation, returning the program plus a synthetic edge
// relation containing `nEdges` rows.
func progTC(nEdges int) (*datalog.Program, map[string]*Relation) {
	prog := &datalog.Program{Rules: []datalog.Rule{
		{
			Head: datalog.Atom{Predicate: "tc", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "y"}}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "edge", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "y"}}}},
			},
		},
		{
			Head: datalog.Atom{Predicate: "tc", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "y"}}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "edge", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "z"}}}},
				{Positive: true, Atom: datalog.Atom{Predicate: "tc", Args: []datalog.Term{datalog.Var{Name: "z"}, datalog.Var{Name: "y"}}}},
			},
		},
	}}
	edge := NewRelation("edge", 2)
	for i := 0; i < nEdges; i++ {
		edge.Add(Tuple{IntVal{V: int64(i)}, IntVal{V: int64(i + 1)}})
	}
	rels := map[string]*Relation{relKey("edge", 2): edge}
	return prog, rels
}

func TestEstimateRecursiveIDBSizes_NoStatsLookupSaturates(t *testing.T) {
	prog, rels := progTC(20)
	hints := map[string]int{"edge": 20}
	updates := EstimateRecursiveIDBSizes(prog, rels, hints, nil)
	got, ok := updates["tc"]
	if !ok {
		t.Fatalf("expected an update for tc, got %v", updates)
	}
	if got != plan.SaturatedSizeHint {
		t.Errorf("expected SaturatedSizeHint with nil lookup, got %d", got)
	}
	if hints["tc"] != plan.SaturatedSizeHint {
		t.Errorf("hints[tc] should be SaturatedSizeHint, got %d", hints["tc"])
	}
}

func TestEstimateRecursiveIDBSizes_GeometricEstimateLowSigma(t *testing.T) {
	prog, rels := progTC(20)
	hints := map[string]int{"edge": 20}
	lookup := fakeLookup{
		rc:  map[string]int64{"edge": 20},
		ndv: map[string]map[int]int64{"edge": {0: 40}}, // σ = 0.5
	}
	updates := EstimateRecursiveIDBSizes(prog, rels, hints, lookup)
	got := updates["tc"]
	// B sampled ≈ 20 (every edge survives). With σ=0.5 → ~40.
	// Test the order-of-magnitude bound, not an exact match.
	if got < 20 || got > 200 {
		t.Errorf("expected ~40 from geometric form, got %d", got)
	}
}

func TestEstimateRecursiveIDBSizes_NoRecursiveIDBs(t *testing.T) {
	// Pure non-recursive program: no recursive IDBs → no updates.
	prog := &datalog.Program{Rules: []datalog.Rule{
		{
			Head: datalog.Atom{Predicate: "p", Args: []datalog.Term{datalog.Var{Name: "x"}}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "edge", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "y"}}}},
			},
		},
	}}
	rels := map[string]*Relation{relKey("edge", 2): NewRelation("edge", 2)}
	updates := EstimateRecursiveIDBSizes(prog, rels, map[string]int{}, nil)
	if len(updates) != 0 {
		t.Errorf("expected no updates for non-recursive program, got %v", updates)
	}
}

func TestEstimateRecursiveIDBSizes_NilProgramSafe(t *testing.T) {
	updates := EstimateRecursiveIDBSizes(nil, nil, nil, nil)
	if len(updates) != 0 {
		t.Errorf("expected no updates for nil program, got %v", updates)
	}
}

func TestEstimateRecursiveIDBSizes_OnlyGrowsHints(t *testing.T) {
	prog, rels := progTC(10)
	// Pre-seed a large hint; estimator must not shrink it.
	hints := map[string]int{"edge": 10, "tc": plan.SaturatedSizeHint}
	EstimateRecursiveIDBSizes(prog, rels, hints, fakeLookup{
		rc:  map[string]int64{"edge": 10},
		ndv: map[string]map[int]int64{"edge": {0: 100}}, // σ = 0.1, geometric tiny
	})
	if hints["tc"] != plan.SaturatedSizeHint {
		t.Errorf("estimator shrank hint from SaturatedSizeHint to %d", hints["tc"])
	}
}

func TestMakeMaterialisingEstimatorHookWithStats_NilLookupRunsRecursivePass(t *testing.T) {
	prog, rels := progTC(5)
	matSink := map[string]*Relation{}
	hook := MakeMaterialisingEstimatorHookWithStats(rels, matSink, nil)
	hints := map[string]int{"edge": 5}
	hook(prog, hints, 1000)
	// Recursive pass must have populated tc with SaturatedSizeHint
	// (default-stats fallback) — not the prior 1000-default behaviour.
	if hints["tc"] != plan.SaturatedSizeHint {
		t.Errorf("expected tc hint = SaturatedSizeHint via default-stats path, got %d", hints["tc"])
	}
}

func TestMakeMaterialisingEstimatorHookWithStats_PreservesInnerHookContract(t *testing.T) {
	// The outer hook must still populate the materialised sink
	// (the inner hook's responsibility) — otherwise injecting the
	// recursive estimator would silently break P2a.
	prog := &datalog.Program{Rules: []datalog.Rule{
		{
			ClassExtent: true,
			Head:        datalog.Atom{Predicate: "MyExtent", Args: []datalog.Term{datalog.Var{Name: "x"}}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "base", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "_y"}}}},
			},
		},
	}}
	base := NewRelation("base", 2)
	base.Add(Tuple{IntVal{V: 1}, IntVal{V: 2}})
	rels := map[string]*Relation{relKey("base", 2): base}
	matSink := map[string]*Relation{}
	hook := MakeMaterialisingEstimatorHookWithStats(rels, matSink, nil)
	extents := hook(prog, map[string]int{"base": 1}, 1000)
	if !extents["MyExtent"] {
		t.Errorf("expected MyExtent in extent set, got %v", extents)
	}
	if _, ok := matSink[relKey("MyExtent", 1)]; !ok {
		t.Errorf("expected materialised sink to contain MyExtent, got keys: %v", keys(matSink))
	}
}

func keys(m map[string]*Relation) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
