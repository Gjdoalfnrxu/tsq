package integration_test

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"sort"
	"strconv"
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

// TestIssue121_BackwardSetStateQueryParityWithV1 is the load-bearing
// acceptance criterion for issue #121 Phase A.2: the rewritten v2 query
// (`testdata/queries/v2/find_setstate_updater_calls_fn.ql`) must return
// exactly the same row set as the v1 baseline CSV
// (`testdata/expected/react_usestate_find_setstate_updater_calls_fn.csv`,
// 3 rows on the react-usestate fixture).
//
// The v2 query uses `SetStateUpdaterTracker extends BackwardTracker`
// with an overridden `step` predicate (containment instead of dataflow).
// If the magic-set rewrite or the override dispatch returns a different
// row set than the v1 baseline, this test fails with a row-by-row diff.
func TestIssue121_BackwardSetStateQueryParityWithV1(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping extraction-heavy integration test in short mode")
	}

	expected := readExpectedCSV(t, "testdata/expected/react_usestate_find_setstate_updater_calls_fn.csv")

	factDB := extractProject(t, "testdata/projects/react-usestate")
	src, err := os.ReadFile("testdata/queries/v2/find_setstate_updater_calls_fn.ql")
	if err != nil {
		t.Fatalf("read query: %v", err)
	}

	bridgeFiles := bridge.LoadBridge()
	importLoader := makeBridgeImportLoader(bridgeFiles)
	p := parse.NewParser(string(src), "find_setstate_updater_calls_fn.ql")
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

	execPlan, planErrs := plan.Plan(prog, hints)
	if len(planErrs) > 0 {
		t.Fatalf("plan: %v", planErrs)
	}

	const cap = 5_000_000
	baseRels, err := eval.LoadBaseRelations(factDB)
	if err != nil {
		t.Fatalf("load base relations: %v", err)
	}
	updates := eval.EstimateNonRecursiveIDBSizes(prog, baseRels, hints, cap)
	for k, v := range updates {
		hints[k] = v
	}
	for i := range execPlan.Strata {
		plan.RePlanStratum(&execPlan.Strata[i], hints)
	}
	if execPlan.Query != nil {
		plan.RePlanQuery(execPlan.Query, hints)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	rs, err := eval.Evaluate(ctx, execPlan, baseRels,
		eval.WithMaxBindingsPerRule(cap),
		eval.WithSizeHints(hints),
	)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}

	got := normaliseEvalRows(rs.Rows)
	want := normaliseRows(expected)

	t.Logf("v1 baseline rows: %d", len(want))
	t.Logf("v2 query rows:    %d", len(got))

	if len(got) != len(want) {
		t.Errorf("row-count parity FAILED: v2 returned %d rows, v1 baseline has %d rows", len(got), len(want))
	}
	if !rowSetEqual(got, want) {
		t.Errorf("row-set parity FAILED.\nv1 baseline: %v\nv2 result:   %v", want, got)
	}
}

// readExpectedCSV reads the v1 baseline CSV. Skips header row.
func readExpectedCSV(t *testing.T, path string) [][]any {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	r := csv.NewReader(f)
	records, err := r.ReadAll()
	if err != nil {
		t.Fatalf("read csv: %v", err)
	}
	var rows [][]any
	for i, rec := range records {
		if i == 0 {
			// header
			continue
		}
		row := make([]any, len(rec))
		for j, cell := range rec {
			if n, err := strconv.ParseInt(cell, 10, 64); err == nil {
				row[j] = n
			} else {
				row[j] = cell
			}
		}
		rows = append(rows, row)
	}
	return rows
}

// normaliseRows converts a row set to a deterministic sorted []string for
// comparison.
func normaliseRows(rows [][]any) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		parts := make([]string, len(r))
		for i, v := range r {
			parts[i] = fmt.Sprintf("%v", v)
		}
		out = append(out, joinPipe(parts))
	}
	sort.Strings(out)
	return out
}

// normaliseEvalRows formats eval.Tuple rows the same way as normaliseRows.
func normaliseEvalRows(rows [][]eval.Value) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		parts := make([]string, len(r))
		for i, v := range r {
			switch vv := v.(type) {
			case eval.IntVal:
				parts[i] = fmt.Sprintf("%d", vv.V)
			case eval.StrVal:
				parts[i] = vv.V
			default:
				parts[i] = fmt.Sprintf("%v", v)
			}
		}
		out = append(out, joinPipe(parts))
	}
	sort.Strings(out)
	return out
}

func joinPipe(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += "|"
		}
		out += p
	}
	return out
}

func rowSetEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
