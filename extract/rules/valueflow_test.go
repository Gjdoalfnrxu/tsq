package rules

import (
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/eval"
)

// valueFlowBaseRels returns the empty base relations needed to evaluate
// ParamBinding (the only rule in ValueFlowRules), with overrides applied.
func valueFlowBaseRels(overrides map[string]*eval.Relation) map[string]*eval.Relation {
	base := compositionBaseRels(nil)
	// CallArgSpread + ParameterRest must be present (negated literals require
	// the relation to exist in the eval context).
	base["CallArgSpread"] = eval.NewRelation("CallArgSpread", 2)
	base["ParameterRest"] = eval.NewRelation("ParameterRest", 2)
	base["ParameterDestructured"] = eval.NewRelation("ParameterDestructured", 2)
	for k, v := range overrides {
		base[k] = v
	}
	return base
}

// TestParamBinding_DirectCall verifies that
//
//	function inc(prev) { return prev + 1; }
//	inc(7);
//
// emits ParamBinding(fn=inc, idx=0, paramSym=prev, argExpr=7-literal).
func TestParamBinding_DirectCall(t *testing.T) {
	// fn=1, paramSym=10, paramNode=80
	// Call site: call=300, argExpr=400 (the `7` literal)
	// Resolution: CallCalleeSym(300, calleeSym=500), FunctionSymbol(500, 1)
	baseRels := valueFlowBaseRels(map[string]*eval.Relation{
		"Parameter":      makeRel("Parameter", 6, iv(1), iv(0), sv("prev"), iv(80), iv(10), sv("")),
		"CallCalleeSym":  makeRel("CallCalleeSym", 2, iv(300), iv(500)),
		"FunctionSymbol": makeRel("FunctionSymbol", 2, iv(500), iv(1)),
		"CallArg":        makeRel("CallArg", 3, iv(300), iv(0), iv(400)),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("fn"), v("idx"), v("paramSym"), v("argExpr")},
		Body: []datalog.Literal{
			pos("ParamBinding", v("fn"), v("idx"), v("paramSym"), v("argExpr")),
		},
	}
	rs := planAndEval(t, AllSystemRules(), query, baseRels)
	if len(rs.Rows) != 1 {
		t.Fatalf("expected 1 ParamBinding row, got %d: %v", len(rs.Rows), rs.Rows)
	}
	if !resultContains(rs, iv(1), iv(0), iv(10), iv(400)) {
		t.Errorf("expected ParamBinding(1, 0, 10, 400), got %v", rs.Rows)
	}
}

// TestParamBinding_MultiArgMultiCall covers the worked example from the plan:
//
//	function add(a, b) { return a + b; }
//	add(1, 2);
//	add(x, 3);
//
// Expected: 4 ParamBinding rows.
func TestParamBinding_MultiArgMultiCall(t *testing.T) {
	// fn=1, paramSyms a=10, b=11
	// Call sites: call1=300 (args 1=400, 2=401), call2=301 (args x=402, 3=403)
	baseRels := valueFlowBaseRels(map[string]*eval.Relation{
		"Parameter": makeRel("Parameter", 6,
			iv(1), iv(0), sv("a"), iv(80), iv(10), sv(""),
			iv(1), iv(1), sv("b"), iv(81), iv(11), sv(""),
		),
		"CallCalleeSym": makeRel("CallCalleeSym", 2,
			iv(300), iv(500),
			iv(301), iv(500),
		),
		"FunctionSymbol": makeRel("FunctionSymbol", 2, iv(500), iv(1)),
		"CallArg": makeRel("CallArg", 3,
			iv(300), iv(0), iv(400),
			iv(300), iv(1), iv(401),
			iv(301), iv(0), iv(402),
			iv(301), iv(1), iv(403),
		),
	})
	query := &datalog.Query{
		Select: []datalog.Term{v("fn"), v("idx"), v("paramSym"), v("argExpr")},
		Body: []datalog.Literal{
			pos("ParamBinding", v("fn"), v("idx"), v("paramSym"), v("argExpr")),
		},
	}
	rs := planAndEval(t, AllSystemRules(), query, baseRels)
	if len(rs.Rows) != 4 {
		t.Fatalf("expected 4 ParamBinding rows, got %d: %v", len(rs.Rows), rs.Rows)
	}
}

// TestParamBinding_SpreadArgSkipped verifies the carve-out: a spread-argument
// position emits 0 ParamBinding rows.
func TestParamBinding_SpreadArgSkipped(t *testing.T) {
	baseRels := valueFlowBaseRels(map[string]*eval.Relation{
		"Parameter":      makeRel("Parameter", 6, iv(1), iv(0), sv("a"), iv(80), iv(10), sv("")),
		"CallCalleeSym":  makeRel("CallCalleeSym", 2, iv(300), iv(500)),
		"FunctionSymbol": makeRel("FunctionSymbol", 2, iv(500), iv(1)),
		"CallArg":        makeRel("CallArg", 3, iv(300), iv(0), iv(400)),
		"CallArgSpread":  makeRel("CallArgSpread", 2, iv(300), iv(0)),
	})
	query := &datalog.Query{
		Select: []datalog.Term{v("fn"), v("idx"), v("paramSym"), v("argExpr")},
		Body: []datalog.Literal{
			pos("ParamBinding", v("fn"), v("idx"), v("paramSym"), v("argExpr")),
		},
	}
	rs := planAndEval(t, AllSystemRules(), query, baseRels)
	if len(rs.Rows) != 0 {
		t.Errorf("expected 0 ParamBinding rows for spread arg, got %d: %v", len(rs.Rows), rs.Rows)
	}
}

