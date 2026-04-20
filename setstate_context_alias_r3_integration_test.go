package integration_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Gjdoalfnrxu/tsq/bridge"
	extractrules "github.com/Gjdoalfnrxu/tsq/extract/rules"
	"github.com/Gjdoalfnrxu/tsq/extract/schema"
	"github.com/Gjdoalfnrxu/tsq/ql/desugar"
	"github.com/Gjdoalfnrxu/tsq/ql/eval"
	"github.com/Gjdoalfnrxu/tsq/ql/parse"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
	"github.com/Gjdoalfnrxu/tsq/ql/resolve"
)

// TestSetStateUpdaterCallsOtherSetStateThroughContext_R3 is the round-3
// positive regression test for the React-Context variant of the
// setStateUpdater rule on the round-3 fixture. The fixture exercises:
//
//   - IndirectValue.tsx: `const actions = {...}; <Provider value={actions}>`
//   - SpreadValue.tsx:   `<Provider value={{...base, setSA}}>` with `const base = {setSB}`
//   - ComputedKey.tsx:   string-literal computed keys (identifier-via-const + inline)
//
// The negative file Negative_NonConstKey.tsx must NOT contribute matches.
func TestSetStateUpdaterCallsOtherSetStateThroughContext_R3(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping extraction-heavy integration test in short mode")
	}

	factDB := extractProject(t, "testdata/projects/react-usestate-context-alias-r3")

	src, err := os.ReadFile("testdata/queries/v2/find_setstate_updater_calls_other_setstate_through_context.ql")
	if err != nil {
		t.Fatalf("read query: %v", err)
	}

	bridgeFiles := bridge.LoadBridge()
	importLoader := makeBridgeImportLoader(bridgeFiles)

	p := parse.NewParser(string(src), "find_setstate_updater_calls_other_setstate_through_context.ql")
	mod, err := p.Parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	resolved, err := resolve.Resolve(mod, importLoader)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(resolved.Errors) > 0 {
		t.Fatalf("resolve errors: %v", resolved.Errors)
	}
	prog, dsErrors := desugar.Desugar(resolved)
	if len(dsErrors) > 0 {
		t.Fatalf("desugar: %v", dsErrors)
	}
	prog = extractrules.MergeSystemRules(prog, extractrules.AllSystemRules())

	hints := make(map[string]int, len(schema.Registry))
	for _, def := range schema.Registry {
		hints[def.Name] = factDB.Relation(def.Name).Tuples()
	}

	const cap = 200_000

	baseRels, err := eval.LoadBaseRelations(factDB)
	if err != nil {
		t.Fatalf("load base relations: %v", err)
	}
	execPlan, planErrs := plan.EstimateAndPlan(
		prog,
		hints,
		cap,
		eval.MakeEstimatorHook(baseRels),
		plan.Plan,
	)
	if len(planErrs) > 0 {
		t.Fatalf("plan: %v", planErrs)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	rs, err := eval.Evaluate(
		ctx,
		execPlan,
		baseRels,
		eval.WithMaxBindingsPerRule(cap),
		eval.WithSizeHints(hints),
	)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}

	// Diagnostic: dump per-relation row counts so a failure leaves a clear
	// breadcrumb about which extraction-side relation is the problem.
	for _, rel := range []string{"ObjectLiteralField", "ObjectLiteralSpread"} {
		if r := factDB.Relation(rel); r != nil {
			t.Logf("%s tuples=%d", rel, r.Tuples())
		}
	}

	if len(rs.Rows) == 0 {
		t.Fatalf("expected at least three context-alias setStateUpdater matches (Indirect/Spread/Computed), got 0 rows")
	}

	// Per-file hit accounting.
	hits := map[string]int{
		"IndirectValue.tsx":        0,
		"SpreadValue.tsx":          0,
		"ComputedKey.tsx":          0,
		"Negative_NonConstKey.tsx": 0,
	}
	for _, row := range rs.Rows {
		for _, cell := range row {
			s := fmt.Sprintf("%v", cell)
			for fname := range hits {
				if strings.Contains(s, fname) {
					hits[fname]++
					break
				}
			}
		}
	}

	for _, fname := range []string{"IndirectValue.tsx", "SpreadValue.tsx", "ComputedKey.tsx"} {
		if hits[fname] == 0 {
			t.Errorf("expected at least one match in %s, got 0 (rows=%v)", fname, rs.Rows)
		}
	}
	if hits["Negative_NonConstKey.tsx"] > 0 {
		t.Errorf("non-const-key negative file matched (over-approximation bug); hits=%d rows=%v",
			hits["Negative_NonConstKey.tsx"], rs.Rows)
	}
	t.Logf("R3 matches: total=%d Indirect=%d Spread=%d Computed=%d Negative=%d",
		len(rs.Rows), hits["IndirectValue.tsx"], hits["SpreadValue.tsx"],
		hits["ComputedKey.tsx"], hits["Negative_NonConstKey.tsx"])
}

