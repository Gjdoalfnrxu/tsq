package rules

import (
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/eval"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

// taintBaseRels returns base relations needed for taint rules evaluation,
// including all relations needed by the full system rule set (call graph,
// local flow, summaries, composition, and taint).
func taintBaseRels(overrides map[string]*eval.Relation) map[string]*eval.Relation {
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
		// Taint base relations
		"TaintSource": eval.NewRelation("TaintSource", 2),
		"TaintSink":   eval.NewRelation("TaintSink", 2),
		"Sanitizer":   eval.NewRelation("Sanitizer", 2),
		// v3 Phase 3d: type-based sanitization
		"SymbolType":       eval.NewRelation("SymbolType", 2),
		"NonTaintableType": eval.NewRelation("NonTaintableType", 1),
	}
	for k, v := range overrides {
		base[k] = v
	}
	return base
}

// TestTaintedSym_DirectSource tests the base case: TaintSource(expr, kind) + ExprMayRef(expr, sym) → TaintedSym(sym, kind).
func TestTaintedSym_DirectSource(t *testing.T) {
	// srcExpr=100, srcSym=10, kind="http_input"
	baseRels := taintBaseRels(map[string]*eval.Relation{
		"TaintSource": makeRel("TaintSource", 2, iv(100), sv("http_input")),
		"ExprMayRef":  makeRel("ExprMayRef", 2, iv(100), iv(10)),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("sym"), v("kind")},
		Body:   []datalog.Literal{pos("TaintedSym", v("sym"), v("kind"))},
	}

	rs := planAndEval(t, AllSystemRules(), query, baseRels)
	if !resultContains(rs, iv(10), sv("http_input")) {
		t.Errorf("expected TaintedSym(10, http_input), got %v", rs.Rows)
	}
}

// TestTaintedSym_PropagationViaFlowStar tests taint propagation through FlowStar.
// Source sym flows to another sym via local flow → TaintedSym on the destination.
func TestTaintedSym_PropagationViaFlowStar(t *testing.T) {
	// fn=1, srcExpr=100, srcSym=10, dstSym=20
	// let x = tainted; let y = x; → LocalFlow(1, 10, 20) → FlowStar(10, 20) → TaintedSym(20, "env")
	baseRels := taintBaseRels(map[string]*eval.Relation{
		"TaintSource": makeRel("TaintSource", 2, iv(100), sv("env")),
		"ExprMayRef":  makeRel("ExprMayRef", 2, iv(100), iv(10), iv(200), iv(10)),
		"Assign":      makeRel("Assign", 3, iv(300), iv(200), iv(20)),
		"SymInFunction": makeRel("SymInFunction", 2,
			iv(10), iv(1),
			iv(20), iv(1),
		),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("sym"), v("kind")},
		Body:   []datalog.Literal{pos("TaintedSym", v("sym"), v("kind"))},
	}

	rs := planAndEval(t, AllSystemRules(), query, baseRels)
	if !resultContains(rs, iv(10), sv("env")) {
		t.Errorf("expected TaintedSym(10, env), got %v", rs.Rows)
	}
	if !resultContains(rs, iv(20), sv("env")) {
		t.Errorf("expected TaintedSym(20, env) via FlowStar, got %v", rs.Rows)
	}
}

// TestTaintedSym_FlowStarChain tests taint through a multi-step FlowStar chain.
func TestTaintedSym_FlowStarChain(t *testing.T) {
	// fn=1, srcExpr=100, srcSym=10, midSym=20, dstSym=30
	// let a = tainted; let b = a; let c = b;
	baseRels := taintBaseRels(map[string]*eval.Relation{
		"TaintSource": makeRel("TaintSource", 2, iv(100), sv("env")),
		"ExprMayRef": makeRel("ExprMayRef", 2,
			iv(100), iv(10),
			iv(200), iv(10), // rhs of b = a
			iv(300), iv(20), // rhs of c = b
		),
		"Assign": makeRel("Assign", 3,
			iv(210), iv(200), iv(20), // b = a
			iv(310), iv(300), iv(30), // c = b
		),
		"SymInFunction": makeRel("SymInFunction", 2,
			iv(10), iv(1),
			iv(20), iv(1),
			iv(30), iv(1),
		),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("sym"), v("kind")},
		Body:   []datalog.Literal{pos("TaintedSym", v("sym"), v("kind"))},
	}

	rs := planAndEval(t, AllSystemRules(), query, baseRels)
	if !resultContains(rs, iv(30), sv("env")) {
		t.Errorf("expected TaintedSym(30, env) via chain, got %v", rs.Rows)
	}
}

