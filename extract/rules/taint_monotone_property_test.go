package rules

import (
	"context"
	"fmt"
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/eval"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
	"pgregory.net/rapid"
)

// TestPropertyTaintMonotonicity is a go test entry point; the actual property
// lives in testPropertyTaintMonotonicityBody so it can be called from both
// this harness and any future coverage driver.
var _ testing.TB = (*testing.T)(nil)

// TestPropertyTaintMonotonicity checks that adding more TaintSource, TaintSink,
// ExprMayRef, or FlowStar-seed (Assign) tuples can only add TaintAlert rows,
// never remove them.
//
// The property explicitly EXCLUDES sanitizer-related inputs (Sanitizer,
// SymbolType, NonTaintableType), because those can legitimately block flow —
// adding one can remove a TaintAlert by design. Adding more "taint-enabling"
// facts to a positive (non-sanitizer) input must be monotone on TaintAlert.
//
// Real-world bug class caught: a rule in the taint stack accidentally using
// stratified negation in a non-monotone position (e.g. deriving an "unsafe"
// predicate via `not SomeTaintPred(...)`), which was exactly the risk we fixed
// in PR #36 (type-based sanitizers). An empirical monotonicity check would
// catch a broken SanitizedEdge derivation or any future regression where a
// positive rule accidentally reads through a negation it shouldn't.
func TestPropertyTaintMonotonicity(t *testing.T) {
	var baseAlertsSeen, extOnlyAlertsSeen int
	rapid.Check(t, func(t *rapid.T) {
		seed := genTaintSeed(t)

		// Evaluate the base program.
		baseAlerts := evalTaintAlerts(t, seed)
		if len(baseAlerts) > 0 {
			baseAlertsSeen++
		}

		// Pick a "category" of fact to add and generate an additional tuple.
		category := rapid.SampledFrom([]string{
			"TaintSource", "TaintSink", "ExprMayRef", "Assign",
		}).Draw(t, "category")

		extended := cloneSeed(seed)
		addTaintFact(t, extended, category)

		extAlerts := evalTaintAlerts(t, extended)
		if len(extAlerts) > len(baseAlerts) {
			extOnlyAlertsSeen++
		}

		// Every base alert must still appear in the extended run.
		extSet := make(map[string]bool, len(extAlerts))
		for _, a := range extAlerts {
			extSet[a] = true
		}
		for _, a := range baseAlerts {
			if !extSet[a] {
				t.Fatalf("monotonicity violated: alert %q present in base but absent after adding %s tuple\nbase: %v\next:  %v",
					a, category, baseAlerts, extAlerts)
			}
		}
	})

	// Guard against a vacuous property: if we never saw a non-empty alert set
	// in either the base or the extended run, the generator never exercised
	// the code under test. Fail loudly rather than silently passing.
	if baseAlertsSeen == 0 && extOnlyAlertsSeen == 0 {
		t.Fatalf("property is vacuous: no iteration produced any TaintAlert — generator does not exercise taint rules")
	}
}

// taintSeed is a bag of tuples for the relations the monotonicity property
// mutates. Other relations stay empty (taintBaseRels handles that).
type taintSeed struct {
	taintSource [][2]eval.Value // (expr, kind)
	taintSink   [][2]eval.Value // (expr, kind)
	exprMayRef  [][2]eval.Value // (expr, sym)
	assign      [][3]eval.Value // (node, rhsExpr, lhsSym)
	symInFn     [][2]eval.Value // (sym, fn)
}

func cloneSeed(s *taintSeed) *taintSeed {
	out := &taintSeed{
		taintSource: append([][2]eval.Value(nil), s.taintSource...),
		taintSink:   append([][2]eval.Value(nil), s.taintSink...),
		exprMayRef:  append([][2]eval.Value(nil), s.exprMayRef...),
		assign:      append([][3]eval.Value(nil), s.assign...),
		symInFn:     append([][2]eval.Value(nil), s.symInFn...),
	}
	return out
}

