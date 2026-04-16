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
//
// Body literals for schema-registered relations use posLit/negLit (named-column
// builder) so that column reordering in schema/relations.go is caught at startup
// rather than producing silent wrong results.
func TaintRules() []datalog.Rule {
	return []datalog.Rule{
		// Rule 1: Taint propagation — base case (identifier sources).
		// TaintedSym(srcSym, kind) :- TaintSource(srcExpr, kind), ExprMayRef(srcExpr, srcSym).
		rule("TaintedSym",
			[]datalog.Term{v("srcSym"), v("kind")},
			posLit("TaintSource", cols{"srcExpr": v("srcExpr"), "sourceKind": v("kind")}),
			posLit("ExprMayRef", cols{"expr": v("srcExpr"), "sym": v("srcSym")}),
		),

		// Rule 1b: Taint propagation — VarDecl init is a taint source (handles
		// FieldRead sources like req.query that don't have ExprMayRef entries).
		// TaintedSym(sym, kind) :- VarDecl(_, sym, initExpr, _), TaintSource(initExpr, kind).
		rule("TaintedSym",
			[]datalog.Term{v("sym"), v("kind")},
			posLit("VarDecl", cols{"sym": v("sym"), "initExpr": v("initExpr")}),
			posLit("TaintSource", cols{"srcExpr": v("initExpr"), "sourceKind": v("kind")}),
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

		// Rule 3b: Type-based sanitization (Phase 3d). If a flow edge lands on
		// a symbol whose resolved type is a non-taintable primitive (number,
		// boolean, bigint, etc.), the value was parsed or converted and no
		// longer carries the original string-shaped taint. We quantify over
		// kinds from TaintSource rather than TaintedSym to avoid a negation
		// cycle with Rule 2 (which uses not SanitizedEdge).
		// SanitizedEdge(srcSym, dstSym, kind) :-
		//     FlowStar(srcSym, dstSym),
		//     SymbolType(dstSym, typeId),
		//     NonTaintableType(typeId),
		//     TaintSource(_, kind).
		rule("SanitizedEdge",
			[]datalog.Term{v("srcSym"), v("dstSym"), v("kind")},
			pos("FlowStar", v("srcSym"), v("dstSym")),
			pos("SymbolType", v("dstSym"), v("typeId")),
			pos("NonTaintableType", v("typeId")),
			posLit("TaintSource", cols{"sourceKind": v("kind")}),
		),

		// Rule 4: Field-sensitive taint — writing tainted value to a field.
		// TaintedField(baseSym, fieldName, kind) :- FieldWrite(_, baseSym, fieldName, rhsExpr),
		//     ExprMayRef(rhsExpr, rhsSym), TaintedSym(rhsSym, kind).
		rule("TaintedField",
			[]datalog.Term{v("baseSym"), v("fieldName"), v("kind")},
			pos("FieldWrite", w(), v("baseSym"), v("fieldName"), v("rhsExpr")),
			posLit("ExprMayRef", cols{"expr": v("rhsExpr"), "sym": v("rhsSym")}),
			pos("TaintedSym", v("rhsSym"), v("kind")),
		),

		// Rule 5: Field-sensitive taint — reading a tainted field propagates taint.
		// TaintedSym(readSym, kind) :- FieldRead(expr, baseSym, fieldName),
		//     ExprMayRef(expr, readSym), TaintedField(baseSym, fieldName, kind).
		rule("TaintedSym",
			[]datalog.Term{v("readSym"), v("kind")},
			pos("FieldRead", v("expr"), v("baseSym"), v("fieldName")),
			posLit("ExprMayRef", cols{"expr": v("expr"), "sym": v("readSym")}),
			pos("TaintedField", v("baseSym"), v("fieldName"), v("kind")),
		),

		// Rule 6: Taint alert — tainted value reaches a sink via identifier flow.
		// TaintAlert(srcExpr, sinkExpr, srcKind, sinkKind) :-
		//     TaintSource(srcExpr, srcKind), ExprMayRef(srcExpr, srcSym),
		//     TaintedSym(sinkSym, srcKind), ExprMayRef(sinkExpr, sinkSym),
		//     TaintSink(sinkExpr, sinkKind).
		rule("TaintAlert",
			[]datalog.Term{v("srcExpr"), v("sinkExpr"), v("srcKind"), v("sinkKind")},
			posLit("TaintSource", cols{"srcExpr": v("srcExpr"), "sourceKind": v("srcKind")}),
			posLit("ExprMayRef", cols{"expr": v("srcExpr"), "sym": v("srcSym")}),
			pos("TaintedSym", v("sinkSym"), v("srcKind")),
			posLit("ExprMayRef", cols{"expr": v("sinkExpr"), "sym": v("sinkSym")}),
			posLit("TaintSink", cols{"sinkExpr": v("sinkExpr"), "sinkKind": v("sinkKind")}),
		),

		// Rule 6b: Taint alert for VarDecl-init-based sources.
		// When the source expression is a FieldRead (MemberExpression) or
		// compound expression that initializes a VarDecl, ExprMayRef won't
		// exist for it. This rule uses the VarDecl linkage to connect the
		// source to a tainted symbol, then checks that the symbol is actually
		// tainted (which respects sanitization via Rule 2's negation).
		// The sink side is scoped by requiring a tainted symbol exists in
		// the same function as the sink expression (via SymInFunction and
		// ExprInFunction). This is an intentional over-approximation: we
		// can't require ExprMayRef(sinkExpr, sinkSym) because the sink expr
		// is typically a compound expression (e.g. concatenation) where
		// the tainted identifier is a sub-expression, not the expr itself.
		// Function-scoping prevents cross-function false positives while
		// accepting intra-function over-approximation — the correct tradeoff
		// for security analysis (false positives > false negatives).
		rule("TaintAlert",
			[]datalog.Term{v("srcExpr"), v("sinkExpr"), v("srcKind"), v("sinkKind")},
			posLit("TaintSource", cols{"srcExpr": v("srcExpr"), "sourceKind": v("srcKind")}),
			posLit("VarDecl", cols{"sym": v("sym"), "initExpr": v("srcExpr")}),
			pos("TaintedSym", v("sym"), v("srcKind")),
			pos("TaintedSym", v("sinkSym"), v("srcKind")),
			pos("SymInFunction", v("sinkSym"), v("fnId")),
			pos("ExprInFunction", v("sinkExpr"), v("fnId")),
			posLit("TaintSink", cols{"sinkExpr": v("sinkExpr"), "sinkKind": v("sinkKind")}),
		),
	}
}
