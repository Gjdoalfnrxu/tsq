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

// TestSetStateUpdaterCallsOtherSetStateThroughContext_Positive is the positive
// regression test for the React-Context variant of the setStateUpdater rule.
//
// Fixture: testdata/projects/react-usestate-context-alias/ contains
//
//	Provider.tsx  — createContext(...) + <Ctx.Provider value={{ setZoom, setPan }}>
//	Hook.tsx      — useViewerActions() { return useContext(Ctx); }
//	Consumer.tsx  — const { setZoom, setPan } = useViewerActions()!;
//	                setZoom(prev => { setPan(...); return ...; });
//	Negative.tsx  — plain useState (direct form only; must NOT be matched
//	                by the context predicate).
//
// The bridge predicate `setStateUpdaterCallsOtherSetStateThroughContext`
// should match the outer `setZoom(...)` call in Consumer.tsx.
func TestSetStateUpdaterCallsOtherSetStateThroughContext_Positive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping extraction-heavy integration test in short mode")
	}

	factDB := extractProject(t, "testdata/projects/react-usestate-context-alias")

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

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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

	// Diagnostic: print ObjectLiteralField rows so a failure leaves a
	// breadcrumb about whether the schema-side plumbing is the problem.
	if olf := factDB.Relation("ObjectLiteralField"); olf != nil {
		t.Logf("ObjectLiteralField tuples=%d", olf.Tuples())
	}

	if len(rs.Rows) == 0 {
		t.Fatalf("expected at least one context-alias setStateUpdater match on the fixture, got 0 rows. Bridge predicate is not seeing the context chain. ObjectLiteralField tuples=%d",
			factDB.Relation("ObjectLiteralField").Tuples())
	}

	// Positive: at least one match should be in Consumer.tsx. Negatives live in
	// Negative.tsx (direct-form matches there must NOT surface via this query).
	var consumerHits, negativeHits int
	for _, row := range rs.Rows {
		for _, cell := range row {
			s := fmt.Sprintf("%v", cell)
			if strings.Contains(s, "Consumer.tsx") {
				consumerHits++
				break
			}
			if strings.Contains(s, "Negative.tsx") {
				negativeHits++
				break
			}
		}
	}
	if consumerHits == 0 {
		t.Fatalf("expected Consumer.tsx in result rows, got: %v", rs.Rows)
	}
	if negativeHits > 0 {
		t.Fatalf("context predicate matched Negative.tsx (direct-only pattern); false-positive count=%d rows=%v", negativeHits, rs.Rows)
	}
	t.Logf("setStateUpdaterCallsOtherSetStateThroughContext matched %d rows (Consumer.tsx hits=%d, Negative.tsx hits=%d)",
		len(rs.Rows), consumerHits, negativeHits)
}

// TestContextChain_LinkPredicates exercises each link of the context chain
// individually so end-to-end test failures localise quickly: if the positive
// test fails, run this and look for the first zero-row link.
func TestContextChain_LinkPredicates(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping extraction-heavy debug test in short mode")
	}
	factDB := extractProject(t, "testdata/projects/react-usestate-context-alias")

	bridgeFiles := bridge.LoadBridge()
	importLoader := makeBridgeImportLoader(bridgeFiles)

	common := "import tsq::react\nimport tsq::base\nimport tsq::expressions\nimport tsq::functions\nimport tsq::calls\n"

	queries := []struct {
		name      string
		src       string
		mustHaveN int // 0 = just report count, no assertion
	}{
		{"contextSym", common + "from int s where contextSym(s) select s", 1},
		{"contextProviderValueObject", common + "from int s, int o where contextProviderValueObject(s, o) select s, o", 1},
		{"contextProviderField", common + "from int s, string f, int v where contextProviderField(s, f, v) select s, f, v", 2},
		{"useContextCall", common + "from int c, int s where useContextCall(c, s) select c, s", 1},
		{"hookIndirection", common + "from int fn, int s where hookIndirection(fn, s) select fn, s", 1},
		{"useContextCallSiteResolvesContext", common + "from int c, int s where useContextCallSiteResolvesContext(c, s) select c, s", 2},
		{"contextDestructureBinding", common + "from int s, string f, int p where contextDestructureBinding(s, f, p) select s, f, p", 2},
		{"contextSetterAliasStep", common + "from int v, int p where contextSetterAliasStep(v, p) select v, p", 2},
		{"useStateSetterAliasV2", common + "from int s where useStateSetterAliasV2(s) select s", 6},
		{"useStateSetterContextAliasCall", common + "from int c where useStateSetterContextAliasCall(c) select c", 2},
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
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		rs, err := eval.Evaluate(ctx, execPlan, baseRels, eval.WithMaxBindingsPerRule(cap), eval.WithSizeHints(hints))
		cancel()
		if err != nil {
			t.Errorf("[%s] eval err: %v", q.name, err)
			continue
		}
		t.Logf("link %-40s rows=%d", q.name, len(rs.Rows))
		if q.mustHaveN > 0 && len(rs.Rows) != q.mustHaveN {
			t.Errorf("[%s] expected %d rows, got %d (rows=%v)", q.name, q.mustHaveN, len(rs.Rows), rs.Rows)
		}
	}
}

// ObjectLiteralField rows for `value={{ setZoom, setPan }}` — the new
// schema plumbing introduced for the context-alias round-2. If this fails,
// the bridge predicate has zero chance of firing.
func TestObjectLiteralField_Extraction(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping extraction-heavy integration test in short mode")
	}
	factDB := extractProject(t, "testdata/projects/react-usestate-context-alias")
	olf := factDB.Relation("ObjectLiteralField")
	if olf == nil {
		t.Fatalf("ObjectLiteralField relation not registered")
	}
	if olf.Tuples() == 0 {
		t.Fatalf("expected ObjectLiteralField tuples for the Provider's value={{ setZoom, setPan }} object literal, got 0")
	}
	// The Provider's value={{ setZoom, setPan }} contributes 2 tuples; total
	// across the fixture is stable but we only assert the floor.
	if olf.Tuples() < 2 {
		t.Fatalf("expected at least 2 ObjectLiteralField tuples (setZoom + setPan shorthand fields), got %d", olf.Tuples())
	}
	t.Logf("ObjectLiteralField tuples=%d (>=2 expected)", olf.Tuples())
}
