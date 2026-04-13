package rules

import (
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/eval"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

// compositionBaseRels returns base relations needed for composition rules,
// including all relations needed by LocalFlow, Summary, and CallGraph rules.
func compositionBaseRels(overrides map[string]*eval.Relation) map[string]*eval.Relation {
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
		// Summary dependencies
		"Parameter":        eval.NewRelation("Parameter", 6),
		"FunctionContains": eval.NewRelation("FunctionContains", 2),
		"CallArg":          eval.NewRelation("CallArg", 3),
		"CallCalleeSym":    eval.NewRelation("CallCalleeSym", 2),
		"CallResultSym":    eval.NewRelation("CallResultSym", 2),
		"TaintSink":        eval.NewRelation("TaintSink", 2),
		"TaintSource":      eval.NewRelation("TaintSource", 2),
		// CallGraph dependencies
		"FunctionSymbol": eval.NewRelation("FunctionSymbol", 2),
		"MethodCall":     eval.NewRelation("MethodCall", 3),
		"ExprType":       eval.NewRelation("ExprType", 2),
		"ClassDecl":      eval.NewRelation("ClassDecl", 3),
		"InterfaceDecl":  eval.NewRelation("InterfaceDecl", 3),
		"MethodDecl":     eval.NewRelation("MethodDecl", 3),
		"Implements":     eval.NewRelation("Implements", 2),
		"Extends":        eval.NewRelation("Extends", 2),
		"NewExpr":        eval.NewRelation("NewExpr", 2),
		// v3 Phase 3d: type-based sanitization
		"SymbolType":       eval.NewRelation("SymbolType", 2),
		"NonTaintableType": eval.NewRelation("NonTaintableType", 1),
		// A3: additional taint/flow steps (default empty)
		"AdditionalTaintStep": eval.NewRelation("AdditionalTaintStep", 2),
		"AdditionalFlowStep":  eval.NewRelation("AdditionalFlowStep", 2),
	}
	for k, v := range overrides {
		base[k] = v
	}
	return base
}

// allRulesForComposition returns all system rules needed for composition evaluation.
func allRulesForComposition() []datalog.Rule {
	return AllSystemRules()
}

// TestInterFlow_PassThrough tests: f(x) { return x; } called as f(tainted) → InterFlow from arg to call result.
func TestInterFlow_PassThrough(t *testing.T) {
	// Function f: fn=1, paramSym=10, retSym=99
	// Caller: call=300, argExpr=400, argSym=50, callRetSym=60
	//
	// Facts needed to derive ParamToReturn(fn=1, idx=0):
	//   Parameter(fn=1, idx=0, "x", paramNode=80, sym=10, "")
	//   ReturnStmt(fn=1, stmtNode=81, retExpr=200)
	//   ExprMayRef(retExpr=200, sym=10)    — return references param
	//   ReturnSym(fn=1, sym=99)
	//   SymInFunction(10, 1), SymInFunction(99, 1)
	//
	// Call site facts:
	//   CallCalleeSym(call=300, calleeSym=500)
	//   FunctionSymbol(calleeSym=500, fn=1)  → CallTarget(300, 1)
	//   CallArg(call=300, idx=0, argExpr=400)
	//   ExprMayRef(argExpr=400, argSym=50)
	//   CallResultSym(call=300, callRetSym=60)
	baseRels := compositionBaseRels(map[string]*eval.Relation{
		"Parameter":  makeRel("Parameter", 6, iv(1), iv(0), sv("x"), iv(80), iv(10), sv("")),
		"ReturnStmt": makeRel("ReturnStmt", 3, iv(1), iv(81), iv(200)),
		"ExprMayRef": makeRel("ExprMayRef", 2,
			iv(200), iv(10), // return expr → param sym
			iv(400), iv(50), // arg expr → arg sym
		),
		"ReturnSym": makeRel("ReturnSym", 2, iv(1), iv(99)),
		"SymInFunction": makeRel("SymInFunction", 2,
			iv(10), iv(1),
			iv(99), iv(1),
		),
		"CallCalleeSym":  makeRel("CallCalleeSym", 2, iv(300), iv(500)),
		"FunctionSymbol": makeRel("FunctionSymbol", 2, iv(500), iv(1)),
		"CallArg":        makeRel("CallArg", 3, iv(300), iv(0), iv(400)),
		"CallResultSym":  makeRel("CallResultSym", 2, iv(300), iv(60)),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("src"), v("dst")},
		Body:   []datalog.Literal{pos("InterFlow", v("src"), v("dst"))},
	}

	rs := planAndEval(t, allRulesForComposition(), query, baseRels)
	if len(rs.Rows) != 1 {
		t.Fatalf("expected 1 InterFlow row, got %d: %v", len(rs.Rows), rs.Rows)
	}
	if !resultContains(rs, iv(50), iv(60)) {
		t.Errorf("expected InterFlow(50, 60), got %v", rs.Rows)
	}
}

