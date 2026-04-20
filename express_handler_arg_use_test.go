package integration_test

import (
	"sort"
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/eval"
)

// Phase D PR2 — `ExpressHandlerArgUse` predicate (additive,
// tsq_express.qll).
//
// The predicate links a use-site expression back to the Express route
// handler whose parameter it value-flow-resolves to. Additive-only:
// no existing predicate is modified, no branch is deleted.
//
// Regression-guard discipline (wiki briefing rules (a)-(g)):
//
//   (a) Non-zero real-fixture assertion — the fixture emits 4
//       expected param uses (req/res in two handlers); test t.Fatals
//       if the query returns zero rows.
//   (b) Per-kind floor — there is one kind (ExpressHandlerArgUse).
//       Floor set at 3, i.e. ~50%+ of the 4 observed pins. Not 1:
//       per wiki rule (b) a floor of 1 is decorative.
//   (c) Overlap check — this predicate wraps `MayResolveTo` +
//       `ExpressHandler` + `Parameter` + `ExprMayRef` and projects
//       on (useExpr, fn, paramIdx). Overlap with PR1's
//       `tsq::dataflow.mayResolveTo` is limited to the use→source
//       closure step being the same system relation; this predicate
//       adds the handler-arg filter on top, which is distinct signal.
//   (d) No carve-outs.
//   (e) N/A — predicate is non-recursive on the QL side; recursion
//       is inside `MayResolveTo`, which has its own base/recursive
//       split tests in Phase C PR7.
//   (f) Manifest `File:` — no new manifest entry (ExpressHandlerArgUse
//       is a predicate, not a class, and does not introduce a new
//       relation). `TestClosure_ManifestFileFieldsGreppable` unaffected.
//   (g) Planner-stack verification — `runClosureQuery` routes through
//       `plan.EstimateAndPlan` + `eval.MakeEstimatorHook` (the same
//       path used by PR1's parity tests), so the predicate's
//       cardinality is sized against live hints.

// expressUseRow is the projection of one ExpressHandlerArgUse row,
// keyed by file-suffix + line for stable equality across walker-
// ordering changes.
type expressUseRow struct {
	usePath  string
	useLine  int64
	paramIdx int64
}

// expressUseRowFull includes the fnLine column — used by negative
// assertions that need to distinguish which handler fn a row resolves
// to without relying on unstable fn node-ids.
type expressUseRowFull struct {
	usePath  string
	useLine  int64
	fn       int64
	paramIdx int64
	fnLine   int64
}

// runExpressHandlerArgUseQuery evaluates
// testdata/queries/v2/find_express_handler_arg_use.ql on `fixtureDir`
// via the full planner stack and returns the projected rows. The `fn`
// column is dropped — fn node-ids are not stable across walker passes;
// `paramIdx` + `useLine` are sufficient to pin semantic identity for
// the fixture's two distinct handlers.
func runExpressHandlerArgUseQuery(t *testing.T, fixtureDir string) []expressUseRow {
	t.Helper()
	rs := runClosureQuery(t,
		"testdata/queries/v2/find_express_handler_arg_use.ql",
		fixtureDir)
	return projectExpressUseRows(t, rs)
}

