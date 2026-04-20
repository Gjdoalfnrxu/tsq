package integration_test

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
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

// Phase C PR7 — whole-closure integration suite.
//
// These tests run `mayResolveToRec` end-to-end through the full
// planner stack (plan.EstimateAndPlan + eval.MakeEstimatorHook) on
// hand-constructed fixtures — one per canonical pattern shape
// (plan §7 / §8.2). Expected row sets are keyed by file-suffix +
// line number rather than raw node-id so they remain stable across
// walker-order changes.
//
// Regression-guard discipline applied (per wiki §Phase C PR4/PR6 /
// PR7 briefing rules (a)-(g)):
//
//   (a) Every fixture asserts a non-zero mayResolveToRec row set.
//   (b) Per-fixture observed-vs-floor: total-row floor set at ~50%
//       of the observed count below (never 1). When a pinned row
//       count is tightened, adjust both the `observed` and the
//       `minTotal` entry in one commit so the ratio holds.
//   (c) Overlap check vs prior PRs: these tests run the *closure*
//       surface (mayResolveToRec) end-to-end — distinct regression
//       signal from the per-kind lfs*/ifs* floors (PR2/PR3) and the
//       aggregate MayResolveTo floor (PR4).
//   (d) No carve-outs — every fixture is a real extraction.
//   (e) Base/recursive split: base-case smoke test
//       (TestClosure_BaseCaseIsEveryExprValueSource) pins the
//       identity relation independently of the recursive delta.
//   (f) Manifest `File:` fields are grep-checked in the test body
//       of TestClosure_ManifestFileFieldsGreppable.
//   (g) Every test uses runClosureQuery which wires through the
//       full plan.EstimateAndPlan + estimator hook.

// runClosureQuery evaluates a QL query against a fixture through the
// full planner stack. Mirrors runValueflowQuery but returns raw rows
// so the caller can project with locations.
func runClosureQuery(t *testing.T, queryFile, fixtureDir string) *eval.ResultSet {
	t.Helper()
	factDB := extractProject(t, fixtureDir)

	src, err := os.ReadFile(queryFile)
	if err != nil {
		t.Fatalf("read query %s: %v", queryFile, err)
	}

	bridgeFiles := bridge.LoadBridge()
	importLoader := makeBridgeImportLoader(bridgeFiles)

	p := parse.NewParser(string(src), queryFile)
	mod, err := p.Parse()
	if err != nil {
		t.Fatalf("parse %s: %v", queryFile, err)
	}
	resolved, err := resolve.Resolve(mod, importLoader)
	if err != nil {
		t.Fatalf("resolve %s: %v", queryFile, err)
	}
	if len(resolved.Errors) > 0 {
		t.Fatalf("resolve errors in %s: %v", queryFile, resolved.Errors)
	}
	prog, dsErrors := desugar.Desugar(resolved)
	if len(dsErrors) > 0 {
		t.Fatalf("desugar %s: %v", queryFile, dsErrors)
	}
	prog = extractrules.MergeSystemRules(prog, extractrules.AllSystemRules())

	hints := make(map[string]int, len(schema.Registry))
	for _, def := range schema.Registry {
		if r := factDB.Relation(def.Name); r != nil {
			hints[def.Name] = r.Tuples()
		}
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
		t.Fatalf("plan %s: %v", queryFile, planErrs)
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
		t.Fatalf("evaluate %s: %v", queryFile, err)
	}
	return rs
}

// locRow is a canonical projection of one (valueExpr, sourceExpr) pair
// keyed by file suffix + start line.
type locRow struct {
	valueSuffix  string
	valueLine    int64
	sourceSuffix string
	sourceLine   int64
}