// TestInterFlow_NoFlow tests: f(x) { return 42; } → no InterFlow.
func TestInterFlow_NoFlow(t *testing.T) {
	// Function f: fn=1, paramSym=10, retSym=99, literalSym=88
	// The return expression references a literal, not the parameter → no ParamToReturn → no InterFlow.
	baseRels := compositionBaseRels(map[string]*eval.Relation{
		"Parameter":  makeRel("Parameter", 6, iv(1), iv(0), sv("x"), iv(80), iv(10), sv("")),
		"ReturnStmt": makeRel("ReturnStmt", 3, iv(1), iv(81), iv(200)),
		"ExprMayRef": makeRel("ExprMayRef", 2,
			iv(200), iv(88), // return expr → literal sym (not param)
			iv(400), iv(50), // arg expr → arg sym
		),
		"ReturnSym": makeRel("ReturnSym", 2, iv(1), iv(99)),
		"SymInFunction": makeRel("SymInFunction", 2,
			iv(10), iv(1),
			iv(88), iv(1),
			iv(99), iv(1),
		),
		"CallCalleeSym":  makeRel("CallCalleeSym", 2, iv(300), iv(500)),
		"FunctionSymbol": makeRel("FunctionSymbol", 2, iv(500), iv(1)),
		"CallArg":        makeRel("CallArg", 3, iv(300), iv(0), iv(400)),
		"CallResultSym":  makeRel("CallResultSym", 2, iv(300), iv(60)),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("src"), v("dst")},
		Body:   []datalog.Literal{pos("InterFlow", v("src"), v("dst"))},
	}

	rs := planAndEval(t, allRulesForComposition(), query, baseRels)
	if len(rs.Rows) != 0 {
		t.Errorf("expected 0 InterFlow rows for non-identity function, got %d: %v", len(rs.Rows), rs.Rows)
	}
}

