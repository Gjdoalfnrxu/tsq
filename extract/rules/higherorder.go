package rules

import (
	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
)

// HigherOrderRules returns the system Datalog rules for higher-order function
// flow models (Phase F). These model dataflow through common higher-order
// patterns: Array.map/forEach/filter/reduce and Promise.then.
func HigherOrderRules() []datalog.Rule {
	return []datalog.Rule{
		// ─── Array.map callback flow ─────────────────────────────────────
		// InterFlow(arraySym, paramSym) :-
		//   MethodCall(call, arrayExpr, "map"),
		//   ExprMayRef(arrayExpr, arraySym),
		//   CallArg(call, 0, callbackExpr),
		//   ExprMayRef(callbackExpr, callbackSym),
		//   FunctionSymbol(callbackSym, callbackFn),
		//   Parameter(callbackFn, 0, _, _, paramSym, _).
		higherOrderArrayRule("map"),

		// ─── Array.forEach callback flow ─────────────────────────────────
		higherOrderArrayRule("forEach"),

		// ─── Array.filter callback flow ──────────────────────────────────
		higherOrderArrayRule("filter"),

		// ─── Array.reduce callback flow ──────────────────────────────────
		// For reduce, the current element is param index 1 (param 0 is the accumulator).
		// InterFlow(arraySym, paramSym) :-
		//   MethodCall(call, arrayExpr, "reduce"),
		//   ExprMayRef(arrayExpr, arraySym),
		//   CallArg(call, 0, callbackExpr),
		//   ExprMayRef(callbackExpr, callbackSym),
		//   FunctionSymbol(callbackSym, callbackFn),
		//   Parameter(callbackFn, 1, _, _, paramSym, _).
		rule("InterFlow",
			[]datalog.Term{v("arraySym"), v("paramSym")},
			pos("MethodCall", v("call"), v("arrayExpr"), s("reduce")),
			pos("ExprMayRef", v("arrayExpr"), v("arraySym")),
			mustNamedLiteral("CallArg", map[string]datalog.Term{
				"call":    v("call"),
				"idx":     datalog.IntConst{Value: 0},
				"argNode": v("callbackExpr"),
			}),
			pos("ExprMayRef", v("callbackExpr"), v("callbackSym")),
			pos("FunctionSymbol", v("callbackSym"), v("callbackFn")),
			pos("Parameter", v("callbackFn"), datalog.IntConst{Value: 1}, w(), w(), v("paramSym"), w()),
		),

		// ─── Promise.then callback flow ──────────────────────────────────
		// InterFlow(promiseSym, paramSym) :-
		//   MethodCall(call, promiseExpr, "then"),
		//   ExprMayRef(promiseExpr, promiseSym),
		//   CallArg(call, 0, callbackExpr),
		//   ExprMayRef(callbackExpr, callbackSym),
		//   FunctionSymbol(callbackSym, callbackFn),
		//   Parameter(callbackFn, 0, _, _, paramSym, _).
		rule("InterFlow",
			[]datalog.Term{v("promiseSym"), v("paramSym")},
			pos("MethodCall", v("call"), v("promiseExpr"), s("then")),
			pos("ExprMayRef", v("promiseExpr"), v("promiseSym")),
			mustNamedLiteral("CallArg", map[string]datalog.Term{
				"call":    v("call"),
				"idx":     datalog.IntConst{Value: 0},
				"argNode": v("callbackExpr"),
			}),
			pos("ExprMayRef", v("callbackExpr"), v("callbackSym")),
			pos("FunctionSymbol", v("callbackSym"), v("callbackFn")),
			pos("Parameter", v("callbackFn"), datalog.IntConst{Value: 0}, w(), w(), v("paramSym"), w()),
		),
	}
}

// higherOrderArrayRule generates an InterFlow rule for array methods where
// the element is passed as the first parameter (index 0) of the callback.
func higherOrderArrayRule(methodName string) datalog.Rule {
	return rule("InterFlow",
		[]datalog.Term{v("arraySym"), v("paramSym")},
		pos("MethodCall", v("call"), v("arrayExpr"), s(methodName)),
		pos("ExprMayRef", v("arrayExpr"), v("arraySym")),
		mustNamedLiteral("CallArg", map[string]datalog.Term{
			"call":    v("call"),
			"idx":     datalog.IntConst{Value: 0},
			"argNode": v("callbackExpr"),
		}),
		pos("ExprMayRef", v("callbackExpr"), v("callbackSym")),
		pos("FunctionSymbol", v("callbackSym"), v("callbackFn")),
		pos("Parameter", v("callbackFn"), datalog.IntConst{Value: 0}, w(), w(), v("paramSym"), w()),
	)
}
