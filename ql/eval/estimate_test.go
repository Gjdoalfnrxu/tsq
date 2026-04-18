package eval_test

import (
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/eval"
)

// makeIntRel constructs a base relation with the given name and a list of
// arity-1 integer tuples. Test helper.
func makeIntRel(t *testing.T, name string, vals ...int64) *eval.Relation {
	t.Helper()
	r := eval.NewRelation(name, 1)
	for _, v := range vals {
		r.Add(eval.Tuple{eval.IntVal{V: v}})
	}
	return r
}

// makeIntRel2 constructs an arity-2 base relation from pairs.
func makeIntRel2(t *testing.T, name string, pairs ...[2]int64) *eval.Relation {
	t.Helper()
	r := eval.NewRelation(name, 2)
	for _, p := range pairs {
		r.Add(eval.Tuple{eval.IntVal{V: p[0]}, eval.IntVal{V: p[1]}})
	}
	return r
}

// TestEstimateNonRecursiveIDBSizesBaseOnly: a rule body composed of a single
// base predicate is sized correctly.
func TestEstimateNonRecursiveIDBSizesBaseOnly(t *testing.T) {
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			{
				Head: datalog.Atom{Predicate: "Q", Args: []datalog.Term{datalog.Var{Name: "x"}}},
				Body: []datalog.Literal{{Positive: true, Atom: datalog.Atom{Predicate: "A", Args: []datalog.Term{datalog.Var{Name: "x"}}}}},
			},
		},
	}
	base := map[string]*eval.Relation{
		"A": makeIntRel(t, "A", 1, 2, 3, 4, 5),
	}
	hints := map[string]int{"A": 5}
	updates := eval.EstimateNonRecursiveIDBSizes(prog, base, hints, 0)
	if updates["Q"] != 5 {
		t.Errorf("Q size: want 5, got %d (updates=%v, hints=%v)", updates["Q"], updates, hints)
	}
	if hints["Q"] != 5 {
		t.Errorf("hints[Q]: want 5, got %d", hints["Q"])
	}
}

// TestEstimateNonRecursiveIDBSizesJoinSelectivity: a join that filters down
// to fewer tuples than either input is sized at the JOINED size, not either
// individual relation. This is the property that makes the seed-selection
// fix work in production. Mutation-killable: if the function were to (e.g.)
// just take min of input sizes, this test would fail.
func TestEstimateNonRecursiveIDBSizesJoinSelectivity(t *testing.T) {
	// A(x): {1,2,3,4,5}     — 5 tuples
	// B(x,y): {(1,10),(2,20),(7,70)} — 3 tuples; only x∈{1,2} overlap with A
	// Q(x,y) :- A(x), B(x,y). — should be exactly 2 tuples.
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			{
				Head: datalog.Atom{Predicate: "Q", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "y"}}},
				Body: []datalog.Literal{
					{Positive: true, Atom: datalog.Atom{Predicate: "A", Args: []datalog.Term{datalog.Var{Name: "x"}}}},
					{Positive: true, Atom: datalog.Atom{Predicate: "B", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "y"}}}},
				},
			},
		},
	}
	base := map[string]*eval.Relation{
		"A": makeIntRel(t, "A", 1, 2, 3, 4, 5),
		"B": makeIntRel2(t, "B", [2]int64{1, 10}, [2]int64{2, 20}, [2]int64{7, 70}),
	}
	hints := map[string]int{"A": 5, "B": 3}
	updates := eval.EstimateNonRecursiveIDBSizes(prog, base, hints, 0)
	if updates["Q"] != 2 {
		t.Errorf("Q join size: want 2 (real intersection), got %d", updates["Q"])
	}
}