// TestTaintAlert_DirectFlow tests a simple source-to-sink alert.
func TestTaintAlert_DirectFlow(t *testing.T) {
	// srcExpr=100, srcSym=10, sinkExpr=200, sinkSym=20
	// let x = tainted; sink(x);
	// Flow: TaintSource(100, "http_input"), ExprMayRef(100, 10),
	//       Assign x=tainted gives local flow 10 → 20,
	//       TaintSink(200, "sql"), ExprMayRef(200, 20)
	baseRels := taintBaseRels(map[string]*eval.Relation{
		"TaintSource": makeRel("TaintSource", 2, iv(100), sv("http_input")),
		"TaintSink":   makeRel("TaintSink", 2, iv(200), sv("sql")),
		"ExprMayRef": makeRel("ExprMayRef", 2,
			iv(100), iv(10),
			iv(200), iv(20),
			iv(300), iv(10), // rhs expr references srcSym
		),
		"Assign": makeRel("Assign", 3, iv(310), iv(300), iv(20)),
		"SymInFunction": makeRel("SymInFunction", 2,
			iv(10), iv(1),
			iv(20), iv(1),
		),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("srcExpr"), v("sinkExpr"), v("srcKind"), v("sinkKind")},
		Body:   []datalog.Literal{pos("TaintAlert", v("srcExpr"), v("sinkExpr"), v("srcKind"), v("sinkKind"))},
	}

	rs := planAndEval(t, AllSystemRules(), query, baseRels)
	if !resultContains(rs, iv(100), iv(200), sv("http_input"), sv("sql")) {
		t.Errorf("expected TaintAlert(100, 200, http_input, sql), got %v", rs.Rows)
	}
}

// TestTaintAlert_SanitizedFlow tests that sanitized flow produces no alert.
func TestTaintAlert_SanitizedFlow(t *testing.T) {
	// Source → sanitizer param → sink. The SanitizedEdge blocks propagation.
	// srcExpr=100, srcSym=10, sanitizerFn=5, sanitizerParamSym=20, sinkExpr=300, sinkSym=30
	//
	// Flow path: 10 → 20 (via FlowStar through inter-proc call to sanitizer)
	// Sanitizer(5, "http_input") marks the edge into param 20.
	// Result: TaintedSym(20, "http_input") should NOT be derived.
	baseRels := taintBaseRels(map[string]*eval.Relation{
		"TaintSource": makeRel("TaintSource", 2, iv(100), sv("http_input")),
		"TaintSink":   makeRel("TaintSink", 2, iv(300), sv("sql")),
		"Sanitizer":   makeRel("Sanitizer", 2, iv(5), sv("http_input")),
		"Parameter":   makeRel("Parameter", 6, iv(5), iv(0), sv("input"), iv(80), iv(20), sv("")),
		"ExprMayRef": makeRel("ExprMayRef", 2,
			iv(100), iv(10),
			iv(300), iv(30),
			iv(400), iv(10), // call arg expr references srcSym
			iv(250), iv(20), // sanitizer return refs param
		),
		// Call to sanitizer: call=500, calleeSym=501
		"CallCalleeSym":  makeRel("CallCalleeSym", 2, iv(500), iv(501)),
		"FunctionSymbol": makeRel("FunctionSymbol", 2, iv(501), iv(5)),
		"CallArg":        makeRel("CallArg", 3, iv(500), iv(0), iv(400)),
		"CallResultSym":  makeRel("CallResultSym", 2, iv(500), iv(30)),
		// ParamToReturn(fn=5, paramIdx=0): sanitizer passes its input through
		// to its return value. Needed for InterFlow to produce an edge from the
		// caller's arg sym (10) to the call result sym (30), which the sanitizer
		// rule can then block. Without this, the SanitizedEdge rule cannot fire
		// and the test was silently skipping (masking regressions).
		"ParamToReturn": makeRel("ParamToReturn", 2, iv(5), iv(0)),
		"ReturnStmt":    makeRel("ReturnStmt", 3, iv(5), iv(81), iv(250)),
		"ReturnSym":     makeRel("ReturnSym", 2, iv(5), iv(29)),
		"SymInFunction": makeRel("SymInFunction", 2,
			iv(10), iv(1),
			iv(30), iv(1),
			iv(20), iv(5),
			iv(29), iv(5),
		),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("srcExpr"), v("sinkExpr"), v("srcKind"), v("sinkKind")},
		Body:   []datalog.Literal{pos("TaintAlert", v("srcExpr"), v("sinkExpr"), v("srcKind"), v("sinkKind"))},
	}

	rs := planAndEval(t, AllSystemRules(), query, baseRels)

	// The sanitizer should block flow of "http_input" taint through sym 20.
	// Check SanitizedEdge exists.
	queryEdge := &datalog.Query{
		Select: []datalog.Term{v("src"), v("dst"), v("kind")},
		Body:   []datalog.Literal{pos("SanitizedEdge", v("src"), v("dst"), v("kind"))},
	}
	rsEdge := planAndEval(t, AllSystemRules(), queryEdge, baseRels)
	if len(rsEdge.Rows) == 0 {
		t.Fatalf("expected at least one SanitizedEdge row, got 0 — sanitizer blocking has regressed or the fixture is broken")
	}

	// The key invariant: sym 30 (the sink sym) should not be tainted with "http_input"
	// if the sanitizer blocks it. But note: this depends on FlowStar reaching param 20.
	// In our simplified setup, the inter-proc flow from 10 → 20 happens via CallTarget+ParamToReturn.
	// The sanitized edge should block TaintedSym propagation through that edge.
	for _, row := range rs.Rows {
		if len(row) >= 4 {
			if row[0] == iv(100) && row[1] == iv(300) {
				t.Errorf("expected no TaintAlert through sanitizer, got %v", row)
			}
		}
	}
}

