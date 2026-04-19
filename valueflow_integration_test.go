package integration_test

import (
	"context"
	"fmt"
	"os"
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

// runValueflowQuery is a thin wrapper that runs a QL query through the full
// pipeline (parse → resolve → desugar → merge system rules → estimate+plan
// → eval) against the in-memory fact DB. The setstate-context integration
// tests use a similar shape; we duplicate it here to keep the value-flow
// test self-contained and to make the planner-cap visible at one site (so a
// future row-count blow-up surfaces as a cap-hit, not a silent OOM).
func runValueflowQuery(t *testing.T, queryFile, fixtureDir string) *eval.ResultSet {
	t.Helper()
	factDB := extractProject(t, fixtureDir)

	src, err := os.ReadFile(queryFile)
	if err != nil {
		t.Fatalf("read query: %v", err)
	}

	bridgeFiles := bridge.LoadBridge()
	importLoader := makeBridgeImportLoader(bridgeFiles)

	p := parse.NewParser(string(src), queryFile)
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
	return rs
}

// TestValueflow_AllBranchesFireOnBase asserts that every Phase A branch of
// `mayResolveTo` returns at least one row when the value-flow base fixture
// is extracted. Each fixture file targets one branch; if the branch returns
// 0 rows, either the fixture stopped exercising the shape or the QL rule
// regressed.
//
// Plan §4.1 design intent: the per-branch fixtures are hand-checkable. This
// test checks the lower-bound cardinality contract (≥1 per branch); the
// `TestValueflow_UnionMatchesSumOfBranches` test below checks the union
// shape (no disjunction-poisoning #166 regression).
func TestValueflow_AllBranchesFireOnBase(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping extraction-heavy integration test in short mode")
	}

	branches := []struct {
		name      string
		queryFile string
	}{
		{"base", "testdata/queries/v2/valueflow/branch_base.ql"},
		{"var_init", "testdata/queries/v2/valueflow/branch_var_init.ql"},
		{"assign", "testdata/queries/v2/valueflow/branch_assign.ql"},
		{"param_bind", "testdata/queries/v2/valueflow/branch_param_bind.ql"},
		{"field_read", "testdata/queries/v2/valueflow/branch_field_read.ql"},
		{"object_field", "testdata/queries/v2/valueflow/branch_object_field.ql"},
		// PR3 amendment — JSX-wrapper-tolerant branch. Fires on the
		// `<Provider value={X} />` shape where the JsxExpression `{X}`
		// is what JsxAttribute.valueExpr points at, NOT the inner
		// identifier. Without this branch the bridge migration would
		// silently drop every `value={X}` case.
		{"jsx_wrapped", "testdata/queries/v2/valueflow/branch_jsx_wrapped.ql"},
	}

	for _, b := range branches {
		b := b
		t.Run(b.name, func(t *testing.T) {
			rs := runValueflowQuery(t, b.queryFile, "testdata/projects/valueflow-base")
			if len(rs.Rows) == 0 {
				t.Fatalf("branch %q returned 0 rows on valueflow-base fixture; "+
					"either the fixture lost coverage of this branch or the QL rule regressed",
					b.name)
			}
			t.Logf("branch %q: %d rows", b.name, len(rs.Rows))
		})
	}
}

