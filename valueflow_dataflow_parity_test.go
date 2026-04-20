package integration_test

import (
	"sort"
	"testing"
)

// Phase D PR1 — parity tests for the additive `mayResolveTo` surface
// re-exported via `tsq::dataflow`.
//
// The new bridge surface is a pure re-export of the system
// `MayResolveTo` relation that already backs `mayResolveToRec` in
// `tsq::valueflow`. Both surfaces must return identical row sets on
// every fixture — any drift is a bug in the re-export wiring.
//
// Regression-guard discipline (per wiki §Phase C PR4/PR6/PR7 briefing
// rules (a)-(g)):
//
//   (a) Non-zero real-fixture assertion — each fixture asserts a
//       positive row count before the parity check. A green parity
//       test with both sides at zero is meaningless.
//   (b) Per-kind floor at ~50% of observed — not applicable here
//       because parity IS the regression guard; the floor is baked
//       in via the non-zero assertion.
//   (c) Overlap with `tsq_valueflow.qll`'s `mayResolveToRec` — this
//       is the point. The re-export is additive; both surfaces wrap
//       the same system relation. Documented in tsq_dataflow.qll's
//       file-level comment.
//   (d) No carve-outs.
//   (e) N/A — no recursive predicate body changes in this PR.
//   (f) Manifest-grep verification — no new manifest entry is added
//       in this PR (the existing `MayResolveTo` relation entry
//       continues to cover the class; `tsq_dataflow.qll` now also
//       grep-matches the relation name, but that only strengthens
//       the existing entry's grep-hit).
//   (g) N/A — no new recursive IDB.

// TestDataflowPR1_MayResolveToParity_DirectProp — primary parity
// test on the `valueflow-closure-direct-prop` fixture. Runs both the
// pre-existing `all_mayResolveToRec_located.ql` (tsq::valueflow
// surface) and the new `all_mayResolveTo_dataflow_located.ql`
// (tsq::dataflow surface); asserts the projected row sets are
// element-wise equal.
func TestDataflowPR1_MayResolveToParity_DirectProp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping extraction-heavy integration test in short mode")
	}
	assertDataflowMayResolveToParity(t,
		"testdata/projects/valueflow-closure-direct-prop")
}

// TestDataflowPR1_MayResolveToParity_ContextSpread — secondary
// parity fixture exercising a richer closure shape (spread +
// computed key). Ensures the re-export is not accidentally
// filtering a subset of step kinds.
func TestDataflowPR1_MayResolveToParity_ContextSpread(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping extraction-heavy integration test in short mode")
	}
	assertDataflowMayResolveToParity(t,
		"testdata/projects/valueflow-closure-context-spread-computed")
}

// TestDataflowPR1_MayResolveToParity_CrossModuleMultihop — third
// parity fixture exercising cross-module `ifsRetToCall` composition.
// Guards against the re-export diverging on inter-procedural
// contribution specifically.
func TestDataflowPR1_MayResolveToParity_CrossModuleMultihop(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping extraction-heavy integration test in short mode")
	}
	assertDataflowMayResolveToParity(t,
		"testdata/projects/valueflow-closure-cross-module-multihop")
}

// assertDataflowMayResolveToParity runs both the reference
// `mayResolveToRec` query (via tsq::valueflow) and the new
// `mayResolveTo` query (via tsq::dataflow) on `fixtureDir` and
// asserts the projected locRow sets are element-wise equal.
//
// Rule (a) — fails if either side produces zero rows (a green
// parity check with both sides empty is a trivial pass, not a
// meaningful regression guard). See PR4 review lesson on decorative
// guards.
func assertDataflowMayResolveToParity(t *testing.T, fixtureDir string) {
	t.Helper()

	rsRec := runClosureQuery(t,
		"testdata/queries/v2/valueflow/all_mayResolveToRec_located.ql",
		fixtureDir)
	rowsRec := projectLocatedRows(t, rsRec)

	rsDf := runClosureQuery(t,
		"testdata/queries/v2/valueflow/all_mayResolveTo_dataflow_located.ql",
		fixtureDir)
	rowsDf := projectLocatedRows(t, rsDf)

	t.Logf("fixture=%s mayResolveToRec_rows=%d mayResolveTo_dataflow_rows=%d",
		fixtureDir, len(rowsRec), len(rowsDf))

	// Rule (a): non-zero real-fixture assertion. Both sides must
	// produce rows on a real fixture before the parity claim can
	// carry any regression signal.
	if len(rowsRec) == 0 {
		t.Fatalf("fixture %s: mayResolveToRec reference produced 0 rows; "+
			"cannot establish parity baseline (rule (a))", fixtureDir)
	}
	if len(rowsDf) == 0 {
		t.Fatalf("fixture %s: dataflow mayResolveTo produced 0 rows; "+
			"re-export is broken or fixture regressed (rule (a))",
			fixtureDir)
	}

	if len(rowsRec) != len(rowsDf) {
		t.Errorf("fixture %s: row-count mismatch — mayResolveToRec=%d, "+
			"dataflow.mayResolveTo=%d. Both surfaces wrap the same "+
			"MayResolveTo system relation; any count drift is a "+
			"re-export bug.\nmayResolveToRec rows:\n%s\ndataflow rows:\n%s",
			fixtureDir, len(rowsRec), len(rowsDf),
			dumpRows(rowsRec), dumpRows(rowsDf))
		return
	}

	// Element-wise set equality on the locRow projection. Ordering
	// is not guaranteed by the evaluator, so sort both sides before
	// comparison.
	sortedRec := sortLocRows(rowsRec)
	sortedDf := sortLocRows(rowsDf)
	for i := range sortedRec {
		if sortedRec[i] != sortedDf[i] {
			t.Errorf("fixture %s: row-set mismatch at sorted index %d — "+
				"mayResolveToRec=%+v, dataflow=%+v.\nmayResolveToRec rows:\n%s\n"+
				"dataflow rows:\n%s",
				fixtureDir, i, sortedRec[i], sortedDf[i],
				dumpRows(rowsRec), dumpRows(rowsDf))
			return
		}
	}
}

