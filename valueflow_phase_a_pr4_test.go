package integration_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Gjdoalfnrxu/tsq/extract/db"
	extractrules "github.com/Gjdoalfnrxu/tsq/extract/rules"
	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/eval"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

// TestValueflow_MultiHopFixture is the Phase A PR4 multi-hop integration
// test. It loads the `valueflow-multihop` fixture and asserts that several
// distinct mayResolveTo branches fire together within a single fixture and
// that the union row set is the (deduped) union of those branches.
//
// Phase A is non-recursive by construction (plan §6 #1) — there are no
// chains *within* mayResolveTo. "Multi-hop" here means a fixture where
// var-init, param-bind, and obj-field branches all contribute rows to the
// union, demonstrating that downstream consumers (bridge through-context
// query, resolvesToFunctionDirect helper) can observe the joint resolution
// without one branch silently masking another.
//
// Acceptance:
//   - var-init, param-bind, and obj-field each return ≥1 row.
//   - Union row count ≥ max single branch (no #166 disjunction-poisoning).
//   - Union row count ≤ sum of all branches (set semantics holds).
//   - Every per-branch (v, s) pair appears in the union.
func TestValueflow_MultiHopFixture(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping extraction-heavy integration test in short mode")
	}

	fixture := "testdata/projects/valueflow-multihop"

	branches := []struct {
		name string
		path string
	}{
		{"base", "testdata/queries/v2/valueflow/branch_base.ql"},
		{"var_init", "testdata/queries/v2/valueflow/branch_var_init.ql"},
		{"assign", "testdata/queries/v2/valueflow/branch_assign.ql"},
		{"param_bind", "testdata/queries/v2/valueflow/branch_param_bind.ql"},
		{"field_read", "testdata/queries/v2/valueflow/branch_field_read.ql"},
		{"object_field", "testdata/queries/v2/valueflow/branch_object_field.ql"},
		{"jsx_wrap", "testdata/queries/v2/valueflow/branch_jsx_wrapped.ql"},
	}

	branchPairs := make(map[string]bool)
	maxBranch := 0
	sumBranches := 0
	branchCounts := map[string]int{}
	for _, b := range branches {
		rs := runValueflowQuery(t, b.path, fixture)
		n := len(rs.Rows)
		branchCounts[b.name] = n
		sumBranches += n
		if n > maxBranch {
			maxBranch = n
		}
		for _, row := range rs.Rows {
			if len(row) >= 2 {
				branchPairs[fmt.Sprintf("%v|%v", row[0], row[1])] = true
			}
		}
	}

	// The fixture is hand-crafted to exercise ALL seven branches; if any
	// returns 0 the fixture has lost coverage of the cooperating shape and
	// the joint property below is meaningless. Each branch has a dedicated
	// sink-call/use-site in multihop.tsx — see the file header for the
	// branch-to-shape mapping.
	for _, required := range []string{"base", "var_init", "assign", "param_bind", "field_read", "object_field", "jsx_wrap"} {
		if branchCounts[required] == 0 {
			t.Errorf("multi-hop fixture: branch %q returned 0 rows; "+
				"fixture must exercise this branch for the joint property to hold",
				required)
		}
	}

	unionRS := runValueflowQuery(t, "testdata/queries/v2/valueflow/all_mayResolveTo.ql", fixture)
	unionPairs := make(map[string]bool)
	for _, row := range unionRS.Rows {
		if len(row) >= 2 {
			unionPairs[fmt.Sprintf("%v|%v", row[0], row[1])] = true
		}
	}

	t.Logf("multi-hop branch counts: %v ; union=%d (deduped=%d) sum=%d max=%d",
		branchCounts, len(unionRS.Rows), len(unionPairs), sumBranches, maxBranch)

	if len(unionPairs) < maxBranch {
		t.Fatalf("multi-hop union (%d) < max single branch (%d) — disjunction-poisoning suspected",
			len(unionPairs), maxBranch)
	}
	if len(unionPairs) > sumBranches {
		t.Fatalf("multi-hop union (%d) > sum of branches (%d) — set semantics violated",
			len(unionPairs), sumBranches)
	}
	for p := range branchPairs {
		if !unionPairs[p] {
			t.Errorf("multi-hop branch pair %s missing from union — disjunction-poisoning suspect", p)
		}
	}
}

