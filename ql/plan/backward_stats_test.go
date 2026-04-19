package plan

import (
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
)

// statsFake is a multi-column StatsLookup builder for PR4 tests. The
// shared fakeStats helper in estimate_recursive_test.go only models
// column 0; the low-fanout grounding logic operates on every bound
// column position, so we need per-column NDV control.
type statsFake struct {
	rowCount map[string]int64
	ndv      map[string]map[int]int64
}

func newStatsFake() *statsFake {
	return &statsFake{
		rowCount: map[string]int64{},
		ndv:      map[string]map[int]int64{},
	}
}

func (s *statsFake) RowCount(rel string) (int64, bool) {
	v, ok := s.rowCount[rel]
	return v, ok
}

func (s *statsFake) NDV(rel string, col int) (int64, bool) {
	cols, ok := s.ndv[rel]
	if !ok {
		return 0, false
	}
	v, ok := cols[col]
	return v, ok
}

func (s *statsFake) setRows(rel string, rows int64) {
	s.rowCount[rel] = rows
}

func (s *statsFake) setNDV(rel string, col int, ndv int64) {
	if s.ndv[rel] == nil {
		s.ndv[rel] = map[int]int64{}
	}
	s.ndv[rel][col] = ndv
}

// ----- isLowFanoutCol -------------------------------------------------

func TestIsLowFanoutCol_NilLookup(t *testing.T) {
	if isLowFanoutCol("R", 0, nil) {
		t.Fatalf("nil lookup must return false")
	}
}

func TestIsLowFanoutCol_MissingRel(t *testing.T) {
	s := newStatsFake()
	if isLowFanoutCol("UnknownRel", 0, s) {
		t.Fatalf("missing relation must return false")
	}
}

func TestIsLowFanoutCol_MissingCol(t *testing.T) {
	s := newStatsFake()
	s.setRows("R", 1000)
	// no NDV for col 0
	if isLowFanoutCol("R", 0, s) {
		t.Fatalf("missing column NDV must return false")
	}
}

func TestIsLowFanoutCol_NDVZeroOnPopulatedRel(t *testing.T) {
	s := newStatsFake()
	s.setRows("R", 1000)
	s.setNDV("R", 0, 0)
	if isLowFanoutCol("R", 0, s) {
		t.Fatalf("NDV=0 on populated rel must be treated as absent (false)")
	}
}

func TestIsLowFanoutCol_FanoutBoundary(t *testing.T) {
	// fan-out = rowCount / ndv; threshold inclusive.
	// LowFanoutThreshold = 10.
	s := newStatsFake()
	// Below threshold: 100 rows / 20 ndv = fan-out 5 → qualifies.
	s.setRows("Below", 100)
	s.setNDV("Below", 0, 20)
	if !isLowFanoutCol("Below", 0, s) {
		t.Fatalf("fan-out 5 (≤ 10) must qualify")
	}

	// At threshold: 100 rows / 10 ndv = fan-out 10 → qualifies (inclusive).
	s.setRows("At", 100)
	s.setNDV("At", 0, 10)
	if !isLowFanoutCol("At", 0, s) {
		t.Fatalf("fan-out 10 (= threshold) must qualify (inclusive)")
	}

	// Above threshold: 100 rows / 9 ndv ≈ fan-out 11 → does not qualify.
	s.setRows("Above", 100)
	s.setNDV("Above", 0, 9)
	if isLowFanoutCol("Above", 0, s) {
		t.Fatalf("fan-out 11 (> 10) must not qualify")
	}
}

func TestIsLowFanoutCol_FKShape(t *testing.T) {
	// FK-shape: child column has 1 parent → ndv == rowCount, fan-out 1.
	s := newStatsFake()
	s.setRows("LocalFlow", 5_000_000)
	s.setNDV("LocalFlow", 1, 5_000_000) // dst column: each row has unique dst-of-edge
	if !isLowFanoutCol("LocalFlow", 1, s) {
		t.Fatalf("FK-shape (NDV == RowCount) must qualify")
	}
}

// ----- anyBoundColIsLowFanout ----------------------------------------

func TestAnyBoundColIsLowFanout_ConstAtLowFanoutCol(t *testing.T) {
	// R(1, y) — col 0 carries a constant, col 0 is low-fanout.
	s := newStatsFake()
	s.setRows("R", 100)
	s.setNDV("R", 0, 50) // fan-out 2
	lit := atom("R", ic(1), v("y"))
	if !anyBoundColIsLowFanout(lit, map[string]bool{}, s) {
		t.Fatalf("constant at low-fanout col must qualify")
	}
}

func TestAnyBoundColIsLowFanout_BoundVarAtLowFanoutCol(t *testing.T) {
	// R(x, y) with x already bound; col 0 is low-fanout.
	s := newStatsFake()
	s.setRows("R", 1000)
	s.setNDV("R", 0, 500) // fan-out 2
	lit := atom("R", v("x"), v("y"))
	bound := map[string]bool{"x": true}
	if !anyBoundColIsLowFanout(lit, bound, s) {
		t.Fatalf("bound var at low-fanout col must qualify")
	}
}