// TestNoTaintSource_NoTaintedSym tests that without taint sources, no TaintedSym is produced.
func TestNoTaintSource_NoTaintedSym(t *testing.T) {
	baseRels := taintBaseRels(map[string]*eval.Relation{
		"ExprMayRef": makeRel("ExprMayRef", 2, iv(100), iv(10)),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("sym"), v("kind")},
		Body:   []datalog.Literal{pos("TaintedSym", v("sym"), v("kind"))},
	}

	rs := planAndEval(t, AllSystemRules(), query, baseRels)
	if len(rs.Rows) != 0 {
		t.Errorf("expected 0 TaintedSym rows without sources, got %d: %v", len(rs.Rows), rs.Rows)
	}
}

// TestEmptySourcesAndSinks_NoAlerts tests that empty TaintSource/TaintSink → no alerts.
func TestEmptySourcesAndSinks_NoAlerts(t *testing.T) {
	baseRels := taintBaseRels(nil)

	query := &datalog.Query{
		Select: []datalog.Term{v("srcExpr"), v("sinkExpr"), v("srcKind"), v("sinkKind")},
		Body:   []datalog.Literal{pos("TaintAlert", v("srcExpr"), v("sinkExpr"), v("srcKind"), v("sinkKind"))},
	}

	rs := planAndEval(t, AllSystemRules(), query, baseRels)
	if len(rs.Rows) != 0 {
		t.Errorf("expected 0 TaintAlert rows from empty relations, got %d: %v", len(rs.Rows), rs.Rows)
	}
}

// TestTaintedField_WriteAndRead tests field-sensitive taint tracking.
// Taint obj.a, read obj.a → tainted. Read obj.b → not tainted.
func TestTaintedField_WriteAndRead(t *testing.T) {
	// baseSym=50 (obj), fieldA="a", fieldB="b"
	// rhsSym=10 (tainted), rhsExpr=100
	// writeNode=200
	// readExprA=300, readSymA=30
	// readExprB=400, readSymB=40
	baseRels := taintBaseRels(map[string]*eval.Relation{
		"TaintSource": makeRel("TaintSource", 2, iv(100), sv("http_input")),
		"ExprMayRef": makeRel("ExprMayRef", 2,
			iv(100), iv(10), // source expr → srcSym
			iv(150), iv(10), // rhs of field write → srcSym
			iv(300), iv(30), // read expr a → readSymA
			iv(400), iv(40), // read expr b → readSymB
		),
		"FieldWrite": makeRel("FieldWrite", 4,
			iv(200), iv(50), sv("a"), iv(150), // obj.a = tainted
		),
		"FieldRead": makeRel("FieldRead", 3,
			iv(300), iv(50), sv("a"), // read obj.a
			iv(400), iv(50), sv("b"), // read obj.b
		),
	})

	// Check TaintedField
	queryField := &datalog.Query{
		Select: []datalog.Term{v("base"), v("field"), v("kind")},
		Body:   []datalog.Literal{pos("TaintedField", v("base"), v("field"), v("kind"))},
	}

	rsField := planAndEval(t, AllSystemRules(), queryField, baseRels)
	if !resultContains(rsField, iv(50), sv("a"), sv("http_input")) {
		t.Errorf("expected TaintedField(50, a, http_input), got %v", rsField.Rows)
	}
	// Field b should NOT be tainted
	if resultContains(rsField, iv(50), sv("b"), sv("http_input")) {
		t.Errorf("field b should not be tainted, got %v", rsField.Rows)
	}

	// Check TaintedSym from field read
	querySym := &datalog.Query{
		Select: []datalog.Term{v("sym"), v("kind")},
		Body:   []datalog.Literal{pos("TaintedSym", v("sym"), v("kind"))},
	}

	rsSym := planAndEval(t, AllSystemRules(), querySym, baseRels)
	// readSymA (30) should be tainted — reading obj.a which is tainted
	if !resultContains(rsSym, iv(30), sv("http_input")) {
		t.Errorf("expected TaintedSym(30, http_input) from field read obj.a, got %v", rsSym.Rows)
	}
	// readSymB (40) should NOT be tainted — reading obj.b which is clean
	if resultContains(rsSym, iv(40), sv("http_input")) {
		t.Errorf("readSymB (40) should not be tainted from obj.b read, got %v", rsSym.Rows)
	}
}