// TestInterFlow_MultiLevel tests: f calls g, g is identity → flow propagates across two levels.
// g(x) { return x; }    ← ParamToReturn(g, 0)
// f(a) { return g(a); } ← f has ParamToCallArg(f, 0, gSym, 0) and CallReturnToReturn(f, call_g)
//
// At the caller calling f(tainted):
//   - InterFlow rule 2 (ParamToCallArg): tainted flows through f to g's arg
//     via ParamToCallArg(f, 0, gSym, 0)
//   - InterFlow rule 1 on g's call: g's arg flows to g's call result (ParamToReturn(g, 0))
//   - FlowStar composes these transitively
func TestInterFlow_MultiLevel(t *testing.T) {
	// g: fn=2, paramSym_g=20, retSym_g=29
	// f: fn=1, paramSym_f=10, retSym_f=19, call_g=300, callRetSym_g=40
	// caller: call_f=600, argExpr_f=700, argSym_f=50, callRetSym_f=60

	baseRels := compositionBaseRels(map[string]*eval.Relation{
		"Parameter": makeRel("Parameter", 6,
			iv(2), iv(0), sv("x"), iv(180), iv(20), sv(""), // g's param
			iv(1), iv(0), sv("a"), iv(80), iv(10), sv(""), // f's param
		),
		"ReturnStmt": makeRel("ReturnStmt", 3,
			iv(2), iv(181), iv(250), // g's return
			iv(1), iv(81), iv(200), // f's return
		),
		"ExprMayRef": makeRel("ExprMayRef", 2,
			iv(250), iv(20), // g's return expr → g's param sym
			iv(200), iv(40), // f's return expr → callRetSym of g (f returns g(a))
			iv(350), iv(10), // f's call arg to g → f's param sym
			iv(700), iv(50), // caller's call arg to f → tainted sym
		),
		"ReturnSym": makeRel("ReturnSym", 2,
			iv(2), iv(29), // g's return sym
			iv(1), iv(19), // f's return sym
		),
		"SymInFunction": makeRel("SymInFunction", 2,
			iv(20), iv(2), // g
			iv(29), iv(2),
			iv(10), iv(1), // f
			iv(19), iv(1),
			iv(40), iv(1), // callRetSym of g is in f
		),
		// g is called from f: call_g=300
		"FunctionContains": makeRel("FunctionContains", 2, iv(1), iv(300)),
		"CallCalleeSym": makeRel("CallCalleeSym", 2,
			iv(300), iv(501), // f's call to g
			iv(600), iv(500), // caller's call to f
		),
		"FunctionSymbol": makeRel("FunctionSymbol", 2,
			iv(500), iv(1), // sym 500 → fn 1 (f)
			iv(501), iv(2), // sym 501 → fn 2 (g)
		),
		"CallArg": makeRel("CallArg", 3,
			iv(300), iv(0), iv(350), // f calls g(a) — arg is expr 350
			iv(600), iv(0), iv(700), // caller calls f(tainted) — arg is expr 700
		),
		"CallResultSym": makeRel("CallResultSym", 2,
			iv(300), iv(40), // g call result sym in f
			iv(600), iv(60), // f call result sym in caller
		),
	})

	// Check InterFlow: f→g pass-through should work (g is identity, called with f's param)
	queryInterFlow := &datalog.Query{
		Select: []datalog.Term{v("src"), v("dst")},
		Body:   []datalog.Literal{pos("InterFlow", v("src"), v("dst"))},
	}

	rs := planAndEval(t, allRulesForComposition(), queryInterFlow, baseRels)

	// InterFlow(10, 40): f's param flows through g to g's call result in f.
	// This uses rule 1: CallTarget(300, 2), CallArg(300, 0, 350), ExprMayRef(350, 10),
	//   ParamToReturn(2, 0), CallResultSym(300, 40).
	if !resultContains(rs, iv(10), iv(40)) {
		t.Errorf("expected InterFlow(10, 40) for f→g pass-through, got %v", rs.Rows)
	}

	// Check FlowStar for end-to-end: tainted(50) → callResult(60)
	// Path: FlowStar(50, 10) via InterFlow rule 2 (ParamToCallArg),
	//        then InterFlow(10, 40), LocalFlowStar(1, 40, 19) (local in f),
	//        then caller gets it.
	// Actually the full chain is: InterFlow(10,40) + LocalFlow(1,40,19)
	// which means FlowStar(10, 19) via local lift + inter compose.
	queryFlowStar := &datalog.Query{
		Select: []datalog.Term{v("src"), v("dst")},
		Body:   []datalog.Literal{pos("FlowStar", v("src"), v("dst"))},
	}

	rsFlow := planAndEval(t, allRulesForComposition(), queryFlowStar, baseRels)

	// FlowStar(10, 40): inter-procedural through g
	if !resultContains(rsFlow, iv(10), iv(40)) {
		t.Errorf("expected FlowStar(10, 40), got %v", rsFlow.Rows)
	}
	// FlowStar(40, 19): local flow in f (return g(a) → retSym)
	if !resultContains(rsFlow, iv(40), iv(19)) {
		t.Errorf("expected FlowStar(40, 19) from local flow in f, got %v", rsFlow.Rows)
	}
	// FlowStar(10, 19): transitive through g and back — param flows to f's return
	if !resultContains(rsFlow, iv(10), iv(19)) {
		t.Errorf("expected FlowStar(10, 19) transitively, got %v", rsFlow.Rows)
	}
}