// TestParamBinding_RestParamSkipped verifies the carve-out: a rest-parameter
// position emits 0 ParamBinding rows.
func TestParamBinding_RestParamSkipped(t *testing.T) {
	baseRels := valueFlowBaseRels(map[string]*eval.Relation{
		"Parameter":      makeRel("Parameter", 6, iv(1), iv(0), sv("args"), iv(80), iv(10), sv("")),
		"ParameterRest":  makeRel("ParameterRest", 2, iv(1), iv(0)),
		"CallCalleeSym":  makeRel("CallCalleeSym", 2, iv(300), iv(500)),
		"FunctionSymbol": makeRel("FunctionSymbol", 2, iv(500), iv(1)),
		"CallArg":        makeRel("CallArg", 3, iv(300), iv(0), iv(400)),
	})
	query := &datalog.Query{
		Select: []datalog.Term{v("fn"), v("idx"), v("paramSym"), v("argExpr")},
		Body: []datalog.Literal{
			pos("ParamBinding", v("fn"), v("idx"), v("paramSym"), v("argExpr")),
		},
	}
	rs := planAndEval(t, AllSystemRules(), query, baseRels)
	if len(rs.Rows) != 0 {
		t.Errorf("expected 0 ParamBinding rows for rest param, got %d: %v", len(rs.Rows), rs.Rows)
	}
}

// TestParamBinding_UnresolvedCallSkipped verifies that a call with no
// CallTarget resolution emits no ParamBinding row.
func TestParamBinding_UnresolvedCallSkipped(t *testing.T) {
	// CallCalleeSym present but no FunctionSymbol → CallTarget never derived.
	baseRels := valueFlowBaseRels(map[string]*eval.Relation{
		"Parameter":     makeRel("Parameter", 6, iv(1), iv(0), sv("a"), iv(80), iv(10), sv("")),
		"CallCalleeSym": makeRel("CallCalleeSym", 2, iv(300), iv(500)),
		// No FunctionSymbol — call is unresolved.
		"CallArg": makeRel("CallArg", 3, iv(300), iv(0), iv(400)),
	})
	query := &datalog.Query{
		Select: []datalog.Term{v("fn"), v("idx"), v("paramSym"), v("argExpr")},
		Body: []datalog.Literal{
			pos("ParamBinding", v("fn"), v("idx"), v("paramSym"), v("argExpr")),
		},
	}
	rs := planAndEval(t, AllSystemRules(), query, baseRels)
	if len(rs.Rows) != 0 {
		t.Errorf("expected 0 ParamBinding rows for unresolved call, got %d: %v", len(rs.Rows), rs.Rows)
	}
}

// TestValueFlowRulesCount documents the rule count (2 today: ParamBinding via
// CallTarget and via CallTargetRTA; will grow in PR3).
func TestValueFlowRulesCount(t *testing.T) {
	rules := ValueFlowRules()
	if len(rules) != 2 {
		t.Errorf("expected 2 value-flow rules (ParamBinding x2), got %d", len(rules))
	}
}

// TestParamBinding_DestructuredParamSkipped verifies that destructured
// parameter slots (ObjectPattern / ArrayPattern) emit no ParamBinding rows.
//
// Models: `function f({a, b}, [x, y], z) {} ; f(o, arr, 5);`
// Slot 0 (ObjectPattern) and slot 1 (ArrayPattern) are flagged via
// ParameterDestructured and must NOT produce bindings; slot 2 (`z`) must.
func TestParamBinding_DestructuredParamSkipped(t *testing.T) {
	// fn=1; paramSyms slot0=10 (bogus, "{a, b}"), slot1=11 (bogus, "[x, y]"),
	// slot2=12 (z). Call: call=300, args 400 (o), 401 (arr), 402 (5).
	baseRels := valueFlowBaseRels(map[string]*eval.Relation{
		"Parameter": makeRel("Parameter", 6,
			iv(1), iv(0), sv("{a, b}"), iv(80), iv(10), sv(""),
			iv(1), iv(1), sv("[x, y]"), iv(81), iv(11), sv(""),
			iv(1), iv(2), sv("z"), iv(82), iv(12), sv(""),
		),
		"ParameterDestructured": makeRel("ParameterDestructured", 2,
			iv(1), iv(0),
			iv(1), iv(1),
		),
		"CallCalleeSym":  makeRel("CallCalleeSym", 2, iv(300), iv(500)),
		"FunctionSymbol": makeRel("FunctionSymbol", 2, iv(500), iv(1)),
		"CallArg": makeRel("CallArg", 3,
			iv(300), iv(0), iv(400),
			iv(300), iv(1), iv(401),
			iv(300), iv(2), iv(402),
		),
	})
	query := &datalog.Query{
		Select: []datalog.Term{v("fn"), v("idx"), v("paramSym"), v("argExpr")},
		Body: []datalog.Literal{
			pos("ParamBinding", v("fn"), v("idx"), v("paramSym"), v("argExpr")),
		},
	}
	rs := planAndEval(t, AllSystemRules(), query, baseRels)
	if len(rs.Rows) != 1 {
		t.Fatalf("expected exactly 1 ParamBinding row (only slot z=2), got %d: %v", len(rs.Rows), rs.Rows)
	}
	if !resultContains(rs, iv(1), iv(2), iv(12), iv(402)) {
		t.Errorf("expected ParamBinding(fn=1, idx=2, sym=12, arg=402), got %v", rs.Rows)
	}
}