func genTaintSeed(t *rapid.T) *taintSeed {
	s := &taintSeed{}

	// Work in small integer ID pools so joins actually fire.
	// exprPool and symPool are deliberately overlapping small ranges — this
	// maximises the chance that a TaintSource expression is also referenced
	// by an ExprMayRef tuple, so the taint rules have something to propagate.
	exprID := func(name string) eval.IntVal {
		return iv(int64(100 + rapid.IntRange(0, 9).Draw(t, name)))
	}
	symID := func(name string) eval.IntVal {
		return iv(int64(10 + rapid.IntRange(0, 7).Draw(t, name)))
	}
	fnID := func(name string) eval.IntVal {
		return iv(int64(1 + rapid.IntRange(0, 2).Draw(t, name)))
	}
	kindV := func(name string) eval.StrVal {
		return sv(rapid.SampledFrom([]string{"http_input", "env", "user"}).Draw(t, name))
	}

	nSource := rapid.IntRange(1, 3).Draw(t, "nSource")
	for i := 0; i < nSource; i++ {
		s.taintSource = append(s.taintSource, [2]eval.Value{
			exprID(fmt.Sprintf("srcExpr_%d", i)),
			kindV(fmt.Sprintf("srcKind_%d", i)),
		})
	}

	nSink := rapid.IntRange(1, 3).Draw(t, "nSink")
	for i := 0; i < nSink; i++ {
		s.taintSink = append(s.taintSink, [2]eval.Value{
			exprID(fmt.Sprintf("sinkExpr_%d", i)),
			kindV(fmt.Sprintf("sinkKind_%d", i)),
		})
	}

	nRef := rapid.IntRange(2, 8).Draw(t, "nRef")
	for i := 0; i < nRef; i++ {
		s.exprMayRef = append(s.exprMayRef, [2]eval.Value{
			exprID(fmt.Sprintf("refExpr_%d", i)),
			symID(fmt.Sprintf("refSym_%d", i)),
		})
	}

	nAssign := rapid.IntRange(0, 4).Draw(t, "nAssign")
	for i := 0; i < nAssign; i++ {
		s.assign = append(s.assign, [3]eval.Value{
			iv(int64(300 + i)),
			exprID(fmt.Sprintf("assignRhs_%d", i)),
			symID(fmt.Sprintf("assignLhs_%d", i)),
		})
	}

	// Register every sym used above in a function so FlowStar can fire.
	// Use a small number of fns so syms co-locate and flow edges compose.
	regSyms := make(map[eval.Value]bool)
	for _, r := range s.exprMayRef {
		regSyms[r[1]] = true
	}
	for _, a := range s.assign {
		regSyms[a[2]] = true
	}
	i := 0
	for sym := range regSyms {
		s.symInFn = append(s.symInFn, [2]eval.Value{
			sym, fnID(fmt.Sprintf("symFn_%d", i)),
		})
		i++
	}

	return s
}

// addTaintFact mutates seed in place, adding one new tuple to the named
// category. The added tuple must be genuinely new (not already present),
// otherwise the "extended" run is identical to the base and the property
// collapses to a tautology.
func addTaintFact(t *rapid.T, seed *taintSeed, category string) {
	switch category {
	case "TaintSource":
		for attempt := 0; attempt < 16; attempt++ {
			expr := iv(int64(100 + rapid.IntRange(0, 15).Draw(t, fmt.Sprintf("newSrcExpr_%d", attempt))))
			kind := sv(rapid.SampledFrom([]string{"http_input", "env", "user", "rpc"}).Draw(t, fmt.Sprintf("newSrcKind_%d", attempt)))
			tup := [2]eval.Value{expr, kind}
			if !containsPair(seed.taintSource, tup) {
				seed.taintSource = append(seed.taintSource, tup)
				return
			}
		}
		// Fallback: force a novel one using a distinct ID range.
		seed.taintSource = append(seed.taintSource, [2]eval.Value{iv(999), sv("__novel__")})
	case "TaintSink":
		for attempt := 0; attempt < 16; attempt++ {
			expr := iv(int64(100 + rapid.IntRange(0, 15).Draw(t, fmt.Sprintf("newSinkExpr_%d", attempt))))
			kind := sv(rapid.SampledFrom([]string{"sql", "cmd", "path"}).Draw(t, fmt.Sprintf("newSinkKind_%d", attempt)))
			tup := [2]eval.Value{expr, kind}
			if !containsPair(seed.taintSink, tup) {
				seed.taintSink = append(seed.taintSink, tup)
				return
			}
		}
		seed.taintSink = append(seed.taintSink, [2]eval.Value{iv(998), sv("__novel_sink__")})
	case "ExprMayRef":
		for attempt := 0; attempt < 16; attempt++ {
			expr := iv(int64(100 + rapid.IntRange(0, 15).Draw(t, fmt.Sprintf("newRefExpr_%d", attempt))))
			sym := iv(int64(10 + rapid.IntRange(0, 9).Draw(t, fmt.Sprintf("newRefSym_%d", attempt))))
			tup := [2]eval.Value{expr, sym}
			if !containsPair(seed.exprMayRef, tup) {
				seed.exprMayRef = append(seed.exprMayRef, tup)
				// Keep the new sym in some function so flow rules can use it.
				seed.symInFn = append(seed.symInFn, [2]eval.Value{sym, iv(1)})
				return
			}
		}
	case "Assign":
		// Assign seeds LocalFlow, which feeds FlowStar. Adding an Assign tuple
		// can only add flow edges, not remove them, so it is monotone on alerts.
		for attempt := 0; attempt < 16; attempt++ {
			node := iv(int64(500 + rapid.IntRange(0, 30).Draw(t, fmt.Sprintf("newAssignNode_%d", attempt))))
			rhs := iv(int64(100 + rapid.IntRange(0, 15).Draw(t, fmt.Sprintf("newAssignRhs_%d", attempt))))
			lhs := iv(int64(10 + rapid.IntRange(0, 9).Draw(t, fmt.Sprintf("newAssignLhs_%d", attempt))))
			tup := [3]eval.Value{node, rhs, lhs}
			if !containsTriple(seed.assign, tup) {
				seed.assign = append(seed.assign, tup)
				seed.symInFn = append(seed.symInFn, [2]eval.Value{lhs, iv(1)})
				return
			}
		}
	}
}

