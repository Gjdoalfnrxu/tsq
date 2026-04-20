package rules

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Gjdoalfnrxu/tsq/extract"
	"github.com/Gjdoalfnrxu/tsq/extract/db"
	"github.com/Gjdoalfnrxu/tsq/extract/schema"
	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/eval"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

// TestParamBindingBudget enforces the value-flow Phase A row-count budget gate
// from the plan §7.3 — ParamBinding must not exceed 5x CallArg row count on
// representative fixtures.
//
// The gate exists specifically to catch RTA blow-up: ParamBinding's rule
// consumes both CallTarget AND CallTargetRTA (one disjunct each in
// valueflow.go), and CallTargetRTA can produce many candidate fns per call
// site. Without the RTA disjunct the gate would be decorative — empirical
// ratios on these fixtures all sit under 0.2x. With RTA wired in, the gate
// is the design's load-bearing contract for the multiplicative cost.
//
// If the gate ever fires, the design choice is to drop CallTargetRTA from
// the rule and document the precision loss (plan §7.3 / §1.2 carve-outs).
//
// Also surfaces per-fixture row counts for CallTargetRTA, ExprValueSource
// and AssignExpr so PR review can sanity-check the new EDB rels.
func TestParamBindingBudget(t *testing.T) {
	// Pick a small set of representative fixtures from testdata/projects.
	// Skip if the working directory isn't the repo root (in CI with -short the
	// subset still exercises the budget gate; standalone benches use the
	// dedicated cmd or a manual CLI run).
	repoRoot := findRepoRoot(t)
	if repoRoot == "" {
		// The budget gate is the only thing keeping ParamBinding's
		// CallTarget ∪ CallTargetRTA blow-up honest. Silently skipping when
		// run outside the repo root means CI could pass without the gate
		// ever firing — make this a hard failure instead.
		t.Fatal("repo root not found from CWD; budget gate cannot run")
	}

	fixtures := []string{
		"react-component",
		"react-usestate",
		"react-usestate-context-alias",
		"react-usestate-context-alias-r3",
		"react-usestate-prop-alias",
		"async-patterns",
		"destructuring",
		"imports",
		"full-ts-project",
	}

	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(repoRoot, "testdata", "projects", name)
			if _, err := os.Stat(dir); err != nil {
				t.Skipf("fixture not present: %s", dir)
			}
			counts := extractAndCount(t, dir)

			t.Logf("%-40s %s", name+":",
				formatCounts(counts))

			// Budget gate from plan §7.3: ParamBinding ≤ 5x CallArg.
			if counts["CallArg"] > 0 {
				ratio := float64(counts["ParamBinding"]) / float64(counts["CallArg"])
				if ratio > 5.0 {
					t.Errorf("budget gate: ParamBinding (%d) > 5x CallArg (%d) — ratio %.2f",
						counts["ParamBinding"], counts["CallArg"], ratio)
				}
			}

			// Sanity: ExprValueSource should generally be on the order of Node
			// row count divided by ~10–50 (small fraction of all AST nodes are
			// value-source kinds). Loose upper bound: ExprValueSource ≤ Node.
			if counts["ExprValueSource"] > counts["Node"] {
				t.Errorf("ExprValueSource (%d) exceeds Node count (%d) — bug in walker",
					counts["ExprValueSource"], counts["Node"])
			}
		})
	}
}