// TestInterFlow_UnresolvedCall tests: no CallTarget → no crash, no InterFlow.
func TestInterFlow_UnresolvedCall(t *testing.T) {
	// Call exists but no CallCalleeSym/FunctionSymbol → no CallTarget → no InterFlow.
	baseRels := compositionBaseRels(map[string]*eval.Relation{
		"CallArg":       makeRel("CallArg", 3, iv(300), iv(0), iv(400)),
		"ExprMayRef":    makeRel("ExprMayRef", 2, iv(400), iv(50)),
		"CallResultSym": makeRel("CallResultSym", 2, iv(300), iv(60)),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("src"), v("dst")},
		Body:   []datalog.Literal{pos("InterFlow", v("src"), v("dst"))},
	}

	rs := planAndEval(t, allRulesForComposition(), query, baseRels)
	if len(rs.Rows) != 0 {
		t.Errorf("expected 0 InterFlow rows for unresolved call, got %d: %v", len(rs.Rows), rs.Rows)
	}
}

// TestFlowStar_Transitivity tests that FlowStar composes local and inter-procedural flow.
// Setup: local flow A→B within a function, inter-procedural flow B→C across a call.
// Expected: FlowStar(A, C) via transitivity.
func TestFlowStar_Transitivity(t *testing.T) {
	// Function f: fn=1, sym A=10, sym B=15, retSym=99
	// let b = a; (local flow A→B)
	// return b; (local flow B→retSym)
	// ParamToReturn(f, 0) derived
	//
	// Caller: call=300, argExpr=400, argSym=50 (tainted), callRetSym=60
	// Local flow: sym 50 → sym 50 (reflexive via LocalFlowStar)
	// InterFlow: 50 → 60

	// For FlowStar transitivity, we also need a local step in the caller.
	// Setup: caller has let x = tainted; let r = f(x);
	// localSymX=50, localSymTainted=45
	// Assign x = tainted → LocalFlow(caller, 45, 50)
	// Then InterFlow(50, 60) from the pass-through
	// FlowStar should give us (45, 60) transitively.

	baseRels := compositionBaseRels(map[string]*eval.Relation{
		"Parameter":  makeRel("Parameter", 6, iv(1), iv(0), sv("b"), iv(80), iv(15), sv("")),
		"ReturnStmt": makeRel("ReturnStmt", 3, iv(1), iv(81), iv(200)),
		"ExprMayRef": makeRel("ExprMayRef", 2,
			iv(200), iv(15), // return expr → B (param sym)
			iv(400), iv(50), // call arg expr → sym 50
			iv(450), iv(45), // init expr for let x = tainted → sym 45
		),
		"ReturnSym": makeRel("ReturnSym", 2, iv(1), iv(99)),
		"SymInFunction": makeRel("SymInFunction", 2,
			iv(15), iv(1),
			iv(99), iv(1),
			iv(45), iv(2), // tainted sym in caller fn=2
			iv(50), iv(2), // x sym in caller fn=2
		),
		// let x = tainted in caller (fn=2)
		"VarDecl":        makeRel("VarDecl", 4, iv(460), iv(50), iv(450), iv(0)),
		"CallCalleeSym":  makeRel("CallCalleeSym", 2, iv(300), iv(500)),
		"FunctionSymbol": makeRel("FunctionSymbol", 2, iv(500), iv(1)),
		"CallArg":        makeRel("CallArg", 3, iv(300), iv(0), iv(400)),
		"CallResultSym":  makeRel("CallResultSym", 2, iv(300), iv(60)),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("src"), v("dst")},
		Body:   []datalog.Literal{pos("FlowStar", v("src"), v("dst"))},
	}

	rs := planAndEval(t, allRulesForComposition(), query, baseRels)

	// FlowStar(45, 50) from local flow in caller
	if !resultContains(rs, iv(45), iv(50)) {
		t.Errorf("expected FlowStar(45, 50) from local flow, got %v", rs.Rows)
	}
	// FlowStar(50, 60) from inter-procedural flow
	if !resultContains(rs, iv(50), iv(60)) {
		t.Errorf("expected FlowStar(50, 60) from inter flow, got %v", rs.Rows)
	}
	// FlowStar(45, 60) from transitivity: local(45→50) + inter(50→60)
	if !resultContains(rs, iv(45), iv(60)) {
		t.Errorf("expected FlowStar(45, 60) from transitivity, got %v", rs.Rows)
	}
}