// TestValueflow_BridgeThroughContextStillResolves is the joint-with-bridge
// integration check the brief calls for: run the bridge's
// `find_setstate_updater_calls_other_setstate_through_context.ql` query —
// which after PR3 consumes `mayResolveTo` via the migrated
// `mayResolveToObjectExpr` helper — against the round-3 multi-hop fixture
// and assert the result is non-empty. This proves the Phase A vocabulary
// is wired through to a real downstream consumer end-to-end.
//
// The exact match count is verified by the dedicated
// TestSetStateUpdaterCallsOtherSetStateThroughContext_R3 test (which has
// pinned per-fixture expected line numbers); this test simply asserts the
// pipeline does not silently regress to zero rows under Phase A.
func TestValueflow_BridgeThroughContextStillResolves(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping extraction-heavy integration test in short mode")
	}
	rs := runValueflowQuery(t,
		"testdata/queries/v2/find_setstate_updater_calls_other_setstate_through_context.ql",
		"testdata/projects/react-usestate-context-alias-r3")
	if len(rs.Rows) == 0 {
		t.Fatal("bridge through-context query returned 0 rows on r3 fixture; " +
			"Phase A's mayResolveTo migration has broken the downstream consumer. " +
			"Either mayResolveToObjectExpr no longer resolves Provider value={X} " +
			"or the JsxWrapped branch regressed.")
	}

	// Assert each of the three positive r3 source files produces ≥1 match.
	// A bare `len > 0` hides regressions where one shape collapses to zero
	// while the other two compensate — that's the exact failure mode the
	// JsxWrapper amendment was meant to prevent. Mirrors the per-file
	// accounting in TestSetStateUpdaterCallsOtherSetStateThroughContext_R3.
	expected := map[string]int{
		"IndirectValue.tsx": 0,
		"SpreadValue.tsx":   0,
		"ComputedKey.tsx":   0,
	}
	negative := "Negative_NonConstKey.tsx"
	negCount := 0
	for _, row := range rs.Rows {
		matched := false
		for _, cell := range row {
			s := fmt.Sprintf("%v", cell)
			for fname := range expected {
				if !matched && strings.Contains(s, fname) {
					expected[fname]++
					matched = true
				}
			}
			if strings.Contains(s, negative) {
				negCount++
				matched = true
			}
		}
	}
	for fname, n := range expected {
		if n == 0 {
			t.Errorf("bridge through-context (r3): expected ≥1 match in %s, got 0; "+
				"Phase A vocabulary may have lost coverage of this shape", fname)
		}
	}
	if negCount > 0 {
		t.Errorf("bridge through-context (r3): negative fixture %s matched %d time(s) — over-approximation", negative, negCount)
	}
	t.Logf("bridge through-context (r3 fixture): total=%d Indirect=%d Spread=%d Computed=%d (negative=%d)",
		len(rs.Rows), expected["IndirectValue.tsx"], expected["SpreadValue.tsx"], expected["ComputedKey.tsx"], negCount)
}

// --- Phase A measurement matrix -----------------------------------------
//
// TestValueflow_MeasurementMatrix is gated on TSQ_PHASE_A_MEASURE=1. When
// enabled it sweeps every fixture in testdata/projects, captures row counts
// for the Phase A relations + each mayResolveTo branch + the union, and
// records extraction wall time. Output is a markdown table written to
// $TSQ_PHASE_A_MEASURE_OUT (or stdout if unset).
//
// Used to produce the pre/post comparison table in the PR4 PR body. Run
// once at HEAD, once at the pre-Phase-A baseline (commit 9d08906), diff.

var phaseAFixtures = []string{
	"testdata/projects/valueflow-base",
	"testdata/projects/valueflow-negative",
	"testdata/projects/valueflow-fnref",
	"testdata/projects/valueflow-multihop",
	"testdata/projects/react-component",
	"testdata/projects/react-usestate",
	"testdata/projects/react-usestate-context-alias",
	"testdata/projects/react-usestate-context-alias-r3",
	"testdata/projects/react-usestate-context-alias-r4",
	"testdata/projects/react-usestate-prop-alias",
	"testdata/projects/full-ts-project",
	"testdata/projects/async-patterns",
	"testdata/projects/destructuring",
	"testdata/projects/imports",
	"testdata/projects/simple",
}