// TestValueflow_UnionMatchesSumOfBranches is the disjunction-poisoning
// regression guard from plan §7.5. It calls `mayResolveTo` (the union) and
// each of the 6 named branches separately, then asserts:
//
//   - mayResolveTo row count ≤ sum of branch row counts (no spurious rows)
//   - mayResolveTo row count ≥ max of branch row counts (no missing rows)
//   - every (v, s) pair that appears in some branch also appears in the union
//
// The union is a set union (Datalog semantics dedupe), so it can be smaller
// than the sum when branches overlap (e.g. a value-source identifier whose
// own VarDecl init is the same expression node — rare in practice). It must
// never be smaller than the largest single branch's contribution. If it is,
// that's the #166 binding-loss signature: a per-branch literal returns rows
// in isolation but disappears under disjunction.
func TestValueflow_UnionMatchesSumOfBranches(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping extraction-heavy integration test in short mode")
	}

	branches := []string{
		"testdata/queries/v2/valueflow/branch_base.ql",
		"testdata/queries/v2/valueflow/branch_var_init.ql",
		"testdata/queries/v2/valueflow/branch_assign.ql",
		"testdata/queries/v2/valueflow/branch_param_bind.ql",
		"testdata/queries/v2/valueflow/branch_field_read.ql",
		"testdata/queries/v2/valueflow/branch_object_field.ql",
		// PR3 amendment — JSX-wrapper-tolerant 7th branch. Must be
		// included in the union-vs-sum invariant or the wrapper rows
		// (which appear in `mayResolveTo` but not in the original 6
		// branches) trip the union > sum bound check.
		"testdata/queries/v2/valueflow/branch_jsx_wrapped.ql",
	}

	branchPairs := make(map[string]bool)
	maxBranch := 0
	sumBranches := 0
	for _, q := range branches {
		rs := runValueflowQuery(t, q, "testdata/projects/valueflow-base")
		n := len(rs.Rows)
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

	unionRS := runValueflowQuery(t, "testdata/queries/v2/valueflow/all_mayResolveTo.ql", "testdata/projects/valueflow-base")
	unionPairs := make(map[string]bool)
	for _, row := range unionRS.Rows {
		if len(row) >= 2 {
			unionPairs[fmt.Sprintf("%v|%v", row[0], row[1])] = true
		}
	}

	t.Logf("branch totals: sum=%d max=%d ; union=%d (deduped=%d)",
		sumBranches, maxBranch, len(unionRS.Rows), len(unionPairs))

	if len(unionPairs) < maxBranch {
		t.Fatalf("disjunction-poisoning regression suspected: union=%d < max single branch=%d. "+
			"Per-branch literals return rows in isolation but disappear under `or`-of-calls. "+
			"This is the bug #166 signature — escalate to the planner team rather than rewriting "+
			"the value-flow rules.", len(unionPairs), maxBranch)
	}
	if len(unionPairs) > sumBranches {
		t.Fatalf("union (%d) > sum of branches (%d) — impossible under set semantics; "+
			"indicates a corrupted result set or a bug in the eval engine", len(unionPairs), sumBranches)
	}
	for p := range branchPairs {
		if !unionPairs[p] {
			t.Errorf("branch result missing from union: %s — disjunction-poisoning suspect", p)
		}
	}
}