// TestCompositionRulesValidate verifies all composition rules pass the planner's validation.
func TestCompositionRulesValidate(t *testing.T) {
	for i, r := range CompositionRules() {
		errs := plan.ValidateRule(r)
		if len(errs) > 0 {
			t.Errorf("rule %d (%s) validation errors: %v", i, r.Head.Predicate, errs)
		}
	}
}

// TestCompositionRulesStratify verifies composition + summary + localflow + callgraph rules stratify together.
func TestCompositionRulesStratify(t *testing.T) {
	prog := &datalog.Program{Rules: AllSystemRules()}
	_, errs := plan.Plan(prog, nil)
	if len(errs) > 0 {
		t.Fatalf("all system rules (including composition) failed to plan: %v", errs)
	}
}

// TestCompositionRulesCount verifies we produce exactly 7 composition rules.
func TestCompositionRulesCount(t *testing.T) {
	rules := CompositionRules()
	if len(rules) != 7 {
		t.Errorf("expected 7 composition rules, got %d", len(rules))
	}
}

// TestAllSystemRulesCountWithComposition verifies AllSystemRules returns combined count.
func TestAllSystemRulesCountWithComposition(t *testing.T) {
	all := AllSystemRules()
	cg := CallGraphRules()
	lf := LocalFlowRules()
	sm := SummaryRules()
	co := CompositionRules()
	ta := TaintRules()
	fw := FrameworkRules()
	ho := HigherOrderRules()
	expected := len(cg) + len(lf) + len(sm) + len(co) + len(ta) + len(fw) + len(ho)
	if len(all) != expected {
		t.Errorf("expected %d rules, got %d", expected, len(all))
	}
}

// TestEmptyRelationsNoInterFlow verifies no InterFlow from empty base relations.
func TestEmptyRelationsNoInterFlow(t *testing.T) {
	baseRels := compositionBaseRels(nil)

	query := &datalog.Query{
		Select: []datalog.Term{v("src"), v("dst")},
		Body:   []datalog.Literal{pos("InterFlow", v("src"), v("dst"))},
	}

	rs := planAndEval(t, allRulesForComposition(), query, baseRels)
	if len(rs.Rows) != 0 {
		t.Errorf("expected 0 InterFlow rows from empty relations, got %d: %v", len(rs.Rows), rs.Rows)
	}
}

// TestEmptyRelationsNoFlowStar verifies no FlowStar from empty base relations.
func TestEmptyRelationsNoFlowStar(t *testing.T) {
	baseRels := compositionBaseRels(nil)

	query := &datalog.Query{
		Select: []datalog.Term{v("src"), v("dst")},
		Body:   []datalog.Literal{pos("FlowStar", v("src"), v("dst"))},
	}

	rs := planAndEval(t, allRulesForComposition(), query, baseRels)
	if len(rs.Rows) != 0 {
		t.Errorf("expected 0 FlowStar rows from empty relations, got %d: %v", len(rs.Rows), rs.Rows)
	}
}

