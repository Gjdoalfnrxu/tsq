package rules

import (
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/eval"
)

// mayResolveToBaseRels supplies empty bases for all relations the
// MayResolveTo recursive closure transitively joins against.
//
// The closure body is `MayResolveTo :- ExprValueSource ; FlowStep, MayResolveTo`.
// FlowStep itself is `LocalFlowStep ∪ InterFlowStep` over the eleven
// `lfs*` and three `ifs*` rules — so seeding all PR2/PR3 join inputs is
// the simplest way to guarantee a self-contained fixture without empty-
// relation panics.
func mayResolveToBaseRels(overrides map[string]*eval.Relation) map[string]*eval.Relation {
	base := interFlowStepBaseRels(nil)
	for k, v := range overrides {
		base[k] = v
	}
	return base
}

// queryMayResolveTo returns a query selecting all (v, s) rows of MayResolveTo.
func queryMayResolveTo() *datalog.Query {
	return &datalog.Query{
		Select: []datalog.Term{v("v"), v("s")},
		Body: []datalog.Literal{
			pos("MayResolveTo", v("v"), v("s")),
		},
	}
}

func evalMayResolveTo(t *testing.T, baseRels map[string]*eval.Relation) *eval.ResultSet {
	t.Helper()
	return planAndEval(t, AllSystemRules(), queryMayResolveTo(), baseRels)
}

// TestMayResolveToBaseCase — every ExprValueSource row appears in
// MayResolveTo as the identity rule. No FlowStep edges populated; only
// the base case fires.
func TestMayResolveToBaseCase(t *testing.T) {
	// Two identity value-source rows: (400, 400) and (401, 401).
	baseRels := mayResolveToBaseRels(map[string]*eval.Relation{
		"ExprValueSource": makeRel("ExprValueSource", 2,
			iv(400), iv(400),
			iv(401), iv(401),
		),
	})
	rs := evalMayResolveTo(t, baseRels)
	if len(rs.Rows) != 2 {
		t.Fatalf("expected 2 base-case rows, got %d: %v", len(rs.Rows), rs.Rows)
	}
	if !resultContains(rs, iv(400), iv(400)) || !resultContains(rs, iv(401), iv(401)) {
		t.Errorf("expected identity rows for both value sources, got %v", rs.Rows)
	}
}

// TestMayResolveToOneHop — a single FlowStep edge composes with the base
// case to produce a one-hop resolution. Built via the `lfsVarInit` step
// kind: `const x = source; use(x);` produces FlowStep(source, useExpr)
// and MayResolveTo(source, source) base, closing into
// MayResolveTo(useExpr, source).
func TestMayResolveToOneHop(t *testing.T) {
	// VarDecl(declId=200, sym=10, initExpr=400, isConst=1); use=500
	// ExprValueSource(400, 400) — initExpr is a value source.
	// Expected closure rows: (400, 400), (500, 400).
	baseRels := mayResolveToBaseRels(map[string]*eval.Relation{
		"VarDecl":         makeRel("VarDecl", 4, iv(200), iv(10), iv(400), iv(1)),
		"ExprMayRef":      makeRel("ExprMayRef", 2, iv(500), iv(10)),
		"ExprValueSource": makeRel("ExprValueSource", 2, iv(400), iv(400)),
	})
	rs := evalMayResolveTo(t, baseRels)
	if !resultContains(rs, iv(400), iv(400)) {
		t.Errorf("missing base-case (400, 400): %v", rs.Rows)
	}
	if !resultContains(rs, iv(500), iv(400)) {
		t.Errorf("missing one-hop (500, 400): %v", rs.Rows)
	}
}