// TestValueflow_NegativeFixtureNoLeakage asserts the negative fixture does
// NOT produce `mayResolveTo` rows for the patterns Phase A explicitly
// excludes (two-hop var indirection, object spread, aliased field write).
//
// Acceptance: union row count is permitted to be > 0 — the negative
// fixture also contains the value-source literals themselves (identity
// branch fires for every literal), and `const` initialisers exercise the
// var-init branch trivially. What matters is that the use-site
// expressions (`use(a)`, `o.k`, `o.cb()`) do NOT join through to the
// underlying value-source.
//
// This test guards against accidental recursive-leakage: if a future edit
// turns one of the named branches recursive (e.g. mayResolveToVarInit
// referencing mayResolveTo internally), the two-hop var fixture would
// start resolving and this test catches it.
//
// The check is structural rather than a hard "0 rows" assertion because:
//   - identity rows are expected (literals are value-sources of themselves)
//   - var-init rows are expected for the depth-1 hop (`const b = {...}` →
//     b's var-init fires; this is legitimate)
//   - what must NOT happen is that the use-site expression on the OTHER
//     side of the unsupported step resolves to the literal.
//
// We assert the negative property by snapshotting the row count and
// comparing against an upper bound derived from per-branch fixture
// expectations. If a real recursion bug fires, the count balloons.
func TestValueflow_NegativeFixtureNoLeakage(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping extraction-heavy integration test in short mode")
	}

	rs := runValueflowQuery(t, "testdata/queries/v2/valueflow/all_mayResolveTo.ql",
		"testdata/projects/valueflow-negative")

	// Defence-in-depth: aggregate row count must stay bounded. 3 fixture
	// files × ~10 expected rows each (identity literals + depth-1 var-init)
	// = ~30. A genuine recursion bug would multiply this by the per-fixture
	// symbol count.
	const upperBound = 60
	if len(rs.Rows) > upperBound {
		t.Errorf("valueflow-negative fixture emitted %d mayResolveTo rows (expected ≤ %d). "+
			"This is the recursion-leakage signature: a depth-1 rule has started "+
			"chaining through itself. Check that no branch body references mayResolveTo.",
			len(rs.Rows), upperBound)
	}
	t.Logf("valueflow-negative mayResolveTo row count: %d (upper bound %d)", len(rs.Rows), upperBound)

	// Per-fixture pinned assertions (plan §4.1). Run the location-projected
	// probe and confirm no row joins a known use-site at line `useLine` to
	// the unreachable literal at `forbiddenSourceLine`. These are the
	// signatures of the Phase A out-of-scope shapes (two-hop var, spread,
	// alias-base field write — the last is documented as a possible
	// over-approximation; see the fixture comment in
	// field_write_aliased_base.ts for context).
	probe := runValueflowQuery(t,
		"testdata/queries/v2/valueflow/negative_use_site_resolutions.ql",
		"testdata/projects/valueflow-negative")

	type forbidden struct {
		fileSuffix          string // matched as suffix on path so test is repo-root agnostic
		useLine             int64  // line of the use-site expression
		forbiddenSourceLine int64  // line of the unreachable literal
		shape               string // human label for the failure message
	}
	cases := []forbidden{
		// two_hop_var.ts:  const b = { k: 1 };  (line 5, literal)
		//                  const a = b;          (line 6)
		//                  const r = use(a);     (line 8, use-site `a`)
		// Phase A must NOT resolve `a` (line 8) to the literal `{ k: 1 }`
		// (line 5) — that requires recursive var-init traversal.
		{"two_hop_var.ts", 8, 5, "two-hop var indirection (a → b → {k:1})"},

		// spread_carrier.ts: const base = { k: 1 };  (line 5, literal)
		//                    const o = { ...base }; (line 6, spread)
		//                    const r = o.k;         (line 7, use-site)
		// Phase A must NOT resolve `o.k` (line 7) to `1` (line 5) — spread
		// is unmodelled.
		{"spread_carrier.ts", 7, 5, "object-literal spread (o.k → ...base → 1)"},
	}
	// Note: field_write_aliased_base.ts is intentionally NOT pinned. Its
	// own fixture comment flags that whether `o` and `o2` collapse to the
	// same baseSym is extractor-dependent; if they do, Phase A's
	// no-alias-tracking field-read branch will resolve through, and that's
	// recorded as a known v1 over-approximation rather than a leak.

	for _, fc := range cases {
		fc := fc
		t.Run(fc.fileSuffix, func(t *testing.T) {
			leaks := 0
			for _, row := range probe.Rows {
				if len(row) < 4 {
					continue
				}
				vpv, ok1 := row[0].(eval.StrVal)
				vlv, ok2 := row[1].(eval.IntVal)
				spv, ok3 := row[2].(eval.StrVal)
				slv, ok4 := row[3].(eval.IntVal)
				if !ok1 || !ok2 || !ok3 || !ok4 {
					continue
				}
				vp, vl, sp, sl := vpv.V, vlv.V, spv.V, slv.V
				if !endsWith(vp, fc.fileSuffix) || !endsWith(sp, fc.fileSuffix) {
					continue
				}
				if vl == fc.useLine && sl == fc.forbiddenSourceLine {
					leaks++
					t.Errorf("Phase A leak: mayResolveTo row %s:%d → %s:%d "+
						"(shape: %s). Phase A must not resolve through this step; "+
						"if a branch was made recursive this is the regression site.",
						vp, vl, sp, sl, fc.shape)
				}
			}
			t.Logf("%s: 0-leak assertion held (use-line %d ↛ source-line %d, shape: %s; %d total leak rows)",
				fc.fileSuffix, fc.useLine, fc.forbiddenSourceLine, fc.shape, leaks)
		})
	}
}

