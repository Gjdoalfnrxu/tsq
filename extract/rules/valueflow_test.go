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

// TestValueFlowRulesCount documents the rule count (3 after Phase C PR1:
// ParamBinding via CallTarget, ParamBinding via CallTargetRTA, and
// CallTargetCrossModule).
func TestValueFlowRulesCount(t *testing.T) {
	rules := ValueFlowRules()
	if len(rules) != 3 {
		t.Errorf("expected 3 value-flow rules (ParamBinding x2 + CallTargetCrossModule), got %d", len(rules))
	}
}

// callTargetCrossModuleBaseRels extends valueFlowBaseRels with the
// import/export EDB rels needed to evaluate CallTargetCrossModule.
func callTargetCrossModuleBaseRels(overrides map[string]*eval.Relation) map[string]*eval.Relation {
	base := valueFlowBaseRels(nil)
	base["ImportBinding"] = eval.NewRelation("ImportBinding", 3)
	base["ExportBinding"] = eval.NewRelation("ExportBinding", 3)
	for k, v := range overrides {
		base[k] = v
	}
	return base
}

// crossModuleQuery returns a Query that selects all CallTargetCrossModule rows.
func crossModuleQuery() *datalog.Query {
	return &datalog.Query{
		Select: []datalog.Term{v("call"), v("fn")},
		Body: []datalog.Literal{
			pos("CallTargetCrossModule", v("call"), v("fn")),
		},
	}
}

// TestCallTargetCrossModule_SingleHop verifies the canonical case:
//
//	// lib.ts
//	export function helper(x) { return x + 1; }
//	// consumer.ts
//	import { helper } from "./lib";
//	helper(7);
//
// emits CallTargetCrossModule(call=helper-call, fn=helper-fn).
func TestCallTargetCrossModule_SingleHop(t *testing.T) {
	// Import side: localSym=10 ("helper" in consumer.ts).
	// Export side: exportedSym=20 ("helper" in lib.ts), targetFn=1.
	// Call site: call=300, callee identifier resolves to localSym=10.
	baseRels := callTargetCrossModuleBaseRels(map[string]*eval.Relation{
		"CallCalleeSym":  makeRel("CallCalleeSym", 2, iv(300), iv(10)),
		"ImportBinding":  makeRel("ImportBinding", 3, iv(10), sv("./lib"), sv("helper")),
		"ExportBinding":  makeRel("ExportBinding", 3, sv("helper"), iv(20), iv(900)),
		"FunctionSymbol": makeRel("FunctionSymbol", 2, iv(20), iv(1)),
	})
	rs := planAndEval(t, AllSystemRules(), crossModuleQuery(), baseRels)
	if len(rs.Rows) != 1 {
		t.Fatalf("expected 1 CallTargetCrossModule row, got %d: %v", len(rs.Rows), rs.Rows)
	}
	if !resultContains(rs, iv(300), iv(1)) {
		t.Errorf("expected CallTargetCrossModule(300, 1), got %v", rs.Rows)
	}
}

// TestCallTargetCrossModule_NameMismatchSkipped verifies that an import of one
// name does not bridge to an export of a different name.
func TestCallTargetCrossModule_NameMismatchSkipped(t *testing.T) {
	baseRels := callTargetCrossModuleBaseRels(map[string]*eval.Relation{
		"CallCalleeSym":  makeRel("CallCalleeSym", 2, iv(300), iv(10)),
		"ImportBinding":  makeRel("ImportBinding", 3, iv(10), sv("./lib"), sv("helper")),
		"ExportBinding":  makeRel("ExportBinding", 3, sv("other"), iv(20), iv(900)),
		"FunctionSymbol": makeRel("FunctionSymbol", 2, iv(20), iv(1)),
	})
	rs := planAndEval(t, AllSystemRules(), crossModuleQuery(), baseRels)
	if len(rs.Rows) != 0 {
		t.Errorf("expected 0 CallTargetCrossModule rows on name mismatch, got %d: %v", len(rs.Rows), rs.Rows)
	}
}

