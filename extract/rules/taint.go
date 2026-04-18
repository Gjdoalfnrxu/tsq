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
// Body literals for TaintSource, TaintSink, VarDecl, and ExprMayRef use
// mustNamedLiteral so column ordering is validated against the schema registry
// at startup rather than silently breaking on a schema column reorder.
func TaintRules() []datalog.Rule {
	return []datalog.Rule{
		// Rule 1: Taint propagation — base case (identifier sources).
		// TaintedSym(srcSym, kind) :- TaintSource(srcExpr, kind), ExprMayRef(srcExpr, srcSym).
		rule("TaintedSym",
			[]datalog.Term{v("srcSym"), v("kind")},
			mustNamedLiteral("TaintSource", map[string]datalog.Term{
				"srcExpr":    v("srcExpr"),
				"sourceKind": v("kind"),
			}),
			mustNamedLiteral("ExprMayRef", map[string]datalog.Term{
				"expr": v("srcExpr"),
				"sym":  v("srcSym"),
			}),
		),

		// Rule 1b: Taint propagation — VarDecl init is a taint source (handles
		// FieldRead sources like req.query that don't have ExprMayRef entries).
		// TaintedSym(sym, kind) :- VarDecl(_, sym, initExpr, _), TaintSource(initExpr, kind).
		rule("TaintedSym",
			[]datalog.Term{v("sym"), v("kind")},
			mustNamedLiteral("VarDecl", map[string]datalog.Term{
				"sym":      v("sym"),
				"initExpr": v("initExpr"),
			}),
			mustNamedLiteral("TaintSource", map[string]datalog.Term{
				"srcExpr":    v("initExpr"),
				"sourceKind": v("kind"),
			}),
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
			mustNamedLiteral("TaintSource", map[string]datalog.Term{
				"sourceKind": v("kind"),
			}),
		),

		// Rule 4: Field-sensitive taint — writing tainted value to a field.
		// TaintedField(baseSym, fieldName, kind) :- FieldWrite(_, baseSym, fieldName, rhsExpr),
		//     ExprMayRef(rhsExpr, rhsSym), TaintedSym(rhsSym, kind).
		rule("TaintedField",
			[]datalog.Term{v("baseSym"), v("fieldName"), v("kind")},
			pos("FieldWrite", w(), v("baseSym"), v("fieldName"), v("rhsExpr")),
			mustNamedLiteral("ExprMayRef", map[string]datalog.Term{
				"expr": v("rhsExpr"),
				"sym":  v("rhsSym"),
			}),
			pos("TaintedSym", v("rhsSym"), v("kind")),
		),

		// Rule 5: Field-sensitive taint — reading a tainted field propagates taint.
		// TaintedSym(readSym, kind) :- FieldRead(expr, baseSym, fieldName),
		//     ExprMayRef(expr, readSym), TaintedField(baseSym, fieldName, kind).
		rule("TaintedSym",
			[]datalog.Term{v("readSym"), v("kind")},
			pos("FieldRead", v("expr"), v("baseSym"), v("fieldName")),
			mustNamedLiteral("ExprMayRef", map[string]datalog.Term{
				"expr": v("expr"),
				"sym":  v("readSym"),
			}),
			pos("TaintedField", v("baseSym"), v("fieldName"), v("kind")),
		),

		// Rule 6: Taint alert — tainted value reaches a sink via identifier flow.
		// TaintAlert(srcExpr, sinkExpr, srcKind, sinkKind) :-
		//     TaintSource(srcExpr, srcKind), ExprMayRef(srcExpr, srcSym),
		//     TaintedSym(sinkSym, srcKind), ExprMayRef(sinkExpr, sinkSym),
		//     TaintSink(sinkExpr, sinkKind).
		rule("TaintAlert",
			[]datalog.Term{v("srcExpr"), v("sinkExpr"), v("srcKind"), v("sinkKind")},
			mustNamedLiteral("TaintSource", map[string]datalog.Term{
				"srcExpr":    v("srcExpr"),
				"sourceKind": v("srcKind"),
			}),
			mustNamedLiteral("ExprMayRef", map[string]datalog.Term{
				"expr": v("srcExpr"),
				"sym":  v("srcSym"),
			}),
			pos("TaintedSym", v("sinkSym"), v("srcKind")),
			mustNamedLiteral("ExprMayRef", map[string]datalog.Term{
				"expr": v("sinkExpr"),
				"sym":  v("sinkSym"),
			}),
			mustNamedLiteral("TaintSink", map[string]datalog.Term{
				"sinkExpr": v("sinkExpr"),
				"sinkKind": v("sinkKind"),
			}),
		),

		// Rule 6b: Taint alert for VarDecl-init-based sources.
		//
		// When the source expression is a FieldRead (MemberExpression) or
		// compound expression that initializes a VarDecl, ExprMayRef won't
		// exist for the source expression itself. Rule 6b uses the VarDecl
		// linkage to connect the source to a tainted symbol, then requires
		// the sink expression to actually reference that symbol (or a
		// FlowStar-reachable derivative) somewhere in its subtree.
		//
		// The "somewhere in its subtree" predicate is `SinkRefSym` (Rule
		// 6b-helper-1/-2 below), defined as: `sinkExpr` is itself the
		// referencing expression OR contains a descendant expression that
		// resolves to `sym` via ExprMayRef. This handles compound sinks
		// like `sql('SELECT ...' + x)` where the sink expression is a Call
		// whose CallArg is a BinaryExpression whose right operand is the
		// tainted identifier — none of which `ExprMayRef(sinkExpr, sym)`
		// alone can capture, but `Contains*(sinkExpr, descExpr) ∧
		// ExprMayRef(descExpr, sym)` does.
		//
		// History (issue #113): The previous Rule 6b accepted ANY tainted
		// symbol of the same kind in the same function as the sink, with
		// no flow link from the VarDecl sym to the sink sym and no
		// requirement that the sink expression actually reference the sink
		// sym. That produced cross-symbol false positives whenever a
		// function contained an unrelated sink-shaped call alongside a
		// tainted variable. The variants below restore the missing
		// source→sink link.

		// Rule 6b-helper SinkContains: transitive closure of AST
		// containment, anchored at sink expressions to bound the
		// search. Used by SinkRefSym (descendant variant) below.
		rule("SinkContains",
			[]datalog.Term{v("sinkExpr"), v("child")},
			mustNamedLiteral("TaintSink", map[string]datalog.Term{
				"sinkExpr": v("sinkExpr"),
			}),
			mustNamedLiteral("Contains", map[string]datalog.Term{
				"parent": v("sinkExpr"),
				"child":  v("child"),
			}),
		),
		rule("SinkContains",
			[]datalog.Term{v("sinkExpr"), v("desc")},
			pos("SinkContains", v("sinkExpr"), v("mid")),
			mustNamedLiteral("Contains", map[string]datalog.Term{
				"parent": v("mid"),
				"child":  v("desc"),
			}),
		),
		// SinkRefSym (direct): sink expression itself is an identifier
		// resolving to sym. e.g. `sink(x)` where the sink expression is x.
		rule("SinkRefSym",
			[]datalog.Term{v("sinkExpr"), v("sym")},
			mustNamedLiteral("TaintSink", map[string]datalog.Term{
				"sinkExpr": v("sinkExpr"),
			}),
			mustNamedLiteral("ExprMayRef", map[string]datalog.Term{
				"expr": v("sinkExpr"),
				"sym":  v("sym"),
			}),
		),
		// SinkRefSym (descendant): sink subtree contains an identifier
		// resolving to sym. Handles compound sinks like
		// `sql('SELECT ' + x)` where the tainted ident lives inside an
		// argument expression.
		rule("SinkRefSym",
			[]datalog.Term{v("sinkExpr"), v("sym")},
			pos("SinkContains", v("sinkExpr"), v("desc")),
			mustNamedLiteral("ExprMayRef", map[string]datalog.Term{
				"expr": v("desc"),
				"sym":  v("sym"),
			}),
		),

		// Variant A (direct): sink references the VarDecl sym itself
		// (either directly or in a sub-expression).
		// TaintAlert(srcExpr, sinkExpr, srcKind, sinkKind) :-
		//   TaintSource(srcExpr, srcKind), VarDecl(_, sym, srcExpr, _),
		//   TaintedSym(sym, srcKind),
		//   SinkRefSym(sinkExpr, sym),
		//   TaintSink(sinkExpr, sinkKind).
		rule("TaintAlert",
			[]datalog.Term{v("srcExpr"), v("sinkExpr"), v("srcKind"), v("sinkKind")},
			mustNamedLiteral("TaintSource", map[string]datalog.Term{
				"srcExpr":    v("srcExpr"),
				"sourceKind": v("srcKind"),
			}),
			mustNamedLiteral("VarDecl", map[string]datalog.Term{
				"sym":      v("sym"),
				"initExpr": v("srcExpr"),
			}),
			pos("TaintedSym", v("sym"), v("srcKind")),
			pos("SinkRefSym", v("sinkExpr"), v("sym")),
			mustNamedLiteral("TaintSink", map[string]datalog.Term{
				"sinkExpr": v("sinkExpr"),
				"sinkKind": v("sinkKind"),
			}),
		),

		// Variant B (flow): sink references some sym reachable from the
		// VarDecl sym via FlowStar. e.g. `let x = req.body; let y = f(x);
		// sql('...' + y);`.
		// TaintAlert(srcExpr, sinkExpr, srcKind, sinkKind) :-
		//   TaintSource(srcExpr, srcKind), VarDecl(_, sym, srcExpr, _),
		//   TaintedSym(sym, srcKind),
		//   FlowStar(sym, sinkSym),
		//   SinkRefSym(sinkExpr, sinkSym),
		//   TaintSink(sinkExpr, sinkKind).
		rule("TaintAlert",
			[]datalog.Term{v("srcExpr"), v("sinkExpr"), v("srcKind"), v("sinkKind")},
			mustNamedLiteral("TaintSource", map[string]datalog.Term{
				"srcExpr":    v("srcExpr"),
				"sourceKind": v("srcKind"),
			}),
			mustNamedLiteral("VarDecl", map[string]datalog.Term{
				"sym":      v("sym"),
				"initExpr": v("srcExpr"),
			}),
			pos("TaintedSym", v("sym"), v("srcKind")),
			pos("FlowStar", v("sym"), v("sinkSym")),
			pos("SinkRefSym", v("sinkExpr"), v("sinkSym")),
			mustNamedLiteral("TaintSink", map[string]datalog.Term{
				"sinkExpr": v("sinkExpr"),
				"sinkKind": v("sinkKind"),
			}),
		),
	}
}