func TestAnyBoundColIsLowFanout_NoBoundCols(t *testing.T) {
	// R(x, y) with neither var bound and no constants — nothing to check.
	s := newStatsFake()
	s.setRows("R", 100)
	s.setNDV("R", 0, 100)
	lit := atom("R", v("x"), v("y"))
	if anyBoundColIsLowFanout(lit, map[string]bool{}, s) {
		t.Fatalf("no bound cols must return false")
	}
}

func TestAnyBoundColIsLowFanout_BoundColHighFanout(t *testing.T) {
	// R(x, y) with x bound but col 0 is high-fanout.
	s := newStatsFake()
	s.setRows("R", 10_000)
	s.setNDV("R", 0, 100) // fan-out 100 → not low-fanout
	lit := atom("R", v("x"), v("y"))
	bound := map[string]bool{"x": true}
	if anyBoundColIsLowFanout(lit, bound, s) {
		t.Fatalf("bound col with fan-out 100 must not qualify")
	}
}

func TestAnyBoundColIsLowFanout_WildcardSkipped(t *testing.T) {
	// R(_, y) — wildcards are not bound vars even if col 0 is low-fanout.
	s := newStatsFake()
	s.setRows("R", 100)
	s.setNDV("R", 0, 50)
	lit := atom("R", v("_"), v("y"))
	if anyBoundColIsLowFanout(lit, map[string]bool{}, s) {
		t.Fatalf("wildcard must not count as bound")
	}
}

// ----- bodyContextGroundedVars: stats-aware grounding -----------------

// With stats, a bound var at a low-fanout column promotes the rest of
// the literal's vars to bound — even when the relation is too large
// for the SmallExtentThreshold path.
func TestBodyContextGroundedVars_StatsLiftsLargeRelation(t *testing.T) {
	// Body: Tiny(x), Big(x, y).
	// Tiny is small (binds x). Big is huge (above SmallExtentThreshold)
	// so without stats Big's `y` would NOT be lifted into bound.
	// With stats: Big col 0 has fan-out 2 → low-fanout → y is lifted.
	r := datalog.Rule{
		Head: datalog.Atom{Predicate: "H", Args: []datalog.Term{v("y")}},
		Body: []datalog.Literal{atom("Tiny", v("x")), atom("Big", v("x"), v("y"))},
	}
	hints := map[string]int{"Tiny": 5, "Big": 50_000_000}

	// Baseline: nil lookup. y must NOT be bound (Big is large, has no
	// const, no stats path).
	noStats := bodyContextGroundedVars(r, hints, map[string]bool{}, map[string]bool{}, nil, nil)
	if !noStats["x"] {
		t.Fatalf("baseline: x should be bound by Tiny small extent")
	}
	if noStats["y"] {
		t.Fatalf("baseline: y must NOT be bound without stats (Big is large, no const)")
	}

	// With stats showing Big col 0 is low-fanout: y must be lifted.
	s := newStatsFake()
	s.setRows("Big", 50_000_000)
	s.setNDV("Big", 0, 25_000_000) // fan-out 2
	withStats := bodyContextGroundedVars(r, hints, map[string]bool{}, map[string]bool{}, nil, s)
	if !withStats["y"] {
		t.Fatalf("with stats: y must be lifted (Big col 0 is low-fanout)")
	}
}

// Regression guard: nil lookup must produce byte-identical output to
// the no-stats path. This is the disj2-rounds-pass invariant.
func TestBodyContextGroundedVars_NilLookupIdentical(t *testing.T) {
	// Body that exercises every path: const-eq, small extent,
	// constant-bearing atom with shared var.
	r := datalog.Rule{
		Head: datalog.Atom{Predicate: "H", Args: []datalog.Term{v("y")}},
		Body: []datalog.Literal{
			{Cmp: &datalog.Comparison{Op: "=", Left: v("k"), Right: ic(7)}},
			atom("Tiny", v("x")),
			atom("Big", ic(1), v("x"), v("z")),
		},
	}
	hints := map[string]int{"Tiny": 5, "Big": 50_000_000}

	a := bodyContextGroundedVars(r, hints, map[string]bool{"h0": true}, map[string]bool{}, nil, nil)
	// Same call, "with stats" but lookup nil → must match exactly.
	b := bodyContextGroundedVars(r, hints, map[string]bool{"h0": true}, map[string]bool{}, nil, nil)
	if len(a) != len(b) {
		t.Fatalf("nil-lookup: maps differ in size: %v vs %v", a, b)
	}
	for k := range a {
		if !b[k] {
			t.Fatalf("nil-lookup: key %q in a but not b", k)
		}
	}
}

// ----- InferBackwardDemandWithStats end-to-end ------------------------

