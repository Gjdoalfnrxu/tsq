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
		// Expression-in-function scoping for Rule 6b
		"ExprInFunction": eval.NewRelation("ExprInFunction", 2),
		// Contains: parent-child AST containment (used by SinkContains
		// transitive closure for Rule 6b SinkRefSym helper).
		"Contains": eval.NewRelation("Contains", 2),
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
//
// Updated for issue #113 fix: Rule 6b now requires the sink expression to
// actually reference the tainted symbol (directly via ExprMayRef on the
// sink itself, or via a CallArg sub-expression). The previous fixture was
// missing that link and was passing only because Rule 6b accepted any
// tainted sym in the same function — the false-positive shape this fix
// removes. The added ExprMayRef(200, 10) makes this a real direct sink.
func TestTaintAlert_VarDeclSource(t *testing.T) {
	// Source: TaintSource(100, "http_input") with VarDecl(_, 10, 100, _)
	// Sink: TaintSink(200, "xss") where sink expression 200 references sym 10
	//       directly (e.g. `let x = req.body; xssSink(x);` where the sink
	//       expression is the identifier `x`).
	// Rule 1b gives TaintedSym(10, "http_input"); Rule 6b variant A
	// (direct identifier sink) gives TaintAlert.
	baseRels := taintBaseRels(map[string]*eval.Relation{
		"TaintSource":    makeRel("TaintSource", 2, iv(100), sv("http_input")),
		"VarDecl":        makeRel("VarDecl", 4, iv(50), iv(10), iv(100), iv(0)),
		"TaintSink":      makeRel("TaintSink", 2, iv(200), sv("xss")),
		"ExprMayRef":     makeRel("ExprMayRef", 2, iv(200), iv(10)),
		"SymInFunction":  makeRel("SymInFunction", 2, iv(10), iv(1)),
		"ExprInFunction": makeRel("ExprInFunction", 2, iv(200), iv(1)),
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

// TestTaintAlert_VarDeclSource_CrossProduct verifies that Rule 6b does not
// produce cross-product false positives across functions. Sinks in different
// functions are excluded because the unrelated sink does not reference any
// symbol reachable from the tainted VarDecl symbol (post-#113-fix the rule
// requires a real source→sink-symbol link, which incidentally also rules out
// cross-function false positives).
//
// Updated for issue #113: connected sink 200 now includes ExprMayRef(200, 10)
// so it actually references the tainted sym (direct identifier sink). The
// unrelated sink 300 is in a different function with no link to sym 10 and
// must not alert.
func TestTaintAlert_VarDeclSource_CrossProduct(t *testing.T) {
	// Source: TaintSource(100, "http_input") -> VarDecl sym 10 in fn 1
	// Connected sink 200 (xss) is in fn 1 and references sym 10
	// Unrelated sink 300 (sql) is in fn 2 and references unrelated sym 30
	baseRels := taintBaseRels(map[string]*eval.Relation{
		"TaintSource": makeRel("TaintSource", 2, iv(100), sv("http_input")),
		"VarDecl":     makeRel("VarDecl", 4, iv(50), iv(10), iv(100), iv(0)),
		"TaintSink": makeRel("TaintSink", 2,
			iv(200), sv("xss"),
			iv(300), sv("sql"),
		),
		"ExprMayRef": makeRel("ExprMayRef", 2,
			iv(200), iv(10), // connected sink references tainted sym
			iv(300), iv(30), // unrelated sink references unrelated sym
		),
		"SymInFunction": makeRel("SymInFunction", 2,
			iv(10), iv(1),
			iv(30), iv(2),
		),
		"ExprInFunction": makeRel("ExprInFunction", 2,
			iv(200), iv(1),
			iv(300), iv(2),
		),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("srcExpr"), v("sinkExpr"), v("srcKind"), v("sinkKind")},
		Body:   []datalog.Literal{pos("TaintAlert", v("srcExpr"), v("sinkExpr"), v("srcKind"), v("sinkKind"))},
	}

	rs := planAndEval(t, AllSystemRules(), query, baseRels)

	gotXss := resultContains(rs, iv(100), iv(200), sv("http_input"), sv("xss"))
	gotSql := resultContains(rs, iv(100), iv(300), sv("http_input"), sv("sql"))

	if !gotXss {
		t.Errorf("expected TaintAlert for connected sink 200, got %v", rs.Rows)
	}
	if gotSql {
		t.Errorf("cross-product false positive: got TaintAlert for unrelated sink 300 in different function, got %v", rs.Rows)
	}
}

// TestTaintAlert_Rule6b_UnrelatedSinkNoAlert is the regression test for issue #113.
//
// Pre-fix bug: TaintAlert Rule 6b (VarDecl-init source path) fired whenever ANY
// tainted symbol of the same kind existed in the same function as the sink
// expression — without requiring that the sink expression actually reference
// (directly or via flow) the tainted symbol. Cross-symbol false positive cannon.
//
// Setup: function fn=1 contains
//   - a tainted VarDecl (sym=10 fed by FieldRead source 100, "http_input")
//   - an unrelated clean variable (sym=20) referenced by a sink expression 200
//
// There is no flow from sym 10 to sym 20. The sink references sym 20 only.
// Expectation: NO TaintAlert is emitted. Pre-fix, an alert was incorrectly
// produced because Rule 6b picked sinkSym=10 (the only tainted sym in the
// function) and accepted sinkExpr=200 purely on ExprInFunction match,
// ignoring whether the sink actually references the tainted value.
func TestTaintAlert_Rule6b_UnrelatedSinkNoAlert(t *testing.T) {
	baseRels := taintBaseRels(map[string]*eval.Relation{
		// FieldRead-shaped source: req.body initialises sym 10 via VarDecl.
		// No ExprMayRef for srcExpr 100 (this is the FieldRead branch that
		// motivates Rule 6b).
		"TaintSource": makeRel("TaintSource", 2, iv(100), sv("http_input")),
		"VarDecl":     makeRel("VarDecl", 4, iv(50), iv(10), iv(100), iv(0)),
		// Sink references an unrelated, clean symbol 20 (e.g. a literal/local
		// variable that never received tainted data).
		"TaintSink":  makeRel("TaintSink", 2, iv(200), sv("sql")),
		"ExprMayRef": makeRel("ExprMayRef", 2, iv(200), iv(20)),
		// Both syms live in fn=1; sink expression also in fn=1.
		"SymInFunction": makeRel("SymInFunction", 2,
			iv(10), iv(1),
			iv(20), iv(1),
		),
		"ExprInFunction": makeRel("ExprInFunction", 2, iv(200), iv(1)),
		// Crucially: NO Assign / VarDecl / FieldWrite / etc. linking sym 10 to
		// sym 20. No LocalFlow, no FlowStar between them.
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("srcExpr"), v("sinkExpr"), v("srcKind"), v("sinkKind")},
		Body:   []datalog.Literal{pos("TaintAlert", v("srcExpr"), v("sinkExpr"), v("srcKind"), v("sinkKind"))},
	}

	rs := planAndEval(t, AllSystemRules(), query, baseRels)
	for _, row := range rs.Rows {
		if len(row) >= 2 && row[0] == iv(100) && row[1] == iv(200) {
			t.Fatalf("issue #113: TaintAlert(100, 200, ...) fired without a flow link "+
				"between source sym 10 and sink sym 20 in the same function. "+
				"Rule 6b must require a real source→sink connection. Got rows: %v", rs.Rows)
		}
	}
}

// TestTaintAlert_Rule6b_FlowToSinkAlerts is the positive companion to the
// negative test above. Same shape but with a real flow chain from sym 10 to
// sym 20 (an Assign that reads from sym 10 and writes to sym 20). This MUST
// still alert — the fix must not over-tighten Rule 6b into uselessness.
func TestTaintAlert_Rule6b_FlowToSinkAlerts(t *testing.T) {
	baseRels := taintBaseRels(map[string]*eval.Relation{
		// FieldRead-shaped source on sym 10.
		"TaintSource": makeRel("TaintSource", 2, iv(100), sv("http_input")),
		"VarDecl":     makeRel("VarDecl", 4, iv(50), iv(10), iv(100), iv(0)),
		// Real flow: y = x → LocalFlow(fn, 10, 20).
		"Assign": makeRel("Assign", 3, iv(310), iv(300), iv(20)),
		"ExprMayRef": makeRel("ExprMayRef", 2,
			iv(300), iv(10), // rhs of y = x references sym 10
			iv(200), iv(20), // sink references sym 20
		),
		"TaintSink": makeRel("TaintSink", 2, iv(200), sv("sql")),
		"SymInFunction": makeRel("SymInFunction", 2,
			iv(10), iv(1),
			iv(20), iv(1),
		),
		"ExprInFunction": makeRel("ExprInFunction", 2, iv(200), iv(1)),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("srcExpr"), v("sinkExpr"), v("srcKind"), v("sinkKind")},
		Body:   []datalog.Literal{pos("TaintAlert", v("srcExpr"), v("sinkExpr"), v("srcKind"), v("sinkKind"))},
	}

	rs := planAndEval(t, AllSystemRules(), query, baseRels)
	if !resultContains(rs, iv(100), iv(200), sv("http_input"), sv("sql")) {
		t.Errorf("expected TaintAlert(100, 200, http_input, sql) — real flow from sym 10 → 20 → sink should alert, got %v", rs.Rows)
	}
}

// TestTaintAlert_VarDeclSource_CompoundSink covers the descendant variant of
// SinkRefSym in Rule 6b. Models a compound sink shape `db.query('SELECT ' + x)`
// where the sink expression is a CallArg whose subtree (a BinaryExpression)
// contains the tainted identifier — there is NO direct ExprMayRef on the sink
// expression itself, only on a descendant. The descendant SinkRefSym rule
// (taint.go:212-219) is the only path that can reach this alert.
//
// Mutation contract: commenting out the descendant SinkRefSym rule body must
// fail this test. Validated manually during PR #126 adversarial review.
func TestTaintAlert_VarDeclSource_CompoundSink(t *testing.T) {
	// Source: req.body initialises sym 10 via VarDecl; FieldRead-shaped
	// (no ExprMayRef on srcExpr 100), forces Rule 6b.
	// Sink: db.query('SELECT ' + x) — sink expression 200 is the CallArg
	// (a BinaryExpression). Its descendant 250 is the identifier `x`,
	// which ExprMayRefs sym 10. Critically NO ExprMayRef(200, 10).
	baseRels := taintBaseRels(map[string]*eval.Relation{
		"TaintSource": makeRel("TaintSource", 2, iv(100), sv("http_input")),
		"VarDecl":     makeRel("VarDecl", 4, iv(50), iv(10), iv(100), iv(0)),
		"TaintSink":   makeRel("TaintSink", 2, iv(200), sv("sql")),
		// Only the descendant identifier resolves to sym 10.
		// No ExprMayRef(200, 10) — sink expression itself never refs the sym.
		"ExprMayRef": makeRel("ExprMayRef", 2, iv(250), iv(10)),
		// AST containment: sink expr 200 (BinaryExpression) contains
		// descendant 250 (the identifier `x` in `'SELECT ' + x`).
		"Contains": makeRel("Contains", 2, iv(200), iv(250)),
		"SymInFunction": makeRel("SymInFunction", 2,
			iv(10), iv(1),
		),
		"ExprInFunction": makeRel("ExprInFunction", 2,
			iv(200), iv(1),
			iv(250), iv(1),
		),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("srcExpr"), v("sinkExpr"), v("srcKind"), v("sinkKind")},
		Body:   []datalog.Literal{pos("TaintAlert", v("srcExpr"), v("sinkExpr"), v("srcKind"), v("sinkKind"))},
	}

	rs := planAndEval(t, AllSystemRules(), query, baseRels)
	if !resultContains(rs, iv(100), iv(200), sv("http_input"), sv("sql")) {
		t.Errorf("expected TaintAlert(100, 200, http_input, sql) via descendant SinkRefSym "+
			"(compound sink — sink subtree contains tainted ident, no direct ExprMayRef on sink), "+
			"got %v", rs.Rows)
	}
}

// TestSinkRefSym_DescendantOnly is a focused unit on the descendant
// SinkRefSym rule head: with only Contains + ExprMayRef on the descendant
// (and TaintSink on the parent), SinkRefSym must derive (sinkExpr, sym).
// Direct counterpart absent.
func TestSinkRefSym_DescendantOnly(t *testing.T) {
	baseRels := taintBaseRels(map[string]*eval.Relation{
		"TaintSink":  makeRel("TaintSink", 2, iv(200), sv("sql")),
		"ExprMayRef": makeRel("ExprMayRef", 2, iv(250), iv(10)),
		"Contains":   makeRel("Contains", 2, iv(200), iv(250)),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("sinkExpr"), v("sym")},
		Body:   []datalog.Literal{pos("SinkRefSym", v("sinkExpr"), v("sym"))},
	}

	rs := planAndEval(t, AllSystemRules(), query, baseRels)
	if !resultContains(rs, iv(200), iv(10)) {
		t.Errorf("expected SinkRefSym(200, 10) via descendant rule, got %v", rs.Rows)
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

// TestTaintRulesHeads asserts the specific rule heads present in TaintRules,
// not just the count. A bare count assertion is a vacuous tripwire — refactors
// that split one rule and drop another would silently balance out. Asserting
// heads + multiplicities catches that class of accident while still tolerating
// rule body refactors.
//
// Expected multiplicities (from the Rule N comments in taint.go):
//
//	TaintedSym    × 4 (Rules 1, 1b, 2, 5)
//	SanitizedEdge × 2 (Rules 3, 3b)
//	TaintedField  × 1 (Rule 4)
//	TaintAlert    × 3 (Rule 6 main + Rule 6b variants A direct / B flow)
//	SinkContains  × 2 (Rule 6b helper transitive closure base + step)
//	SinkRefSym    × 2 (Rule 6b helper direct + descendant)
//
// Total: 14.
func TestTaintRulesHeads(t *testing.T) {
	rules := TaintRules()
	got := map[string]int{}
	for _, r := range rules {
		got[r.Head.Predicate]++
	}
	want := map[string]int{
		"TaintedSym":    4,
		"SanitizedEdge": 2,
		"TaintedField":  1,
		"TaintAlert":    3,
		"SinkContains":  2,
		"SinkRefSym":    2,
	}
	for head, n := range want {
		if got[head] != n {
			t.Errorf("rule head %q: want %d, got %d (full: %v)", head, n, got[head], got)
		}
	}
	for head := range got {
		if _, ok := want[head]; !ok {
			t.Errorf("unexpected rule head %q (count %d) in TaintRules — update want map if intentional", head, got[head])
		}
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
	vf := ValueFlowRules()
	expected := len(cg) + len(lf) + len(sm) + len(co) + len(ta) + len(fw) + len(ho) + len(vf)
	if len(all) != expected {
		t.Errorf("expected %d rules, got %d", expected, len(all))
	}
}