// TestEstimateNonRecursiveIDBSizesTransitive: an IDB defined on top of
// another IDB is correctly estimated using the previously-computed IDB,
// not the default hint. This is the case that broke without transitive
// closure (R(x) :- A(x), wasL(x); wasL(x) :- B(x)).
func TestEstimateNonRecursiveIDBSizesTransitive(t *testing.T) {
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			// L(x) :- B(x).
			{
				Head: datalog.Atom{Predicate: "L", Args: []datalog.Term{datalog.Var{Name: "x"}}},
				Body: []datalog.Literal{{Positive: true, Atom: datalog.Atom{Predicate: "B", Args: []datalog.Term{datalog.Var{Name: "x"}}}}},
			},
			// Q(x) :- A(x), L(x).
			{
				Head: datalog.Atom{Predicate: "Q", Args: []datalog.Term{datalog.Var{Name: "x"}}},
				Body: []datalog.Literal{
					{Positive: true, Atom: datalog.Atom{Predicate: "A", Args: []datalog.Term{datalog.Var{Name: "x"}}}},
					{Positive: true, Atom: datalog.Atom{Predicate: "L", Args: []datalog.Term{datalog.Var{Name: "x"}}}},
				},
			},
		},
	}
	base := map[string]*eval.Relation{
		"A": makeIntRel(t, "A", 1, 2, 3, 4, 5, 6, 7, 8),
		"B": makeIntRel(t, "B", 1, 2),
	}
	hints := map[string]int{"A": 8, "B": 2}
	updates := eval.EstimateNonRecursiveIDBSizes(prog, base, hints, 0)
	if updates["L"] != 2 {
		t.Errorf("L: want 2, got %d", updates["L"])
	}
	if updates["Q"] != 2 {
		t.Errorf("Q (transitive on L): want 2, got %d", updates["Q"])
	}
	if hints["L"] != 2 || hints["Q"] != 2 {
		t.Errorf("hints not updated: hints=%v", hints)
	}
}

// TestEstimateNonRecursiveIDBSizesNeverShrinks: an existing larger hint is
// not overwritten by a smaller computed size (defensive — protects against
// upstream name collisions and matches the parallel rule in seminaive's
// between-strata refresh).
func TestEstimateNonRecursiveIDBSizesNeverShrinks(t *testing.T) {
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			{
				Head: datalog.Atom{Predicate: "Q", Args: []datalog.Term{datalog.Var{Name: "x"}}},
				Body: []datalog.Literal{{Positive: true, Atom: datalog.Atom{Predicate: "A", Args: []datalog.Term{datalog.Var{Name: "x"}}}}},
			},
		},
	}
	base := map[string]*eval.Relation{
		"A": makeIntRel(t, "A", 1),
	}
	hints := map[string]int{"A": 1, "Q": 9999}
	eval.EstimateNonRecursiveIDBSizes(prog, base, hints, 0)
	if hints["Q"] != 9999 {
		t.Errorf("Q hint shrunk from 9999 to %d", hints["Q"])
	}
}

// TestEstimateNonRecursiveIDBSizesHonoursBindingCap is the issue #130
// regression guard. A trivial IDB whose body forms an unbound cross-product
// (no shared variables across the first two literals) would, with the
// pre-pass uncapped, materialise the full N×M intermediate just to count head
// facts — on real corpora this OOMs the host before the main eval ever runs.
//
// Construction: A(x) and B(y) share no variables in `Q(x,y) :- A(x), B(y).`
// Cross-product cardinality with |A|=|B|=200 is 40,000 — well above the
// cap=1000 we set, but small enough not to actually OOM the test host.
//
// With the cap correctly threaded, the pre-pass should hit the binding cap
// inside Rule(), `failed` should fire, the IDB drops out of `updates` and
// `hints` falls back to default. With the cap NOT threaded (the buggy
// behaviour pre-#130), Rule() would run unbounded and produce updates["Q"]
// = 40000.
//
// Mutation-killable: if the cap parameter were dropped or hard-coded back
// to 0, this test would see updates["Q"] == 40000 and fail.
func TestEstimateNonRecursiveIDBSizesHonoursBindingCap(t *testing.T) {
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			{
				Head: datalog.Atom{Predicate: "Q", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "y"}}},
				Body: []datalog.Literal{
					{Positive: true, Atom: datalog.Atom{Predicate: "A", Args: []datalog.Term{datalog.Var{Name: "x"}}}},
					{Positive: true, Atom: datalog.Atom{Predicate: "B", Args: []datalog.Term{datalog.Var{Name: "y"}}}},
				},
			},
		},
	}
	a := eval.NewRelation("A", 1)
	b := eval.NewRelation("B", 1)
	for i := int64(0); i < 200; i++ {
		a.Add(eval.Tuple{eval.IntVal{V: i}})
		b.Add(eval.Tuple{eval.IntVal{V: i + 10000}})
	}
	base := map[string]*eval.Relation{"A": a, "B": b}
	hints := map[string]int{"A": 200, "B": 200}

	updates := eval.EstimateNonRecursiveIDBSizes(prog, base, hints, 1000)

	// Bug case: cap ignored, full 40k cross-product computed.
	if updates["Q"] == 40000 {
		t.Fatalf("issue #130 regression: pre-pass ignored binding cap and materialised full %d-tuple cross-product", updates["Q"])
	}
	// Expected: cap fired, IDB skipped, no entry written.
	if _, ok := updates["Q"]; ok {
		t.Errorf("Q exceeded cap; expected best-effort skip, got updates[Q]=%d", updates["Q"])
	}
	if _, ok := hints["Q"]; ok {
		t.Errorf("Q hint should be absent on cap-skip; got hints[Q]=%d", hints["Q"])
	}
}

