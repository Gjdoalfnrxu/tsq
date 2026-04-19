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

	common := "import tsq::react\nimport tsq::base\nimport tsq::expressions\nimport tsq::functions\nimport tsq::calls\n"

	type qcase struct {
		name      string
		src       string
		mustHaveN int // floor (>=)
	}
	queries := []qcase{
		// 6 useState setters across 3 positive fixtures (setIA, setIB, setSA, setSB, setCA, setCB)
		// + 1 in negative (setNN) = 7
		{"useStateSetterSym", common + "from int s where useStateSetterSym(s) select s", 7},
		{"isObjectLiteralExpr", common + "from int o where isObjectLiteralExpr(o) select o", 4},
		{"resolveToObjectExprDirect", common + "from int v, int o where resolveToObjectExprDirect(v, o) select v, o", 4},
		{"resolveToObjectExprWrapped", common + "from int v, int o where resolveToObjectExprWrapped(v, o) select v, o", 1},
		{"resolveToObjectExprVarD1", common + "from int v, int o where resolveToObjectExprVarD1(v, o) select v, o", 2},
		// resolveToObjectExpr should fire for at least Indirect (1), Computed (1).
		{"resolveToObjectExpr", common + "from int v, int o where resolveToObjectExpr(v, o) select v, o", 2},
		// objectLiteralFieldThroughSpread: Indirect(2) + Spread(2 own + 1 spread) + Computed(2) + Negative(0) = 7
		{"objectLiteralFieldOwn", common + "from int o, string f, int v where objectLiteralFieldOwn(o, f, v) select o, f, v", 5},
		{"objectLiteralFieldSpreadD1", common + "from int o, string f, int v where objectLiteralFieldSpreadD1(o, f, v) select o, f, v", 1},
		{"objectLiteralFieldThroughSpread", common + "from int o, string f, int v where objectLiteralFieldThroughSpread(o, f, v) select o, f, v", 6},
		// contextProviderField: 6 setter fields visible across the 3 positive providers.
		{"contextProviderFieldR2", common + "from int s, string f, int v where contextProviderFieldR2(s, f, v) select s, f, v", 0},
		{"contextProviderFieldR3DirectOwn", common + "from int s, string f, int v where contextProviderFieldR3DirectOwn(s, f, v) select s, f, v", 1},
		{"contextProviderFieldR3DirectSpreadD1", common + "from int s, string f, int v where contextProviderFieldR3DirectSpreadD1(s, f, v) select s, f, v", 1},
		{"contextProviderFieldR3DirectSpread", common + "from int s, string f, int v where contextProviderFieldR3DirectSpread(s, f, v) select s, f, v", 2},
		{"contextProviderFieldR3VarIndirect", common + "from int s, string f, int v where contextProviderFieldR3VarIndirect(s, f, v) select s, f, v", 4},
		{"contextProviderField", common + "from int s, string f, int v where contextProviderField(s, f, v) select s, f, v", 6},
		// useStateSetterContextAliasCall: at least one outer + inner per positive = 6.
		{"contextSym", common + "from int s where contextSym(s) select s", 4},
		{"contextDestructureBinding", common + "from int s, string f, int p where contextDestructureBinding(s, f, p) select s, f, p", 6},
		{"contextSetterAliasStepR2", common + "from int v, int p where contextSetterAliasStepR2(v, p) select v, p", 0},
		{"contextSetterAliasStepR3DirectSpread", common + "from int v, int p where contextSetterAliasStepR3DirectSpread(v, p) select v, p", 1},
		{"contextSetterAliasStepR3VarIndirect", common + "from int v, int p where contextSetterAliasStepR3VarIndirect(v, p) select v, p", 4},
		{"contextSymLink", common + "from int p, int c where contextSymLink(p, c) select p, c", 4},
		{"contextSetterAliasStep", common + "from int v, int p where contextSetterAliasStep(v, p) select v, p", 6},
		{"useStateSetterAliasV2", common + "from int s where useStateSetterAliasV2(s) select s", 13},
		{"isContextAliasedSetterSym", common + "from int s where isContextAliasedSetterSym(s) select s", 6},
		{"useStateSetterContextAliasCall", common + "from int c where useStateSetterContextAliasCall(c) select c", 6},
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
		t.Logf("link %-40s rows=%d (>=%d expected)", q.name, len(rs.Rows), q.mustHaveN)
		if len(rs.Rows) < q.mustHaveN {
			t.Errorf("[%s] expected >=%d rows, got %d", q.name, q.mustHaveN, len(rs.Rows))
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