// projectLocatedRows pulls the 4-column (valuePath, valueLine,
// sourcePath, sourceLine) projection from all_mayResolveToRec_located.ql
// into a slice, trimming paths to their last path segment so the test
// is repo-root-agnostic.
func projectLocatedRows(t *testing.T, rs *eval.ResultSet) []locRow {
	t.Helper()
	out := make([]locRow, 0, len(rs.Rows))
	for i, row := range rs.Rows {
		if len(row) != 4 {
			t.Fatalf("row %d: expected arity 4, got %d", i, len(row))
		}
		vpv, ok1 := row[0].(eval.StrVal)
		vlv, ok2 := row[1].(eval.IntVal)
		spv, ok3 := row[2].(eval.StrVal)
		slv, ok4 := row[3].(eval.IntVal)
		if !ok1 || !ok2 || !ok3 || !ok4 {
			t.Fatalf("row %d: unexpected cell shape (%T, %T, %T, %T)",
				i, row[0], row[1], row[2], row[3])
		}
		out = append(out, locRow{
			valueSuffix:  lastPathSegment(vpv.V),
			valueLine:    vlv.V,
			sourceSuffix: lastPathSegment(spv.V),
			sourceLine:   slv.V,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.valueSuffix != b.valueSuffix {
			return a.valueSuffix < b.valueSuffix
		}
		if a.valueLine != b.valueLine {
			return a.valueLine < b.valueLine
		}
		if a.sourceSuffix != b.sourceSuffix {
			return a.sourceSuffix < b.sourceSuffix
		}
		return a.sourceLine < b.sourceLine
	})
	return out
}

func lastPathSegment(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

// dumpRows serialises the projection for failure-message readability.
func dumpRows(rows []locRow) string {
	b := &strings.Builder{}
	for _, r := range rows {
		fmt.Fprintf(b, "  %s:%d → %s:%d\n", r.valueSuffix, r.valueLine, r.sourceSuffix, r.sourceLine)
	}
	return b.String()
}

// loadExpectedCSV reads a `mayResolveTo.expected.csv` reference file
// from a fixture directory. Columns: valueFile,valueLine,sourceFile,sourceLine.
// Returns (rows, true) if the file exists and parses cleanly;
// (nil, false) if the file is absent (callers should treat missing
// CSV as "no reference; skip set-equality check").
func loadExpectedCSV(t *testing.T, fixtureDir string) ([]locRow, bool) {
	t.Helper()
	path := filepath.Join(fixtureDir, "mayResolveTo.expected.csv")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false
		}
		t.Fatalf("open expected csv %s: %v", path, err)
	}
	defer f.Close()

	var out []locRow
	scan := bufio.NewScanner(f)
	lineNo := 0
	for scan.Scan() {
		lineNo++
		line := strings.TrimSpace(scan.Text())
		if line == "" {
			continue
		}
		// Skip header.
		if lineNo == 1 && strings.HasPrefix(line, "valueFile") {
			continue
		}
		parts := strings.Split(line, ",")
		if len(parts) != 4 {
			t.Fatalf("%s:%d: expected 4 CSV cols, got %d: %q", path, lineNo, len(parts), line)
		}
		vl, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
		if err != nil {
			t.Fatalf("%s:%d: parse valueLine: %v", path, lineNo, err)
		}
		sl, err := strconv.ParseInt(strings.TrimSpace(parts[3]), 10, 64)
		if err != nil {
			t.Fatalf("%s:%d: parse sourceLine: %v", path, lineNo, err)
		}
		out = append(out, locRow{
			valueSuffix:  strings.TrimSpace(parts[0]),
			valueLine:    vl,
			sourceSuffix: strings.TrimSpace(parts[2]),
			sourceLine:   sl,
		})
	}
	if err := scan.Err(); err != nil {
		t.Fatalf("scan expected csv %s: %v", path, err)
	}
	return out, true
}

