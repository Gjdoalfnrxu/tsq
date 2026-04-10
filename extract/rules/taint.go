package rules

import (
	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
)

// TaintRules returns the system Datalog rules for taint tracking (Phase D).
// These propagate taint from sources through the program's data flow graph,
// apply sanitizers, track field-level taint, and produce alerts when taint
// reaches sinks.
//
// TaintPath is deferred to Phase E — it requires arithmetic (step+1, step < 50)
// which is not supported in standard Datalog. The current rules cover correctness:
// TaintedSym, SanitizedEdge, TaintedField, and TaintAlert.
func TaintRules() []datalog.Rule {
	return []datalog.Rule{
		// Rule 1: Taint propagation — base case.
		// TaintedSym(srcSym, kind) :- TaintSource(srcExpr, kind), ExprMayRef(srcExpr, srcSym).
		rule("TaintedSym",
			[]datalog.Term{v("srcSym"), v("kind")},
			pos("TaintSource", v("srcExpr"), v("kind")),
			pos("ExprMayRef", v("srcExpr"), v("srcSym")),
		),

		// Rule 2: Taint propagation — transitive via FlowStar, blocked by sanitizers.
		// TaintedSym(dstSym, kind) :- TaintedSym(srcSym, kind), FlowStar(srcSym, dstSym),
		//     not SanitizedEdge(srcSym, dstSym, kind).
		rule("TaintedSym",
			[]datalog.Term{v("dstSym"), v("kind")},
			pos("TaintedSym", v("srcSym"), v("kind")),
			pos("FlowStar", v("srcSym"), v("dstSym")),
			neg("SanitizedEdge", v("srcSym"), v("dstSym"), v("kind")),
		),

		// Rule 3: Sanitization — marks edges where the destination is a call result
		// of a sanitizer function. When taint flows through a call to a sanitizer,
		// the InterFlow edge from caller arg to call result is blocked.
		// SanitizedEdge(srcSym, callRetSym, kind) :-
		//     FlowStar(srcSym, callRetSym),
		//     CallResultSym(call, callRetSym), CallTarget(call, fn), Sanitizer(fn, kind).
		rule("SanitizedEdge",
			[]datalog.Term{v("srcSym"), v("callRetSym"), v("kind")},
			pos("FlowStar", v("srcSym"), v("callRetSym")),
			pos("CallResultSym", v("call"), v("callRetSym")),
			pos("CallTarget", v("call"), v("fn")),
			pos("Sanitizer", v("fn"), v("kind")),
		),

		// Rule 4: Field-sensitive taint — writing tainted value to a field.
		// TaintedField(baseSym, fieldName, kind) :- FieldWrite(_, baseSym, fieldName, rhsExpr),
		//     ExprMayRef(rhsExpr, rhsSym), TaintedSym(rhsSym, kind).
		rule("TaintedField",
			[]datalog.Term{v("baseSym"), v("fieldName"), v("kind")},
			pos("FieldWrite", w(), v("baseSym"), v("fieldName"), v("rhsExpr")),
			pos("ExprMayRef", v("rhsExpr"), v("rhsSym")),
			pos("TaintedSym", v("rhsSym"), v("kind")),
		),

		// Rule 5: Field-sensitive taint — reading a tainted field propagates taint.
		// TaintedSym(readSym, kind) :- FieldRead(expr, baseSym, fieldName),
		//     ExprMayRef(expr, readSym), TaintedField(baseSym, fieldName, kind).
		rule("TaintedSym",
			[]datalog.Term{v("readSym"), v("kind")},
			pos("FieldRead", v("expr"), v("baseSym"), v("fieldName")),
			pos("ExprMayRef", v("expr"), v("readSym")),
			pos("TaintedField", v("baseSym"), v("fieldName"), v("kind")),
		),

		// Rule 6: Taint alert — tainted value reaches a sink.
		// TaintAlert(srcExpr, sinkExpr, srcKind, sinkKind) :-
		//     TaintSource(srcExpr, srcKind), ExprMayRef(srcExpr, srcSym),
		//     TaintedSym(sinkSym, srcKind), ExprMayRef(sinkExpr, sinkSym),
		//     TaintSink(sinkExpr, sinkKind).
		rule("TaintAlert",
			[]datalog.Term{v("srcExpr"), v("sinkExpr"), v("srcKind"), v("sinkKind")},
			pos("TaintSource", v("srcExpr"), v("srcKind")),
			pos("ExprMayRef", v("srcExpr"), v("srcSym")),
			pos("TaintedSym", v("sinkSym"), v("srcKind")),
			pos("ExprMayRef", v("sinkExpr"), v("sinkSym")),
			pos("TaintSink", v("sinkExpr"), v("sinkKind")),
		),
	}
}
