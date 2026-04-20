package rules

import (
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/eval"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

// localFlowStepBaseRels supplies empty bases for all relations that the
// lfs* / LocalFlowStep rules join against. Tests override the few rels
// they actually populate.
func localFlowStepBaseRels(overrides map[string]*eval.Relation) map[string]*eval.Relation {
	base := valueFlowBaseRels(nil)
	// Step rules' EDB inputs (most are already in compositionBaseRels via
	// valueFlowBaseRels, but make the dependency explicit here).
	base["Assign"] = eval.NewRelation("Assign", 3)
	base["VarDecl"] = eval.NewRelation("VarDecl", 4)
	base["ExprMayRef"] = eval.NewRelation("ExprMayRef", 2)
	base["ExprIsCall"] = eval.NewRelation("ExprIsCall", 2)
	base["ReturnStmt"] = eval.NewRelation("ReturnStmt", 3)
	// Phase E PR1 (#210): arities bumped by +1 for the new `path` column
	// on the six path-bearing relations. Rules ignore the column (named-
	// literal bindings); tests now seed it with a placeholder.
	base["DestructureField"] = eval.NewRelation("DestructureField", 6)
	base["ArrayDestructure"] = eval.NewRelation("ArrayDestructure", 4)
	base["ObjectLiteralField"] = eval.NewRelation("ObjectLiteralField", 4)
	base["ObjectLiteralSpread"] = eval.NewRelation("ObjectLiteralSpread", 3)
	base["FieldRead"] = eval.NewRelation("FieldRead", 4)
	base["FieldWrite"] = eval.NewRelation("FieldWrite", 5)
	base["Await"] = eval.NewRelation("Await", 2)
	// PR8 (#202 Gap A): lfsJsxPropBind EDB inputs.
	base["JsxAttribute"] = eval.NewRelation("JsxAttribute", 3)
	base["JsxElement"] = eval.NewRelation("JsxElement", 3)
	base["ParamDestructurePattern"] = eval.NewRelation("ParamDestructurePattern", 2)
	base["JsxExpressionInner"] = eval.NewRelation("JsxExpressionInner", 2)
	for k, v := range overrides {
		base[k] = v
	}
	return base
}

// queryStep returns a query selecting all rows of a (from, to) IDB.
func queryStep(pred string) *datalog.Query {
	return &datalog.Query{
		Select: []datalog.Term{v("from"), v("to")},
		Body: []datalog.Literal{
			pos(pred, v("from"), v("to")),
		},
	}
}

func evalStep(t *testing.T, baseRels map[string]*eval.Relation, pred string) *eval.ResultSet {
	t.Helper()
	return planAndEval(t, AllSystemRules(), queryStep(pred), baseRels)
}

// TestLfsAssign — `x = expr; use(x);` produces lfsAssign(rhs, useExpr).
func TestLfsAssign(t *testing.T) {
	// rhsExpr=400, lhsSym=10, useExpr=500
	baseRels := localFlowStepBaseRels(map[string]*eval.Relation{
		"Assign":     makeRel("Assign", 3, iv(100), iv(400), iv(10)),
		"ExprMayRef": makeRel("ExprMayRef", 2, iv(500), iv(10)),
	})
	rs := evalStep(t, baseRels, "lfsAssign")
	if len(rs.Rows) != 1 || !resultContains(rs, iv(400), iv(500)) {
		t.Fatalf("expected lfsAssign(400, 500), got %v", rs.Rows)
	}
	rsUnion := evalStep(t, baseRels, "LocalFlowStep")
	if !resultContains(rsUnion, iv(400), iv(500)) {
		t.Errorf("LocalFlowStep should contain (400, 500), got %v", rsUnion.Rows)
	}
}

// TestLfsVarInit — `const x = expr; use(x);` produces lfsVarInit(initExpr, useExpr).
func TestLfsVarInit(t *testing.T) {
	// VarDecl(declId=200, sym=10, initExpr=400, isConst=1); use=500
	baseRels := localFlowStepBaseRels(map[string]*eval.Relation{
		"VarDecl":    makeRel("VarDecl", 4, iv(200), iv(10), iv(400), iv(1)),
		"ExprMayRef": makeRel("ExprMayRef", 2, iv(500), iv(10)),
	})
	rs := evalStep(t, baseRels, "lfsVarInit")
	if len(rs.Rows) != 1 || !resultContains(rs, iv(400), iv(500)) {
		t.Fatalf("expected lfsVarInit(400, 500), got %v", rs.Rows)
	}
}

// TestLfsParamBind — call-arg flows to in-callee references of the param.
func TestLfsParamBind(t *testing.T) {
	// Build the ParamBinding row first via the existing ParamBinding rule.
	// fn=1, paramSym=10, paramNode=80, argExpr=400, useExpr=500.
	baseRels := localFlowStepBaseRels(map[string]*eval.Relation{
		"Parameter":      makeRel("Parameter", 6, iv(1), iv(0), sv("p"), iv(80), iv(10), sv("")),
		"CallCalleeSym":  makeRel("CallCalleeSym", 2, iv(300), iv(500)),
		"FunctionSymbol": makeRel("FunctionSymbol", 2, iv(500), iv(1)),
		"CallArg":        makeRel("CallArg", 3, iv(300), iv(0), iv(400)),
		"ExprMayRef":     makeRel("ExprMayRef", 2, iv(700), iv(10)),
	})
	rs := evalStep(t, baseRels, "lfsParamBind")
	if len(rs.Rows) != 1 || !resultContains(rs, iv(400), iv(700)) {
		t.Fatalf("expected lfsParamBind(400, 700), got %v", rs.Rows)
	}
}

// TestLfsReturnToCallSite — same-module return flows to caller's call expr.
func TestLfsReturnToCallSite(t *testing.T) {
	// fn=1; return expr=400; call=300; ExprIsCall(callExpr=600, call=300).
	baseRels := localFlowStepBaseRels(map[string]*eval.Relation{
		"ReturnStmt":     makeRel("ReturnStmt", 3, iv(1), iv(81), iv(400)),
		"CallCalleeSym":  makeRel("CallCalleeSym", 2, iv(300), iv(500)),
		"FunctionSymbol": makeRel("FunctionSymbol", 2, iv(500), iv(1)),
		"ExprIsCall":     makeRel("ExprIsCall", 2, iv(600), iv(300)),
	})
	rs := evalStep(t, baseRels, "lfsReturnToCallSite")
	if len(rs.Rows) != 1 || !resultContains(rs, iv(400), iv(600)) {
		t.Fatalf("expected lfsReturnToCallSite(400, 600), got %v", rs.Rows)
	}
}

// TestLfsDestructureField — `const { foo } = obj; use(foo);` flows obj→use.
func TestLfsDestructureField(t *testing.T) {
	// DestructureField(parent=400, srcField="foo", bindName="foo", bindSym=10, idx=0)
	// use=500 references sym=10.
	baseRels := localFlowStepBaseRels(map[string]*eval.Relation{
		"DestructureField": makeRel("DestructureField", 6,
			iv(400), sv("foo"), sv("foo"), iv(10), iv(0), sv(".foo"),
		),
		"ExprMayRef": makeRel("ExprMayRef", 2, iv(500), iv(10)),
	})
	rs := evalStep(t, baseRels, "lfsDestructureField")
	if len(rs.Rows) != 1 || !resultContains(rs, iv(400), iv(500)) {
		t.Fatalf("expected lfsDestructureField(400, 500), got %v", rs.Rows)
	}
}

// TestLfsArrayDestructure — `const [x, y] = arr; use(x);` flows arr→use.
func TestLfsArrayDestructure(t *testing.T) {
	baseRels := localFlowStepBaseRels(map[string]*eval.Relation{
		"ArrayDestructure": makeRel("ArrayDestructure", 4, iv(400), iv(0), iv(10), sv(".[0]")),
		"ExprMayRef":       makeRel("ExprMayRef", 2, iv(500), iv(10)),
	})
	rs := evalStep(t, baseRels, "lfsArrayDestructure")
	if len(rs.Rows) != 1 || !resultContains(rs, iv(400), iv(500)) {
		t.Fatalf("expected lfsArrayDestructure(400, 500), got %v", rs.Rows)
	}
}

// TestLfsObjectLiteralStore — `{ foo: x }` flows x into the object literal.
func TestLfsObjectLiteralStore(t *testing.T) {
	// ObjectLiteralField(parent=600, fieldName="foo", valueExpr=400)
	baseRels := localFlowStepBaseRels(map[string]*eval.Relation{
		"ObjectLiteralField": makeRel("ObjectLiteralField", 4,
			iv(600), sv("foo"), iv(400), sv(".foo"),
		),
	})
	rs := evalStep(t, baseRels, "lfsObjectLiteralStore")
	if len(rs.Rows) != 1 || !resultContains(rs, iv(400), iv(600)) {
		t.Fatalf("expected lfsObjectLiteralStore(400, 600), got %v", rs.Rows)
	}
}

// TestLfsSpreadElement — `{ ...rest }` flows rest into the object literal.
func TestLfsSpreadElement(t *testing.T) {
	baseRels := localFlowStepBaseRels(map[string]*eval.Relation{
		"ObjectLiteralSpread": makeRel("ObjectLiteralSpread", 3, iv(600), iv(400), sv(".{*}")),
	})
	rs := evalStep(t, baseRels, "lfsSpreadElement")
	if len(rs.Rows) != 1 || !resultContains(rs, iv(400), iv(600)) {
		t.Fatalf("expected lfsSpreadElement(400, 600), got %v", rs.Rows)
	}
}

// TestLfsFieldRead — `obj.foo` flows from the obj-bearing expression to
// the field-read expression. Path-erased (PR2) — does NOT discriminate
// fieldName.
func TestLfsFieldRead(t *testing.T) {
	// FieldRead(expr=600, baseSym=10, "foo"); ExprMayRef(from=400, sym=10).
	baseRels := localFlowStepBaseRels(map[string]*eval.Relation{
		"FieldRead":  makeRel("FieldRead", 4, iv(600), iv(10), sv("foo"), sv(".foo")),
		"ExprMayRef": makeRel("ExprMayRef", 2, iv(400), iv(10)),
	})
	rs := evalStep(t, baseRels, "lfsFieldRead")
	if len(rs.Rows) != 1 || !resultContains(rs, iv(400), iv(600)) {
		t.Fatalf("expected lfsFieldRead(400, 600), got %v", rs.Rows)
	}
}

// TestLfsFieldWrite — `obj.foo = expr` flows expr to refs of obj. Path-
// erased.
func TestLfsFieldWrite(t *testing.T) {
	// FieldWrite(assignNode=700, baseSym=10, "foo", rhsExpr=400); ExprMayRef(to=500, sym=10).
	baseRels := localFlowStepBaseRels(map[string]*eval.Relation{
		"FieldWrite": makeRel("FieldWrite", 5, iv(700), iv(10), sv("foo"), iv(400), sv(".foo")),
		"ExprMayRef": makeRel("ExprMayRef", 2, iv(500), iv(10)),
	})
	rs := evalStep(t, baseRels, "lfsFieldWrite")
	if len(rs.Rows) != 1 || !resultContains(rs, iv(400), iv(500)) {
		t.Fatalf("expected lfsFieldWrite(400, 500), got %v", rs.Rows)
	}
}

// TestLfsAwait — `await e` is treated as `e`; flow goes innerExpr → awaitExpr.
func TestLfsAwait(t *testing.T) {
	baseRels := localFlowStepBaseRels(map[string]*eval.Relation{
		"Await": makeRel("Await", 2, iv(600), iv(400)),
	})
	rs := evalStep(t, baseRels, "lfsAwait")
	if len(rs.Rows) != 1 || !resultContains(rs, iv(400), iv(600)) {
		t.Fatalf("expected lfsAwait(400, 600), got %v", rs.Rows)
	}
}

// TestLfsJsxPropBind — `<Inner value={cfg} onClick={handler} />` flows
// `cfg` only to in-body references of the `value` destructured binding,
// and `handler` only to references of the `onClick` binding. Exercises
// field-sensitivity on the DestructureField.sourceField ↔ JsxAttribute.name
// join: a single-attribute-single-field fixture would pass even if the
// field-name join were ignored. See #202 Gap A.
func TestLfsJsxPropBind(t *testing.T) {
	// JSX site: elem=700, tagSym=50, fn=1.
	// value attr: wrapper=412, inner cfg=413.
	// onClick attr: wrapper=422, inner handler=423.
	// Component: Parameter(fn=1, idx=0, paramNode=80, paramSym=10).
	// Destructure: ParamDestructurePattern(80, 85).
	// Fields: value→bindSym=20, onClick→bindSym=30.
	// In-body uses: ExprMayRef(513, 20) [value use], ExprMayRef(523, 30) [onClick use].
	baseRels := localFlowStepBaseRels(map[string]*eval.Relation{
		"JsxAttribute": makeRel("JsxAttribute", 3,
			iv(700), sv("value"), iv(412),
			iv(700), sv("onClick"), iv(422),
		),
		"JsxExpressionInner": makeRel("JsxExpressionInner", 2,
			iv(412), iv(413),
			iv(422), iv(423),
		),
		"JsxElement":              makeRel("JsxElement", 3, iv(700), iv(701), iv(50)),
		"FunctionSymbol":          makeRel("FunctionSymbol", 2, iv(50), iv(1)),
		"Parameter":               makeRel("Parameter", 6, iv(1), iv(0), sv("{ value, onClick }"), iv(80), iv(10), sv("")),
		"ParamDestructurePattern": makeRel("ParamDestructurePattern", 2, iv(80), iv(85)),
		"DestructureField": makeRel("DestructureField", 6,
			iv(85), sv("value"), sv("value"), iv(20), iv(0), sv(".value"),
			iv(85), sv("onClick"), sv("onClick"), iv(30), iv(1), sv(".onClick"),
		),
		"ExprMayRef": makeRel("ExprMayRef", 2,
			iv(513), iv(20),
			iv(523), iv(30),
		),
	})
	rs := evalStep(t, baseRels, "lfsJsxPropBind")
	// Pin exact expected row set — two field-sensitive edges and nothing else.
	// Critically: NO (413 → 523) and NO (423 → 513) — those would indicate
	// the field-name join was dropped.
	want := [][2]eval.IntVal{
		{iv(413), iv(513)}, // cfg → value-use
		{iv(423), iv(523)}, // handler → onClick-use
	}
	if len(rs.Rows) != len(want) {
		t.Fatalf("expected %d lfsJsxPropBind rows, got %d: %v", len(want), len(rs.Rows), rs.Rows)
	}
	for _, w := range want {
		if !resultContains(rs, w[0], w[1]) {
			t.Errorf("expected lfsJsxPropBind(%v, %v) in rows, got %v", w[0], w[1], rs.Rows)
		}
	}
	// Negative: field-name join must reject cross-field wiring.
	if resultContains(rs, iv(413), iv(523)) {
		t.Errorf("field-sensitivity violated: cfg (413) leaked to onClick-use (523); rows=%v", rs.Rows)
	}
	if resultContains(rs, iv(423), iv(513)) {
		t.Errorf("field-sensitivity violated: handler (423) leaked to value-use (513); rows=%v", rs.Rows)
	}
	rsUnion := evalStep(t, baseRels, "LocalFlowStep")
	if !resultContains(rsUnion, iv(413), iv(513)) {
		t.Errorf("LocalFlowStep should contain (413, 513), got %v", rsUnion.Rows)
	}
	if !resultContains(rsUnion, iv(423), iv(523)) {
		t.Errorf("LocalFlowStep should contain (423, 523), got %v", rsUnion.Rows)
	}
}

// TestLocalFlowStepUnion verifies the union folds eleven step kinds into
// one relation, and that each kind contributes at least one independently-
// observable row when its EDB inputs are populated together.
func TestLocalFlowStepUnion(t *testing.T) {
	// Populate one row per kind across disjoint id ranges so the union
	// row count equals the sum.
	baseRels := localFlowStepBaseRels(map[string]*eval.Relation{
		// lfsAssign:  Assign(_, 401, 11), ExprMayRef(501, 11)
		"Assign": makeRel("Assign", 3, iv(101), iv(401), iv(11)),
		// lfsVarInit: VarDecl(_, 12, 402, _), ExprMayRef(502, 12)
		"VarDecl": makeRel("VarDecl", 4, iv(202), iv(12), iv(402), iv(1)),
		// lfsParamBind: requires Parameter+CallArg+CallCalleeSym+FunctionSymbol
		"Parameter":      makeRel("Parameter", 6, iv(1), iv(0), sv("p"), iv(80), iv(13), sv("")),
		"CallCalleeSym":  makeRel("CallCalleeSym", 2, iv(303), iv(503)),
		"FunctionSymbol": makeRel("FunctionSymbol", 2, iv(503), iv(1)),
		"CallArg":        makeRel("CallArg", 3, iv(303), iv(0), iv(403)),
		// lfsReturnToCallSite: ReturnStmt(fn=1, _, 404), ExprIsCall(604, call=303)
		// — reuses CallTarget derived from the lfsParamBind facts above.
		"ReturnStmt": makeRel("ReturnStmt", 3, iv(1), iv(81), iv(404)),
		"ExprIsCall": makeRel("ExprIsCall", 2, iv(604), iv(303)),
		// lfsDestructureField: parent=405, bindSym=15
		"DestructureField": makeRel("DestructureField", 6,
			iv(405), sv("k"), sv("k"), iv(15), iv(0), sv(".k"),
		),
		// lfsArrayDestructure: parent=406, bindSym=16
		"ArrayDestructure": makeRel("ArrayDestructure", 4, iv(406), iv(0), iv(16), sv(".[0]")),
		// lfsObjectLiteralStore: ObjectLiteralField(607, "f", 407)
		"ObjectLiteralField": makeRel("ObjectLiteralField", 4,
			iv(607), sv("f"), iv(407), sv(".f"),
		),
		// lfsSpreadElement: ObjectLiteralSpread(608, 408)
		"ObjectLiteralSpread": makeRel("ObjectLiteralSpread", 3, iv(608), iv(408), sv(".{*}")),
		// lfsFieldRead: FieldRead(609, 17, "f"), ExprMayRef(409, 17)
		"FieldRead": makeRel("FieldRead", 4, iv(609), iv(17), sv("f"), sv(".f")),
		// lfsFieldWrite: FieldWrite(_, 18, "f", 410), ExprMayRef(510, 18)
		"FieldWrite": makeRel("FieldWrite", 5, iv(710), iv(18), sv("f"), iv(410), sv(".f")),
		// lfsAwait: Await(611, 411)
		"Await": makeRel("Await", 2, iv(611), iv(411)),
		// ExprMayRef rows for the kinds that need them.
		"ExprMayRef": makeRel("ExprMayRef", 2,
			iv(501), iv(11), // for lfsAssign
			iv(502), iv(12), // for lfsVarInit
			iv(703), iv(13), // for lfsParamBind (use inside callee body)
			iv(505), iv(15), // for lfsDestructureField
			iv(506), iv(16), // for lfsArrayDestructure
			iv(409), iv(17), // for lfsFieldRead (`from` references baseSym)
			iv(510), iv(18), // for lfsFieldWrite (`to` references baseSym)
		),
	})
	rs := evalStep(t, baseRels, "LocalFlowStep")
	// 11 step kinds, one row each.
	if len(rs.Rows) != 11 {
		t.Errorf("expected 11 LocalFlowStep rows (one per kind), got %d: %v", len(rs.Rows), rs.Rows)
	}
	// Spot-check coverage of each kind's contribution.
	want := [][2]int64{
		{401, 501}, // lfsAssign
		{402, 502}, // lfsVarInit
		{403, 703}, // lfsParamBind
		{404, 604}, // lfsReturnToCallSite
		{405, 505}, // lfsDestructureField
		{406, 506}, // lfsArrayDestructure
		{407, 607}, // lfsObjectLiteralStore
		{408, 608}, // lfsSpreadElement
		{409, 609}, // lfsFieldRead
		{410, 510}, // lfsFieldWrite
		{411, 611}, // lfsAwait
	}
	for _, w := range want {
		if !resultContains(rs, iv(w[0]), iv(w[1])) {
			t.Errorf("LocalFlowStep missing (%d, %d) — one of the lfs* kinds is not contributing", w[0], w[1])
		}
	}
}

// TestLocalFlowStepRulesShape asserts the minimum rule shape: at least one
// head per lfs* kind plus union branches. The 11-branch shape is the
// disjunction-poisoning workaround for issue #166 (each kind gets its own
// head + union rule rather than a single multi-disjunct LocalFlowStep
// definition); we assert >= 11 rather than == 22 so adding genuine new
// kinds in PR3+ doesn't require touching this test.
func TestLocalFlowStepRulesShape(t *testing.T) {
	got := len(LocalFlowStepRules())
	if got < 11 {
		t.Errorf("expected >= 11 LocalFlowStep rules (one per kind minimum, #166 workaround), got %d", got)
	}
}

// TestLocalFlowStepRulesValidate makes sure all rules pass the planner's
// own validation. Catches predicate-name typos / arity slip-ups before
// they reach integration tests.
func TestLocalFlowStepRulesValidate(t *testing.T) {
	for i, r := range LocalFlowStepRules() {
		errs := plan.ValidateRule(r)
		if len(errs) > 0 {
			t.Errorf("rule %d (%s) validation errors: %v", i, r.Head.Predicate, errs)
		}
	}
}

// TestLocalFlowStepEmpty — empty EDBs produce zero LocalFlowStep rows.
func TestLocalFlowStepEmpty(t *testing.T) {
	rs := evalStep(t, localFlowStepBaseRels(nil), "LocalFlowStep")
	if len(rs.Rows) != 0 {
		t.Errorf("expected 0 LocalFlowStep rows from empty EDBs, got %d: %v", len(rs.Rows), rs.Rows)
	}
}