func containsPair(xs [][2]eval.Value, t [2]eval.Value) bool {
	for _, x := range xs {
		if x == t {
			return true
		}
	}
	return false
}
func containsTriple(xs [][3]eval.Value, t [3]eval.Value) bool {
	for _, x := range xs {
		if x == t {
			return true
		}
	}
	return false
}

// evalTaintAlerts runs AllSystemRules over the seeded base relations and
// returns the TaintAlert rows as sorted strings. Any failure to plan or
// evaluate is a hard test failure — not a property violation, but an infra
// problem with the seed.
func evalTaintAlerts(t *rapid.T, seed *taintSeed) []string {
	// Build the base relations from the seed.
	srcVals := make([]eval.Value, 0, 2*len(seed.taintSource))
	for _, x := range seed.taintSource {
		srcVals = append(srcVals, x[0], x[1])
	}
	sinkVals := make([]eval.Value, 0, 2*len(seed.taintSink))
	for _, x := range seed.taintSink {
		sinkVals = append(sinkVals, x[0], x[1])
	}
	refVals := make([]eval.Value, 0, 2*len(seed.exprMayRef))
	for _, x := range seed.exprMayRef {
		refVals = append(refVals, x[0], x[1])
	}
	assignVals := make([]eval.Value, 0, 3*len(seed.assign))
	for _, x := range seed.assign {
		assignVals = append(assignVals, x[0], x[1], x[2])
	}
	sifVals := make([]eval.Value, 0, 2*len(seed.symInFn))
	for _, x := range seed.symInFn {
		sifVals = append(sifVals, x[0], x[1])
	}

	base := taintBaseRels(map[string]*eval.Relation{
		"TaintSource":   makeRel("TaintSource", 2, srcVals...),
		"TaintSink":     makeRel("TaintSink", 2, sinkVals...),
		"ExprMayRef":    makeRel("ExprMayRef", 2, refVals...),
		"Assign":        makeRel("Assign", 3, assignVals...),
		"SymInFunction": makeRel("SymInFunction", 2, sifVals...),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("srcExpr"), v("sinkExpr"), v("srcKind"), v("sinkKind")},
		Body:   []datalog.Literal{pos("TaintAlert", v("srcExpr"), v("sinkExpr"), v("srcKind"), v("sinkKind"))},
	}

	prog := &datalog.Program{Rules: AllSystemRules(), Query: query}
	ep, errs := plan.Plan(prog, nil)
	if len(errs) > 0 {
		t.Fatalf("plan: %v", errs)
	}
	rs, err := eval.Evaluate(context.Background(), ep, base)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}

	out := make([]string, 0, len(rs.Rows))
	for _, row := range rs.Rows {
		out = append(out, fmt.Sprintf("%v", row))
	}
	return out
}