// endsWith is a small dependency-free suffix check so the negative-fixture
// test is repo-root-agnostic (CI may extract from absolute paths).
func endsWith(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

// TestValueflow_RowCountBudget enforces the row-count budget gate from plan
// §2.7: `mayResolveTo` aggregate row count is bounded by N × ExprValueSource.
//
// Two thresholds are in play:
//
//   - Small-fixture local guard: 3.0×. Observed ratios on the four small
//     fixtures here sit between 1.08× and 1.85×; a 5× regression would
//     pass a 10× cap silently, so the small-fixture gate is tightened to
//     3.0× and a regression rules out routine drift while still leaving
//     headroom for fixture growth.
//   - Mastodon-scale gate: 10.0× — appropriate when this test is re-run
//     against the larger fixtures (full-ts-project,
//     react-usestate-context-alias-r3) where per-symbol fan-out is denser
//     and Phase A's union sits at ~10^5–10^6 rows over an ExprValueSource
//     population of ~10^5–5*10^5.
//
// When wiring the larger fixtures in a follow-up, hoist `cap` into the
// fixture struct so each fixture carries its own threshold.
func TestValueflow_RowCountBudget(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping extraction-heavy integration test in short mode")
	}

	fixtures := []string{
		"testdata/projects/valueflow-base",
		"testdata/projects/valueflow-negative",
		"testdata/projects/react-usestate",
		"testdata/projects/react-usestate-context-alias",
	}
	for _, fixture := range fixtures {
		fixture := fixture
		t.Run(fixture, func(t *testing.T) {
			factDB := extractProject(t, fixture)
			evsCount := 0
			if r := factDB.Relation("ExprValueSource"); r != nil {
				evsCount = r.Tuples()
			}
			rs := runValueflowQuery(t, "testdata/queries/v2/valueflow/all_mayResolveTo.ql", fixture)
			ratio := 0.0
			if evsCount > 0 {
				ratio = float64(len(rs.Rows)) / float64(evsCount)
			}
			t.Logf("fixture=%s ExprValueSource=%d mayResolveTo=%d ratio=%.2f",
				fixture, evsCount, len(rs.Rows), ratio)
			// Small-fixture cap. See doc above for the 10× Mastodon-scale
			// threshold that applies once larger fixtures are wired in.
			const smallFixtureRatioCap = 3.0
			if evsCount > 0 && ratio > smallFixtureRatioCap {
				t.Errorf("budget gate: mayResolveTo (%d) > %.1fx ExprValueSource (%d) — ratio %.2f. "+
					"Phase A union should not balloon multiplicatively over the EDB base; "+
					"investigate per-branch row counts via the branch_*.ql queries. "+
					"(10× is the Mastodon-scale threshold; this 3× gate is the small-fixture local guard.)",
					len(rs.Rows), smallFixtureRatioCap, evsCount, ratio)
			}
		})
	}
}