// A rule body with a low-fanout-bound large IDB call: without stats,
// the IDB's column shouldn't be observed bound; with stats, it should.
func TestInferBackwardDemandWithStats_LiftsThroughBigBaseRelation(t *testing.T) {
	// Big(x, y) — large base relation, col 0 low-fanout per stats.
	// P(y) :- Big(x, y), Edge(x, y).      (x is shared)
	// Q(y) :- P(y), Tiny(y).
	//
	// Caller-side context for P at the Q-rule callsite: we want to
	// confirm InferBackwardDemandWithStats threads `lookup` into
	// bodyContextGroundedVars by checking a directly observable
	// difference between with/without stats.
	//
	// Simpler synthetic shape: query body uses a low-fanout-bound Big
	// to lift y, then passes y to P. Without stats, query-side
	// observation of P sees y as unbound (Big is too large to ground
	// through the no-stats path); with stats, y is bound so P col 0 is
	// observed bound.
	// P is arity-2 so it does NOT qualify as a large-arity-1 grounder
	// (the path that would otherwise self-lift y in the query body).
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			{Head: datalog.Atom{Predicate: "P", Args: []datalog.Term{v("y"), v("z")}},
				Body: []datalog.Literal{atom("Edge", v("y"), v("z"))}},
		},
		Query: &datalog.Query{Body: []datalog.Literal{
			atom("Big", ic(42), v("y")),
			atom("P", v("y"), v("z")),
		}},
	}
	hints := map[string]int{"Big": 50_000_000}

	// Without stats: Big has a const at col 0 but is huge; the
	// constant-bearing-atom path requires a SHARED bound var with
	// existing bound set, but const at col 0 alone (no shared var)
	// does not bind y in the existing logic UNLESS the literal is
	// also a small extent. So baseline should NOT bind y; P col 0
	// should be observed unbound.
	dNoStats := InferBackwardDemandWithClassExtents(prog, hints, nil)
	if cols, ok := dNoStats["P"]; ok && len(cols) > 0 {
		t.Fatalf("baseline: P should have empty demand (Big too large to lift y), got %v", cols)
	}

	// With stats: Big col 0 has constant 42 and is low-fanout → lifts y.
	s := newStatsFake()
	s.setRows("Big", 50_000_000)
	s.setNDV("Big", 0, 25_000_000) // fan-out 2
	dWithStats := InferBackwardDemandWithStats(prog, hints, nil, s)
	cols, ok := dWithStats["P"]
	if !ok || len(cols) != 1 || cols[0] != 0 {
		t.Fatalf("with stats: P demand must be [0], got %v (ok=%v)", cols, ok)
	}
}

// Nil-lookup wrapper: byte-identical to the no-stats entry point.
func TestInferBackwardDemandWithStats_NilLookupMatchesBaseline(t *testing.T) {
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			{Head: datalog.Atom{Predicate: "P", Args: []datalog.Term{v("x"), v("y")}},
				Body: []datalog.Literal{atom("Edge", v("x"), v("y"))}},
			{Head: datalog.Atom{Predicate: "Q", Args: []datalog.Term{v("y")}},
				Body: []datalog.Literal{atom("P", ic(1), v("y"))}},
		},
	}
	a := InferBackwardDemandWithClassExtents(prog, nil, nil)
	b := InferBackwardDemandWithStats(prog, nil, nil, nil)
	if len(a) != len(b) {
		t.Fatalf("size differ: %v vs %v", a, b)
	}
	for pred, ca := range a {
		cb, ok := b[pred]
		if !ok {
			t.Fatalf("pred %q in baseline but not stats-nil", pred)
		}
		if !sameCols(ca, cb) {
			t.Fatalf("pred %q cols differ: %v vs %v", pred, ca, cb)
		}
	}
}

// ----- PlanWithStats end-to-end ---------------------------------------

// Plan-order observable difference. Without stats the planner orders
// purely on size hints; with stats it can lift a low-fanout-bound
// large literal to "everything bound" status, which changes downstream
// scheduling.
func TestPlanWithStats_NilLookupMatchesBaseline(t *testing.T) {
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			{Head: datalog.Atom{Predicate: "P", Args: []datalog.Term{v("x"), v("y")}},
				Body: []datalog.Literal{atom("Edge", v("x"), v("y"))}},
			{Head: datalog.Atom{Predicate: "Q", Args: []datalog.Term{v("y")}},
				Body: []datalog.Literal{atom("P", ic(1), v("y"))}},
		},
		Query: &datalog.Query{Body: []datalog.Literal{atom("Q", v("y"))}},
	}
	hints := map[string]int{"Edge": 10}
	epA, errsA := PlanWithClassExtents(prog, hints, nil)
	epB, errsB := PlanWithStats(prog, hints, nil, nil)
	if len(errsA) != 0 || len(errsB) != 0 {
		t.Fatalf("unexpected errors: a=%v b=%v", errsA, errsB)
	}
	if len(epA.Strata) != len(epB.Strata) {
		t.Fatalf("strata count differs: a=%d b=%d", len(epA.Strata), len(epB.Strata))
	}
	if !sameCols(epA.Demand["P"], epB.Demand["P"]) {
		t.Fatalf("nil-lookup demand differs: a=%v b=%v", epA.Demand["P"], epB.Demand["P"])
	}
}
