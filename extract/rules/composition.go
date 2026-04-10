package rules

import (
	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
)

// CompositionRules returns the system Datalog rules for inter-procedural
// flow composition at call sites (Phase C3). These compose function-level
// summaries (from Phase C2) with call graph edges (Phase B) to propagate
// dataflow across function boundaries, and define whole-program FlowStar
// as the transitive closure of local + inter-procedural flow.
func CompositionRules() []datalog.Rule {
	return []datalog.Rule{
		// Rule 1: Argument flows through callee to return (InterFlow).
		// InterFlow(argSym, callRetSym) :-
		//   CallTarget(call, fn), CallArg(call, idx, argExpr),
		//   ExprMayRef(argExpr, argSym), ParamToReturn(fn, idx),
		//   CallResultSym(call, callRetSym).
		rule("InterFlow",
			[]datalog.Term{v("argSym"), v("callRetSym")},
			pos("CallTarget", v("call"), v("fn")),
			pos("CallArg", v("call"), v("idx"), v("argExpr")),
			pos("ExprMayRef", v("argExpr"), v("argSym")),
			pos("ParamToReturn", v("fn"), v("idx")),
			pos("CallResultSym", v("call"), v("callRetSym")),
		),

		// Rule 2: Argument flows through callee to another callee's argument.
		// InterFlow(argSym, innerArgSym) :-
		//   CallTarget(outerCall, fn), CallArg(outerCall, idx, argExpr),
		//   ExprMayRef(argExpr, argSym), ParamToCallArg(fn, idx, innerCalleeSym, innerIdx),
		//   FunctionSymbol(innerCalleeSym, innerFn),
		//   CallTarget(innerCall, innerFn), CallArg(innerCall, innerIdx, innerArgExpr),
		//   ExprMayRef(innerArgExpr, innerArgSym).
		rule("InterFlow",
			[]datalog.Term{v("argSym"), v("innerArgSym")},
			pos("CallTarget", v("outerCall"), v("fn")),
			pos("CallArg", v("outerCall"), v("idx"), v("argExpr")),
			pos("ExprMayRef", v("argExpr"), v("argSym")),
			pos("ParamToCallArg", v("fn"), v("idx"), v("innerCalleeSym"), v("innerIdx")),
			pos("FunctionSymbol", v("innerCalleeSym"), v("innerFn")),
			pos("CallTarget", v("innerCall"), v("innerFn")),
			pos("CallArg", v("innerCall"), v("innerIdx"), v("innerArgExpr")),
			pos("ExprMayRef", v("innerArgExpr"), v("innerArgSym")),
		),

		// Rule 3: FlowStar — lift local to global.
		// FlowStar(src, dst) :- LocalFlowStar(_, src, dst).
		rule("FlowStar",
			[]datalog.Term{v("src"), v("dst")},
			pos("LocalFlowStar", w(), v("src"), v("dst")),
		),

		// Rule 4: FlowStar — inter-procedural base.
		// FlowStar(src, dst) :- InterFlow(src, dst).
		rule("FlowStar",
			[]datalog.Term{v("src"), v("dst")},
			pos("InterFlow", v("src"), v("dst")),
		),

		// Rule 5: FlowStar — transitivity.
		// FlowStar(src, dst) :- FlowStar(src, mid), FlowStar(mid, dst).
		rule("FlowStar",
			[]datalog.Term{v("src"), v("dst")},
			pos("FlowStar", v("src"), v("mid")),
			pos("FlowStar", v("mid"), v("dst")),
		),
	}
}
