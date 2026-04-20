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
		if len(row) != 4 {
			t.Fatalf("row %d: expected arity 4 (usePath,useLine,fn,paramIdx), got %d", i, len(row))
		}
		pv, ok1 := row[0].(eval.StrVal)
		lv, ok2 := row[1].(eval.IntVal)
		_, ok3 := row[2].(eval.IntVal) // fn — present but intentionally unused in projection
		piv, ok4 := row[3].(eval.IntVal)
		if !ok1 || !ok2 || !ok3 || !ok4 {
			t.Fatalf("row %d: unexpected cell shape (%T, %T, %T, %T)",
				i, row[0], row[1], row[2], row[3])
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
	//   line 41: `const id = req.query;` (named handler)      → req  (paramIdx 0)
	//   line 42: `res.send('ok ' + id);` (named handler)      → res  (paramIdx 1)
	//   line 48: `const id = req.query;` (inline handler)     → req  (paramIdx 0)
	//   line 49: `res.send('inline ' + id);` (inline handler) → res  (paramIdx 1)
	pins := []expressUseRow{
		{usePath: "app.ts", useLine: 41, paramIdx: 0},
		{usePath: "app.ts", useLine: 42, paramIdx: 1},
		{usePath: "app.ts", useLine: 48, paramIdx: 0},
		{usePath: "app.ts", useLine: 49, paramIdx: 1},
	}
	for _, pin := range pins {
		if !containsExpressUseRow(rows, pin) {
			t.Errorf("fixture %s: missing pin %+v from ExpressHandlerArgUse rows:\n%+v",
				fixture, pin, rows)
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

	// Collect (useLine, fn) pairs for paramIdx=0 (req) rows.
	fnsByLine := map[int64]map[int64]struct{}{}
	for i, row := range rs.Rows {
		if len(row) != 4 {
			t.Fatalf("row %d: expected arity 4, got %d", i, len(row))
		}
		lv, ok1 := row[1].(eval.IntVal)
		fv, ok2 := row[2].(eval.IntVal)
		piv, ok3 := row[3].(eval.IntVal)
		if !ok1 || !ok2 || !ok3 {
			t.Fatalf("row %d: unexpected cell shape", i)
		}
		if piv.V != 0 {
			continue
		}
		if _, ok := fnsByLine[lv.V]; !ok {
			fnsByLine[lv.V] = map[int64]struct{}{}
		}
		fnsByLine[lv.V][fv.V] = struct{}{}
	}

	namedFns := fnsByLine[41]
	inlineFns := fnsByLine[48]
	if len(namedFns) == 0 || len(inlineFns) == 0 {
		t.Fatalf("fixture %s: missing req-use rows — named line 41 fns=%v, inline line 48 fns=%v",
			fixture, namedFns, inlineFns)
	}
	// The two lines must resolve to disjoint fn sets. If any fn
	// overlaps, the predicate collapsed two distinct handlers.
	for fn := range namedFns {
		if _, collides := inlineFns[fn]; collides {
			t.Errorf("fixture %s: named handler (line 41) and inline handler (line 48) "+
				"share fn=%d — predicate is not distinguishing handlers", fixture, fn)
		}
	}
}