// TestMultipleTaintKinds tests that different taint kinds are tracked separately.
func TestMultipleTaintKinds(t *testing.T) {
	// Two sources with different kinds: "env" and "http_input"
	// srcExpr1=100, srcSym1=10, kind1="env"
	// srcExpr2=200, srcSym2=20, kind2="http_input"
	baseRels := taintBaseRels(map[string]*eval.Relation{
		"TaintSource": makeRel("TaintSource", 2,
			iv(100), sv("env"),
			iv(200), sv("http_input"),
		),
		"ExprMayRef": makeRel("ExprMayRef", 2,
			iv(100), iv(10),
			iv(200), iv(20),
		),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("sym"), v("kind")},
		Body:   []datalog.Literal{pos("TaintedSym", v("sym"), v("kind"))},
	}

	rs := planAndEval(t, AllSystemRules(), query, baseRels)
	if !resultContains(rs, iv(10), sv("env")) {
		t.Errorf("expected TaintedSym(10, env), got %v", rs.Rows)
	}
	if !resultContains(rs, iv(20), sv("http_input")) {
		t.Errorf("expected TaintedSym(20, http_input), got %v", rs.Rows)
	}
	// sym 10 should NOT have "http_input" kind
	if resultContains(rs, iv(10), sv("http_input")) {
		t.Errorf("sym 10 should not have http_input taint, got %v", rs.Rows)
	}
	// sym 20 should NOT have "env" kind
	if resultContains(rs, iv(20), sv("env")) {
		t.Errorf("sym 20 should not have env taint, got %v", rs.Rows)
	}
}

// TestTaintSanitized_TypeBased_Number verifies that taint is blocked when it
// flows into a symbol whose resolved type is a non-taintable primitive
// (e.g., the result of parseInt → number).
func TestTaintSanitized_TypeBased_Number(t *testing.T) {
	// Flow: let s = req.query.x; let n = parseInt(s); sink(n);
	// srcExpr=100, srcSym=10 (string, tainted)
	// dstSym=20 (number, should be sanitized by type)
	// sinkExpr=300, sinkSym=20
	// typeId=500 marked as NonTaintableType (number)
	baseRels := taintBaseRels(map[string]*eval.Relation{
		"TaintSource": makeRel("TaintSource", 2, iv(100), sv("http_input")),
		"TaintSink":   makeRel("TaintSink", 2, iv(300), sv("sql")),
		"ExprMayRef": makeRel("ExprMayRef", 2,
			iv(100), iv(10),
			iv(200), iv(10), // rhs of n = parseInt(s): expr refs s
			iv(300), iv(20), // sink expr refs n
		),
		"Assign": makeRel("Assign", 3, iv(210), iv(200), iv(20)),
		"SymInFunction": makeRel("SymInFunction", 2,
			iv(10), iv(1),
			iv(20), iv(1),
		),
		// Type-based sanitizer: dstSym 20 has type 500, which is number.
		"SymbolType":       makeRel("SymbolType", 2, iv(20), iv(500)),
		"NonTaintableType": makeRel("NonTaintableType", 1, iv(500)),
	})

	// First assert SanitizedEdge(10, 20, http_input) is derived.
	edgeQuery := &datalog.Query{
		Select: []datalog.Term{v("src"), v("dst"), v("kind")},
		Body:   []datalog.Literal{pos("SanitizedEdge", v("src"), v("dst"), v("kind"))},
	}
	rsEdge := planAndEval(t, AllSystemRules(), edgeQuery, baseRels)
	if !resultContains(rsEdge, iv(10), iv(20), sv("http_input")) {
		t.Errorf("expected SanitizedEdge(10, 20, http_input) from type-based sanitizer, got %v", rsEdge.Rows)
	}

	// Then: no TaintAlert should be produced because sym 20 is type-sanitized.
	alertQuery := &datalog.Query{
		Select: []datalog.Term{v("srcExpr"), v("sinkExpr"), v("srcKind"), v("sinkKind")},
		Body:   []datalog.Literal{pos("TaintAlert", v("srcExpr"), v("sinkExpr"), v("srcKind"), v("sinkKind"))},
	}
	rs := planAndEval(t, AllSystemRules(), alertQuery, baseRels)
	for _, row := range rs.Rows {
		if len(row) >= 2 && row[0] == iv(100) && row[1] == iv(300) {
			t.Errorf("expected no TaintAlert through type-based sanitizer, got %v", row)
		}
	}
}

