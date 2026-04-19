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

	// Loose upper bound: 3 fixture files × ~10 expected rows each (identity
	// literals + 1 depth-1 var-init each) = ~30. A genuine recursion bug
	// would multiply this by the per-fixture symbol count.
	const upperBound = 60
	if len(rs.Rows) > upperBound {
		t.Errorf("valueflow-negative fixture emitted %d mayResolveTo rows (expected ≤ %d). "+
			"This is the recursion-leakage signature: a depth-1 rule has started "+
			"chaining through itself. Check that no branch body references mayResolveTo.",
			len(rs.Rows), upperBound)
	}
	t.Logf("valueflow-negative mayResolveTo row count: %d (upper bound %d)", len(rs.Rows), upperBound)
}

// TestValueflow_RowCountBudget enforces the row-count budget gate from plan
// §2.7: `mayResolveTo` aggregate row count is bounded by N × ExprValueSource.
// Per the plan, on Mastodon the union sits at ~10^5–10^6 rows against an
// ExprValueSource population of ~10^5–5*10^5. Conservative ratio cap: 10x.
//
// On the small valueflow-base fixture this is decorative; the gate exists
// to break CI loud and early when the same predicate is re-run against
// larger fixtures (full-ts-project, react-usestate-context-alias-r3).
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
			if evsCount > 0 && ratio > 10.0 {
				t.Errorf("budget gate: mayResolveTo (%d) > 10x ExprValueSource (%d) — ratio %.2f. "+
					"Phase A union should not balloon multiplicatively over the EDB base; "+
					"investigate per-branch row counts via the branch_*.ql queries.",
					len(rs.Rows), evsCount, ratio)
			}
		})
	}
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
