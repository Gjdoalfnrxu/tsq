package rules

import (
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/eval"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

// summaryBaseRels returns base relations needed for summary rules evaluation,
// including all relations needed by LocalFlow rules (which summaries depend on).
func summaryBaseRels(overrides map[string]*eval.Relation) map[string]*eval.Relation {
	base := map[string]*eval.Relation{
		// LocalFlow dependencies
		"Assign":           eval.NewRelation("Assign", 3),
		"ExprMayRef":       eval.NewRelation("ExprMayRef", 2),
		"SymInFunction":    eval.NewRelation("SymInFunction", 2),
		"VarDecl":          eval.NewRelation("VarDecl", 4),
		"ReturnStmt":       eval.NewRelation("ReturnStmt", 3),
		"ReturnSym":        eval.NewRelation("ReturnSym", 2),
		"DestructureField": eval.NewRelation("DestructureField", 5),
		"FieldRead":        eval.NewRelation("FieldRead", 3),
		"FieldWrite":       eval.NewRelation("FieldWrite", 4),
		// Summary-specific dependencies
		"Parameter":        eval.NewRelation("Parameter", 6),
		"FunctionContains": eval.NewRelation("FunctionContains", 2),
		"CallArg":          eval.NewRelation("CallArg", 3),
		"CallCalleeSym":    eval.NewRelation("CallCalleeSym", 2),
		"CallResultSym":    eval.NewRelation("CallResultSym", 2),
		"TaintSink":        eval.NewRelation("TaintSink", 2),
		"TaintSource":      eval.NewRelation("TaintSource", 2),
	}
	for k, v := range overrides {
		base[k] = v
	}
	return base
}

// summaryAndFlowRules returns LocalFlow + Summary rules combined.
func summaryAndFlowRules() []datalog.Rule {
	var all []datalog.Rule
	all = append(all, LocalFlowRules()...)
	all = append(all, SummaryRules()...)
	return all
}

// TestParamToReturn_Identity tests identity function: id(x) { return x; } → ParamToReturn(id, 0).
func TestParamToReturn_Identity(t *testing.T) {
	// fn=1, paramSym=10, retSym=99
	// Parameter(fn=1, idx=0, name="x", paramNode=50, sym=10, type="")
	// ReturnStmt(fn=1, stmtNode=60, retExpr=200)
	// ExprMayRef(retExpr=200, sym=10) — return expression references param symbol
	// ReturnSym(fn=1, sym=99)
	// SymInFunction(10, 1), SymInFunction(99, 1)
	baseRels := summaryBaseRels(map[string]*eval.Relation{
		"Parameter":     makeRel("Parameter", 6, iv(1), iv(0), sv("x"), iv(50), iv(10), sv("")),
		"ReturnStmt":    makeRel("ReturnStmt", 3, iv(1), iv(60), iv(200)),
		"ExprMayRef":    makeRel("ExprMayRef", 2, iv(200), iv(10)),
		"ReturnSym":     makeRel("ReturnSym", 2, iv(1), iv(99)),
		"SymInFunction": makeRel("SymInFunction", 2, iv(10), iv(1), iv(99), iv(1)),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("fn"), v("idx")},
		Body:   []datalog.Literal{pos("ParamToReturn", v("fn"), v("idx"))},
	}

	rs := planAndEval(t, summaryAndFlowRules(), query, baseRels)
	if len(rs.Rows) != 1 {
		t.Fatalf("expected 1 ParamToReturn row, got %d: %v", len(rs.Rows), rs.Rows)
	}
	if !resultContains(rs, iv(1), iv(0)) {
		t.Errorf("expected ParamToReturn(1, 0), got %v", rs.Rows)
	}
}

// TestParamToReturn_NoFlow tests f(x) { return 42; } → no ParamToReturn.
func TestParamToReturn_NoFlow(t *testing.T) {
	// fn=1, paramSym=10, retSym=99, literalSym=88
	// The return expression references a literal symbol, not the parameter.
	baseRels := summaryBaseRels(map[string]*eval.Relation{
		"Parameter":  makeRel("Parameter", 6, iv(1), iv(0), sv("x"), iv(50), iv(10), sv("")),
		"ReturnStmt": makeRel("ReturnStmt", 3, iv(1), iv(60), iv(200)),
		"ExprMayRef": makeRel("ExprMayRef", 2, iv(200), iv(88)), // references literal, not param
		"ReturnSym":  makeRel("ReturnSym", 2, iv(1), iv(99)),
		"SymInFunction": makeRel("SymInFunction", 2,
			iv(10), iv(1),
			iv(88), iv(1),
			iv(99), iv(1),
		),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("fn"), v("idx")},
		Body:   []datalog.Literal{pos("ParamToReturn", v("fn"), v("idx"))},
	}

	rs := planAndEval(t, summaryAndFlowRules(), query, baseRels)
	if len(rs.Rows) != 0 {
		t.Errorf("expected 0 ParamToReturn rows for non-identity, got %d: %v", len(rs.Rows), rs.Rows)
	}
}

// TestParamToReturn_MultiParam tests f(a, b) { return a; } → only ParamToReturn(f, 0).
func TestParamToReturn_MultiParam(t *testing.T) {
	// fn=1, paramSym_a=10, paramSym_b=20, retSym=99
	baseRels := summaryBaseRels(map[string]*eval.Relation{
		"Parameter": makeRel("Parameter", 6,
			iv(1), iv(0), sv("a"), iv(50), iv(10), sv(""),
			iv(1), iv(1), sv("b"), iv(51), iv(20), sv(""),
		),
		"ReturnStmt": makeRel("ReturnStmt", 3, iv(1), iv(60), iv(200)),
		"ExprMayRef": makeRel("ExprMayRef", 2, iv(200), iv(10)), // only 'a' flows to return
		"ReturnSym":  makeRel("ReturnSym", 2, iv(1), iv(99)),
		"SymInFunction": makeRel("SymInFunction", 2,
			iv(10), iv(1),
			iv(20), iv(1),
			iv(99), iv(1),
		),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("fn"), v("idx")},
		Body:   []datalog.Literal{pos("ParamToReturn", v("fn"), v("idx"))},
	}

	rs := planAndEval(t, summaryAndFlowRules(), query, baseRels)
	if len(rs.Rows) != 1 {
		t.Fatalf("expected 1 ParamToReturn row, got %d: %v", len(rs.Rows), rs.Rows)
	}
	if !resultContains(rs, iv(1), iv(0)) {
		t.Errorf("expected ParamToReturn(1, 0), got %v", rs.Rows)
	}
}

// TestParamToCallArg tests f(x) { let y = x; g(y); } → ParamToCallArg(f, 0, g_sym, 0).
func TestParamToCallArg(t *testing.T) {
	// fn=1, paramSym=10, ySym=15, call=300, argExpr=400, calleeSym=500
	// VarDecl y = x creates LocalFlow(fn, paramSym, ySym)
	// g(y) passes ySym to callee
	baseRels := summaryBaseRels(map[string]*eval.Relation{
		"Parameter":        makeRel("Parameter", 6, iv(1), iv(0), sv("x"), iv(50), iv(10), sv("")),
		"FunctionContains": makeRel("FunctionContains", 2, iv(1), iv(300)),
		"CallArg":          makeRel("CallArg", 3, iv(300), iv(0), iv(400)),
		"ExprMayRef": makeRel("ExprMayRef", 2,
			iv(400), iv(15), // arg references ySym
			iv(600), iv(10), // initExpr references paramSym
		),
		"CallCalleeSym": makeRel("CallCalleeSym", 2, iv(300), iv(500)),
		"VarDecl":       makeRel("VarDecl", 4, iv(700), iv(15), iv(600), iv(0)), // let y = x
		"SymInFunction": makeRel("SymInFunction", 2,
			iv(10), iv(1),
			iv(15), iv(1),
		),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("fn"), v("paramIdx"), v("calleeSym"), v("argIdx")},
		Body:   []datalog.Literal{pos("ParamToCallArg", v("fn"), v("paramIdx"), v("calleeSym"), v("argIdx"))},
	}

	rs := planAndEval(t, summaryAndFlowRules(), query, baseRels)
	if len(rs.Rows) != 1 {
		t.Fatalf("expected 1 ParamToCallArg row, got %d: %v", len(rs.Rows), rs.Rows)
	}
	if !resultContains(rs, iv(1), iv(0), iv(500), iv(0)) {
		t.Errorf("expected ParamToCallArg(1, 0, 500, 0), got %v", rs.Rows)
	}
}

// TestParamToFieldWrite tests f(obj, val) { let v = val; obj.x = v; } → ParamToFieldWrite(f, 1, "x").
func TestParamToFieldWrite(t *testing.T) {
	// fn=1, paramSym_obj=10, paramSym_val=20, vSym=25, assignNode=300, rhsExpr=400
	// VarDecl v = val creates LocalFlow(fn, paramSym_val, vSym)
	// FieldWrite obj.x = v references vSym
	baseRels := summaryBaseRels(map[string]*eval.Relation{
		"Parameter": makeRel("Parameter", 6,
			iv(1), iv(0), sv("obj"), iv(50), iv(10), sv(""),
			iv(1), iv(1), sv("val"), iv(51), iv(20), sv(""),
		),
		"FunctionContains": makeRel("FunctionContains", 2, iv(1), iv(300)),
		"FieldWrite":       makeRel("FieldWrite", 4, iv(300), iv(10), sv("x"), iv(400)),
		"ExprMayRef": makeRel("ExprMayRef", 2,
			iv(400), iv(25), // rhs references vSym
			iv(600), iv(20), // initExpr references val param
		),
		"VarDecl": makeRel("VarDecl", 4, iv(700), iv(25), iv(600), iv(0)), // let v = val
		"SymInFunction": makeRel("SymInFunction", 2,
			iv(10), iv(1),
			iv(20), iv(1),
			iv(25), iv(1),
		),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("fn"), v("paramIdx"), v("fieldName")},
		Body:   []datalog.Literal{pos("ParamToFieldWrite", v("fn"), v("paramIdx"), v("fieldName"))},
	}

	rs := planAndEval(t, summaryAndFlowRules(), query, baseRels)
	if len(rs.Rows) != 1 {
		t.Fatalf("expected 1 ParamToFieldWrite row, got %d: %v", len(rs.Rows), rs.Rows)
	}
	if !resultContains(rs, iv(1), iv(1), sv("x")) {
		t.Errorf("expected ParamToFieldWrite(1, 1, 'x'), got %v", rs.Rows)
	}
}

// TestCallReturnToReturn tests f(x) { return g(x); } → CallReturnToReturn(f, call).
func TestCallReturnToReturn(t *testing.T) {
	// fn=1, call=300, callRetSym=40, retSym=99
	// g(x) is called, result sym is callRetSym=40
	// return stmt references callRetSym, which flows to retSym
	baseRels := summaryBaseRels(map[string]*eval.Relation{
		"FunctionContains": makeRel("FunctionContains", 2, iv(1), iv(300)),
		"CallResultSym":    makeRel("CallResultSym", 2, iv(300), iv(40)),
		"ReturnSym":        makeRel("ReturnSym", 2, iv(1), iv(99)),
		"ReturnStmt":       makeRel("ReturnStmt", 3, iv(1), iv(60), iv(200)),
		"ExprMayRef":       makeRel("ExprMayRef", 2, iv(200), iv(40)), // return expr references callRetSym
		"SymInFunction": makeRel("SymInFunction", 2,
			iv(40), iv(1),
			iv(99), iv(1),
		),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("fn"), v("call")},
		Body:   []datalog.Literal{pos("CallReturnToReturn", v("fn"), v("call"))},
	}

	rs := planAndEval(t, summaryAndFlowRules(), query, baseRels)
	if len(rs.Rows) != 1 {
		t.Fatalf("expected 1 CallReturnToReturn row, got %d: %v", len(rs.Rows), rs.Rows)
	}
	if !resultContains(rs, iv(1), iv(300)) {
		t.Errorf("expected CallReturnToReturn(1, 300), got %v", rs.Rows)
	}
}

// TestTaintSinkEmpty tests that ParamToSink produces nothing when TaintSink is empty.
func TestTaintSinkEmpty(t *testing.T) {
	baseRels := summaryBaseRels(map[string]*eval.Relation{
		"Parameter":     makeRel("Parameter", 6, iv(1), iv(0), sv("x"), iv(50), iv(10), sv("")),
		"SymInFunction": makeRel("SymInFunction", 2, iv(10), iv(1)),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("fn"), v("paramIdx"), v("sinkKind")},
		Body:   []datalog.Literal{pos("ParamToSink", v("fn"), v("paramIdx"), v("sinkKind"))},
	}

	rs := planAndEval(t, summaryAndFlowRules(), query, baseRels)
	if len(rs.Rows) != 0 {
		t.Errorf("expected 0 ParamToSink rows with empty TaintSink, got %d: %v", len(rs.Rows), rs.Rows)
	}
}

// TestTaintSourceEmpty tests that SourceToReturn produces nothing when TaintSource is empty.
func TestTaintSourceEmpty(t *testing.T) {
	baseRels := summaryBaseRels(map[string]*eval.Relation{
		"ReturnSym":     makeRel("ReturnSym", 2, iv(1), iv(99)),
		"SymInFunction": makeRel("SymInFunction", 2, iv(99), iv(1)),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("fn"), v("sourceKind")},
		Body:   []datalog.Literal{pos("SourceToReturn", v("fn"), v("sourceKind"))},
	}

	rs := planAndEval(t, summaryAndFlowRules(), query, baseRels)
	if len(rs.Rows) != 0 {
		t.Errorf("expected 0 SourceToReturn rows with empty TaintSource, got %d: %v", len(rs.Rows), rs.Rows)
	}
}

// TestSummaryRulesValidate verifies all summary rules pass the planner's validation.
func TestSummaryRulesValidate(t *testing.T) {
	for i, r := range SummaryRules() {
		errs := plan.ValidateRule(r)
		if len(errs) > 0 {
			t.Errorf("rule %d (%s) validation errors: %v", i, r.Head.Predicate, errs)
		}
	}
}

// TestSummaryRulesStratify verifies summary rules can be stratified with all other system rules.
func TestSummaryRulesStratify(t *testing.T) {
	prog := &datalog.Program{Rules: AllSystemRules()}
	_, errs := plan.Plan(prog, nil)
	if len(errs) > 0 {
		t.Fatalf("all system rules (including summaries) failed to plan: %v", errs)
	}
}

// TestSummaryRulesCount verifies we produce exactly 6 summary rules.
func TestSummaryRulesCount(t *testing.T) {
	rules := SummaryRules()
	if len(rules) != 6 {
		t.Errorf("expected 6 summary rules, got %d", len(rules))
	}
}

// TestAllSystemRulesCountWithSummaries verifies AllSystemRules returns combined count.
func TestAllSystemRulesCountWithSummaries(t *testing.T) {
	all := AllSystemRules()
	cg := CallGraphRules()
	lf := LocalFlowRules()
	sm := SummaryRules()
	co := CompositionRules()
	ta := TaintRules()
	fw := FrameworkRules()
	ho := HigherOrderRules()
	vf := ValueFlowRules()
	lfs := LocalFlowStepRules()
	expected := len(cg) + len(lf) + len(sm) + len(co) + len(ta) + len(fw) + len(ho) + len(vf) + len(lfs)
	if len(all) != expected {
		t.Errorf("expected %d rules, got %d", expected, len(all))
	}
}

// TestEmptyRelationsNoSummaries verifies no summaries are produced from empty base relations.
func TestEmptyRelationsNoSummaries(t *testing.T) {
	baseRels := summaryBaseRels(nil)

	for _, tc := range []struct {
		name  string
		query *datalog.Query
	}{
		{
			"ParamToReturn",
			&datalog.Query{
				Select: []datalog.Term{v("fn"), v("idx")},
				Body:   []datalog.Literal{pos("ParamToReturn", v("fn"), v("idx"))},
			},
		},
		{
			"ParamToCallArg",
			&datalog.Query{
				Select: []datalog.Term{v("fn"), v("pi"), v("cs"), v("ai")},
				Body:   []datalog.Literal{pos("ParamToCallArg", v("fn"), v("pi"), v("cs"), v("ai"))},
			},
		},
		{
			"ParamToFieldWrite",
			&datalog.Query{
				Select: []datalog.Term{v("fn"), v("pi"), v("f")},
				Body:   []datalog.Literal{pos("ParamToFieldWrite", v("fn"), v("pi"), v("f"))},
			},
		},
		{
			"CallReturnToReturn",
			&datalog.Query{
				Select: []datalog.Term{v("fn"), v("call")},
				Body:   []datalog.Literal{pos("CallReturnToReturn", v("fn"), v("call"))},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rs := planAndEval(t, summaryAndFlowRules(), tc.query, baseRels)
			if len(rs.Rows) != 0 {
				t.Errorf("expected 0 %s rows from empty relations, got %d", tc.name, len(rs.Rows))
			}
		})
	}
}
