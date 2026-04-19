package rules

import (
	"strings"
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/eval"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
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

// TestMayResolveTo_FlowStepEmpty_ExprValueSourceNonEmpty (PR4 review MINOR)
// — explicit assertion that with NO FlowStep edges populated, MayResolveTo
// equals exactly ExprValueSource (base-case-only behaviour). The shared
// `mayResolveToBaseRels` helper masks this property because it pre-seeds
// every PR2/PR3 join input; if the recursive rule's body order ever
// changed in a way that depended on FlowStep being populated for the
// closure to terminate at base, the existing tests would still pass via
// the helper. This test names the property explicitly.
func TestMayResolveTo_FlowStepEmpty_ExprValueSourceNonEmpty(t *testing.T) {
	// NOTE: still goes through mayResolveToBaseRels so the seminaive
	// evaluator has every transitive base relation to read from
	// (otherwise the test fails on a missing-relation panic, not on the
	// closure semantics it's asserting). The override seeds
	// ExprValueSource and explicitly does NOT add anything that would
	// produce FlowStep rows — every PR2/PR3 lfs*/ifs* input is empty
	// in interFlowStepBaseRels(nil)'s default seeding.
	baseRels := mayResolveToBaseRels(map[string]*eval.Relation{
		"ExprValueSource": makeRel("ExprValueSource", 2,
			iv(700), iv(700),
			iv(701), iv(701),
			iv(702), iv(702),
		),
	})
	// Sanity guard: prove FlowStep really is empty in this fixture so a
	// future change to the helper that started populating it would be
	// caught here, not silently pass.
	fsCount, err := evalCount(baseRels, "FlowStep", 2)
	if err != nil {
		t.Fatalf("eval FlowStep: %v", err)
	}
	if fsCount != 0 {
		t.Fatalf("test pre-condition violated: FlowStep is non-empty (%d rows); base-case-only assertion no longer meaningful", fsCount)
	}

	rs := evalMayResolveTo(t, baseRels)
	if len(rs.Rows) != 3 {
		t.Fatalf("expected exactly 3 base-case rows, got %d: %v", len(rs.Rows), rs.Rows)
	}
	for _, want := range [][]eval.Value{
		{iv(700), iv(700)},
		{iv(701), iv(701)},
		{iv(702), iv(702)},
	} {
		if !resultContains(rs, want[0], want[1]) {
			t.Errorf("missing identity row (%v, %v): %v", want[0], want[1], rs.Rows)
		}
	}
}

// TestMayResolveTo_PlannerStackEndToEnd (PR4 review M4) — verifies the
// "Phase B planner stack works end-to-end on the real shipped
// MayResolveTo rule" claim. The synthetic
// TestEstimateRecursiveIDB_MayResolveToShape_SaturatesUnderHighFanOut in
// ql/plan uses a fictional `step` predicate and lowercase
// `mayResolveTo` — proves the math on a mock body, not the real rule.
//
// This test wires AllSystemRules() (which includes MayResolveToRules())
// through plan.IdentifyRecursiveIDBs and the eval-side
// EstimateRecursiveIDBSizes pass, then asserts:
//
//  1. `MayResolveTo` is recognised as a recursive IDB.
//  2. The estimator writes a non-default size hint (not the default 1000
//     and not unset).
//  3. With a bound-arg query, the magic-set rewrite emits a
//     `magic_MayResolveTo` rule.
//
// If (3) fails, that means magic-set is NOT firing on the recursive
// predicate end-to-end — a real design issue, not a quick fix.
func TestMayResolveTo_PlannerStackEndToEnd(t *testing.T) {
	// (1) Recursive-IDB identification --------------------------------
	allRules := AllSystemRules()
	prog := &datalog.Program{
		Rules: allRules,
		Query: &datalog.Query{
			Select: []datalog.Term{v("v"), v("s")},
			Body: []datalog.Literal{
				pos("MayResolveTo", v("v"), v("s")),
			},
		},
	}

	// Build the basePredicates set from the empty seeded base relations
	// (same shape the eval pass would build it from).
	emptyBase := mayResolveToBaseRels(nil)
	basePreds := map[string]bool{}
	for name := range emptyBase {
		basePreds[name] = true
	}
	recursives := plan.IdentifyRecursiveIDBs(prog, basePreds)
	foundMRT := false
	for _, r := range recursives {
		if r.Name == "MayResolveTo" {
			foundMRT = true
			break
		}
	}
	if !foundMRT {
		t.Fatalf("MayResolveTo not identified as recursive IDB; got recursives: %v", recursiveNames(recursives))
	}

	// (2) Size-hint pass -----------------------------------------------
	sizeHints := map[string]int{}
	updates := eval.EstimateRecursiveIDBSizes(prog, emptyBase, sizeHints, nil)
	hint, ok := sizeHints["MayResolveTo"]
	if !ok {
		t.Fatalf("EstimateRecursiveIDBSizes did not write a hint for MayResolveTo (updates=%v)", updates)
	}
	// nil lookup forces SaturatedSizeHint per the documented default-
	// stats degradation. That IS a non-default hint (default for unknown
	// relations elsewhere is 1000); the assertion the reviewer asked for
	// is "not the default 1000-row guess and not SaturatedSizeHint
	// because the relation was missing from the IDB list" — i.e. the
	// estimator actually saw and processed it. SaturatedSizeHint here is
	// the correct documented behaviour for nil lookup, so accept it
	// alongside any concrete numeric hint.
	if hint == 1000 || hint <= 0 {
		t.Errorf("expected non-default hint for MayResolveTo, got %d", hint)
	}

	// (3) Magic-set rewrite on bound-arg query --------------------------
	boundQuery := &datalog.Query{
		Select: []datalog.Term{v("s")},
		Body: []datalog.Literal{
			pos("MayResolveTo", datalog.IntConst{Value: 42}, v("s")),
		},
	}
	boundProg := &datalog.Program{Rules: allRules, Query: boundQuery}
	queryBindings := map[string][]int{"MayResolveTo": {0}}
	transformed := plan.MagicSetTransform(boundProg, queryBindings)
	if transformed == nil {
		t.Fatal("MagicSetTransform returned nil program")
	}
	foundMagicRule := false
	for _, r := range transformed.Rules {
		if strings.HasPrefix(r.Head.Predicate, "magic_MayResolveTo") {
			foundMagicRule = true
			break
		}
	}
	if !foundMagicRule {
		// This is the design-issue surface from the M4 brief: if magic-
		// set isn't firing on the recursive predicate end-to-end, the
		// "Phase B planner stack works end-to-end" claim is false.
		// Surface as Errorf with explicit framing so a future reader
		// understands it's a planner-design finding, not a test bug.
		t.Errorf("magic-set rewrite did not emit any magic_MayResolveTo rule for bound-arg query; planner stack may not be wiring magic-set onto the recursive predicate end-to-end (PR4 review M4 design surface)")
	}
}

func recursiveNames(rs []plan.RecursiveIDB) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Name
	}
	return out
}