// TestLocalFlowStepKindsNonZero is the Phase C PR2 regression guard mirror
// of TestCallTargetCrossModuleNonZero. Each new lfs* primitive must emit
// non-zero rows on at least one real fixture, summed across the corpus.
// Without this, a body-typo (wrong column name, wrong rel name, dropped
// literal) could silently zero a kind out and CI would still pass — the
// per-kind unit tests use synthetic IDs and don't catch that. The
// equivalent gap was caught at PR1 review by removing this same form of
// regression guard; see the wiki PR1 outcomes section.
//
// Floors are intentionally low. The bridge corpus does not exercise every
// kind (`lfsAwait` notably has no async-heavy fixture beyond
// async-patterns) — the floors must reflect what the corpus actually
// contains, not theoretical maxima. A regression would zero out a kind
// entirely on a fixture where the unit test confirms it should fire.
func TestLocalFlowStepKindsNonZero(t *testing.T) {
	repoRoot := findRepoRoot(t)
	if repoRoot == "" {
		t.Fatal("repo root not found from CWD; regression guard cannot run")
	}

	// Cover the same fixture set as TestParamBindingBudget plus the
	// dedicated valueflow-* fixtures that exercise field-write and
	// multi-hop shapes the React fixtures don't reach.
	fixtures := []string{
		"react-component",
		"react-usestate",
		"react-usestate-context-alias",
		"react-usestate-context-alias-r3",
		"react-usestate-prop-alias",
		"async-patterns",
		"destructuring",
		"imports",
		"full-ts-project",
		"valueflow-base",
		"valueflow-multihop",
		"valueflow-negative",
		"valueflow-fnref",
		// PR8 (#202 Gap A): JSX prop → destructured-param closure
		// fixture. The only corpus fixture that currently exercises
		// the `lfsJsxPropBind` shape; if it goes missing, the
		// per-kind floor below would read 0 and the regression guard
		// would fire.
		"valueflow-closure-direct-prop",
	}

	// Per-kind floors. Each value is ~50% of observed total — catches
	// partial regressions while permitting legitimate fixture churn.
	// (Floors of 1 only catch total-absence regressions; a rule that
	// silently drops half its output would still pass.)
	//
	// Follow-up: PR3+ should bake the same posture in for ifs* kinds —
	// set floors at ~50% of observed actuals, never default to 1.
	floors := map[string]int{
		"lfsAssign":             6,
		"lfsVarInit":            70,
		"lfsParamBind":          5,
		"lfsReturnToCallSite":   6,
		"lfsDestructureField":   10,
		"lfsArrayDestructure":   38,
		"lfsObjectLiteralStore": 28,
		"lfsSpreadElement":      3,
		"lfsFieldRead":          50,
		"lfsFieldWrite":         4,
		"lfsAwait":              3,
		// PR8 (#202 Gap A): closed by `valueflow-closure-direct-prop`
		// (the only corpus fixture that currently exercises the JSX
		// prop → destructured-param shape). Floor = 1 row keeps the
		// regression guard active without over-fitting to a single
		// fixture's expected row count — if more fixtures land that
		// exercise the shape, bump this honestly.
		"lfsJsxPropBind": 1,
	}

	totals := map[string]int{}
	present := 0
	for _, name := range fixtures {
		dir := filepath.Join(repoRoot, "testdata", "projects", name)
		if _, err := os.Stat(dir); err != nil {
			t.Logf("fixture not present: %s", dir)
			continue
		}
		present++
		baseRels := dbToRelations(extractDB(t, dir))
		var b strings.Builder
		b.WriteString(name)
		b.WriteString(": ")
		for kind := range floors {
			c, err := evalCount(baseRels, kind, 2)
			if err != nil {
				t.Fatalf("eval %s on %s: %v", kind, name, err)
			}
			totals[kind] += c
			b.WriteString(kind)
			b.WriteString("=")
			b.WriteString(itoa(c))
			b.WriteString(" ")
		}
		// Also surface the union row count for context.
		uc, err := evalCount(baseRels, "LocalFlowStep", 2)
		if err != nil {
			t.Fatalf("eval LocalFlowStep on %s: %v", name, err)
		}
		b.WriteString("LocalFlowStep=")
		b.WriteString(itoa(uc))
		t.Log(b.String())
	}
	if present == 0 {
		t.Fatal("no fixtures present")
	}

	for kind, floor := range floors {
		if totals[kind] < floor {
			t.Errorf("regression guard: %s emitted %d rows across corpus, want >= %d",
				kind, totals[kind], floor)
		}
	}
}

// extractDB runs the type-aware walker on a project and returns the raw
// fact DB. Lifted out of extractAndCount so the per-kind regression guard
// can re-use the conversion to eval relations.
func extractDB(t *testing.T, projectDir string) *db.DB {
	t.Helper()
	database := db.NewDB()
	walker := extract.NewTypeAwareWalker(database)
	backend := &extract.TreeSitterBackend{}
	if err := walker.Run(context.Background(), backend, extract.ProjectConfig{RootDir: projectDir}); err != nil {
		t.Fatalf("walker.Run: %v", err)
	}
	backend.Close()
	return database
}