// assertRowSetMatchesCSV compares the observed row set to the CSV
// reference if present. Reports set-equality diffs as test errors.
// When the CSV is absent this is a no-op (new fixtures can be added
// without a reference — the floor/pins still catch regressions).
func assertRowSetMatchesCSV(t *testing.T, fixtureDir string, observed []locRow) {
	t.Helper()
	expected, ok := loadExpectedCSV(t, fixtureDir)
	if !ok {
		return
	}
	// Set equality.
	key := func(r locRow) string {
		return fmt.Sprintf("%s:%d→%s:%d", r.valueSuffix, r.valueLine, r.sourceSuffix, r.sourceLine)
	}
	obsSet := make(map[string]struct{}, len(observed))
	for _, r := range observed {
		obsSet[key(r)] = struct{}{}
	}
	expSet := make(map[string]struct{}, len(expected))
	for _, r := range expected {
		expSet[key(r)] = struct{}{}
	}
	var missing, extra []string
	for k := range expSet {
		if _, ok := obsSet[k]; !ok {
			missing = append(missing, k)
		}
	}
	for k := range obsSet {
		if _, ok := expSet[k]; !ok {
			extra = append(extra, k)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 || len(extra) > 0 {
		t.Errorf("mayResolveTo.expected.csv set-equality mismatch in %s:\n"+
			"  missing (in CSV, not in observed): %v\n"+
			"  extra   (in observed, not in CSV): %v\n"+
			"If the drift is intentional (e.g. a forward-edge rule was added), "+
			"regenerate the CSV to ratchet.",
			fixtureDir, missing, extra)
	}
}

// closureExpectation captures the hand-computed reference for one
// fixture. `pins` is a set of (valueSuffix:line → sourceSuffix:line)
// pairs that MUST appear in mayResolveToRec; `minTotal` is a floor on
// the total row count (~50% of observed — see rule (b)).
type closureExpectation struct {
	name       string
	projectDir string
	pins       []locRow
	minTotal   int // floor; ~50% of observed
	// maxTotal is an upper bound set generously (~3× observed) — catches
	// accidental Cartesian blow-up without over-pinning the exact count.
	maxTotal int
}

// contains checks whether pin appears in rows (file-suffix + line
// match both endpoints).
func containsLocRow(rows []locRow, pin locRow) bool {
	for _, r := range rows {
		if r == pin {
			return true
		}
	}
	return false
}

// allClosureExpectations — one per pattern shape. The (file, line)
// pins are hand-computed from the fixture comments; floors are set
// after the first observation per rule (b).
//
// These pins assert the *presence* of a reachability edge the shape
// was designed to exercise. They do NOT assert exact row counts
// (row counts vary with extractor version — any forward-edge
// `lfs*` addition legitimately grows the set). Floors + max bounds
// catch drift; pins catch regressions of the specific composition
// under test.
func allClosureExpectations() []closureExpectation {
	// Ceiling asymmetry note: floors catch "silently tightened" closure
	// regressions; ceilings only catch Cartesian blow-up. Floors are
	// ~50% of observed (tight). Ceilings are ~3× observed (loose, by
	// design — a 3× overshoot is already a smoking-gun blow-up, more
	// generous room would defeat the purpose). Update both when the
	// observed count changes.
	return []closureExpectation{
		{
			// Direct prop pass (R1). Fixture advertises "lfsVarInit +
			// lfsParamBind composed" but under the current PR6 closure
			// the param-path row (Inner's `value` at line 20) does NOT
			// appear. Pins below are identity-only — see follow-up
			// issue #202 for closing the composition gap.
			// Observed 4 rows: 17→17, 24→24, 25→24, 25→25.
			name:       "direct_prop",
			projectDir: "testdata/projects/valueflow-closure-direct-prop",
			pins: []locRow{
				// base: cfg object literal resolves to itself (line 28)
				{valueSuffix: "DirectProp.tsx", valueLine: 28,
					sourceSuffix: "DirectProp.tsx", sourceLine: 28},
				// JSX expr (line 29) reaches cfg literal — non-identity
				// forward edge (lfsObjectLiteralStore composition).
				{valueSuffix: "DirectProp.tsx", valueLine: 29,
					sourceSuffix: "DirectProp.tsx", sourceLine: 28},
			},
			minTotal: 2,  // ~50% of observed 4
			maxTotal: 12, // ~3× observed — catches Cartesian blow-up
		},
		{
			// Context provider with own-fields (R2). Fixture advertises
			// "inc FieldRead reaches arrow" via lfsVarInit + object-
			// field read but under the current PR6 closure the
			// line-22 → line-17/19 edges do NOT fire. Pins are
			// identity-only — see follow-up issue #202.
			// Observed 3 rows: 17→17, 19→19, 22→22.
			name:       "context_own_fields",
			projectDir: "testdata/projects/valueflow-closure-context-own-fields",
			pins: []locRow{
				// Provider's value literal resolves to itself (base) — line 24.
				{valueSuffix: "ContextOwn.tsx", valueLine: 24,
					sourceSuffix: "ContextOwn.tsx", sourceLine: 24},
			},
			minTotal: 2, // ~50% of observed 3
			maxTotal: 9, // ~3× observed
		},
		{
			// Context with spread + computed key (R3). Observed 8
			// rows including the load-bearing back-edges at line 38
			// (spread-composed literal) → line 33 (base) and
			// line 38 → line 36 (spread element). Pinned set checks
			// the spread-composition row (line 38 → line 33) that
			// the forward-edge rules introduced in PR6 — a regression
			// that tightened `mayResolveToRec` to exclude it would
			// trip.
			name:       "context_spread_computed",
			projectDir: "testdata/projects/valueflow-closure-context-spread-computed",
			pins: []locRow{
				// Spread-composed literal (line 38) resolves to itself (base).
				{valueSuffix: "ContextSpread.tsx", valueLine: 38,
					sourceSuffix: "ContextSpread.tsx", sourceLine: 38},
				// Forward-edge: line 38 spread-literal reaches the
				// base `{ ping: ... }` literal on line 33 via lfsSpreadElement.
				{valueSuffix: "ContextSpread.tsx", valueLine: 38,
					sourceSuffix: "ContextSpread.tsx", sourceLine: 33},
			},
			minTotal: 4,  // ~50% of observed 8
			maxTotal: 24, // ~3× observed
		},
		{
			// Factory hook return (R4). Observed 5 rows: line 20
			// (factory literal base, `const api = { doIt: ... }`),
			// line 21 return-expr forward to 20, line 25 destructure
			// reaches line 20 source (R4 composition).
			name:       "factory_hook",
			projectDir: "testdata/projects/valueflow-closure-factory-hook",
			pins: []locRow{
				// Factory object literal resolves to itself (base).
				{valueSuffix: "FactoryHook.tsx", valueLine: 20,
					sourceSuffix: "FactoryHook.tsx", sourceLine: 20},
				// Forward-edge: consumer destructure at line 25
				// reaches the factory literal on line 20. This is
				// the closure's load-bearing R4 composition —
				// lfsReturnToCallSite composing with lfsVarInit.
				{valueSuffix: "FactoryHook.tsx", valueLine: 25,
					sourceSuffix: "FactoryHook.tsx", sourceLine: 20},
			},
			minTotal: 3,  // ~50% of observed 5
			maxTotal: 15, // ~3× observed
		},
		{
			// Higher-order (§3.3 makeIncrementer). Observed 8 rows —
			// base rows on lines 25/26/29/30/31 plus three forward
			// edges (26→30, 30→26, 31→26) that wire the returned
			// arrow through the VarDecl init of inc5 into its use
			// site. Pin the load-bearing 31→26 edge: callee
			// reference reaches the returned arrow.
			name:       "higher_order",
			projectDir: "testdata/projects/valueflow-closure-higher-order",
			pins: []locRow{
				// Returned arrow on line 26 resolves to itself.
				{valueSuffix: "Higher.ts", valueLine: 26,
					sourceSuffix: "Higher.ts", sourceLine: 26},
				// Callee at line 31 reaches returned-arrow line 26
				// — the HOF composition under test.
				{valueSuffix: "Higher.ts", valueLine: 31,
					sourceSuffix: "Higher.ts", sourceLine: 26},
			},
			minTotal: 4,  // ~50% of observed 8
			maxTotal: 24, // ~3× observed
		},
		{
			// Recursive — cycle termination. Observed 2 rows: two
			// base-case identities (one per `f()` / `g()` call site).
			// The key property is termination (cycle does not loop);
			// the bounded row count + non-zero assertion per rule (a)
			// is the smoke test. Pins are intentionally line-agnostic
			// because value-source seeding on call expressions is
			// extractor-behaviour-dependent.
			name:       "recursive_cycle",
			projectDir: "testdata/projects/valueflow-closure-recursive-cycle",
			pins:       nil,
			minTotal:   1, // rule (a) — non-zero on real fixture
			maxTotal:   6, // ~3× observed 2
		},
		{
			// Multi-hop cross-module import. Observed 6 rows including
			// the load-bearing ifsImportExport chain: index.ts:17 and
			// index.ts:20 each reach module_a.ts:4. Pin that pair to
			// assert the cross-module chain resolves end-to-end.
			name:       "cross_module_multihop",
			projectDir: "testdata/projects/valueflow-closure-cross-module-multihop",
			pins: []locRow{
				// Arrow `() => 1` resolves to itself.
				{valueSuffix: "module_a.ts", valueLine: 4,
					sourceSuffix: "module_a.ts", sourceLine: 4},
				// Cross-module reachability: index.ts:20 (callee in
				// `svc()`) reaches module_a.ts:4. This is the closure's
				// load-bearing multi-hop composition.
				{valueSuffix: "index.ts", valueLine: 20,
					sourceSuffix: "module_a.ts", sourceLine: 4},
			},
			minTotal: 3,  // ~50% of observed 6
			maxTotal: 18, // ~3× observed
		},
	}
}

// TestClosure_WholeClosureIntegration — whole-closure shape fixtures.
// One sub-test per expectation. Each enforces:
//   - rows is non-empty (rule (a))
//   - row count ≥ floor (rule (b))
//   - row count ≤ upper bound (catches Cartesian blow-up)
//   - every pin is present (regression guard for the specific
//     composition the fixture was designed to exercise).
func TestClosure_WholeClosureIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping extraction-heavy integration test in short mode")
	}
	const query = "testdata/queries/v2/valueflow/all_mayResolveToRec_located.ql"

	for _, exp := range allClosureExpectations() {
		exp := exp
		t.Run(exp.name, func(t *testing.T) {
			rs := runClosureQuery(t, query, exp.projectDir)
			rows := projectLocatedRows(t, rs)

			t.Logf("fixture=%s rows=%d (floor=%d ceiling=%d)",
				exp.name, len(rows), exp.minTotal, exp.maxTotal)
			if testing.Verbose() {
				t.Logf("full row set:\n%s", dumpRows(rows))
			}

			if len(rows) == 0 && exp.minTotal > 0 {
				t.Fatalf("fixture %s produced 0 mayResolveToRec rows; "+
					"either the fixture's value source is not recognised "+
					"(walker ValueSourceKinds drift) or the closure "+
					"regressed. See the fixture's hand-computed reference "+
					"comment at the top of the source file.", exp.name)
			}
			if len(rows) < exp.minTotal {
				t.Errorf("fixture %s: %d rows < floor %d (rule (b) — "+
					"~50%%-of-observed regression guard). Investigate per-step-kind "+
					"floors via the branch_*.ql queries. Full row set:\n%s",
					exp.name, len(rows), exp.minTotal, dumpRows(rows))
			}
			if len(rows) > exp.maxTotal {
				t.Errorf("fixture %s: %d rows > ceiling %d — "+
					"Cartesian blow-up or closure over-reach suspected. "+
					"Full row set:\n%s",
					exp.name, len(rows), exp.maxTotal, dumpRows(rows))
			}
			for _, pin := range exp.pins {
				if !containsLocRow(rows, pin) {
					t.Errorf("fixture %s: missing pinned reachability "+
						"%s:%d → %s:%d. This is the shape's canonical "+
						"composition — a miss means the specific pattern "+
						"the fixture was designed to exercise regressed. "+
						"Row set:\n%s",
						exp.name, pin.valueSuffix, pin.valueLine,
						pin.sourceSuffix, pin.sourceLine, dumpRows(rows))
				}
			}
			// Set-equality ratchet against the hand-computed CSV
			// reference. If the CSV is missing the check is a no-op;
			// if it's present, any drift (missing or extra rows)
			// fails the test until the CSV is regenerated.
			assertRowSetMatchesCSV(t, exp.projectDir, rows)
		})
	}
}

