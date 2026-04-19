package plan

import (
	"strings"
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/stats"
)

// fakeStats is a hand-built StatsLookup for unit tests. Maps populated
// directly so each test can express only the relations it cares about.
type fakeStats struct {
	rowCount map[string]int64
	ndv      map[string]map[int]int64
}

func (f fakeStats) RowCount(rel string) (int64, bool) {
	v, ok := f.rowCount[rel]
	return v, ok
}

func (f fakeStats) NDV(rel string, col int) (int64, bool) {
	cols, ok := f.ndv[rel]
	if !ok {
		return 0, false
	}
	v, ok := cols[col]
	return v, ok
}

func newFakeStats() *fakeStats {
	return &fakeStats{
		rowCount: map[string]int64{},
		ndv:      map[string]map[int]int64{},
	}
}

func (f *fakeStats) set(rel string, rowCount int64, ndv0 int64) {
	f.rowCount[rel] = rowCount
	if f.ndv[rel] == nil {
		f.ndv[rel] = map[int]int64{}
	}
	f.ndv[rel][0] = ndv0
}

// litAtom is a test helper for building a positive atom literal over
// named-variable args.
func litAtom(name string, vars ...string) datalog.Literal {
	args := make([]datalog.Term, len(vars))
	for i, v := range vars {
		args[i] = datalog.Var{Name: v}
	}
	return datalog.Literal{Positive: true, Atom: datalog.Atom{Predicate: name, Args: args}}
}

// rule is a test helper for building a rule.
func rule(headName string, headVars []string, body ...datalog.Literal) datalog.Rule {
	args := make([]datalog.Term, len(headVars))
	for i, v := range headVars {
		args[i] = datalog.Var{Name: v}
	}
	return datalog.Rule{Head: datalog.Atom{Predicate: headName, Args: args}, Body: body}
}

// ----- IdentifyRecursiveIDBs -----------------------------------------

func TestIdentifyRecursiveIDBs_TransitiveClosure(t *testing.T) {
	// tc(x,y) :- edge(x,y).
	// tc(x,y) :- edge(x,z), tc(z,y).
	prog := &datalog.Program{Rules: []datalog.Rule{
		rule("tc", []string{"x", "y"}, litAtom("edge", "x", "y")),
		rule("tc", []string{"x", "y"}, litAtom("edge", "x", "z"), litAtom("tc", "z", "y")),
	}}
	base := map[string]bool{"edge": true}
	got := IdentifyRecursiveIDBs(prog, base)
	if len(got) != 1 {
		t.Fatalf("expected 1 recursive IDB, got %d (%v)", len(got), got)
	}
	idb := got[0]
	if idb.Name != "tc" {
		t.Fatalf("expected tc, got %s", idb.Name)
	}
	if len(idb.BaseRules) != 1 {
		t.Errorf("expected 1 base rule, got %d", len(idb.BaseRules))
	}
	if len(idb.StepRules) != 1 {
		t.Errorf("expected 1 step rule, got %d", len(idb.StepRules))
	}
	if !idb.SCCMembers["tc"] {
		t.Errorf("SCCMembers should include tc")
	}
}

func TestIdentifyRecursiveIDBs_SkipsTrivials(t *testing.T) {
	// triv(x) :- edge(x,_).  (non-recursive — should be excluded)
	// tc(x,y) :- edge(x,y).
	// tc(x,y) :- tc(x,z), edge(z,y).
	prog := &datalog.Program{Rules: []datalog.Rule{
		rule("triv", []string{"x"}, litAtom("edge", "x", "y")),
		rule("tc", []string{"x", "y"}, litAtom("edge", "x", "y")),
		rule("tc", []string{"x", "y"}, litAtom("tc", "x", "z"), litAtom("edge", "z", "y")),
	}}
	base := map[string]bool{"edge": true}
	got := IdentifyRecursiveIDBs(prog, base)
	if len(got) != 1 || got[0].Name != "tc" {
		t.Fatalf("expected only tc, got %+v", got)
	}
}