func projectExpressUseRows(t *testing.T, rs *eval.ResultSet) []expressUseRow {
	t.Helper()
	out := make([]expressUseRow, 0, len(rs.Rows))
	for i, row := range rs.Rows {
		if len(row) != 5 {
			t.Fatalf("row %d: expected arity 5 (usePath,useLine,fn,paramIdx,fnLine), got %d", i, len(row))
		}
		pv, ok1 := row[0].(eval.StrVal)
		lv, ok2 := row[1].(eval.IntVal)
		_, ok3 := row[2].(eval.IntVal) // fn — present but intentionally unused in projection
		piv, ok4 := row[3].(eval.IntVal)
		_, ok5 := row[4].(eval.IntVal) // fnLine — present but unused in this projection
		if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 {
			t.Fatalf("row %d: unexpected cell shape (%T, %T, %T, %T, %T)",
				i, row[0], row[1], row[2], row[3], row[4])
		}
		out = append(out, expressUseRow{
			usePath:  lastPathSegment(pv.V),
			useLine:  lv.V,
			paramIdx: piv.V,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.usePath != b.usePath {
			return a.usePath < b.usePath
		}
		if a.useLine != b.useLine {
			return a.useLine < b.useLine
		}
		return a.paramIdx < b.paramIdx
	})
	return out
}

// projectExpressUseRowsFull returns the full 5-column projection
// including fn and fnLine — needed for negative assertions that must
// distinguish which handler function each row resolves to.
func projectExpressUseRowsFull(t *testing.T, rs *eval.ResultSet) []expressUseRowFull {
	t.Helper()
	out := make([]expressUseRowFull, 0, len(rs.Rows))
	for i, row := range rs.Rows {
		if len(row) != 5 {
			t.Fatalf("row %d: expected arity 5, got %d", i, len(row))
		}
		pv, ok1 := row[0].(eval.StrVal)
		lv, ok2 := row[1].(eval.IntVal)
		fv, ok3 := row[2].(eval.IntVal)
		piv, ok4 := row[3].(eval.IntVal)
		flv, ok5 := row[4].(eval.IntVal)
		if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 {
			t.Fatalf("row %d: unexpected cell shape", i)
		}
		out = append(out, expressUseRowFull{
			usePath:  lastPathSegment(pv.V),
			useLine:  lv.V,
			fn:       fv.V,
			paramIdx: piv.V,
			fnLine:   flv.V,
		})
	}
	return out
}

func containsExpressUseRow(rows []expressUseRow, pin expressUseRow) bool {
	for _, r := range rows {
		if r == pin {
			return true
		}
	}
	return false
}

// TestExpressHandlerArgUse_Pins — primary regression-guard test.
// Asserts both named-variable and inline-arrow handlers produce the
// expected (req=paramIdx 0, res=paramIdx 1) param-use rows at the
// documented line numbers in the fixture.
func TestExpressHandlerArgUse_Pins(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping extraction-heavy integration test in short mode")
	}

	fixture := "testdata/projects/express-handler-arg-use"
	rows := runExpressHandlerArgUseQuery(t, fixture)

	// Rule (a): non-zero real-fixture assertion.
	if len(rows) == 0 {
		t.Fatalf("fixture %s: ExpressHandlerArgUse produced 0 rows; "+
			"predicate is broken or fixture regressed (rule (a))", fixture)
	}

	// Rule (b): ≥3 total rows (≥50%+ of the 4 observed pins). Not 1 —
	// per wiki rule (b) a floor of 1 is a decorative guard.
	const minTotal = 3
	if len(rows) < minTotal {
		t.Errorf("fixture %s: expected ≥%d ExpressHandlerArgUse rows, got %d; rows:\n%+v",
			fixture, minTotal, len(rows), rows)
	}

	// Pinned rows — file:line:paramIdx tuples the predicate must return.
	//
	//   line 50: `const id = req.query;` (named handler)      → req  (paramIdx 0)
	//   line 51: `res.send('ok ' + id);` (named handler)      → res  (paramIdx 1)
	//   line 57: `const id = req.query;` (inline handler)     → req  (paramIdx 0)
	//   line 58: `res.send('inline ' + id);` (inline handler) → res  (paramIdx 1)
	pins := []expressUseRow{
		{usePath: "app.ts", useLine: 50, paramIdx: 0},
		{usePath: "app.ts", useLine: 51, paramIdx: 1},
		{usePath: "app.ts", useLine: 57, paramIdx: 0},
		{usePath: "app.ts", useLine: 58, paramIdx: 1},
	}
	for _, pin := range pins {
		if !containsExpressUseRow(rows, pin) {
			t.Errorf("fixture %s: missing pin %+v from ExpressHandlerArgUse rows:\n%+v",
				fixture, pin, rows)
		}
	}

	// Adversarial negative assertions — these rows MUST NOT appear.
	// They prove the predicate actually filters via `CallArg` +
	// `MayResolveTo(handlerArgExpr, fn)` + method-name allow-list.
	//
	// Mutation probe: commenting out `CallArg(call, _, handlerArgExpr)`
	// OR `MayResolveTo(handlerArgExpr, fn)` at bridge/tsq_express.qll
	// should surface `decoy`'s param uses (lines 66-67). Commenting
	// out any of the method-name disjuncts together with adding `on`
	// would surface `otherHandler`'s (lines 75-76). Either leg going
	// silent means the predicate has decayed to a parameter-only
	// overapproximation.
	//
	//   line 69: `const x = req.query;` inside `decoyHandler` fn expr
	//   line 70: `res.send(x);`         inside `decoyHandler` fn expr
	//   line 81: `const y = req.query;` inside `function otherHandler`
	//   line 82: `res.send(y);`         inside `function otherHandler`
	forbidden := []expressUseRow{
		{usePath: "app.ts", useLine: 69, paramIdx: 0},
		{usePath: "app.ts", useLine: 70, paramIdx: 1},
		{usePath: "app.ts", useLine: 81, paramIdx: 0},
		{usePath: "app.ts", useLine: 82, paramIdx: 1},
	}
	for _, bad := range forbidden {
		if containsExpressUseRow(rows, bad) {
			t.Errorf("fixture %s: forbidden row %+v present in ExpressHandlerArgUse rows — "+
				"predicate has decayed to parameter-only overapproximation or method filter is broken; rows:\n%+v",
				fixture, bad, rows)
		}
	}
}