// TestAdditionalTaintStep_FlowStar verifies that AdditionalTaintStep facts
// are lifted into FlowStar, enabling taint to propagate through user-defined steps.
func TestAdditionalTaintStep_FlowStar(t *testing.T) {
	additionalStep := eval.NewRelation("AdditionalTaintStep", 2)
	additionalStep.Add(eval.Tuple{eval.IntVal{10}, eval.IntVal{20}})

	baseRels := compositionBaseRels(map[string]*eval.Relation{
		"AdditionalTaintStep": additionalStep,
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("src"), v("dst")},
		Body:   []datalog.Literal{pos("FlowStar", v("src"), v("dst"))},
	}

	rs := planAndEval(t, allRulesForComposition(), query, baseRels)
	if len(rs.Rows) == 0 {
		t.Fatal("expected FlowStar rows from AdditionalTaintStep, got 0")
	}
	found := false
	for _, row := range rs.Rows {
		if row[0] == (eval.IntVal{10}) && row[1] == (eval.IntVal{20}) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("FlowStar should contain (10, 20) from AdditionalTaintStep; rows: %v", rs.Rows)
	}
}

// TestAdditionalFlowStep_FlowStar verifies that AdditionalFlowStep facts
// are lifted into FlowStar.
func TestAdditionalFlowStep_FlowStar(t *testing.T) {
	additionalStep := eval.NewRelation("AdditionalFlowStep", 2)
	additionalStep.Add(eval.Tuple{eval.IntVal{30}, eval.IntVal{40}})

	baseRels := compositionBaseRels(map[string]*eval.Relation{
		"AdditionalFlowStep": additionalStep,
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("src"), v("dst")},
		Body:   []datalog.Literal{pos("FlowStar", v("src"), v("dst"))},
	}

	rs := planAndEval(t, allRulesForComposition(), query, baseRels)
	found := false
	for _, row := range rs.Rows {
		if row[0] == (eval.IntVal{30}) && row[1] == (eval.IntVal{40}) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("FlowStar should contain (30, 40) from AdditionalFlowStep; rows: %v", rs.Rows)
	}
}

// TestAdditionalTaintStep_Transitivity verifies that AdditionalTaintStep
// composes with LocalFlowStar for transitive FlowStar.
func TestAdditionalTaintStep_Transitivity(t *testing.T) {
	// LocalFlow: 10 → 20 in fn=1
	assign := eval.NewRelation("Assign", 3)
	assign.Add(eval.Tuple{eval.IntVal{100}, eval.IntVal{200}, eval.IntVal{20}}) // lhsNode=100, rhsExpr=200, lhsSym=20
	exprMayRef := eval.NewRelation("ExprMayRef", 2)
	exprMayRef.Add(eval.Tuple{eval.IntVal{200}, eval.IntVal{10}}) // rhsExpr=200 refers to sym 10
	symInFn := eval.NewRelation("SymInFunction", 2)
	symInFn.Add(eval.Tuple{eval.IntVal{10}, eval.IntVal{1}})
	symInFn.Add(eval.Tuple{eval.IntVal{20}, eval.IntVal{1}})

	// AdditionalTaintStep: 20 → 30 (crosses function boundary via user-defined step)
	additionalStep := eval.NewRelation("AdditionalTaintStep", 2)
	additionalStep.Add(eval.Tuple{eval.IntVal{20}, eval.IntVal{30}})

	baseRels := compositionBaseRels(map[string]*eval.Relation{
		"Assign":              assign,
		"ExprMayRef":          exprMayRef,
		"SymInFunction":       symInFn,
		"AdditionalTaintStep": additionalStep,
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("src"), v("dst")},
		Body:   []datalog.Literal{pos("FlowStar", v("src"), v("dst"))},
	}

	rs := planAndEval(t, allRulesForComposition(), query, baseRels)
	// Should have: 10→20 (local), 20→30 (additional), 10→30 (transitive)
	flowPairs := make(map[[2]int]bool)
	for _, row := range rs.Rows {
		src, _ := row[0].(eval.IntVal)
		dst, _ := row[1].(eval.IntVal)
		flowPairs[[2]int{int(src.V), int(dst.V)}] = true
	}
	for _, pair := range [][2]int{{10, 20}, {20, 30}, {10, 30}} {
		if !flowPairs[pair] {
			t.Errorf("expected FlowStar(%d, %d) from local + additional step transitivity", pair[0], pair[1])
		}
	}
}