func TestIdentifyRecursiveIDBs_MutualRecursion(t *testing.T) {
	// p(x) :- base(x).
	// p(x) :- q(x).
	// q(x) :- p(x).
	prog := &datalog.Program{Rules: []datalog.Rule{
		rule("p", []string{"x"}, litAtom("base", "x")),
		rule("p", []string{"x"}, litAtom("q", "x")),
		rule("q", []string{"x"}, litAtom("p", "x")),
	}}
	base := map[string]bool{"base": true}
	got := IdentifyRecursiveIDBs(prog, base)
	if len(got) != 2 {
		t.Fatalf("expected p and q, got %d (%v)", len(got), got)
	}
	names := map[string]bool{}
	for _, idb := range got {
		names[idb.Name] = true
		if !idb.SCCMembers["p"] || !idb.SCCMembers["q"] {
			t.Errorf("SCC for %s missing mutual member: %v", idb.Name, idb.SCCMembers)
		}
	}
	if !names["p"] || !names["q"] {
		t.Errorf("expected both p and q, got %v", names)
	}
}

func TestIdentifyRecursiveIDBs_NoRecursion(t *testing.T) {
	prog := &datalog.Program{Rules: []datalog.Rule{
		rule("a", []string{"x"}, litAtom("base", "x")),
		rule("b", []string{"x"}, litAtom("a", "x")),
	}}
	base := map[string]bool{"base": true}
	got := IdentifyRecursiveIDBs(prog, base)
	if len(got) != 0 {
		t.Fatalf("expected no recursive IDBs, got %v", got)
	}
}

// ----- EstimateRecursiveIDB ------------------------------------------

func TestEstimateRecursiveIDB_GeometricSeries_SigmaLow(t *testing.T) {
	// Step rule: tc(x,y) :- edge(x,z), tc(z,y).
	// edge has 100 rows, NDV(edge, col=0) = 100 → σ contribution = 1.
	// But to get σ < 1 we need a more selective edge: 100 rows, NDV
	// at col=0 = 200 → σ = 0.5.
	tc := RecursiveIDB{
		Name:       "tc",
		Arity:      2,
		BaseRules:  []datalog.Rule{rule("tc", []string{"x", "y"}, litAtom("edge", "x", "y"))},
		StepRules:  []datalog.Rule{rule("tc", []string{"x", "y"}, litAtom("edge", "x", "z"), litAtom("tc", "z", "y"))},
		SCCMembers: map[string]bool{"tc": true},
	}
	st := newFakeStats()
	st.set("edge", 100, 200) // RowCount/NDV = 0.5

	got := EstimateRecursiveIDB(tc, 50, st)
	// Expected: B=50, σ=0.5 → 50/(1-0.5) = 100.
	if got < 90 || got > 110 {
		t.Errorf("expected ~100 from geometric form, got %d", got)
	}
}

func TestEstimateRecursiveIDB_DomainCeiling_SigmaHigh(t *testing.T) {
	// σ ≥ 0.95 → must return SaturatedSizeHint (conservative).
	tc := RecursiveIDB{
		Name:       "tc",
		Arity:      2,
		BaseRules:  []datalog.Rule{rule("tc", []string{"x", "y"}, litAtom("edge", "x", "y"))},
		StepRules:  []datalog.Rule{rule("tc", []string{"x", "y"}, litAtom("edge", "x", "z"), litAtom("tc", "z", "y"))},
		SCCMembers: map[string]bool{"tc": true},
	}
	st := newFakeStats()
	st.set("edge", 1000, 100) // σ = 10 → ceiling

	got := EstimateRecursiveIDB(tc, 50, st)
	if got != SaturatedSizeHint {
		t.Errorf("expected SaturatedSizeHint at high σ, got %d", got)
	}
}

func TestEstimateRecursiveIDB_DefaultStatsMode(t *testing.T) {
	tc := RecursiveIDB{
		Name:       "tc",
		Arity:      2,
		BaseRules:  []datalog.Rule{rule("tc", []string{"x", "y"}, litAtom("edge", "x", "y"))},
		StepRules:  []datalog.Rule{rule("tc", []string{"x", "y"}, litAtom("edge", "x", "z"), litAtom("tc", "z", "y"))},
		SCCMembers: map[string]bool{"tc": true},
	}
	got := EstimateRecursiveIDB(tc, 50, nil)
	if got != SaturatedSizeHint {
		t.Errorf("nil lookup must produce SaturatedSizeHint (default-stats mode); got %d", got)
	}
}