// TestInterFlowStepKindsNonZero is the Phase C PR3 regression guard mirror
// of TestLocalFlowStepKindsNonZero (PR2). Each new ifs* primitive must emit
// non-zero rows on at least one real fixture, summed across the corpus.
// Floors set at ~50% of observed actuals per the PR2-established discipline
// (a uniform floor=1 only catches total absence; ~50% catches partial
// regressions where a kind silently drops half its output).
//
// The `valueflow-rta-dispatch` fixture (added in PR3 review F1) is the
// dedicated minimal RTA carrier — interface I, class C implements I, a
// `new C()` instantiation, and an `o.f(v)` method call against an `I`
// receiver. It supplies all the AST-derivable inputs `CallTargetRTA`
// needs (`MethodCall`, `InterfaceDecl`, `Implements`, `NewExpr`,
// `MethodDecl`).
//
// Why ifsCallTargetRTA's floor is still 0: `CallTargetRTA` additionally
// requires `ExprType(recv, ifaceId)`, which is a tsgo-derived semantic
// fact — see `extract/walker_v2.go` ("ExprType and SymbolType relations
// are left empty" without tsgo). The test harness runs without tsgo, so
// `CallTargetRTA` (and therefore `ifsCallTargetRTA`) is structurally 0
// across the corpus. The floor reflects an environmental constraint, not
// laziness. When tsgo enrichment lands in the test path, the
// `valueflow-rta-dispatch` fixture will start emitting non-zero rows and
// this floor must be raised to ~50% of observed.
func TestInterFlowStepKindsNonZero(t *testing.T) {
	repoRoot := findRepoRoot(t)
	if repoRoot == "" {
		t.Fatal("repo root not found from CWD; regression guard cannot run")
	}

	fixtures := []string{
		"react-component",
		"react-usestate",
		"react-usestate-context-alias",
		"react-usestate-context-alias-r3",
		"react-usestate-prop-alias",
		"async-patterns",
		"destructuring",
		"imports",
		"full-ts-project",
		"valueflow-base",
		"valueflow-multihop",
		"valueflow-negative",
		"valueflow-fnref",
		"valueflow-rta-dispatch",
	}

	// Per-kind floors (~50% of observed totals during PR3 implementation).
	// `InterFlowStep` and `FlowStep` are union/composite floors — they're
	// derived from the per-kind sums above and don't add independent
	// regression-guard signal beyond a smoke-test that the unions wire
	// up. Kept for cheap end-to-end coverage.
	// Per-kind floors (~50% of observed totals on this corpus).
	// Observed (PR3 review F1+F2 measurement): ifsRetToCall=16,
	// ifsImportExport=37, ifsCallTargetRTA=0 (tsgo-gated, see comment above),
	// InterFlowStep=53, FlowStep=526.
	//
	// `InterFlowStep` and `FlowStep` floors are derived from per-kind
	// sums and don't add independent regression-guard signal beyond a
	// smoke-test that the union/composite rules wire up. Cheap to keep.
	floors := map[string]int{
		"ifsRetToCall":     8,
		"ifsImportExport":  18,
		"ifsCallTargetRTA": 0, // tsgo-gated; see method comment above
		"InterFlowStep":    26,
		"FlowStep":         263,
	}

	totals := map[string]int{}
	present := 0
	for _, name := range fixtures {
		dir := filepath.Join(repoRoot, "testdata", "projects", name)
		if _, err := os.Stat(dir); err != nil {
			t.Logf("fixture not present: %s", dir)
			continue
		}
		present++
		baseRels := dbToRelations(extractDB(t, dir))
		var b strings.Builder
		b.WriteString(name)
		b.WriteString(": ")
		for kind := range floors {
			c, err := evalCount(baseRels, kind, 2)
			if err != nil {
				t.Fatalf("eval %s on %s: %v", kind, name, err)
			}
			totals[kind] += c
			b.WriteString(kind)
			b.WriteString("=")
			b.WriteString(itoa(c))
			b.WriteString(" ")
		}
		t.Log(b.String())
	}
	if present == 0 {
		t.Fatal("no fixtures present")
	}

	for kind, floor := range floors {
		if floor == 0 {
			// Tsgo-gated kind — see method-level comment for ifsCallTargetRTA.
			// Synthetic unit test (TestIfsCallTargetRTA) guards body correctness.
			continue
		}
		if totals[kind] < floor {
			t.Errorf("regression guard: %s emitted %d rows across corpus, want >= %d",
				kind, totals[kind], floor)
		}
	}
}

