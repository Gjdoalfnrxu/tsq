package rules

import (
	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
)

// LocalFlowRules returns the system Datalog rules for intra-procedural dataflow.
// These compute LocalFlow(fn, srcSym, dstSym) edges within a single function,
// and LocalFlowStar(fn, srcSym, dstSym) as the transitive closure.
//
// Body literals for schema-registered relations use posLit/negLit (named-column
// builder) so that column reordering in schema/relations.go is caught at startup
// rather than producing silent wrong results.
func LocalFlowRules() []datalog.Rule {
	return []datalog.Rule{
		// Rule 1: Assignment flow.
		// LocalFlow(fn, rhsSym, lhsSym) :-
		//   Assign(lhsNode, rhsExpr, lhsSym),
		//   ExprMayRef(rhsExpr, rhsSym),
		//   SymInFunction(lhsSym, fn),
		//   SymInFunction(rhsSym, fn).
		rule("LocalFlow",
			[]datalog.Term{v("fn"), v("rhsSym"), v("lhsSym")},
			posLit("Assign", cols{"rhsExpr": v("rhsExpr"), "lhsSym": v("lhsSym")}),
			posLit("ExprMayRef", cols{"expr": v("rhsExpr"), "sym": v("rhsSym")}),
			pos("SymInFunction", v("lhsSym"), v("fn")),
			pos("SymInFunction", v("rhsSym"), v("fn")),
		),

		// Rule 2: VarDecl init flow.
		// LocalFlow(fn, initSym, sym) :-
		//   VarDecl(_, sym, initExpr, _),
		//   ExprMayRef(initExpr, initSym),
		//   SymInFunction(sym, fn),
		//   SymInFunction(initSym, fn).
		rule("LocalFlow",
			[]datalog.Term{v("fn"), v("initSym"), v("sym")},
			posLit("VarDecl", cols{"sym": v("sym"), "initExpr": v("initExpr")}),
			posLit("ExprMayRef", cols{"expr": v("initExpr"), "sym": v("initSym")}),
			pos("SymInFunction", v("sym"), v("fn")),
			pos("SymInFunction", v("initSym"), v("fn")),
		),

		// Rule 3: Return value flow.
		// LocalFlow(fn, retSym, returnSym) :-
		//   ReturnStmt(fn, _, retExpr),
		//   ExprMayRef(retExpr, retSym),
		//   ReturnSym(fn, returnSym),
		//   SymInFunction(retSym, fn).
		rule("LocalFlow",
			[]datalog.Term{v("fn"), v("retSym"), v("returnSym")},
			pos("ReturnStmt", v("fn"), w(), v("retExpr")),
			posLit("ExprMayRef", cols{"expr": v("retExpr"), "sym": v("retSym")}),
			pos("ReturnSym", v("fn"), v("returnSym")),
			pos("SymInFunction", v("retSym"), v("fn")),
		),

		// Rule 4: Destructuring flow (field-insensitive).
		// LocalFlow(fn, parentSym, bindSym) :-
		//   DestructureField(parent, _, _, bindSym, _),
		//   VarDecl(parent, parentDeclSym, initExpr, _),
		//   ExprMayRef(initExpr, parentSym),
		//   SymInFunction(bindSym, fn),
		//   SymInFunction(parentSym, fn).
		rule("LocalFlow",
			[]datalog.Term{v("fn"), v("parentSym"), v("bindSym")},
			pos("DestructureField", v("parent"), w(), w(), v("bindSym"), w()),
			posLit("VarDecl", cols{"id": v("parent"), "initExpr": v("initExpr")}),
			posLit("ExprMayRef", cols{"expr": v("initExpr"), "sym": v("parentSym")}),
			pos("SymInFunction", v("bindSym"), v("fn")),
			pos("SymInFunction", v("parentSym"), v("fn")),
		),

		// Rule 5: Field read flow (field-insensitive).
		// LocalFlow(fn, baseSym, exprSym) :-
		//   FieldRead(expr, baseSym, _),
		//   ExprMayRef(expr, exprSym),
		//   SymInFunction(baseSym, fn),
		//   SymInFunction(exprSym, fn).
		rule("LocalFlow",
			[]datalog.Term{v("fn"), v("baseSym"), v("exprSym")},
			pos("FieldRead", v("expr"), v("baseSym"), w()),
			posLit("ExprMayRef", cols{"expr": v("expr"), "sym": v("exprSym")}),
			pos("SymInFunction", v("baseSym"), v("fn")),
			pos("SymInFunction", v("exprSym"), v("fn")),
		),

		// Rule 6: Field write flow (field-insensitive).
		// LocalFlow(fn, rhsSym, baseSym) :-
		//   FieldWrite(_, baseSym, _, rhsExpr),
		//   ExprMayRef(rhsExpr, rhsSym),
		//   SymInFunction(baseSym, fn),
		//   SymInFunction(rhsSym, fn).
		rule("LocalFlow",
			[]datalog.Term{v("fn"), v("rhsSym"), v("baseSym")},
			pos("FieldWrite", w(), v("baseSym"), w(), v("rhsExpr")),
			posLit("ExprMayRef", cols{"expr": v("rhsExpr"), "sym": v("rhsSym")}),
			pos("SymInFunction", v("baseSym"), v("fn")),
			pos("SymInFunction", v("rhsSym"), v("fn")),
		),

		// Rule 7: Transitive closure base case.
		// LocalFlowStar(fn, src, dst) :- LocalFlow(fn, src, dst).
		rule("LocalFlowStar",
			[]datalog.Term{v("fn"), v("src"), v("dst")},
			pos("LocalFlow", v("fn"), v("src"), v("dst")),
		),

		// Rule 8: Transitive closure recursive step.
		// LocalFlowStar(fn, src, dst) :- LocalFlowStar(fn, src, mid), LocalFlow(fn, mid, dst).
		rule("LocalFlowStar",
			[]datalog.Term{v("fn"), v("src"), v("dst")},
			pos("LocalFlowStar", v("fn"), v("src"), v("mid")),
			pos("LocalFlow", v("fn"), v("mid"), v("dst")),
		),
	}
}