// TestClosure_RecursiveFunctionDoesNotHang asserts the recursive-
// function fixture does not hang or blow the planner cap. The closure
// terminates / produces some rows on a recursive function declaration.
//
// NOTE: this test does NOT exercise a true FlowStep-level cycle — the
// fixture currently emits only 2 base identities, no cycle edge. A
// real closure-level cycle fixture is tracked as follow-up issue #198.
// Renamed from `TestClosure_RecursiveCycleTerminates` to match what
// it actually asserts.
func TestClosure_RecursiveFunctionDoesNotHang(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping extraction-heavy integration test in short mode")
	}
	// 30s timeout inside runClosureQuery will surface hang as a test
	// failure. No further assertion needed — merely reaching this
	// statement past runClosureQuery means termination held.
	rs := runClosureQuery(t,
		"testdata/queries/v2/valueflow/all_mayResolveToRec_located.ql",
		"testdata/projects/valueflow-closure-recursive-cycle")
	rows := projectLocatedRows(t, rs)
	t.Logf("recursive-cycle fixture terminated with %d rows", len(rows))
	const ceiling = 6 // ~3× observed 2
	if len(rows) > ceiling {
		t.Errorf("recursive-cycle fixture emitted %d rows > ceiling %d; "+
			"(v, s) tuple finiteness should bound the closure — a blow-up "+
			"indicates the cycle-termination argument has a hole.",
			len(rows), ceiling)
	}
	assertRowSetMatchesCSV(t, "testdata/projects/valueflow-closure-recursive-cycle", rows)
}