// TestMayResolveToNonZero is the Phase C PR4 regression guard for the
// recursive MayResolveTo closure. Mirrors the PR2 / PR3 discipline (rule
// (a) non-zero on real fixtures, rule (b) per-kind floor at ~50% of
// observed actuals).
//
// MayResolveTo is a single closure relation, not a multi-kind union, so
// only one floor applies — but the same shape: catch a regression where
// the closure silently drops half its rows. PR2/PR3's per-kind guards
// already cover every step kind that feeds FlowStep, so this guard
// specifically protects the *closure* (the recursive rule body, the
// stratification, and the FlowStep ∪ ExprValueSource composition) from
// regressing without one of the upstream guards firing first.
//
// Rule (c) — overlap audit with PR2/PR3 guards: the per-step-kind PR2
// floors (lfsAssign, lfsVarInit, …) catch zeroing of any step kind; the
// PR3 InterFlowStep / FlowStep floors catch wiring-loss of the unions.
// Neither catches a closure-side bug (e.g. the recursive rule's edge
// direction reversed — found and fixed during PR4 implementation, see
// extract/rules/mayresolveto.go comment "Edge direction"). This guard
// adds independent signal at the closure level.
//
// Rule (d) — no carve-out from the discipline. Floor is a real ~50% of
// observed sum across the corpus, no structural-constraint exemption.
func TestMayResolveToNonZero(t *testing.T) {
	repoRoot := findRepoRoot(t)
	if repoRoot == "" {
		t.Fatal("repo root not found from CWD; regression guard cannot run")
	}

	// Same fixture set as TestInterFlowStepKindsNonZero so the closure
	// guard sees the same input scope as the step-kind guards. A
	// regression that zeroes the closure on, say, valueflow-multihop
	// (the multi-step transitivity fixture) but not on valueflow-base
	// would still bite the sum if the multi-hop fixture is the load-
	// bearing one for the closure.
	fixtures := []string{
		"react-component",
		"react-usestate",
		"react-usestate-context-alias",
		"react-usestate-context-alias-r3",
		"react-usestate-prop-alias",
		"async-patterns",
		"destructuring",
		"imports",
		"full-ts-project",
		"valueflow-base",
		"valueflow-multihop",
		"valueflow-negative",
		"valueflow-fnref",
		"valueflow-rta-dispatch",
	}

	// Observed sum across the present corpus during PR4 implementation:
	// 19 + 39 + 45 + 89 + 38 + 11 + 28 + 18 + 116 + 87 + 84 + 67 + 4 + 8
	// = 653. Floor at ~50% per the PR2/PR3 discipline = 326. Catches a
	// regression where the closure silently drops half its rows; floor
	// of 1 would only catch total-absence and is rejected per rule (b).
	const mayResolveToFloor = 326

	// PR4 review M1 — split base/recursive floors.
	//
	// MayResolveTo has a non-recursive base case (`ExprValueSource` identity
	// rows) plus a recursive step case. Across the corpus the base case
	// alone produces 392 of the 653 total rows; a regression that completely
	// deleted the recursive rule would still leave ~392 rows (above the 326
	// total floor). The total floor is therefore decorative w.r.t. closure
	// regressions — we need a separate guard on the recursive contribution.
	//
	// Observed recursive delta = total(MayResolveTo) - total(ExprValueSource)
	// = 653 - 392 = 261. Floor at ~50% = 130. Names "recursive rule
	// contribution" so a future failure is diagnosable.
	const mayResolveToRecursiveDeltaFloor = 130

	// Per-fixture floor on `valueflow-multihop` — the load-bearing
	// transitivity fixture. Observed delta on this fixture: 84 - 27 = 57.
	// Per the M1 reviewer call, set the floor at >= 40 (the sharpest
	// closure-only guard).
	const multihopRecursiveDeltaFloor = 40

	total := 0
	totalExprValueSource := 0
	multihopDelta := -1
	present := 0
	for _, name := range fixtures {
		dir := filepath.Join(repoRoot, "testdata", "projects", name)
		if _, err := os.Stat(dir); err != nil {
			t.Logf("fixture not present: %s", dir)
			continue
		}
		present++
		baseRels := dbToRelations(extractDB(t, dir))
		c, err := evalCount(baseRels, "MayResolveTo", 2)
		if err != nil {
			t.Fatalf("eval MayResolveTo on %s: %v", name, err)
		}
		// Also surface FlowStep + ExprValueSource for ratio sanity.
		fs, err := evalCount(baseRels, "FlowStep", 2)
		if err != nil {
			t.Fatalf("eval FlowStep on %s: %v", name, err)
		}
		evs, err := evalCount(baseRels, "ExprValueSource", 2)
		if err != nil {
			t.Fatalf("eval ExprValueSource on %s: %v", name, err)
		}
		t.Logf("%s: MayResolveTo=%d (FlowStep=%d, ExprValueSource=%d)",
			name, c, fs, evs)
		total += c
		totalExprValueSource += evs
		if name == "valueflow-multihop" {
			multihopDelta = c - evs
		}

		// PR4 review M3 — row-ratio guard. Plan §6 PR4 gate caps
		// MayResolveTo / FlowStep at "≤2× baseline"; PR claims 1.77×
		// worst case. Use 5.0 as a looser ceiling that absorbs noise
		// but still catches real blow-ups (a future change pushing
		// the ratio to 4× would otherwise pass silently). Per-fixture,
		// failure message names the fixture and observed ratio.
		if fs > 0 {
			ratio := float64(c) / float64(fs)
			if ratio > 5.0 {
				t.Errorf("row-ratio guard (plan §6 PR4): %s MayResolveTo/FlowStep = %d/%d = %.2f, want <= 5.0",
					name, c, fs, ratio)
			}
		}
	}
	if present == 0 {
		t.Fatal("no fixtures present")
	}
	if total < mayResolveToFloor {
		t.Errorf("regression guard: MayResolveTo emitted %d rows across corpus, want >= %d",
			total, mayResolveToFloor)
	}
	recursiveDelta := total - totalExprValueSource
	if recursiveDelta < mayResolveToRecursiveDeltaFloor {
		t.Errorf("regression guard: MayResolveTo recursive rule contribution (total - ExprValueSource) = %d - %d = %d, want >= %d",
			total, totalExprValueSource, recursiveDelta, mayResolveToRecursiveDeltaFloor)
	}
	if multihopDelta >= 0 && multihopDelta < multihopRecursiveDeltaFloor {
		t.Errorf("regression guard: valueflow-multihop recursive rule contribution = %d, want >= %d",
			multihopDelta, multihopRecursiveDeltaFloor)
	}
}