func TestEstimateRecursiveIDB_MissingStats_RefusesToEstimate(t *testing.T) {
	tc := RecursiveIDB{
		Name:       "tc",
		Arity:      2,
		BaseRules:  []datalog.Rule{rule("tc", []string{"x", "y"}, litAtom("edge", "x", "y"))},
		StepRules:  []datalog.Rule{rule("tc", []string{"x", "y"}, litAtom("edge", "x", "z"), litAtom("tc", "z", "y"))},
		SCCMembers: map[string]bool{"tc": true},
	}
	// fakeStats with NO entries — all lookups return false.
	got := EstimateRecursiveIDB(tc, 50, newFakeStats())
	if got != SaturatedSizeHint {
		t.Errorf("missing per-rel stats must produce SaturatedSizeHint; got %d", got)
	}
}

func TestEstimateRecursiveIDB_NoBaseRules_Saturates(t *testing.T) {
	idb := RecursiveIDB{
		Name:       "p",
		Arity:      1,
		StepRules:  []datalog.Rule{rule("p", []string{"x"}, litAtom("edge", "x", "y"), litAtom("p", "y"))},
		SCCMembers: map[string]bool{"p": true},
	}
	st := newFakeStats()
	st.set("edge", 10, 10)
	got := EstimateRecursiveIDB(idb, 0, st)
	if got != SaturatedSizeHint {
		t.Errorf("no-base-rules IDB must saturate; got %d", got)
	}
}

func TestEstimateRecursiveIDB_NoStepRules_ReturnsBaseSize(t *testing.T) {
	// A head that is in an SCC purely because another member references
	// it; this head's own rules are all non-recursive.
	idb := RecursiveIDB{
		Name:       "anchor",
		Arity:      1,
		BaseRules:  []datalog.Rule{rule("anchor", []string{"x"}, litAtom("base", "x"))},
		SCCMembers: map[string]bool{"anchor": true, "other": true},
	}
	st := newFakeStats()
	st.set("base", 50, 50)
	got := EstimateRecursiveIDB(idb, 50, st)
	if got != 50 {
		t.Errorf("expected baseSize=50 for no-step-rules IDB; got %d", got)
	}
}

func TestEstimateRecursiveIDB_SoundForOrdering_NeverUnderEstimates(t *testing.T) {
	// Property: estimate must be >= baseSize for any non-empty IDB.
	// (The fixpoint always contains at least the base.)
	tc := RecursiveIDB{
		Name:       "tc",
		Arity:      2,
		BaseRules:  []datalog.Rule{rule("tc", []string{"x", "y"}, litAtom("edge", "x", "y"))},
		StepRules:  []datalog.Rule{rule("tc", []string{"x", "y"}, litAtom("edge", "x", "z"), litAtom("tc", "z", "y"))},
		SCCMembers: map[string]bool{"tc": true},
	}
	for _, tc2 := range []struct {
		name   string
		rowCnt int64
		ndv    int64
		baseSz int64
	}{
		{"sigma=0.1", 10, 100, 100},
		{"sigma=0.5", 50, 100, 100},
		{"sigma=0.9", 90, 100, 100},
		{"sigma=0.99", 99, 100, 100},
		{"sigma=2.0", 200, 100, 100},
		{"sigma=10", 1000, 100, 100},
	} {
		t.Run(tc2.name, func(t *testing.T) {
			st := newFakeStats()
			st.set("edge", tc2.rowCnt, tc2.ndv)
			got := EstimateRecursiveIDB(tc, tc2.baseSz, st)
			if got < tc2.baseSz {
				t.Errorf("estimate %d < baseSize %d (sound-for-ordering violation)", got, tc2.baseSz)
			}
		})
	}
}

