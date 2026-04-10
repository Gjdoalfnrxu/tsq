package rules

import (
	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
)

// s returns a StringConst term.
func s(val string) datalog.StringConst { return datalog.StringConst{Value: val} }

// FrameworkRules returns the system Datalog rules for framework-specific
// taint source/sink identification (Phase F). These are pattern-based
// heuristics that match on function names and method names to populate
// TaintSource, TaintSink, and ExpressHandler relations.
func FrameworkRules() []datalog.Rule {
	return []datalog.Rule{
		// ─── Express handler detection ───────────────────────────────────
		// ExpressHandler(fn) :-
		//   MethodCall(call, recv, "get"), CallArg(call, _, cbExpr),
		//   ExprMayRef(cbExpr, cbSym), FunctionSymbol(cbSym, fn).
		// (Also for "post", "put", "delete", "use", "patch")
		expressHandlerRule("get"),
		expressHandlerRule("post"),
		expressHandlerRule("put"),
		expressHandlerRule("delete"),
		expressHandlerRule("use"),
		expressHandlerRule("patch"),

		// ─── Express sources: req.query ──────────────────────────────────
		// TaintSource(callExpr, "http_input") :-
		//   MethodCall(call, recv, "query"), ExprMayRef(recv, reqSym),
		//   Parameter(fn, 0, "req", _, reqSym, _), ExpressHandler(fn),
		//   ExprMayRef(call, callExpr).
		// Note: MethodCall here represents property access like req.query.
		// Since we match on FieldRead instead (req.query is a field read, not method call):
		// TaintSource(expr, "http_input") :-
		//   FieldRead(expr, reqSym, "query"),
		//   Parameter(fn, 0, _, _, reqSym, _), ExpressHandler(fn).
		rule("TaintSource",
			[]datalog.Term{v("expr"), s("http_input")},
			pos("FieldRead", v("expr"), v("reqSym"), s("query")),
			pos("Parameter", v("fn"), datalog.IntConst{Value: 0}, w(), w(), v("reqSym"), w()),
			pos("ExpressHandler", v("fn")),
		),

		// TaintSource(expr, "http_input") :- FieldRead(expr, reqSym, "params"), ...
		rule("TaintSource",
			[]datalog.Term{v("expr"), s("http_input")},
			pos("FieldRead", v("expr"), v("reqSym"), s("params")),
			pos("Parameter", v("fn"), datalog.IntConst{Value: 0}, w(), w(), v("reqSym"), w()),
			pos("ExpressHandler", v("fn")),
		),

		// TaintSource(expr, "http_input") :- FieldRead(expr, reqSym, "body"), ...
		rule("TaintSource",
			[]datalog.Term{v("expr"), s("http_input")},
			pos("FieldRead", v("expr"), v("reqSym"), s("body")),
			pos("Parameter", v("fn"), datalog.IntConst{Value: 0}, w(), w(), v("reqSym"), w()),
			pos("ExpressHandler", v("fn")),
		),

		// ─── Express sinks: res.send ─────────────────────────────────────
		// TaintSink(argExpr, "xss") :-
		//   MethodCall(call, recv, "send"), ExprMayRef(recv, resSym),
		//   Parameter(fn, 1, _, _, resSym, _), ExpressHandler(fn),
		//   CallArg(call, 0, argExpr).
		rule("TaintSink",
			[]datalog.Term{v("argExpr"), s("xss")},
			pos("MethodCall", v("call"), v("recv"), s("send")),
			pos("ExprMayRef", v("recv"), v("resSym")),
			pos("Parameter", v("fn"), datalog.IntConst{Value: 1}, w(), w(), v("resSym"), w()),
			pos("ExpressHandler", v("fn")),
			pos("CallArg", v("call"), datalog.IntConst{Value: 0}, v("argExpr")),
		),

		// ─── Node.js sinks: child_process.exec ──────────────────────────
		// TaintSink(argExpr, "command_injection") :-
		//   CallCalleeSym(call, execSym), FunctionSymbol(execSym, execFn),
		//   Function(execFn, "exec", _, _, _, _), CallArg(call, 0, argExpr).
		rule("TaintSink",
			[]datalog.Term{v("argExpr"), s("command_injection")},
			pos("CallCalleeSym", v("call"), v("execSym")),
			pos("FunctionSymbol", v("execSym"), v("execFn")),
			pos("Function", v("execFn"), s("exec"), w(), w(), w(), w()),
			pos("CallArg", v("call"), datalog.IntConst{Value: 0}, v("argExpr")),
		),

		// ─── React XSS: dangerouslySetInnerHTML ─────────────────────────
		// TaintSink(valueExpr, "xss") :-
		//   JsxAttribute(elem, "dangerouslySetInnerHTML", valueExpr).
		rule("TaintSink",
			[]datalog.Term{v("valueExpr"), s("xss")},
			pos("JsxAttribute", w(), s("dangerouslySetInnerHTML"), v("valueExpr")),
		),
	}
}

// expressHandlerRule generates:
//
//	ExpressHandler(fn) :-
//	  MethodCall(call, _, methodName), CallArg(call, _, cbExpr),
//	  ExprMayRef(cbExpr, cbSym), FunctionSymbol(cbSym, fn).
func expressHandlerRule(methodName string) datalog.Rule {
	return rule("ExpressHandler",
		[]datalog.Term{v("fn")},
		pos("MethodCall", v("call"), w(), s(methodName)),
		pos("CallArg", v("call"), w(), v("cbExpr")),
		pos("ExprMayRef", v("cbExpr"), v("cbSym")),
		pos("FunctionSymbol", v("cbSym"), v("fn")),
	)
}