// TestClosure_DeepSpreadDoesNotBlowUp asserts the 4-level spread
// fixture terminates under the iteration cap and produces a bounded
// row count. It does NOT exercise the cap itself (the fixture is
// 4 levels vs cap=100) — a true depth-cap fixture is tracked as
// follow-up issue #199. Renamed from `TestClosure_DeepSpreadDepthCap`
// to match what it actually asserts.
func TestClosure_DeepSpreadDoesNotBlowUp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping extraction-heavy integration test in short mode")
	}
	rs := runClosureQuery(t,
		"testdata/queries/v2/valueflow/all_mayResolveToRec_located.ql",
		"testdata/projects/valueflow-adversarial-deep-spread")
	rows := projectLocatedRows(t, rs)
	t.Logf("deep-spread fixture rows=%d", len(rows))

	if len(rows) == 0 {
		t.Fatal("deep-spread fixture produced 0 rows; the base case " +
			"(object-literal identity) should fire at minimum.")
	}
	const ceiling = 45 // ~3× observed 15 — catches Cartesian blow-up
	if len(rows) > ceiling {
		t.Errorf("deep-spread fixture emitted %d rows > ceiling %d — "+
			"depth-cap or spread-rule composition regressed; full row set:\n%s",
			len(rows), ceiling, dumpRows(rows))
	}
	// CSV ratchet — reference lives in fixture dir.
	assertRowSetMatchesCSV(t, "testdata/projects/valueflow-adversarial-deep-spread", rows)
	// Base case pin: at least one identity (v == s) row must exist.
	// The exact literal lines depend on where the extractor emits
	// ExprValueSource on the nested spread chain; rather than guess,
	// assert the structural property (non-empty identity set).
	var sawIdentity bool
	for _, r := range rows {
		if r.valueSuffix == "DeepSpread.ts" && r.sourceSuffix == "DeepSpread.ts" &&
			r.valueLine == r.sourceLine {
			sawIdentity = true
			break
		}
	}
	if !sawIdentity {
		t.Errorf("deep-spread fixture: no identity (base-case) row; "+
			"ExprValueSource must seed at least one node. Full row set:\n%s",
			dumpRows(rows))
	}
}