// TestTaintSanitized_TypeBased_StringPassthrough verifies that taint is NOT
// blocked when the destination symbol has a taintable (string) type — i.e.,
// the type-based sanitizer rule only fires for non-taintable primitives.
func TestTaintSanitized_TypeBased_StringPassthrough(t *testing.T) {
	// Same shape as above but dstSym 20 has a string type (not in NonTaintableType).
	baseRels := taintBaseRels(map[string]*eval.Relation{
		"TaintSource": makeRel("TaintSource", 2, iv(100), sv("http_input")),
		"TaintSink":   makeRel("TaintSink", 2, iv(300), sv("sql")),
		"ExprMayRef": makeRel("ExprMayRef", 2,
			iv(100), iv(10),
			iv(200), iv(10),
			iv(300), iv(20),
		),
		"Assign": makeRel("Assign", 3, iv(210), iv(200), iv(20)),
		"SymInFunction": makeRel("SymInFunction", 2,
			iv(10), iv(1),
			iv(20), iv(1),
		),
		// Symbol type present, but type id 600 is NOT in NonTaintableType.
		"SymbolType":       makeRel("SymbolType", 2, iv(20), iv(600)),
		"NonTaintableType": eval.NewRelation("NonTaintableType", 1),
	})

	alertQuery := &datalog.Query{
		Select: []datalog.Term{v("srcExpr"), v("sinkExpr"), v("srcKind"), v("sinkKind")},
		Body:   []datalog.Literal{pos("TaintAlert", v("srcExpr"), v("sinkExpr"), v("srcKind"), v("sinkKind"))},
	}
	rs := planAndEval(t, AllSystemRules(), alertQuery, baseRels)
	if !resultContains(rs, iv(100), iv(300), sv("http_input"), sv("sql")) {
		t.Errorf("expected TaintAlert(100, 300, http_input, sql) — string type should not sanitize, got %v", rs.Rows)
	}
}

// TestTaintedSym_VarDeclInit tests Rule 1b: taint propagation via VarDecl init
// for FieldRead-based sources that lack ExprMayRef entries.
func TestTaintedSym_VarDeclInit(t *testing.T) {
	// TaintSource(expr=100, "http_input"), VarDecl(_, sym=10, initExpr=100, _)
	// → TaintedSym(10, "http_input") via Rule 1b
	baseRels := taintBaseRels(map[string]*eval.Relation{
		"TaintSource": makeRel("TaintSource", 2, iv(100), sv("http_input")),
		"VarDecl":     makeRel("VarDecl", 4, iv(50), iv(10), iv(100), iv(0)),
		// No ExprMayRef for expr 100 — this is the FieldRead case
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("sym"), v("kind")},
		Body:   []datalog.Literal{pos("TaintedSym", v("sym"), v("kind"))},
	}

	rs := planAndEval(t, AllSystemRules(), query, baseRels)
	if !resultContains(rs, iv(10), sv("http_input")) {
		t.Errorf("expected TaintedSym(10, http_input) via VarDecl init (Rule 1b), got %v", rs.Rows)
	}
}