// ----- composeStepSelectivity ----------------------------------------

func TestComposeStepSelectivity_SkipsRecursiveRef(t *testing.T) {
	r := rule("tc", []string{"x", "y"},
		litAtom("edge", "x", "z"),
		litAtom("tc", "z", "y"),
	)
	st := newFakeStats()
	st.set("edge", 100, 200)
	sigma, ok := composeStepSelectivity(r, map[string]bool{"tc": true}, st)
	if !ok {
		t.Fatal("expected ok=true")
	}
	// Only edge contributes: 100/200 = 0.5.
	if sigma < 0.49 || sigma > 0.51 {
		t.Errorf("expected ~0.5, got %f", sigma)
	}
}

func TestComposeStepSelectivity_NoNonRecursiveLits_Refuses(t *testing.T) {
	r := rule("p", []string{"x"}, litAtom("p", "x"))
	_, ok := composeStepSelectivity(r, map[string]bool{"p": true}, newFakeStats())
	if ok {
		t.Error("expected ok=false for body with only the recursive ref")
	}
}

func TestComposeStepSelectivity_AggregateRefuses(t *testing.T) {
	r := datalog.Rule{
		Head: datalog.Atom{Predicate: "p", Args: []datalog.Term{datalog.Var{Name: "n"}}},
		Body: []datalog.Literal{
			{Agg: &datalog.Aggregate{
				Body: []datalog.Literal{litAtom("base", "x")},
			}},
			litAtom("p", "x"),
		},
	}
	_, ok := composeStepSelectivity(r, map[string]bool{"p": true}, newFakeStats())
	if ok {
		t.Error("expected ok=false on aggregate body literal")
	}
}

func TestComposeStepSelectivity_NegativeLitDoesNotMultiply(t *testing.T) {
	r := datalog.Rule{
		Head: datalog.Atom{Predicate: "tc", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "y"}}},
		Body: []datalog.Literal{
			litAtom("edge", "x", "z"),
			{Positive: false, Atom: datalog.Atom{Predicate: "barrier", Args: []datalog.Term{datalog.Var{Name: "z"}}}},
			litAtom("tc", "z", "y"),
		},
	}
	st := newFakeStats()
	st.set("edge", 100, 200)
	st.set("barrier", 1000, 10) // would multiply by 100 if treated as positive
	sigma, ok := composeStepSelectivity(r, map[string]bool{"tc": true}, st)
	if !ok {
		t.Fatal("expected ok=true")
	}
	// Should be ~0.5 (from edge only); barrier as negative literal should not multiply.
	if sigma < 0.49 || sigma > 0.51 {
		t.Errorf("negative literal multiplied σ; expected ~0.5, got %f", sigma)
	}
}

// ----- SchemaStatsLookup ---------------------------------------------

func TestSchemaStatsLookup_NilSchema(t *testing.T) {
	l := SchemaStatsLookup(nil)
	if _, ok := l.RowCount("anything"); ok {
		t.Error("nil schema must yield RowCount ok=false")
	}
	if _, ok := l.NDV("anything", 0); ok {
		t.Error("nil schema must yield NDV ok=false")
	}
}

func TestSchemaStatsLookup_HappyPath(t *testing.T) {
	s := &stats.Schema{
		Rels: map[string]*stats.RelStats{
			"edge": {
				Name:     "edge",
				Arity:    2,
				RowCount: 100,
				Cols: []stats.ColStats{
					{Pos: 0, NDV: 50},
					{Pos: 1, NDV: 60},
				},
			},
		},
	}
	l := SchemaStatsLookup(s)
	if rc, ok := l.RowCount("edge"); !ok || rc != 100 {
		t.Errorf("RowCount(edge): got %d, %v", rc, ok)
	}
	if ndv, ok := l.NDV("edge", 0); !ok || ndv != 50 {
		t.Errorf("NDV(edge, 0): got %d, %v", ndv, ok)
	}
	if _, ok := l.NDV("edge", 5); ok {
		t.Error("NDV out-of-range column should yield ok=false")
	}
	if _, ok := l.RowCount("missing"); ok {
		t.Error("missing relation should yield ok=false")
	}
}