// TestR3_LinkPredicates exercises the new round-3 helpers individually so
// regressions localise quickly. Each entry is an inline QL query against a
// single bridge predicate; mustHaveN is a floor-only assertion (>=) since the
// exact tuple counts shift slightly with fixture refactoring.
func TestR3_LinkPredicates(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping extraction-heavy debug test in short mode")
	}
	factDB := extractProject(t, "testdata/projects/react-usestate-context-alias-r3")

	bridgeFiles := bridge.LoadBridge()
	importLoader := makeBridgeImportLoader(bridgeFiles)

	// PR3 / Phase D PR6: tsq::valueflow imported because
	// `mayResolveToObjectExpr` (and `contextProviderFieldR3VarIndirect*`
	// which calls it) depends on `mayResolveToRec` from tsq_valueflow.qll.
	// Without this import the union silently returns only the surviving
	// non-valueflow branches.
	common := "import tsq::react\nimport tsq::valueflow\nimport tsq::base\nimport tsq::expressions\nimport tsq::functions\nimport tsq::calls\n"

	type qcase struct {
		name      string
		src       string
		mustHaveN int  // floor (>=) when exact=false; exact equality when exact=true
		exact     bool // pin to == (downstream consumers); upstream relations stay floor-only
	}
	queries := []qcase{
		// 6 useState setters across 3 positive fixtures (setIA, setIB, setSA, setSB, setCA, setCB)
		// + 1 in negative (setNN) = 7
		{"useStateSetterSym", common + "from int s where useStateSetterSym(s) select s", 7, false},
		{"isObjectLiteralExpr", common + "from int o where isObjectLiteralExpr(o) select o", 4, false},
		// Phase C PR6: mayResolveToObjectExpr is now the
		// {mayResolveToRec ∪ JsxExpression-wrapper-tolerant} composition.
		// The closure picks up 6 additional (v, o) pairs on r3 vs Phase
		// A's shape-enumeration (4 Direct + 6 VarD1 + 3 JsxWrapper = 13).
		//
		// CORRECTION (post-review, vs earlier "transitive lfsVarInit"
		// framing): the r3 fixtures have ZERO VarD2 chains — each positive
		// fixture does one `const X = { ... }` hop at most. The real
		// mechanism behind the +6 is the FORWARD `lfs*` edges folded into
		// the recursive closure:
		//
		//   - `lfsObjectLiteralStore(from, to)` :- ObjectLiteralField(to, _, from)
		//       — a field-VALUE expression gains an edge INTO the enclosing
		//       object literal. Read as "v is a field-of o."
		//   - `lfsSpreadElement(from, to)`     :- ObjectLiteralSpread(to, from)
		//       — a spread source identifier gains an edge into the object
		//       literal it's spread into. Read as "v is spread-into o."
		//   - `lfsFieldRead(from, to)`         :- FieldRead(to, baseSym, _) ∧
		//                                         ExprMayRef(from, baseSym)
		//       — the base carrier gains an edge into the `obj.f` read node.
		//
		// Combined with `ExprValueSource` seeding every ObjectLiteral with
		// `(o, o)` plus `lfsVarInit` for the `const X = {...}` hop, the
		// closure admits pairs where `v` is a **contained-by / stored-into**
		// relation w.r.t. `o` rather than the Phase A "evaluates-to"
		// relation. That is a semantic direction shift, not just a depth
		// extension.
		//
		// LATENT CONCERN (not a bug today, but filed as follow-up):
		// `mayResolveToObjectExpr` therefore computes a strict SUPERSET
		// of the Phase A "evaluates-to object literal" contract. All
		// current callers in the bridge compose it with
		// `Contains(provider, valueAttrExpr)` / JsxAttribute binding that
		// pins `valueExpr` to a Provider `value={...}` attribute position,
		// so the overreach cannot materialise through the existing caller
		// discipline. A future consumer that binds `valueExpr` less
		// tightly (e.g. any arbitrary expression, not a JsxAttribute
		// value) would see the forward-edge noise. Tightening tracked as
		// a follow-up on Gjdoalfnrxu/tsq (see PR6 wiki section).
		//
		// Pinned at 19 — silent growth would still mask over-approximation
		// regressions, just against the new (post-PR6) surface area.
		{"mayResolveToObjectExpr", common + "from int v, int o where mayResolveToObjectExpr(v, o) select v, o", 19, true},
		{"resolveToObjectExprWrapped", common + "from int v, int o where resolveToObjectExprWrapped(v, o) select v, o", 1, false},
		// Phase D PR6 (20 Apr 2026): `resolveToObjectExpr` predicate deleted
		// from tsq_react.qll. It had become a trivial one-line alias for
		// `mayResolveToObjectExpr` after Phase C PR6. The exact-pinned
		// `mayResolveToObjectExpr` row above (19 rows) is the canonical
		// regression guard for the union; the old row here was a
		// floor-of-2 that the 19-row union trivially subsumed.
		// objectLiteralFieldThroughSpread: Indirect(2) + Spread(2 own + 1 spread) + Computed(2) + Negative(0) = 7
		{"objectLiteralFieldOwn", common + "from int o, string f, int v where objectLiteralFieldOwn(o, f, v) select o, f, v", 5, false},
		{"objectLiteralFieldSpreadD1", common + "from int o, string f, int v where objectLiteralFieldSpreadD1(o, f, v) select o, f, v", 1, false},
		{"objectLiteralFieldThroughSpread", common + "from int o, string f, int v where objectLiteralFieldThroughSpread(o, f, v) select o, f, v", 6, false},
		// contextProviderField: 6 setter fields visible across the 3 positive providers.
		{"contextProviderFieldR2", common + "from int s, string f, int v where contextProviderFieldR2(s, f, v) select s, f, v", 0, false},
		{"contextProviderFieldR3DirectOwn", common + "from int s, string f, int v where contextProviderFieldR3DirectOwn(s, f, v) select s, f, v", 1, false},
		{"contextProviderFieldR3DirectSpreadD1", common + "from int s, string f, int v where contextProviderFieldR3DirectSpreadD1(s, f, v) select s, f, v", 1, false},
		{"contextProviderFieldR3DirectSpread", common + "from int s, string f, int v where contextProviderFieldR3DirectSpread(s, f, v) select s, f, v", 2, false},
		{"contextProviderFieldR3VarIndirect", common + "from int s, string f, int v where contextProviderFieldR3VarIndirect(s, f, v) select s, f, v", 4, false},
		// PR3 amendment — downstream consumer relation, pinned to exact equality.
		// Silent growth here would mask precision regressions in the upstream
		// resolve* helpers feeding it. Update only when fixture changes intentionally.
		{"contextProviderField", common + "from int s, string f, int v where contextProviderField(s, f, v) select s, f, v", 6, true},
		// useStateSetterContextAliasCall: at least one outer + inner per positive = 6.
		{"contextSym", common + "from int s where contextSym(s) select s", 4, false},
		{"contextDestructureBinding", common + "from int s, string f, int p where contextDestructureBinding(s, f, p) select s, f, p", 6, false},
		{"contextSetterAliasStepR2", common + "from int v, int p where contextSetterAliasStepR2(v, p) select v, p", 0, false},
		{"contextSetterAliasStepR3DirectSpread", common + "from int v, int p where contextSetterAliasStepR3DirectSpread(v, p) select v, p", 1, false},
		{"contextSetterAliasStepR3VarIndirect", common + "from int v, int p where contextSetterAliasStepR3VarIndirect(v, p) select v, p", 4, false},
		{"contextSymLink", common + "from int p, int c where contextSymLink(p, c) select p, c", 4, false},
		{"contextSetterAliasStep", common + "from int v, int p where contextSetterAliasStep(v, p) select v, p", 6, false},
		// PR3 amendment — downstream consumer pinned to exact equality (see contextProviderField note).
		{"useStateSetterAliasV2", common + "from int s where useStateSetterAliasV2(s) select s", 13, true},
		{"isContextAliasedSetterSym", common + "from int s where isContextAliasedSetterSym(s) select s", 6, false},
		// PR3 amendment — downstream consumer pinned to exact equality (see contextProviderField note).
		{"useStateSetterContextAliasCall", common + "from int c where useStateSetterContextAliasCall(c) select c", 6, true},
	}
	hints := make(map[string]int, len(schema.Registry))
	for _, def := range schema.Registry {
		hints[def.Name] = factDB.Relation(def.Name).Tuples()
	}
	const cap = 200_000
	baseRels, err := eval.LoadBaseRelations(factDB)
	if err != nil {
		t.Fatalf("load base relations: %v", err)
	}
	for _, q := range queries {
		p := parse.NewParser(q.src, q.name+".ql")
		mod, err := p.Parse()
		if err != nil {
			t.Errorf("[%s] parse err: %v", q.name, err)
			continue
		}
		resolved, err := resolve.Resolve(mod, importLoader)
		if err != nil {
			t.Errorf("[%s] resolve err: %v", q.name, err)
			continue
		}
		if len(resolved.Errors) > 0 {
			t.Errorf("[%s] resolve errors: %v", q.name, resolved.Errors)
			continue
		}
		prog, dsErrors := desugar.Desugar(resolved)
		if len(dsErrors) > 0 {
			t.Errorf("[%s] desugar errors: %v", q.name, dsErrors)
			continue
		}
		prog = extractrules.MergeSystemRules(prog, extractrules.AllSystemRules())
		execPlan, planErrs := plan.EstimateAndPlan(prog, hints, cap, eval.MakeEstimatorHook(baseRels), plan.Plan)
		if len(planErrs) > 0 {
			t.Errorf("[%s] plan err: %v", q.name, planErrs)
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		rs, err := eval.Evaluate(ctx, execPlan, baseRels, eval.WithMaxBindingsPerRule(cap), eval.WithSizeHints(hints))
		cancel()
		if err != nil {
			t.Errorf("[%s] eval err: %v", q.name, err)
			continue
		}
		if q.exact {
			t.Logf("link %-40s rows=%d (==%d expected)", q.name, len(rs.Rows), q.mustHaveN)
			if len(rs.Rows) != q.mustHaveN {
				t.Errorf("[%s] expected exactly %d rows, got %d (downstream consumer — silent drift not permitted)", q.name, q.mustHaveN, len(rs.Rows))
			}
		} else {
			t.Logf("link %-40s rows=%d (>=%d expected)", q.name, len(rs.Rows), q.mustHaveN)
			if len(rs.Rows) < q.mustHaveN {
				t.Errorf("[%s] expected >=%d rows, got %d", q.name, q.mustHaveN, len(rs.Rows))
			}
		}
	}
}

// TestR3_ObjectLiteralSpread_Extraction asserts that the new ObjectLiteralSpread
// schema relation actually fires on the round-3 fixture's spread literal.
func TestR3_ObjectLiteralSpread_Extraction(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping extraction-heavy integration test in short mode")
	}
	factDB := extractProject(t, "testdata/projects/react-usestate-context-alias-r3")
	rel := factDB.Relation("ObjectLiteralSpread")
	if rel == nil {
		t.Fatalf("ObjectLiteralSpread relation not registered")
	}
	if rel.Tuples() == 0 {
		t.Fatalf("expected at least one ObjectLiteralSpread tuple for SpreadValue.tsx, got 0")
	}
	t.Logf("ObjectLiteralSpread tuples=%d", rel.Tuples())
}