// TestValueflow_ResolvesToFunctionDirect exercises the
// `resolvesToFunctionDirect(callee, fnId)` derived helper in
// `bridge/tsq_valueflow.qll`. The helper is the Phase A surface PR3 will
// consume to rewrite `tsq_react.qll`'s easy `resolveToObjectExpr*` branches
// onto value-flow. Shipping it without a direct test means a slot-swap bug
// in the `FunctionSymbol(sym, fnId)` wiring would land silently and only
// surface when PR3 lands; this test catches it at PR2 boundary.
//
// Fixture: `const cb = () => 42; const r = cb();` — the use-site `cb` in
// `cb()` is the callee, and the arrow-function init is both the
// ExprValueSource (var-init branch resolves through to it) and the
// FunctionSymbol target. The two must agree on the function node id.
func TestValueflow_ResolvesToFunctionDirect(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping extraction-heavy integration test in short mode")
	}
	rs := runValueflowQuery(t,
		"testdata/queries/v2/valueflow/resolves_to_function_direct.ql",
		"testdata/projects/valueflow-fnref")
	if len(rs.Rows) == 0 {
		t.Fatal("resolvesToFunctionDirect returned 0 rows on valueflow-fnref " +
			"fixture; either the FunctionSymbol/sourceExpr equality wiring is " +
			"broken or the var-init branch regressed. Each row should bind " +
			"(useExprId, arrowFnNodeId).")
	}
	// Every row must be a (callee, fnId) IntVal pair where fnId is non-zero
	// (a real node id). Defensive shape check guards against a future schema
	// drift that would silently land null/zero ids and pass the count check.
	for i, row := range rs.Rows {
		if len(row) != 2 {
			t.Fatalf("row %d: expected arity 2, got %d", i, len(row))
		}
		c, ok1 := row[0].(eval.IntVal)
		f, ok2 := row[1].(eval.IntVal)
		if !ok1 || !ok2 {
			t.Fatalf("row %d: expected (IntVal, IntVal), got (%T, %T)", i, row[0], row[1])
		}
		if c.V == 0 || f.V == 0 {
			t.Errorf("row %d: zero node id (callee=%d fnId=%d) — schema drift suspected", i, c.V, f.V)
		}
	}
	t.Logf("resolvesToFunctionDirect: %d row(s) on valueflow-fnref", len(rs.Rows))
}

// TestValueflow_JsxWrappedBranch asserts the JSX-wrapper-tolerant branch
// fires on the canonical `<Provider value={X} />` shape and resolves to
// the inner identifier's VarDecl init via mayResolveToCore.
//
// This is the regression guard for the subsumption gap surfaced when PR3
// first attempted to substitute mayResolveToVarInit for
// resolveToObjectExprVarD1: that earlier shape silently dropped every
// wrapped case because the JsxAttribute valueExpr column points at the
// JsxExpression `{X}`, not at `X`. The wrapper branch closes that gap.
//
// The fixture jsx_wrapped.tsx has exactly one Provider with `value={theme}`
// where `theme` is a VarDecl initialised to an object literal. That
// pattern must produce at least one mayResolveToJsxWrapped row whose
// sourceExpr is the object literal.
func TestValueflow_JsxWrappedBranch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping extraction-heavy integration test in short mode")
	}
	rs := runValueflowQuery(t,
		"testdata/queries/v2/valueflow/branch_jsx_wrapped.ql",
		"testdata/projects/valueflow-base")
	if len(rs.Rows) == 0 {
		t.Fatal("mayResolveToJsxWrapped returned 0 rows on valueflow-base; " +
			"either the JsxExpression Node row is missing (extractor regression) " +
			"or jsxExpressionUnwrap's Contains/Kind check is broken. " +
			"Without this branch the bridge migration silently drops every " +
			"`<Provider value={X} />` case.")
	}
	t.Logf("mayResolveToJsxWrapped: %d row(s) on valueflow-base jsx_wrapped fixture", len(rs.Rows))
}

// TestValueflow_IntegrationOnReactFixture is the smoke test that
// `mayResolveTo` runs end-to-end on a realistic React fixture without
// blowing the planner cap. The react-usestate-context-alias fixture is
// the same one the bridge's setStateUpdaterCallsOtherSetStateThroughContext
// predicate exercises; PR4 of Phase A will rewrite the bridge's easy
// resolveToObjectExpr branches onto mayResolveTo, and this test ensures
// the union runs cleanly first.
func TestValueflow_IntegrationOnReactFixture(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping extraction-heavy integration test in short mode")
	}
	rs := runValueflowQuery(t, "testdata/queries/v2/valueflow/all_mayResolveTo.ql",
		"testdata/projects/react-usestate-context-alias")
	if len(rs.Rows) == 0 {
		t.Fatal("expected mayResolveTo to return at least one row on react-usestate-context-alias")
	}
	t.Logf("react-usestate-context-alias mayResolveTo: %d rows", len(rs.Rows))
}