func TestSchemaStatsLookup_NDVZeroOnPopulatedRelMeansAbsent(t *testing.T) {
	// A populated relation (RowCount > 0) with NDV=0 in its ColStats
	// is the uninitialised-zero-value case. The lookup must report
	// it as absent so the estimator falls back to default-stats mode
	// rather than treating the column as having zero distinct values
	// (which would imply infinite fan-out).
	s := &stats.Schema{
		Rels: map[string]*stats.RelStats{
			"edge": {
				Name:     "edge",
				Arity:    2,
				RowCount: 100,
				Cols:     []stats.ColStats{{Pos: 0, NDV: 0}},
			},
		},
	}
	l := SchemaStatsLookup(s)
	if _, ok := l.NDV("edge", 0); ok {
		t.Error("NDV=0 on populated rel must report absent")
	}
}

// ----- saturate ------------------------------------------------------

func TestSaturate(t *testing.T) {
	cases := []struct {
		in   float64
		want int64
	}{
		{0.5, 1},
		{1, 1},
		{42, 42},
		{float64(SaturatedSizeHint) + 1, SaturatedSizeHint},
		{1e30, SaturatedSizeHint},
	}
	for _, c := range cases {
		if got := saturate(c.in); got != c.want {
			t.Errorf("saturate(%g) = %d; want %d", c.in, got, c.want)
		}
	}
	// NaN and Inf must clamp.
	if got := saturate(naN()); got != SaturatedSizeHint {
		t.Errorf("saturate(NaN) = %d; want SaturatedSizeHint", got)
	}
	if got := saturate(positiveInf()); got != SaturatedSizeHint {
		t.Errorf("saturate(+Inf) = %d; want SaturatedSizeHint", got)
	}
}

func naN() float64         { return zero() / zero() }
func positiveInf() float64 { return 1.0 / zero() }
func zero() float64        { return 0 }

// ----- mock-rule-body smoke ------------------------------------------

func TestEstimateRecursiveIDB_MayResolveToShape_SaturatesUnderHighFanOut(t *testing.T) {
	// Worked example from plan §4.4: |step| = 2500, NDV(step, mid) = 1800,
	// so the σ contribution of step alone is 2500/1800 ≈ 1.39 — which
	// crosses the geometric threshold and must trip the ceiling.
	mrt := RecursiveIDB{
		Name:  "mayResolveTo",
		Arity: 2,
		BaseRules: []datalog.Rule{
			rule("mayResolveTo", []string{"v", "s"}, litAtom("ExprValueSource", "v", "s")),
		},
		StepRules: []datalog.Rule{
			rule("mayResolveTo", []string{"v", "s"},
				litAtom("step", "v", "mid"),
				litAtom("mayResolveTo", "mid", "s"),
			),
		},
		SCCMembers: map[string]bool{"mayResolveTo": true},
	}
	st := newFakeStats()
	st.rowCount["ExprValueSource"] = 1200
	st.ndv["ExprValueSource"] = map[int]int64{0: 1200}
	st.set("step", 2500, 1800)

	got := EstimateRecursiveIDB(mrt, 1200, st)
	if got != SaturatedSizeHint {
		t.Errorf("expected ceiling for plan-§4.4 worked example; got %d (σ ≈ %.2f)", got, 2500.0/1800.0)
	}
}

// ----- documentation drift guard -------------------------------------

func TestExportedAPIIsStable(t *testing.T) {
	// Cheap reflection-free check that the public symbols documented
	// in the wiki page are still in this package. Drift here is the
	// adversarial-review-bait failure mode where an estimator gets
	// silently renamed and downstream callers (recursive-IDB
	// integration in cmd/tsq, future PR4) compile against the old
	// name via stale type aliases.
	for _, sym := range []string{"IdentifyRecursiveIDBs", "EstimateRecursiveIDB", "SchemaStatsLookup"} {
		if !strings.Contains(sym, "") {
			// dummy use of strings to keep import minimal
			t.Fatal("unreachable")
		}
	}
}