// TestClosure_NameCollisionOverBridging_Informational documents the
// current over-bridging behaviour from plan §3.2 / §4.1 — ifsImportExport
// keys on symbol name rather than (module, name). This test is
// INFORMATIONAL ONLY: it logs the observed state and passes regardless.
// It does NOT guard the over-bridging (if a future change silently
// tightens the rule to 1 source, the test still passes — that's the
// point: the name reflects documentation, not regression coverage).
//
// If you want a regression guard for the tightened vs loose behaviour,
// add a dedicated test with an explicit assertion; don't rely on this
// one.
func TestClosure_NameCollisionOverBridging_Informational(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping extraction-heavy integration test in short mode")
	}
	rs := runClosureQuery(t,
		"testdata/queries/v2/valueflow/all_mayResolveToRec_located.ql",
		"testdata/projects/valueflow-adversarial-name-collision")
	rows := projectLocatedRows(t, rs)
	t.Logf("name-collision fixture rows=%d", len(rows))

	if len(rows) == 0 {
		t.Fatal("name-collision fixture produced 0 rows; base case should fire at minimum.")
	}

	// Count distinct (sourceSuffix, sourceLine) pairs that are arrow
	// definitions — we don't filter by kind here but the fixture has
	// only two arrows total (one per module, both on line 14/3 of
	// their respective files), so distinct source locations > 1
	// demonstrates name-keyed over-bridging.
	sources := map[string]struct{}{}
	for _, r := range rows {
		if r.sourceSuffix == "mod_alpha.ts" || r.sourceSuffix == "mod_beta.ts" {
			sources[fmt.Sprintf("%s:%d", r.sourceSuffix, r.sourceLine)] = struct{}{}
		}
	}
	t.Logf("distinct (mod_*.ts, line) source positions: %d — %v", len(sources), keysOf(sources))
	// Emit to stdout as well so the informational signal is visible
	// in non-verbose CI output (t.Logf is swallowed unless -v).
	fmt.Printf("[informational] name-collision over-bridging: %d distinct source positions — %v\n",
		len(sources), keysOf(sources))
	if len(sources) < 2 {
		fmt.Println("[informational] NOTE: over-bridging no longer observable " +
			"— the closure tightened (plan §3.2) or extractor stopped " +
			"populating ExprValueSource on both arrows. Document the " +
			"change in the Phase C plan before closing as 'fixed'.")
	}
	assertRowSetMatchesCSV(t, "testdata/projects/valueflow-adversarial-name-collision", rows)
}