// TestCallTargetCrossModuleNonZero is a regression guard for the Phase C PR1
// CallTargetCrossModule rule. The budget test above only logs per-fixture
// counts; if a future change broke the rule body or column semantics drifted,
// the rule could silently emit zero rows on every fixture and CI would still
// pass. This test asserts that at least one of the cross-module-shaped
// fixtures (imports, destructuring, full-ts-project) yields a non-trivial
// row count. Floor is intentionally low to avoid brittleness — a true
// regression would zero out all three.
func TestCallTargetCrossModuleNonZero(t *testing.T) {
	repoRoot := findRepoRoot(t)
	if repoRoot == "" {
		t.Fatal("repo root not found from CWD; regression guard cannot run")
	}

	fixtures := []string{"imports", "destructuring", "full-ts-project"}
	total := 0
	present := 0
	for _, name := range fixtures {
		dir := filepath.Join(repoRoot, "testdata", "projects", name)
		if _, err := os.Stat(dir); err != nil {
			t.Logf("fixture not present: %s", dir)
			continue
		}
		present++
		counts := extractAndCount(t, dir)
		t.Logf("%s: CallTargetCrossModule=%d", name, counts["CallTargetCrossModule"])
		total += counts["CallTargetCrossModule"]
	}
	if present == 0 {
		t.Fatal("no cross-module fixtures present")
	}
	if total < 5 {
		t.Errorf("CallTargetCrossModule regression: sum across %v = %d, want >= 5", fixtures, total)
	}
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
	return ""
}