// edbMeasureRels are the EDB relations new to Phase A whose row counts we
// capture from the in-memory DB directly. ParamBinding is a derived rule
// (see extract/rules/valueflow.go) and is measured via QL below, NOT here.
var edbMeasureRels = []string{
	"ExprValueSource",
	"AssignExpr",
}

// countDerivedRel evaluates a system-rule predicate (e.g. ParamBinding)
// against the in-memory DB and returns its row count. ParamBinding is
// materialised by extract/rules/valueflow.go, not stored as EDB tuples,
// so the EDB Relation lookup returns 0 even when rows exist. This helper
// runs the system rules against the DB and counts the derived predicate.
//
// Cost note: each call re-loads base relations + plans + evaluates the
// FULL system-rule program, only to project one head. This is fine for
// the per-fixture measurement matrix (called once per fixture under
// TSQ_PHASE_A_MEASURE=1, not in the normal CI path), but DO NOT call it
// in a hot loop. If a future caller needs many derived-rel counts per DB,
// hoist a single eval and project locally rather than calling this N
// times. Currently exactly one call site: ParamBinding in
// TestValueflow_MeasurementMatrix.
func countDerivedRel(t *testing.T, factDB *db.DB, pred string, arity int) int {
	t.Helper()
	baseRels, err := eval.LoadBaseRelations(factDB)
	if err != nil {
		t.Logf("load base relations: %v", err)
		return -1
	}
	terms := make([]datalog.Term, arity)
	for i := range terms {
		terms[i] = datalog.Var{Name: fmt.Sprintf("x%d", i)}
	}
	prog := &datalog.Program{
		Rules: extractrules.AllSystemRules(),
		Query: &datalog.Query{
			Select: terms,
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: pred, Args: terms}},
			},
		},
	}
	ep, errs := plan.Plan(prog, nil)
	if len(errs) > 0 {
		t.Logf("plan %s: %v", pred, errs)
		return -1
	}
	rs, err := eval.Evaluate(context.Background(), ep, baseRels)
	if err != nil {
		t.Logf("eval %s: %v", pred, err)
		return -1
	}
	return len(rs.Rows)
}

var idbMeasureBranches = []struct {
	label string
	query string
}{
	{"mayResolveToBase", "testdata/queries/v2/valueflow/branch_base.ql"},
	{"mayResolveToVarInit", "testdata/queries/v2/valueflow/branch_var_init.ql"},
	{"mayResolveToAssign", "testdata/queries/v2/valueflow/branch_assign.ql"},
	{"mayResolveToParamBind", "testdata/queries/v2/valueflow/branch_param_bind.ql"},
	{"mayResolveToFieldRead", "testdata/queries/v2/valueflow/branch_field_read.ql"},
	{"mayResolveToObjectField", "testdata/queries/v2/valueflow/branch_object_field.ql"},
	{"mayResolveToJsxWrapped", "testdata/queries/v2/valueflow/branch_jsx_wrapped.ql"},
	{"mayResolveTo (union)", "testdata/queries/v2/valueflow/all_mayResolveTo.ql"},
}