// TestExpressHandlerArgUse_NamedVsInlineDistinctFns — sanity check
// that the predicate distinguishes the two handlers via the `fn`
// column. The named handler and the inline handler are different
// function expressions; the two req-uses (lines 35, 42) must resolve
// to different `fn` ids. Guards against a bug where the predicate
// collapses all handlers onto one fn (e.g. if `exists` over fn was
// dropped).
func TestExpressHandlerArgUse_NamedVsInlineDistinctFns(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping extraction-heavy integration test in short mode")
	}

	fixture := "testdata/projects/express-handler-arg-use"
	rs := runClosureQuery(t,
		"testdata/queries/v2/find_express_handler_arg_use.ql",
		fixture)
	full := projectExpressUseRowsFull(t, rs)

	// Collect (useLine → {fn, fnLine}) for paramIdx=0 (req) rows.
	type fnPair struct {
		fn     int64
		fnLine int64
	}
	fnsByLine := map[int64]map[fnPair]struct{}{}
	for _, r := range full {
		if r.paramIdx != 0 {
			continue
		}
		if _, ok := fnsByLine[r.useLine]; !ok {
			fnsByLine[r.useLine] = map[fnPair]struct{}{}
		}
		fnsByLine[r.useLine][fnPair{fn: r.fn, fnLine: r.fnLine}] = struct{}{}
	}

	namedFns := fnsByLine[50]
	inlineFns := fnsByLine[57]
	if len(namedFns) == 0 || len(inlineFns) == 0 {
		t.Fatalf("fixture %s: missing req-use rows — named line 50 pairs=%v, inline line 57 pairs=%v",
			fixture, namedFns, inlineFns)
	}
	// The two lines must resolve to disjoint fn sets. If any fn
	// overlaps, the predicate collapsed two distinct handlers.
	for p := range namedFns {
		for q := range inlineFns {
			if p.fn == q.fn {
				t.Errorf("fixture %s: named handler (line 50) and inline handler (line 57) "+
					"share fn=%d — predicate is not distinguishing handlers", fixture, p.fn)
			}
		}
	}

	// MINOR 1 — fnLine pinning. The handler fn each row resolves to
	// must land at a handler decl line in the fixture:
	//
	//   line 49: `const namedHandler = function(req, res) { … };`
	//            — the function expression literal on the RHS.
	//   line 56: `app.get('/inline', (req, res) => { … });`
	//            — the arrow function expression literal.
	//
	// Fixture decoys: `decoyHandler` var at line 68 (fn expr on the
	// RHS of `const decoyHandler = function(...)`) and `otherHandler`
	// at line 80 (`function otherHandler`). The predicate must NOT
	// resolve to either of those. Using a disjoint-from-decoys check
	// rather than an exact-line pin: extractor may report the fn
	// literal's start at either the `function`/`(` token or the
	// enclosing initialiser — both acceptable, as long as it isn't
	// a decoy.
	forbiddenFnLines := map[int64]string{
		68: "decoyHandler",
		80: "otherHandler",
	}
	for useLine, pairs := range fnsByLine {
		for p := range pairs {
			if name, bad := forbiddenFnLines[p.fnLine]; bad {
				t.Errorf("fixture %s: useLine=%d resolved to fnLine=%d (%s decoy) — "+
					"predicate is leaking into a non-registered handler",
					fixture, useLine, p.fnLine, name)
			}
		}
	}
	// Also assert the named and inline rows' fnLines are disjoint —
	// catches a fn-id collision that somehow preserves fn diversity
	// (vanishingly unlikely, but cheap).
	namedFnLines := map[int64]struct{}{}
	for p := range namedFns {
		namedFnLines[p.fnLine] = struct{}{}
	}
	for p := range inlineFns {
		if _, collides := namedFnLines[p.fnLine]; collides {
			t.Errorf("fixture %s: named and inline handlers share fnLine=%d — "+
				"expected distinct handler decl sites", fixture, p.fnLine)
		}
	}
}

// TestExpressHandlerArgUse_MethodFilter_AppOnNegative — MINOR 2.
// `app.on('evt', otherHandler)` registers a handler via a method
// (`on`) that is NOT in the ExpressHandlerArgUse allow-list
// (get|post|put|delete|patch|use). Its parameter uses (lines 81, 82)
// must produce zero rows. If this test starts failing, the method
// filter in bridge/tsq_express.qll has been widened — review the
// allow-list change is intentional.
func TestExpressHandlerArgUse_MethodFilter_AppOnNegative(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping extraction-heavy integration test in short mode")
	}

	fixture := "testdata/projects/express-handler-arg-use"
	rows := runExpressHandlerArgUseQuery(t, fixture)

	forbidden := []expressUseRow{
		{usePath: "app.ts", useLine: 81, paramIdx: 0},
		{usePath: "app.ts", useLine: 82, paramIdx: 1},
	}
	for _, bad := range forbidden {
		if containsExpressUseRow(rows, bad) {
			t.Errorf("fixture %s: app.on handler param use %+v leaked into "+
				"ExpressHandlerArgUse — method filter is broken; rows:\n%+v",
				fixture, bad, rows)
		}
	}
}