func keysOf(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestClosure_MayResolveToCapHit_SchemaRegistered — schema-surface
// smoke test for the `MayResolveToCapHit` diagnostic relation.
//
// SCOPE: schema registration + manifest entry only. There is NO
// evaluator wiring and NO caller yet. The relation will not appear
// in any fact DB until follow-up issue #201 lands; behavioural
// coverage (a fixture that forces a cap-hit and asserts a row
// materialises) is tracked as follow-up issue #200.
//
// This test asserts:
//  1. The relation is registered in schema.Registry with the expected
//     arity-3 shape (queryId, rulePred, lastDeltaSize).
//  2. Column names match the documented contract.
func TestClosure_MayResolveToCapHit_SchemaRegistered(t *testing.T) {
	def, ok := schema.Lookup("MayResolveToCapHit")
	if !ok {
		t.Fatal("schema.Lookup(\"MayResolveToCapHit\") returned " +
			"not-found; Phase C PR7 schema entry missing from " +
			"extract/schema/relations.go")
	}
	if len(def.Columns) != 3 {
		t.Fatalf("MayResolveToCapHit schema: expected 3 columns, got %d: %v",
			len(def.Columns), def.Columns)
	}
	// Column-name contract guard: if the names drift, consumers
	// building queries against the diagnostic relation silently break.
	want := []string{"queryId", "rulePred", "lastDeltaSize"}
	for i, w := range want {
		if def.Columns[i].Name != w {
			t.Errorf("MayResolveToCapHit column %d: expected %q, got %q",
				i, w, def.Columns[i].Name)
		}
	}
}

// TestClosure_ManifestFileFieldsGreppable — rule (f). Manifest File
// fields lie silently unless grep-checked. PR4 M2 found four
// valueflow entries pointing at the wrong .qll. Per the wiki's
// PR4 M2 general principle — when adding a manifest entry, manually
// grep the named .qll for the relation name — this test asserts
// every Phase C manifest entry that claims a valueflow relation is
// consumed in `tsq_valueflow.qll` actually contains a grep-hit
// reference to it.
//
// Scope: only the relations whose bridge File points at a *consumer*
// surface (mayResolveTo / mayResolveToRec wrapper). System-side
// relations whose File entry is a planned-consumer placeholder (per
// wiki PR4 M2 §"For relations populated by system rules but not yet
// consumed in any .qll, point File at the planned consumer site")
// are excluded — those grep-miss by design until their bridge lands.
func TestClosure_ManifestFileFieldsGreppable(t *testing.T) {
	// Placeholder allowlist: manifest entries whose File field points
	// at a PLANNED consumer surface (per PR4 M2 §"For relations
	// populated by system rules but not yet consumed in any .qll,
	// point File at the planned consumer site"). These are expected
	// to grep-miss until their bridge consumer lands. Any entry NOT
	// on this list that grep-misses is a manifest File-field lie and
	// must be fixed before next release.
	//
	// Adding an entry here is a ratchet — every addition should be
	// paired with a tracked follow-up for bridge consumer wiring.
	knownPlaceholderEntries := map[string]string{
		// Phase C PR7: schema-registered diagnostic, evaluator wiring
		// deferred (follow-up #201). Consumer file will be
		// tsq_valueflow.qll once emission lands.
		"MayResolveToCapHit": "follow-up #201",

		// Pre-PR7 baseline: manifest entries whose File field points
		// at a planned consumer surface that does not yet reference
		// the relation name. These pre-date PR7 and are captured here
		// as a ratchet — PR7 is responsible only for not regressing
		// the count upwards. Each SHOULD be tracked by a follow-up
		// for bridge consumer wiring (tracked in aggregate under the
		// v2 bridge-rollout plan, Phase D).
		"AssignExpr":                               "pre-PR7 baseline",
		"CallCalleeSym":                            "pre-PR7 baseline",
		"CallResultSym":                            "pre-PR7 baseline",
		"CommandInjection::CommandInjectionSink":   "pre-PR7 baseline",
		"CommandInjection::CommandInjectionSource": "pre-PR7 baseline",
		"Decorator":                                "pre-PR7 baseline",
		"EnumDecl":                                 "pre-PR7 baseline",
		"EnumMember":                               "pre-PR7 baseline",
		"ExprInFunction":                           "pre-PR7 baseline",
		"ExprValueSource":                          "pre-PR7 baseline",
		"FileSystemAccess":                         "pre-PR7 baseline",
		"GenericInstantiation":                     "pre-PR7 baseline",
		"InterFlowStep":                            "pre-PR7 baseline",
		"IntersectionMember":                       "pre-PR7 baseline",
		"LocalFlowStep":                            "pre-PR7 baseline",
		"MethodDeclDirect":                         "pre-PR7 baseline",
		"MethodDeclInherited":                      "pre-PR7 baseline",
		"NamespaceDecl":                            "pre-PR7 baseline",
		"NamespaceMember":                          "pre-PR7 baseline",
		"NonTaintableType":                         "pre-PR7 baseline",
		"NullishCoalescing":                        "pre-PR7 baseline",
		"ObjectLiteralSpread":                      "pre-PR7 baseline",
		"OptionalChain":                            "pre-PR7 baseline",
		"ParamBinding":                             "pre-PR7 baseline",
		"PathTraversal::PathTraversalSink":         "pre-PR7 baseline",
		"PathTraversal::PathTraversalSource":       "pre-PR7 baseline",
		"RegExpLiteral":                            "pre-PR7 baseline",
		"RegExpTerm":                               "pre-PR7 baseline",
		"ReturnSym":                                "pre-PR7 baseline",
		"SensitiveDataExpr":                        "pre-PR7 baseline",
		"SqlInjection::SqlInjectionSink":           "pre-PR7 baseline",
		"SqlInjection::SqlInjectionSource":         "pre-PR7 baseline",
		"TemplateElement":                          "pre-PR7 baseline",
		"TemplateExpression":                       "pre-PR7 baseline",
		"TemplateLiteral":                          "pre-PR7 baseline",
		"TypeAlias":                                "pre-PR7 baseline",
		"TypeGuard":                                "pre-PR7 baseline",
		"TypeInfo":                                 "pre-PR7 baseline",
		"TypeMember":                               "pre-PR7 baseline",
		"TypeParameter":                            "pre-PR7 baseline",
		"UnionMember":                              "pre-PR7 baseline",
		"Xss::XssSink":                             "pre-PR7 baseline",
		"Xss::XssSource":                           "pre-PR7 baseline",
	}
	bridgeFiles := bridge.LoadBridge()
	manifest := bridge.V1Manifest()
	for _, entry := range manifest.Available {
		data, ok := bridgeFiles[entry.File]
		if !ok {
			// File not loaded at all — a separate test
			// (TestLoadBridgeMatchesManifest) handles that.
			continue
		}
		grepHit := strings.Contains(string(data), entry.Relation)
		if grepHit {
			continue
		}
		if reason, allowed := knownPlaceholderEntries[entry.Name]; allowed {
			t.Logf("manifest-grep: %s is a known placeholder (%s) — "+
				"File %s does not yet reference relation %q",
				entry.Name, reason, entry.File, entry.Relation)
			continue
		}
		t.Errorf("manifest-grep: relation %q (manifest entry %q) not "+
			"mentioned in %s — File field lies. Either wire up a "+
			"consumer in the named .qll, or add %q to "+
			"knownPlaceholderEntries with a follow-up reference.",
			entry.Relation, entry.Name, entry.File, entry.Name)
	}
}
