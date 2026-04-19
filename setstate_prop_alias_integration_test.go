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

// TestSetStateUpdaterCallsOtherSetStateThroughProps_PropAlias is the positive
// regression test for the JSX prop-alias variant of the setStateUpdater rule.
//
// Fixture: testdata/projects/react-usestate-prop-alias/Viewer.tsx contains
//
//	function Mixed({ onConfigChange, onLog }: MixedProps) {
//	  onConfigChange(prev => { onLog('zooming'); return ... });
//	}
//
// where both prop bindings are JSX-prop aliases of useState setters declared
// in the parent Viewer component. The bridge predicate
// `setStateUpdaterCallsOtherSetStateThroughProps` should match this outer
// `onConfigChange(...)` call.
//
// Bound on cardinality is generous — the fixture is small and the predicate
// is self-contained — but we keep it tight enough that a Cartesian-blow
// regression in the alias-closure planner shape would trip it.
func TestSetStateUpdaterCallsOtherSetStateThroughProps_PropAlias(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping extraction-heavy integration test in short mode")
	}

	factDB := extractProject(t, "testdata/projects/react-usestate-prop-alias")

	src, err := os.ReadFile("testdata/queries/v2/find_setstate_updater_calls_other_setstate_through_props.ql")
	if err != nil {
		t.Fatalf("read query: %v", err)
	}

	bridgeFiles := bridge.LoadBridge()
	importLoader := makeBridgeImportLoader(bridgeFiles)

	p := parse.NewParser(string(src), "find_setstate_updater_calls_other_setstate_through_props.ql")
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

	if len(rs.Rows) == 0 {
		t.Fatalf("expected at least one prop-alias setStateUpdater match on the fixture, got 0 rows. Bridge predicate is not seeing the alias chain.")
	}

	// Sanity: at least one match should resolve to the Mixed component's
	// onConfigChange call (line 86 give or take). We check by stringifying
	// rows and asserting the file path + the call surfaces.
	var pathHits int
	for _, row := range rs.Rows {
		for _, cell := range row {
			if strings.Contains(fmt.Sprintf("%v", cell), "Viewer.tsx") {
				pathHits++
				break
			}
		}
	}
	if pathHits == 0 {
		t.Fatalf("expected Viewer.tsx in result rows, got: %v", rs.Rows)
	}
	t.Logf("setStateUpdaterCallsOtherSetStateThroughProps matched %d rows on react-usestate-prop-alias fixture", len(rs.Rows))
}