// TestMayResolveToMultiHop — two FlowStep edges compose transitively.
// Models `const a = source; const b = a; use(b);` — two lfsVarInit
// edges chain through the closure.
func TestMayResolveToMultiHop(t *testing.T) {
	// VarDecl(decl1, symA=10, initExpr=400, _) + ExprMayRef(refA=600, symA=10)
	// VarDecl(decl2, symB=11, initExpr=600 [the ref to a], _) + ExprMayRef(useB=500, symB=11)
	// ExprValueSource(400, 400)
	// FlowStep edges: (400 → 600) via lfsVarInit on decl1;
	//                 (600 → 500) via lfsVarInit on decl2.
	// Expected closure rows include: (400, 400), (600, 400), (500, 400).
	baseRels := mayResolveToBaseRels(map[string]*eval.Relation{
		"VarDecl": makeRel("VarDecl", 4,
			iv(200), iv(10), iv(400), iv(1),
			iv(201), iv(11), iv(600), iv(1),
		),
		"ExprMayRef": makeRel("ExprMayRef", 2,
			iv(600), iv(10), // ref to a (symA=10) is at expr 600
			iv(500), iv(11), // use of b (symB=11) is at expr 500
		),
		"ExprValueSource": makeRel("ExprValueSource", 2, iv(400), iv(400)),
	})
	rs := evalMayResolveTo(t, baseRels)
	for _, want := range [][]eval.Value{
		{iv(400), iv(400)}, // base case
		{iv(600), iv(400)}, // one hop
		{iv(500), iv(400)}, // two hops — the load-bearing transitivity
	} {
		if !resultContains(rs, want[0], want[1]) {
			t.Errorf("missing expected closure row (%v, %v): got %v", want[0], want[1], rs.Rows)
		}
	}
}

// TestMayResolveToCycleTerminates — pathological self-cycle (`a = b; b = a`)
// must terminate. The (v, s) tuple set is finite so the seminaive
// fixpoint converges; this test asserts that the closure does not loop.
//
// Construction: two lfsAssign edges that form a cycle in FlowStep
// without any ExprValueSource. Closure should produce zero rows
// (no base case to seed) and terminate.
func TestMayResolveToCycleTerminates(t *testing.T) {
	// Assign(_, rhsExpr=400, lhsSym=10) + ExprMayRef(useExpr=500, sym=10)
	// Assign(_, rhsExpr=500, lhsSym=11) + ExprMayRef(useExpr=400, sym=11)
	// FlowStep would yield (400 → 500) and (500 → 400). No ExprValueSource
	// rows — base case produces nothing — closure produces nothing.
	baseRels := mayResolveToBaseRels(map[string]*eval.Relation{
		"Assign": makeRel("Assign", 3,
			iv(100), iv(400), iv(10),
			iv(101), iv(500), iv(11),
		),
		"ExprMayRef": makeRel("ExprMayRef", 2,
			iv(500), iv(10),
			iv(400), iv(11),
		),
	})
	rs := evalMayResolveTo(t, baseRels)
	if len(rs.Rows) != 0 {
		t.Errorf("cycle without value source should produce 0 rows, got %d: %v",
			len(rs.Rows), rs.Rows)
	}
}

// TestMayResolveToCycleWithSourceTerminates — the cycle case but with
// one ExprValueSource seeding the closure. Must terminate AND must
// produce only the finite set of reachable (v, s) tuples.
func TestMayResolveToCycleWithSourceTerminates(t *testing.T) {
	// Same edges as TestMayResolveToCycleTerminates plus
	// ExprValueSource(400, 400). Both 400 and 500 are reachable from
	// source 400 (via the 400 ↔ 500 cycle). Expected: (400, 400) and
	// (500, 400). The cycle does not produce extra spurious rows because
	// (v, s) is finite.
	baseRels := mayResolveToBaseRels(map[string]*eval.Relation{
		"Assign": makeRel("Assign", 3,
			iv(100), iv(400), iv(10),
			iv(101), iv(500), iv(11),
		),
		"ExprMayRef": makeRel("ExprMayRef", 2,
			iv(500), iv(10),
			iv(400), iv(11),
		),
		"ExprValueSource": makeRel("ExprValueSource", 2, iv(400), iv(400)),
	})
	rs := evalMayResolveTo(t, baseRels)
	if !resultContains(rs, iv(400), iv(400)) {
		t.Errorf("missing base (400, 400): %v", rs.Rows)
	}
	if !resultContains(rs, iv(500), iv(400)) {
		t.Errorf("missing reachable-via-cycle (500, 400): %v", rs.Rows)
	}
	// No source seeded at 500 — must NOT see (400, 500) or (500, 500).
	for _, bad := range [][]eval.Value{
		{iv(400), iv(500)},
		{iv(500), iv(500)},
	} {
		if resultContains(rs, bad[0], bad[1]) {
			t.Errorf("cycle produced spurious row (%v, %v): %v", bad[0], bad[1], rs.Rows)
		}
	}
}