// TestDataflowPR1_MayResolveToClassParity_DirectProp — class-surface
// parity. Guards the `class MayResolveTo` char pred + `getSource()`
// getter against silent regression. The predicate-surface query
// (TestDataflowPR1_MayResolveToParity_*) exercises only the sibling
// `mayResolveTo` predicate; a bug in the class's char pred or getter
// would not be caught by predicate-surface parity alone. This test
// cross-checks the class-surface row set against the predicate-
// surface row set on the same fixture — they must be identical
// because both wrap the same system `MayResolveTo` relation.
func TestDataflowPR1_MayResolveToClassParity_DirectProp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping extraction-heavy integration test in short mode")
	}
	assertDataflowMayResolveToClassParity(t,
		"testdata/projects/valueflow-closure-direct-prop")
}

// TestDataflowPR1_MayResolveToClassParity_ContextSpread — class-
// surface parity on the richer spread/computed-key fixture.
func TestDataflowPR1_MayResolveToClassParity_ContextSpread(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping extraction-heavy integration test in short mode")
	}
	assertDataflowMayResolveToClassParity(t,
		"testdata/projects/valueflow-closure-context-spread-computed")
}

// TestDataflowPR1_MayResolveToClassParity_CrossModuleMultihop —
// class-surface parity on the cross-module `ifsRetToCall` fixture.
func TestDataflowPR1_MayResolveToClassParity_CrossModuleMultihop(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping extraction-heavy integration test in short mode")
	}
	assertDataflowMayResolveToClassParity(t,
		"testdata/projects/valueflow-closure-cross-module-multihop")
}

// assertDataflowMayResolveToClassParity runs all three surfaces on
// `fixtureDir` — the reference `mayResolveToRec` predicate, the
// `mayResolveTo` predicate re-export, and the `MayResolveTo` class
// re-export — and asserts all three produce the same non-zero row
// set. This catches char pred / getter regressions that the
// predicate-surface parity test cannot.
func assertDataflowMayResolveToClassParity(t *testing.T, fixtureDir string) {
	t.Helper()

	rsRec := runClosureQuery(t,
		"testdata/queries/v2/valueflow/all_mayResolveToRec_located.ql",
		fixtureDir)
	rowsRec := projectLocatedRows(t, rsRec)

	rsClass := runClosureQuery(t,
		"testdata/queries/v2/valueflow/all_mayResolveTo_dataflow_class_located.ql",
		fixtureDir)
	rowsClass := projectLocatedRows(t, rsClass)

	t.Logf("fixture=%s mayResolveToRec_rows=%d class_rows=%d",
		fixtureDir, len(rowsRec), len(rowsClass))

	// Rule (a): non-zero real-fixture assertion on BOTH sides. A
	// passing parity with both sides empty would hide a char-pred
	// regression that drops every member of the class extent.
	if len(rowsRec) == 0 {
		t.Fatalf("fixture %s: mayResolveToRec reference produced 0 rows; "+
			"cannot establish class-surface parity baseline (rule (a))",
			fixtureDir)
	}
	if len(rowsClass) == 0 {
		t.Fatalf("fixture %s: class MayResolveTo produced 0 rows — "+
			"char pred or getSource() getter is broken (rule (a))",
			fixtureDir)
	}

	if len(rowsRec) != len(rowsClass) {
		t.Errorf("fixture %s: class-surface row-count mismatch — "+
			"mayResolveToRec=%d, class MayResolveTo=%d. Class wraps the "+
			"same MayResolveTo relation; any drift indicates char pred "+
			"or getter regression.\nrec rows:\n%s\nclass rows:\n%s",
			fixtureDir, len(rowsRec), len(rowsClass),
			dumpRows(rowsRec), dumpRows(rowsClass))
		return
	}

	sortedRec := sortLocRows(rowsRec)
	sortedClass := sortLocRows(rowsClass)
	for i := range sortedRec {
		if sortedRec[i] != sortedClass[i] {
			t.Errorf("fixture %s: class-surface row-set mismatch at "+
				"sorted index %d — rec=%+v, class=%+v.\nrec rows:\n%s\n"+
				"class rows:\n%s",
				fixtureDir, i, sortedRec[i], sortedClass[i],
				dumpRows(rowsRec), dumpRows(rowsClass))
			return
		}
	}
}

// sortLocRows returns a new slice of locRow sorted canonically so
// two equivalent row sets compare element-wise equal regardless of
// evaluator output order.
func sortLocRows(rows []locRow) []locRow {
	out := make([]locRow, len(rows))
	copy(out, rows)
	sort.Slice(out, func(i, j int) bool {
		if out[i].valueSuffix != out[j].valueSuffix {
			return out[i].valueSuffix < out[j].valueSuffix
		}
		if out[i].valueLine != out[j].valueLine {
			return out[i].valueLine < out[j].valueLine
		}
		if out[i].sourceSuffix != out[j].sourceSuffix {
			return out[i].sourceSuffix < out[j].sourceSuffix
		}
		return out[i].sourceLine < out[j].sourceLine
	})
	return out
}