// TestTaintAlert_VarDeclSource tests Rule 6b: TaintAlert via VarDecl linkage
// when the source expression is a FieldRead without ExprMayRef.
func TestTaintAlert_VarDeclSource(t *testing.T) {
	// Source: TaintSource(100, "http_input") with VarDecl(_, 10, 100, _)
	// Sink: TaintSink(200, "xss")
	// Rule 1b gives TaintedSym(10, "http_input"), Rule 6b gives TaintAlert.
	baseRels := taintBaseRels(map[string]*eval.Relation{
		"TaintSource": makeRel("TaintSource", 2, iv(100), sv("http_input")),
		"VarDecl":     makeRel("VarDecl", 4, iv(50), iv(10), iv(100), iv(0)),
		"TaintSink":   makeRel("TaintSink", 2, iv(200), sv("xss")),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("srcExpr"), v("sinkExpr"), v("srcKind"), v("sinkKind")},
		Body:   []datalog.Literal{pos("TaintAlert", v("srcExpr"), v("sinkExpr"), v("srcKind"), v("sinkKind"))},
	}

	rs := planAndEval(t, AllSystemRules(), query, baseRels)
	if !resultContains(rs, iv(100), iv(200), sv("http_input"), sv("xss")) {
		t.Errorf("expected TaintAlert(100, 200, http_input, xss) via Rule 6b, got %v", rs.Rows)
	}
}

// TestTaintAlert_VarDeclSource_CrossProduct documents the known precision
// limitation of Rule 6b: independent source/sink pairs across functions
// produce cross-product false positives because the sink side lacks function
// scope constraints (no ExprInFunction relation exists yet).
func TestTaintAlert_VarDeclSource_CrossProduct(t *testing.T) {
	// Source: TaintSource(100, "http_input") → VarDecl sym 10, sink 200 (xss)
	// Unrelated sink 300 (sql) in a different part of the program
	baseRels := taintBaseRels(map[string]*eval.Relation{
		"TaintSource": makeRel("TaintSource", 2, iv(100), sv("http_input")),
		"VarDecl":     makeRel("VarDecl", 4, iv(50), iv(10), iv(100), iv(0)),
		"TaintSink": makeRel("TaintSink", 2,
			iv(200), sv("xss"),
			iv(300), sv("sql"), // unrelated sink
		),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("srcExpr"), v("sinkExpr"), v("srcKind"), v("sinkKind")},
		Body:   []datalog.Literal{pos("TaintAlert", v("srcExpr"), v("sinkExpr"), v("srcKind"), v("sinkKind"))},
	}

	rs := planAndEval(t, AllSystemRules(), query, baseRels)

	// Known limitation: Rule 6b produces alerts for BOTH sinks, even though
	// sink 300 is unrelated. This cross-product false positive will be fixed
	// when ExprInFunction is added to the schema.
	gotXss := resultContains(rs, iv(100), iv(200), sv("http_input"), sv("xss"))
	gotSql := resultContains(rs, iv(100), iv(300), sv("http_input"), sv("sql"))

	if !gotXss {
		t.Errorf("expected TaintAlert for connected sink 200, got %v", rs.Rows)
	}
	if !gotSql {
		// When this starts failing, the cross-product fix has landed —
		// update this test to assert !gotSql instead.
		t.Log("cross-product false positive for sink 300 is expected (known Rule 6b limitation)")
	}
}

// TestTaintRulesValidate verifies all taint rules pass the planner's validation.
func TestTaintRulesValidate(t *testing.T) {
	for i, r := range TaintRules() {
		errs := plan.ValidateRule(r)
		if len(errs) > 0 {
			t.Errorf("rule %d (%s) validation errors: %v", i, r.Head.Predicate, errs)
		}
	}
}

// TestTaintRulesStratify verifies taint rules stratify together with all other system rules.
func TestTaintRulesStratify(t *testing.T) {
	prog := &datalog.Program{Rules: AllSystemRules()}
	_, errs := plan.Plan(prog, nil)
	if len(errs) > 0 {
		t.Fatalf("all system rules (including taint) failed to plan: %v", errs)
	}
}

// TestTaintRulesCount verifies we produce exactly 9 taint rules.
func TestTaintRulesCount(t *testing.T) {
	rules := TaintRules()
	if len(rules) != 9 {
		t.Errorf("expected 9 taint rules, got %d", len(rules))
	}
}

// TestAllSystemRulesCountWithTaint verifies AllSystemRules includes taint rules.
func TestAllSystemRulesCountWithTaint(t *testing.T) {
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
