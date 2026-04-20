package rules

import (
	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
)

// SummaryRules returns the system Datalog rules for function-level summaries.
// These compute inter-procedural flow summaries by combining intra-procedural
// LocalFlowStar with function boundary facts (parameters, return values, calls).
func SummaryRules() []datalog.Rule {
	return []datalog.Rule{
		// 1. ParamToReturn(fn, idx) — parameter flows to return value.
		// ParamToReturn(fn, idx) :-
		//   Parameter(fn, idx, _, _, paramSym, _),
		//   ReturnSym(fn, retSym),
		//   LocalFlowStar(fn, paramSym, retSym).
		rule("ParamToReturn",
			[]datalog.Term{v("fn"), v("idx")},
			pos("Parameter", v("fn"), v("idx"), w(), w(), v("paramSym"), w()),
			pos("ReturnSym", v("fn"), v("retSym")),
			pos("LocalFlowStar", v("fn"), v("paramSym"), v("retSym")),
		),

		// 2. ParamToCallArg(fn, paramIdx, calleeSym, argIdx) — parameter flows to callee argument.
		// ParamToCallArg(fn, paramIdx, calleeSym, argIdx) :-
		//   Parameter(fn, paramIdx, _, _, paramSym, _),
		//   FunctionContains(fn, call),
		//   CallArg(call, argIdx, argExpr),
		//   ExprMayRef(argExpr, argSym),
		//   LocalFlowStar(fn, paramSym, argSym),
		//   CallCalleeSym(call, calleeSym).
		rule("ParamToCallArg",
			[]datalog.Term{v("fn"), v("paramIdx"), v("calleeSym"), v("argIdx")},
			pos("Parameter", v("fn"), v("paramIdx"), w(), w(), v("paramSym"), w()),
			pos("FunctionContains", v("fn"), v("call")),
			mustNamedLiteral("CallArg", map[string]datalog.Term{
				"call":    v("call"),
				"idx":     v("argIdx"),
				"argNode": v("argExpr"),
			}),
			pos("ExprMayRef", v("argExpr"), v("argSym")),
			pos("LocalFlowStar", v("fn"), v("paramSym"), v("argSym")),
			pos("CallCalleeSym", v("call"), v("calleeSym")),
		),

		// 3. ParamToFieldWrite(fn, paramIdx, fieldName) — parameter flows to field write.
		// ParamToFieldWrite(fn, paramIdx, fieldName) :-
		//   Parameter(fn, paramIdx, _, _, paramSym, _),
		//   FunctionContains(fn, assignNode),
		//   FieldWrite(assignNode, _, fieldName, rhsExpr),
		//   ExprMayRef(rhsExpr, rhsSym),
		//   LocalFlowStar(fn, paramSym, rhsSym).
		rule("ParamToFieldWrite",
			[]datalog.Term{v("fn"), v("paramIdx"), v("fieldName")},
			pos("Parameter", v("fn"), v("paramIdx"), w(), w(), v("paramSym"), w()),
			pos("FunctionContains", v("fn"), v("assignNode")),
			pos("FieldWrite", v("assignNode"), w(), v("fieldName"), v("rhsExpr"), w()),
			pos("ExprMayRef", v("rhsExpr"), v("rhsSym")),
			pos("LocalFlowStar", v("fn"), v("paramSym"), v("rhsSym")),
		),

		// 4. ParamToSink(fn, paramIdx, sinkKind) — parameter reaches a taint sink.
		// Inactive until Phase D populates TaintSink.
		// ParamToSink(fn, paramIdx, sinkKind) :-
		//   Parameter(fn, paramIdx, _, _, paramSym, _),
		//   TaintSink(sinkExpr, sinkKind),
		//   ExprMayRef(sinkExpr, sinkSym),
		//   LocalFlowStar(fn, paramSym, sinkSym).
		rule("ParamToSink",
			[]datalog.Term{v("fn"), v("paramIdx"), v("sinkKind")},
			pos("Parameter", v("fn"), v("paramIdx"), w(), w(), v("paramSym"), w()),
			pos("TaintSink", v("sinkExpr"), v("sinkKind")),
			pos("ExprMayRef", v("sinkExpr"), v("sinkSym")),
			pos("LocalFlowStar", v("fn"), v("paramSym"), v("sinkSym")),
		),

		// 5. SourceToReturn(fn, sourceKind) — taint source flows to return.
		// Inactive until Phase D populates TaintSource.
		// SourceToReturn(fn, sourceKind) :-
		//   TaintSource(srcExpr, sourceKind),
		//   FunctionContains(fn, srcExpr),
		//   ExprMayRef(srcExpr, srcSym),
		//   ReturnSym(fn, retSym),
		//   LocalFlowStar(fn, srcSym, retSym).
		rule("SourceToReturn",
			[]datalog.Term{v("fn"), v("sourceKind")},
			pos("TaintSource", v("srcExpr"), v("sourceKind")),
			pos("FunctionContains", v("fn"), v("srcExpr")),
			pos("ExprMayRef", v("srcExpr"), v("srcSym")),
			pos("ReturnSym", v("fn"), v("retSym")),
			pos("LocalFlowStar", v("fn"), v("srcSym"), v("retSym")),
		),

		// 6. CallReturnToReturn(fn, call) — callee's return value flows to this function's return.
		// CallReturnToReturn(fn, call) :-
		//   FunctionContains(fn, call),
		//   CallResultSym(call, callRetSym),
		//   ReturnSym(fn, retSym),
		//   LocalFlowStar(fn, callRetSym, retSym).
		rule("CallReturnToReturn",
			[]datalog.Term{v("fn"), v("call")},
			pos("FunctionContains", v("fn"), v("call")),
			pos("CallResultSym", v("call"), v("callRetSym")),
			pos("ReturnSym", v("fn"), v("retSym")),
			pos("LocalFlowStar", v("fn"), v("callRetSym"), v("retSym")),
		),
	}
}
