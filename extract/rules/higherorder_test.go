package rules

import (
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/eval"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

// higherOrderBaseRels returns the base relations needed for higher-order rules evaluation.
func higherOrderBaseRels(overrides map[string]*eval.Relation) map[string]*eval.Relation {
	base := taintBaseRels(nil)
	base["FieldRead"] = eval.NewRelation("FieldRead", 4)
	base["Function"] = eval.NewRelation("Function", 6)
	base["JsxElement"] = eval.NewRelation("JsxElement", 3)
	base["JsxAttribute"] = eval.NewRelation("JsxAttribute", 3)
	for k, v := range overrides {
		base[k] = v
	}
	return base
}

// TestArrayMap_FlowToCallback tests that array.map(cb) flows array elements to callback param.
func TestArrayMap_FlowToCallback(t *testing.T) {
	// arr.map(callback): MethodCall(call=500, arrayExpr=400, "map"),
	// ExprMayRef(400, arraySym=40), CallArg(500, 0, cbExpr=600),
	// ExprMayRef(600, cbSym=60), FunctionSymbol(60, cbFn=7),
	// Parameter(7, 0, "item", _, paramSym=70, _).
	baseRels := higherOrderBaseRels(map[string]*eval.Relation{
		"MethodCall":     makeRel("MethodCall", 3, iv(500), iv(400), sv("map")),
		"ExprMayRef":     makeRel("ExprMayRef", 2, iv(400), iv(40), iv(600), iv(60)),
		"CallArg":        makeRel("CallArg", 3, iv(500), iv(0), iv(600)),
		"FunctionSymbol": makeRel("FunctionSymbol", 2, iv(60), iv(7)),
		"Parameter":      makeRel("Parameter", 6, iv(7), iv(0), sv("item"), iv(80), iv(70), sv("")),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("src"), v("dst")},
		Body:   []datalog.Literal{pos("InterFlow", v("src"), v("dst"))},
	}

	rs := planAndEval(t, AllSystemRules(), query, baseRels)
	if !resultContains(rs, iv(40), iv(70)) {
		t.Errorf("expected InterFlow(40, 70) from array.map, got %v", rs.Rows)
	}
}

// TestPromiseThen_FlowToCallback tests that promise.then(cb) flows promise value to callback param.
func TestPromiseThen_FlowToCallback(t *testing.T) {
	// promise.then(callback): MethodCall(call=500, promiseExpr=400, "then"),
	// ExprMayRef(400, promiseSym=40), CallArg(500, 0, cbExpr=600),
	// ExprMayRef(600, cbSym=60), FunctionSymbol(60, cbFn=7),
	// Parameter(7, 0, "value", _, paramSym=70, _).
	baseRels := higherOrderBaseRels(map[string]*eval.Relation{
		"MethodCall":     makeRel("MethodCall", 3, iv(500), iv(400), sv("then")),
		"ExprMayRef":     makeRel("ExprMayRef", 2, iv(400), iv(40), iv(600), iv(60)),
		"CallArg":        makeRel("CallArg", 3, iv(500), iv(0), iv(600)),
		"FunctionSymbol": makeRel("FunctionSymbol", 2, iv(60), iv(7)),
		"Parameter":      makeRel("Parameter", 6, iv(7), iv(0), sv("value"), iv(80), iv(70), sv("")),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("src"), v("dst")},
		Body:   []datalog.Literal{pos("InterFlow", v("src"), v("dst"))},
	}

	rs := planAndEval(t, AllSystemRules(), query, baseRels)
	if !resultContains(rs, iv(40), iv(70)) {
		t.Errorf("expected InterFlow(40, 70) from promise.then, got %v", rs.Rows)
	}
}

// TestNoFlow_WrongMethodName tests that no InterFlow is produced for non-matching method names.
func TestNoFlow_WrongMethodName(t *testing.T) {
	// arr.slice(callback) — should NOT produce InterFlow.
	baseRels := higherOrderBaseRels(map[string]*eval.Relation{
		"MethodCall":     makeRel("MethodCall", 3, iv(500), iv(400), sv("slice")),
		"ExprMayRef":     makeRel("ExprMayRef", 2, iv(400), iv(40), iv(600), iv(60)),
		"CallArg":        makeRel("CallArg", 3, iv(500), iv(0), iv(600)),
		"FunctionSymbol": makeRel("FunctionSymbol", 2, iv(60), iv(7)),
		"Parameter":      makeRel("Parameter", 6, iv(7), iv(0), sv("item"), iv(80), iv(70), sv("")),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("src"), v("dst")},
		Body:   []datalog.Literal{pos("InterFlow", v("src"), v("dst"))},
	}

	rs := planAndEval(t, AllSystemRules(), query, baseRels)
	// InterFlow from higher-order rules should not include (40, 70)
	if resultContains(rs, iv(40), iv(70)) {
		t.Errorf("expected no InterFlow(40, 70) for slice, got %v", rs.Rows)
	}
}

// TestHigherOrderRulesValidate verifies all higher-order rules pass validation.
func TestHigherOrderRulesValidate(t *testing.T) {
	for i, r := range HigherOrderRules() {
		errs := plan.ValidateRule(r)
		if len(errs) > 0 {
			t.Errorf("rule %d (%s) validation errors: %v", i, r.Head.Predicate, errs)
		}
	}
}

// TestHigherOrderRulesStratify verifies higher-order rules stratify with all system rules.
func TestHigherOrderRulesStratify(t *testing.T) {
	prog := &datalog.Program{Rules: AllSystemRules()}
	_, errs := plan.Plan(prog, nil)
	if len(errs) > 0 {
		t.Fatalf("all system rules (including higher-order) failed to plan: %v", errs)
	}
}
