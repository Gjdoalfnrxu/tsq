package rules

import (
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/eval"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

// interFlowStepBaseRels supplies empty bases for all relations the ifs* /
// InterFlowStep / FlowStep rules join against. Tests override the few rels
// they actually populate.
func interFlowStepBaseRels(overrides map[string]*eval.Relation) map[string]*eval.Relation {
	base := localFlowStepBaseRels(nil)
	// PR3 EDB inputs not already in localFlowStepBaseRels.
	base["ImportBinding"] = eval.NewRelation("ImportBinding", 3)
	base["ExportBinding"] = eval.NewRelation("ExportBinding", 3)
	for k, v := range overrides {
		base[k] = v
	}
	return base
}

func evalInterStep(t *testing.T, baseRels map[string]*eval.Relation, pred string) *eval.ResultSet {
	t.Helper()
	return planAndEval(t, AllSystemRules(), queryStep(pred), baseRels)
}

// TestIfsCallArgToParam — direct-call argument flows to in-callee references
// of the bound parameter.
func TestIfsCallArgToParam(t *testing.T) {
	// fn=1, paramSym=10, paramNode=80, idx=0
	// CallTarget(call=300, fn=1) — derived from CallCalleeSym × FunctionSymbol.
	// CallArg(300, 0, argExpr=400). ExprMayRef(useExpr=700, paramSym=10).
	baseRels := interFlowStepBaseRels(map[string]*eval.Relation{
		"Parameter":      makeRel("Parameter", 6, iv(1), iv(0), sv("p"), iv(80), iv(10), sv("")),
		"CallCalleeSym":  makeRel("CallCalleeSym", 2, iv(300), iv(500)),
		"FunctionSymbol": makeRel("FunctionSymbol", 2, iv(500), iv(1)),
		"CallArg":        makeRel("CallArg", 3, iv(300), iv(0), iv(400)),
		"ExprMayRef":     makeRel("ExprMayRef", 2, iv(700), iv(10)),
	})
	rs := evalInterStep(t, baseRels, "ifsCallArgToParam")
	if len(rs.Rows) != 1 || !resultContains(rs, iv(400), iv(700)) {
		t.Fatalf("expected ifsCallArgToParam(400, 700), got %v", rs.Rows)
	}
	rsUnion := evalInterStep(t, baseRels, "InterFlowStep")
	if !resultContains(rsUnion, iv(400), iv(700)) {
		t.Errorf("InterFlowStep should contain (400, 700), got %v", rsUnion.Rows)
	}
}

// TestIfsRetToCall — cross-module return-to-call edge resolved through
// CallTargetCrossModule (PR1's deferred consumer).
func TestIfsRetToCall(t *testing.T) {
	// CallTargetCrossModule built from:
	//   CallCalleeSym(call=300, localSym=50)
	//   ImportBinding(localSym=50, _, importedName="foo")
	//   ExportBinding(exportedName="foo", exportedSym=60, _)
	//   FunctionSymbol(exportedSym=60, fn=1)
	// ReturnStmt(fn=1, _, returnExpr=400). ExprIsCall(callExpr=600, call=300).
	baseRels := interFlowStepBaseRels(map[string]*eval.Relation{
		"CallCalleeSym":  makeRel("CallCalleeSym", 2, iv(300), iv(50)),
		"ImportBinding":  makeRel("ImportBinding", 3, iv(50), sv("./mod"), sv("foo")),
		"ExportBinding":  makeRel("ExportBinding", 3, sv("foo"), iv(60), iv(900)),
		"FunctionSymbol": makeRel("FunctionSymbol", 2, iv(60), iv(1)),
		"ReturnStmt":     makeRel("ReturnStmt", 3, iv(1), iv(81), iv(400)),
		"ExprIsCall":     makeRel("ExprIsCall", 2, iv(600), iv(300)),
	})
	rs := evalInterStep(t, baseRels, "ifsRetToCall")
	if len(rs.Rows) != 1 || !resultContains(rs, iv(400), iv(600)) {
		t.Fatalf("expected ifsRetToCall(400, 600), got %v", rs.Rows)
	}
}

// TestIfsImportExport — symbol-level bridge: an expression referencing the
// exported sym in module B flows to expressions referencing the matching
// imported local sym in module A.
func TestIfsImportExport(t *testing.T) {
	// ImportBinding(localSym=50, _, "foo"); ExportBinding("foo", exportedSym=60, _).
	// ExprMayRef(srcExpr=400, exportedSym=60); ExprMayRef(useExpr=700, localSym=50).
	baseRels := interFlowStepBaseRels(map[string]*eval.Relation{
		"ImportBinding": makeRel("ImportBinding", 3, iv(50), sv("./mod"), sv("foo")),
		"ExportBinding": makeRel("ExportBinding", 3, sv("foo"), iv(60), iv(900)),
		"ExprMayRef": makeRel("ExprMayRef", 2,
			iv(400), iv(60),
			iv(700), iv(50),
		),
	})
	rs := evalInterStep(t, baseRels, "ifsImportExport")
	if len(rs.Rows) != 1 || !resultContains(rs, iv(400), iv(700)) {
		t.Fatalf("expected ifsImportExport(400, 700), got %v", rs.Rows)
	}
}

// TestIfsImportExportNameCollision — two modules export the same name; the
// rule deliberately over-bridges (documented unsoundness, plan §4.1). This
// test pins the behaviour so a future change can't silently tighten or
// loosen it without updating the documented contract.
func TestIfsImportExportNameCollision(t *testing.T) {
	// Two ExportBinding rows with the same name "foo" (different exporting
	// modules / syms). One ImportBinding for "foo" in importing module.
	// Rule should produce edges from BOTH exports to the import use.
	baseRels := interFlowStepBaseRels(map[string]*eval.Relation{
		"ImportBinding": makeRel("ImportBinding", 3, iv(50), sv("./modA"), sv("foo")),
		"ExportBinding": makeRel("ExportBinding", 3,
			sv("foo"), iv(60), iv(901),
			sv("foo"), iv(70), iv(902),
		),
		"ExprMayRef": makeRel("ExprMayRef", 2,
			iv(400), iv(60), // ref to first export
			iv(401), iv(70), // ref to second export
			iv(700), iv(50), // import-side use
		),
	})
	rs := evalInterStep(t, baseRels, "ifsImportExport")
	if len(rs.Rows) != 2 {
		t.Fatalf("expected 2 over-bridged rows on name collision, got %d: %v", len(rs.Rows), rs.Rows)
	}
	if !resultContains(rs, iv(400), iv(700)) || !resultContains(rs, iv(401), iv(700)) {
		t.Errorf("name-collision rows missing — over-bridging contract regressed: %v", rs.Rows)
	}
}

// TestIfsCallTargetRTA — RTA-resolved method dispatch return-to-call.
// CallTargetRTA is itself a system rule (callgraph.go) over MethodCall ×
// ExprType × InterfaceDecl × Implements × Instantiated × MethodDecl, so
// the EDB setup mirrors that shape: receiver `recv` of interface `ifaceId`
// implemented by instantiated class `classId` whose method `name` is `fn`.
func TestIfsCallTargetRTA(t *testing.T) {
	baseRels := interFlowStepBaseRels(map[string]*eval.Relation{
		"MethodCall":    makeRel("MethodCall", 3, iv(300), iv(550), sv("m")),
		"ExprType":      makeRel("ExprType", 2, iv(550), iv(900)),
		"InterfaceDecl": makeRel("InterfaceDecl", 3, iv(900), sv("I"), iv(0)),
		"Implements":    makeRel("Implements", 2, iv(910), iv(900)),
		// Instantiated is itself a rule (Instantiated(c) :- NewExpr(_, c)),
		// so populate the NewExpr base; the rule fires for class 910.
		"NewExpr":    makeRel("NewExpr", 2, iv(950), iv(910)),
		"MethodDecl": makeRel("MethodDecl", 3, iv(910), sv("m"), iv(1)),
		"ReturnStmt": makeRel("ReturnStmt", 3, iv(1), iv(81), iv(400)),
		"ExprIsCall": makeRel("ExprIsCall", 2, iv(600), iv(300)),
	})
	rs := evalInterStep(t, baseRels, "ifsCallTargetRTA")
	if len(rs.Rows) != 1 || !resultContains(rs, iv(400), iv(600)) {
		t.Fatalf("expected ifsCallTargetRTA(400, 600), got %v", rs.Rows)
	}
}

// TestInterFlowStepUnion populates one row per kind across disjoint id
// ranges so the union row count equals the sum (modulo RTA, which can
// over-bridge into other kinds via the same ExprIsCall edge — asserted
// >= 4 rather than ==4).
func TestInterFlowStepUnion(t *testing.T) {
	baseRels := interFlowStepBaseRels(map[string]*eval.Relation{
		// --- ifsCallArgToParam: direct CHA call.
		// CallCalleeSym(301, 501), FunctionSymbol(501, 1)  =>  CallTarget(301, 1).
		// CallArg(301, 0, 401), Parameter(1, 0, _, _, 11, _), ExprMayRef(701, 11).
		"Parameter": makeRel("Parameter", 6, iv(1), iv(0), sv("p"), iv(80), iv(11), sv("")),
		"CallArg":   makeRel("CallArg", 3, iv(301), iv(0), iv(401)),

		// --- ifsRetToCall: cross-module call.
		// CallCalleeSym(302, 52), ImportBinding(52, _, "g"),
		// ExportBinding("g", 60, _), FunctionSymbol(60, 2).
		// ReturnStmt(2, _, 402), ExprIsCall(602, 302).
		"ImportBinding": makeRel("ImportBinding", 3,
			iv(52), sv("./m1"), sv("g"),
			iv(54), sv("./m2"), sv("h"),
		),
		"ExportBinding": makeRel("ExportBinding", 3,
			sv("g"), iv(60), iv(901),
			sv("h"), iv(62), iv(902),
		),

		// --- ifsCallTargetRTA: method dispatch via interface + instantiated impl.
		// MethodCall(303, recv=560, "m"), ExprType(560, iface=900),
		// InterfaceDecl(900, "I", _), Implements(910, 900),
		// NewExpr(_, 910)  =>  Instantiated(910),
		// MethodDecl(910, "m", fn=3). ReturnStmt(3, _, 404), ExprIsCall(604, 303).
		"MethodCall":    makeRel("MethodCall", 3, iv(303), iv(560), sv("m")),
		"ExprType":      makeRel("ExprType", 2, iv(560), iv(900)),
		"InterfaceDecl": makeRel("InterfaceDecl", 3, iv(900), sv("I"), iv(0)),
		"Implements":    makeRel("Implements", 2, iv(910), iv(900)),
		"NewExpr":       makeRel("NewExpr", 2, iv(950), iv(910)),
		"MethodDecl":    makeRel("MethodDecl", 3, iv(910), sv("m"), iv(3)),

		// FunctionSymbol covers the symbols used above (call-graph CHA + cross-module).
		"FunctionSymbol": makeRel("FunctionSymbol", 2,
			iv(501), iv(1),
			iv(60), iv(2),
		),
		"CallCalleeSym": makeRel("CallCalleeSym", 2,
			iv(301), iv(501), // ifsCallArgToParam (CHA)
			iv(302), iv(52), // ifsRetToCall (cross-module)
		),

		"ReturnStmt": makeRel("ReturnStmt", 3,
			iv(2), iv(81), iv(402), // for ifsRetToCall
			iv(3), iv(82), iv(404), // for ifsCallTargetRTA
		),
		"ExprIsCall": makeRel("ExprIsCall", 2,
			iv(602), iv(302),
			iv(604), iv(303),
		),

		// --- ifsImportExport: use the second ImportBinding/ExportBinding pair.
		// ExprMayRef(403, 62) on the export side; ExprMayRef(703, 54) on the import side.
		"ExprMayRef": makeRel("ExprMayRef", 2,
			iv(701), iv(11), // for ifsCallArgToParam
			iv(403), iv(62), // for ifsImportExport (export-side ref)
			iv(703), iv(54), // for ifsImportExport (import-side ref)
		),
	})

	rs := evalInterStep(t, baseRels, "InterFlowStep")
	if len(rs.Rows) < 4 {
		t.Errorf("expected >= 4 InterFlowStep rows (one per kind minimum), got %d: %v", len(rs.Rows), rs.Rows)
	}
	want := [][2]int64{
		{401, 701}, // ifsCallArgToParam
		{402, 602}, // ifsRetToCall
		{403, 703}, // ifsImportExport
		{404, 604}, // ifsCallTargetRTA
	}
	for _, w := range want {
		if !resultContains(rs, iv(w[0]), iv(w[1])) {
			t.Errorf("InterFlowStep missing (%d, %d) — one of the ifs* kinds is not contributing", w[0], w[1])
		}
	}
}

// TestFlowStepUnion verifies FlowStep folds LocalFlowStep ∪ InterFlowStep.
// Populate one local-side and one inter-side row; assert both reachable
// through the top-level union.
func TestFlowStepUnion(t *testing.T) {
	baseRels := interFlowStepBaseRels(map[string]*eval.Relation{
		// Local-side: lfsAssign(401, 501)
		"Assign": makeRel("Assign", 3, iv(101), iv(401), iv(11)),
		// Inter-side: ifsCallArgToParam(402, 702)
		"Parameter":      makeRel("Parameter", 6, iv(1), iv(0), sv("p"), iv(80), iv(12), sv("")),
		"CallCalleeSym":  makeRel("CallCalleeSym", 2, iv(302), iv(502)),
		"FunctionSymbol": makeRel("FunctionSymbol", 2, iv(502), iv(1)),
		"CallArg":        makeRel("CallArg", 3, iv(302), iv(0), iv(402)),
		"ExprMayRef": makeRel("ExprMayRef", 2,
			iv(501), iv(11), // for lfsAssign
			iv(702), iv(12), // for ifsCallArgToParam
		),
	})
	rs := evalInterStep(t, baseRels, "FlowStep")
	if !resultContains(rs, iv(401), iv(501)) {
		t.Errorf("FlowStep missing local-side (401, 501): %v", rs.Rows)
	}
	if !resultContains(rs, iv(402), iv(702)) {
		t.Errorf("FlowStep missing inter-side (402, 702): %v", rs.Rows)
	}
}

// TestInterFlowStepRulesShape — at least one head per kind plus union
// branches. >= 4 rather than ==N so adding genuine new kinds in PR4+
// doesn't require touching this test (#166 disjunction-poisoning shape:
// each kind gets its own head, never an inline `or`).
func TestInterFlowStepRulesShape(t *testing.T) {
	got := len(InterFlowStepRules())
	if got < 4 {
		t.Errorf("expected >= 4 InterFlowStep rules (one per kind minimum, #166 workaround), got %d", got)
	}
}

// TestInterFlowStepRulesValidate runs the planner's own validation against
// each rule. Catches predicate-name typos / arity slip-ups.
func TestInterFlowStepRulesValidate(t *testing.T) {
	for i, r := range InterFlowStepRules() {
		errs := plan.ValidateRule(r)
		if len(errs) > 0 {
			t.Errorf("rule %d (%s) validation errors: %v", i, r.Head.Predicate, errs)
		}
	}
}

// TestInterFlowStepEmpty — empty EDBs produce zero InterFlowStep / FlowStep
// rows.
func TestInterFlowStepEmpty(t *testing.T) {
	base := interFlowStepBaseRels(nil)
	if rs := evalInterStep(t, base, "InterFlowStep"); len(rs.Rows) != 0 {
		t.Errorf("expected 0 InterFlowStep rows from empty EDBs, got %d", len(rs.Rows))
	}
	if rs := evalInterStep(t, base, "FlowStep"); len(rs.Rows) != 0 {
		t.Errorf("expected 0 FlowStep rows from empty EDBs, got %d", len(rs.Rows))
	}
}