// TestCallTargetCrossModule_NonFunctionExportSkipped verifies that an
// imported name resolving to a non-function exported symbol (e.g. `const VERSION = "1.0"`)
// does not produce a CallTargetCrossModule row — the FunctionSymbol join filters
// it out.
func TestCallTargetCrossModule_NonFunctionExportSkipped(t *testing.T) {
	baseRels := callTargetCrossModuleBaseRels(map[string]*eval.Relation{
		"CallCalleeSym": makeRel("CallCalleeSym", 2, iv(300), iv(10)),
		"ImportBinding": makeRel("ImportBinding", 3, iv(10), sv("./lib"), sv("VERSION")),
		"ExportBinding": makeRel("ExportBinding", 3, sv("VERSION"), iv(20), iv(900)),
		// No FunctionSymbol(20, _) — VERSION is not a function.
	})
	rs := planAndEval(t, AllSystemRules(), crossModuleQuery(), baseRels)
	if len(rs.Rows) != 0 {
		t.Errorf("expected 0 CallTargetCrossModule rows for non-function export, got %d: %v", len(rs.Rows), rs.Rows)
	}
}

// TestCallTargetCrossModule_NameCollisionOverBridges documents the v1
// over-bridging behaviour (plan §3.2 / §4.1): two modules exporting the same
// name produce two CallTargetCrossModule rows for one import/call. This is
// load-bearing — the bridge's `importedFunctionSymbol` has the same posture,
// and tightening to a per-module match requires a real module resolver
// (deferred). The test exists so the documented unsoundness can't drift
// silently.
func TestCallTargetCrossModule_NameCollisionOverBridges(t *testing.T) {
	// Two modules both export "helper": one resolves to fn=1, the other fn=2.
	// The call site imports "helper" from ./libA but the join is name-only,
	// so both fns surface. Documented unsoundness.
	baseRels := callTargetCrossModuleBaseRels(map[string]*eval.Relation{
		"CallCalleeSym": makeRel("CallCalleeSym", 2, iv(300), iv(10)),
		"ImportBinding": makeRel("ImportBinding", 3, iv(10), sv("./libA"), sv("helper")),
		"ExportBinding": makeRel("ExportBinding", 3,
			sv("helper"), iv(20), iv(900),
			sv("helper"), iv(21), iv(901),
		),
		"FunctionSymbol": makeRel("FunctionSymbol", 2,
			iv(20), iv(1),
			iv(21), iv(2),
		),
	})
	rs := planAndEval(t, AllSystemRules(), crossModuleQuery(), baseRels)
	if len(rs.Rows) != 2 {
		t.Fatalf("expected 2 CallTargetCrossModule rows for name collision (documented over-bridging), got %d: %v",
			len(rs.Rows), rs.Rows)
	}
	if !resultContains(rs, iv(300), iv(1)) || !resultContains(rs, iv(300), iv(2)) {
		t.Errorf("expected both CallTargetCrossModule(300, 1) and (300, 2), got %v", rs.Rows)
	}
}

// TestCallTargetCrossModule_LocalCallNotBridged verifies that a same-module
// call (no ImportBinding for the callee) emits no CallTargetCrossModule row —
// the cross-module rule must not subsume direct CallTarget cases.
func TestCallTargetCrossModule_LocalCallNotBridged(t *testing.T) {
	// localSym=10 has a CallCalleeSym row but is NOT in ImportBinding.
	baseRels := callTargetCrossModuleBaseRels(map[string]*eval.Relation{
		"CallCalleeSym":  makeRel("CallCalleeSym", 2, iv(300), iv(10)),
		"FunctionSymbol": makeRel("FunctionSymbol", 2, iv(10), iv(1)),
	})
	rs := planAndEval(t, AllSystemRules(), crossModuleQuery(), baseRels)
	if len(rs.Rows) != 0 {
		t.Errorf("expected 0 CallTargetCrossModule rows for local call, got %d: %v", len(rs.Rows), rs.Rows)
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