func TestValueflow_MeasurementMatrix(t *testing.T) {
	if os.Getenv("TSQ_PHASE_A_MEASURE") != "1" {
		t.Skip("set TSQ_PHASE_A_MEASURE=1 to run the Phase A measurement matrix")
	}

	type row struct {
		fixture       string
		extractMillis int64
		edb           map[string]int
		idb           map[string]int
		idbMillis     map[string]int64
		err           string
	}

	var rows []row
	for _, fix := range phaseAFixtures {
		if _, err := os.Stat(fix); err != nil {
			t.Logf("skipping missing fixture: %s", fix)
			continue
		}
		r := row{
			fixture:   filepath.Base(fix),
			edb:       map[string]int{},
			idb:       map[string]int{},
			idbMillis: map[string]int64{},
		}

		// Extraction wall time.
		start := time.Now()
		var factDB *db.DB
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					r.err = fmt.Sprintf("extract panic: %v", rec)
				}
			}()
			factDB = extractProject(t, fix)
		}()
		r.extractMillis = time.Since(start).Milliseconds()

		if factDB != nil {
			for _, rel := range edbMeasureRels {
				if rr := factDB.Relation(rel); rr != nil {
					r.edb[rel] = rr.Tuples()
				} else {
					r.edb[rel] = 0
				}
			}

			// ParamBinding is derived, not stored — eval the system rule.
			r.edb["ParamBinding"] = countDerivedRel(t, factDB, "ParamBinding", 4)

			for _, b := range idbMeasureBranches {
				bs := time.Now()
				var rs *eval.ResultSet
				func() {
					defer func() {
						if rec := recover(); rec != nil {
							r.idb[b.label] = -1
						}
					}()
					rs = runValueflowQuery(t, b.query, fix)
				}()
				r.idbMillis[b.label] = time.Since(bs).Milliseconds()
				if rs != nil {
					r.idb[b.label] = len(rs.Rows)
				}
			}
		}

		rows = append(rows, r)
	}

	// Render markdown.
	var out strings.Builder
	out.WriteString("# Phase A measurement matrix\n\n")
	out.WriteString(fmt.Sprintf("Generated: %s\n\n", time.Now().UTC().Format(time.RFC3339)))

	out.WriteString("## EDB row counts (Phase A new relations)\n\n")
	out.WriteString("| Fixture | extract (ms) | ExprValueSource | ParamBinding | AssignExpr |\n")
	out.WriteString("|---|---:|---:|---:|---:|\n")
	for _, r := range rows {
		out.WriteString(fmt.Sprintf("| %s | %d | %d | %d | %d |\n",
			r.fixture, r.extractMillis,
			r.edb["ExprValueSource"], r.edb["ParamBinding"], r.edb["AssignExpr"]))
	}

	out.WriteString("\n## IDB row counts (mayResolveTo union + branches)\n\n")
	out.WriteString("| Fixture | base | varInit | assign | paramBind | fieldRead | objField | jsxWrap | union |\n")
	out.WriteString("|---|---:|---:|---:|---:|---:|---:|---:|---:|\n")
	for _, r := range rows {
		out.WriteString(fmt.Sprintf("| %s | %d | %d | %d | %d | %d | %d | %d | %d |\n",
			r.fixture,
			r.idb["mayResolveToBase"],
			r.idb["mayResolveToVarInit"],
			r.idb["mayResolveToAssign"],
			r.idb["mayResolveToParamBind"],
			r.idb["mayResolveToFieldRead"],
			r.idb["mayResolveToObjectField"],
			r.idb["mayResolveToJsxWrapped"],
			r.idb["mayResolveTo (union)"]))
	}

	out.WriteString("\n## IDB query wall time (ms)\n\n")
	out.WriteString("| Fixture | base | varInit | assign | paramBind | fieldRead | objField | jsxWrap | union |\n")
	out.WriteString("|---|---:|---:|---:|---:|---:|---:|---:|---:|\n")
	for _, r := range rows {
		out.WriteString(fmt.Sprintf("| %s | %d | %d | %d | %d | %d | %d | %d | %d |\n",
			r.fixture,
			r.idbMillis["mayResolveToBase"],
			r.idbMillis["mayResolveToVarInit"],
			r.idbMillis["mayResolveToAssign"],
			r.idbMillis["mayResolveToParamBind"],
			r.idbMillis["mayResolveToFieldRead"],
			r.idbMillis["mayResolveToObjectField"],
			r.idbMillis["mayResolveToJsxWrapped"],
			r.idbMillis["mayResolveTo (union)"]))
	}

	output := out.String()
	if dest := os.Getenv("TSQ_PHASE_A_MEASURE_OUT"); dest != "" {
		if err := os.WriteFile(dest, []byte(output), 0o644); err != nil {
			t.Fatalf("write measurement output: %v", err)
		}
		t.Logf("measurement matrix written to %s", dest)
	} else {
		t.Logf("\n%s", output)
	}
}