// TestEstimateNonRecursiveIDBSizesZeroCapIsUnbounded confirms the legacy
// escape hatch: passing 0 disables the cap. Mutation-killable: if cap=0
// were silently coerced to a non-zero default, this rule would be skipped
// instead of producing the full count.
func TestEstimateNonRecursiveIDBSizesZeroCapIsUnbounded(t *testing.T) {
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			{
				Head: datalog.Atom{Predicate: "Q", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "y"}}},
				Body: []datalog.Literal{
					{Positive: true, Atom: datalog.Atom{Predicate: "A", Args: []datalog.Term{datalog.Var{Name: "x"}}}},
					{Positive: true, Atom: datalog.Atom{Predicate: "B", Args: []datalog.Term{datalog.Var{Name: "y"}}}},
				},
			},
		},
	}
	base := map[string]*eval.Relation{
		"A": makeIntRel(t, "A", 1, 2, 3, 4, 5),
		"B": makeIntRel(t, "B", 10, 20, 30, 40),
	}
	hints := map[string]int{"A": 5, "B": 4}
	updates := eval.EstimateNonRecursiveIDBSizes(prog, base, hints, 0)
	if updates["Q"] != 20 {
		t.Errorf("Q (cap=0, full cross-product): want 20, got %d", updates["Q"])
	}
}

// TestEstimateNonRecursiveIDBSizesSkipsRecursive: a recursive rule is left
// at the default hint (no entry written) — the pre-pass declines rather
// than risking divergence.
func TestEstimateNonRecursiveIDBSizesSkipsRecursive(t *testing.T) {
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			// Path(x,y) :- Edge(x,y).
			{
				Head: datalog.Atom{Predicate: "Path", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "y"}}},
				Body: []datalog.Literal{{Positive: true, Atom: datalog.Atom{Predicate: "Edge", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "y"}}}}},
			},
			// Path(x,z) :- Edge(x,y), Path(y,z).  (recursive)
			{
				Head: datalog.Atom{Predicate: "Path", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "z"}}},
				Body: []datalog.Literal{
					{Positive: true, Atom: datalog.Atom{Predicate: "Edge", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "y"}}}},
					{Positive: true, Atom: datalog.Atom{Predicate: "Path", Args: []datalog.Term{datalog.Var{Name: "y"}, datalog.Var{Name: "z"}}}},
				},
			},
		},
	}
	base := map[string]*eval.Relation{
		"Edge": makeIntRel2(t, "Edge", [2]int64{1, 2}),
	}
	hints := map[string]int{"Edge": 1}
	updates := eval.EstimateNonRecursiveIDBSizes(prog, base, hints, 0)
	if _, ok := updates["Path"]; ok {
		t.Errorf("Path is recursive — should be skipped, got update %d", updates["Path"])
	}
	if _, ok := hints["Path"]; ok {
		t.Errorf("Path hint should be absent, got %d", hints["Path"])
	}
}