func extractAndCount(t *testing.T, projectDir string) map[string]int {
	t.Helper()
	database := db.NewDB()
	walker := extract.NewTypeAwareWalker(database)
	backend := &extract.TreeSitterBackend{}
	if err := walker.Run(context.Background(), backend, extract.ProjectConfig{RootDir: projectDir}); err != nil {
		t.Fatalf("walker.Run: %v", err)
	}
	backend.Close()

	counts := map[string]int{}
	for _, name := range []string{"Node", "CallArg", "Parameter", "ExprValueSource", "AssignExpr", "Assign"} {
		if r := database.Relation(name); r != nil {
			counts[name] = r.Tuples()
		}
	}

	// Evaluate ParamBinding via system rules.
	baseRels := dbToRelations(database)
	pbCount, err := evalCount(baseRels, "ParamBinding", 4)
	if err != nil {
		t.Fatalf("eval ParamBinding: %v", err)
	}
	counts["ParamBinding"] = pbCount

	ctCount, err := evalCount(baseRels, "CallTarget", 2)
	if err != nil {
		t.Fatalf("eval CallTarget: %v", err)
	}
	counts["CallTarget"] = ctCount

	rtaCount, err := evalCount(baseRels, "CallTargetRTA", 2)
	if err != nil {
		t.Fatalf("eval CallTargetRTA: %v", err)
	}
	counts["CallTargetRTA"] = rtaCount

	xmodCount, err := evalCount(baseRels, "CallTargetCrossModule", 2)
	if err != nil {
		t.Fatalf("eval CallTargetCrossModule: %v", err)
	}
	counts["CallTargetCrossModule"] = xmodCount
	return counts
}

func dbToRelations(database *db.DB) map[string]*eval.Relation {
	out := map[string]*eval.Relation{}
	for _, def := range schema.Registry {
		r := database.Relation(def.Name)
		if r == nil {
			out[def.Name] = eval.NewRelation(def.Name, def.Arity())
			continue
		}
		er := eval.NewRelation(def.Name, def.Arity())
		for i := 0; i < r.Tuples(); i++ {
			row := make(eval.Tuple, def.Arity())
			for c := 0; c < def.Arity(); c++ {
				if def.Columns[c].Type == schema.TypeString {
					s, _ := r.GetString(database, i, c)
					row[c] = eval.StrVal{V: s}
				} else {
					v, _ := r.GetInt(i, c)
					row[c] = eval.IntVal{V: int64(v)}
				}
			}
			er.Add(row)
		}
		out[def.Name] = er
	}
	return out
}

func evalCount(baseRels map[string]*eval.Relation, pred string, arity int) (int, error) {
	terms := make([]datalog.Term, arity)
	for i := range terms {
		terms[i] = datalog.Var{Name: "x" + string(rune('0'+i))}
	}
	query := &datalog.Query{
		Select: terms,
		Body: []datalog.Literal{
			{Positive: true, Atom: datalog.Atom{Predicate: pred, Args: terms}},
		},
	}
	prog := &datalog.Program{Rules: AllSystemRules(), Query: query}
	ep, errs := plan.Plan(prog, nil)
	if len(errs) > 0 {
		return 0, errs[0]
	}
	rs, err := eval.Evaluate(context.Background(), ep, baseRels)
	if err != nil {
		return 0, err
	}
	return len(rs.Rows), nil
}

func formatCounts(c map[string]int) string {
	keys := []string{"Node", "CallArg", "Parameter", "CallTarget", "CallTargetRTA", "CallTargetCrossModule", "ParamBinding", "ExprValueSource", "AssignExpr", "Assign"}
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteString("=")
		b.WriteString(itoa(c[k]))
		b.WriteString(" ")
	}
	return b.String()
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	buf := make([]byte, 0, 12)
	for i > 0 {
		buf = append([]byte{byte('0' + i%10)}, buf...)
		i /= 10
	}
	if neg {
		buf = append([]byte{'-'}, buf...)
	}
	return string(buf)
}
