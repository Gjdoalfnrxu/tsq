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

// TestSetStateUpdaterCallsOtherSetStateThroughContext_R4 is the round-4
// positive regression test. It exercises the factory-hook-return shape:
// the Provider's `value={actions}` resolves to an ObjectLiteral via a
// CallExpression initialiser (the hook), which round-3's
// `resolveToObjectExpr` did not handle.
//
// Two return shapes are exercised:
//   - Actions.tsx + Consumer.tsx: hook returns a VarDecl-bound ObjectLiteral
//     (`const actions = {...}; return actions;`).
//   - DirectReturn.tsx: hook returns the ObjectLiteral directly
//     (`return { setDA, setDB };`).
//
// Negative.tsx must contribute zero rows.
func TestSetStateUpdaterCallsOtherSetStateThroughContext_R4(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping extraction-heavy integration test in short mode")
	}

	factDB := extractProject(t, "testdata/projects/react-usestate-context-alias-r4")

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

	hits := map[string]int{
		"Consumer.tsx":     0,
		"DirectReturn.tsx": 0,
		"Negative.tsx":     0,
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

	if hits["Consumer.tsx"] < 1 {
		t.Errorf("expected >=1 match in Consumer.tsx (FactoryConsumer hook-return-via-VarDecl), got %d (rows=%v)", hits["Consumer.tsx"], rs.Rows)
	}
	if hits["DirectReturn.tsx"] < 1 {
		t.Errorf("expected >=1 match in DirectReturn.tsx (DirectFactoryConsumer hook-return-direct), got %d (rows=%v)", hits["DirectReturn.tsx"], rs.Rows)
	}
	if hits["Negative.tsx"] > 0 {
		t.Errorf("Negative.tsx unexpectedly matched %d times: rows=%v", hits["Negative.tsx"], rs.Rows)
	}
	t.Logf("R4 matches: total=%d Consumer=%d DirectReturn=%d Negative=%d",
		len(rs.Rows), hits["Consumer.tsx"], hits["DirectReturn.tsx"], hits["Negative.tsx"])
}
