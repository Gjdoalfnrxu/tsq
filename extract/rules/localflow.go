package rules

import (
	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
)

// LocalFlowRules returns the system Datalog rules for intra-procedural dataflow.
// These compute LocalFlow(fn, srcSym, dstSym) edges within a single function,
// and LocalFlowStar(fn, srcSym, dstSym) as the transitive closure.
//
// Body literals for Assign, Call, CallArg, LocalFlow, and TaintAlert use
// mustNamedLiteral so column ordering is validated against the schema registry
// at startup rather than silently breaking on a schema column reorder.
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
			mustNamedLiteral("Assign", map[string]datalog.Term{
				"rhsExpr": v("rhsExpr"),
				"lhsSym":  v("lhsSym"),
			}),
			pos("ExprMayRef", v("rhsExpr"), v("rhsSym")),
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
			pos("VarDecl", w(), v("sym"), v("initExpr"), w()),
			pos("ExprMayRef", v("initExpr"), v("initSym")),
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
			pos("ExprMayRef", v("retExpr"), v("retSym")),
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
			pos("DestructureField", v("parent"), w(), w(), v("bindSym"), w(), w()),
			pos("VarDecl", v("parent"), w(), v("initExpr"), w()),
			pos("ExprMayRef", v("initExpr"), v("parentSym")),
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
			pos("FieldRead", v("expr"), v("baseSym"), w(), w()),
			pos("ExprMayRef", v("expr"), v("exprSym")),
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
			pos("FieldWrite", w(), v("baseSym"), w(), v("rhsExpr"), w()),
			pos("ExprMayRef", v("rhsExpr"), v("rhsSym")),
			pos("SymInFunction", v("baseSym"), v("fn")),
			pos("SymInFunction", v("rhsSym"), v("fn")),
		),

		// Rule 7: Transitive closure base case.
		// LocalFlowStar(fn, src, dst) :- LocalFlow(fn, src, dst).
		rule("LocalFlowStar",
			[]datalog.Term{v("fn"), v("src"), v("dst")},
			mustNamedLiteral("LocalFlow", map[string]datalog.Term{
				"fnId":   v("fn"),
				"srcSym": v("src"),
				"dstSym": v("dst"),
			}),
		),

		// Rule 8: Transitive closure recursive step.
		// LocalFlowStar(fn, src, dst) :- LocalFlowStar(fn, src, mid), LocalFlow(fn, mid, dst).
		rule("LocalFlowStar",
			[]datalog.Term{v("fn"), v("src"), v("dst")},
			pos("LocalFlowStar", v("fn"), v("src"), v("mid")),
			mustNamedLiteral("LocalFlow", map[string]datalog.Term{
				"fnId":   v("fn"),
				"srcSym": v("mid"),
				"dstSym": v("dst"),
			}),
		),
	}
}
