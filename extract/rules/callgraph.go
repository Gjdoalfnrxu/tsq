// Package rules provides system-level Datalog rules for derived relations.
// These rules are injected alongside user-written QL queries to compute
// call graphs, inheritance chains, and other derived facts.
package rules

import (
	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
)

// v returns a Var term.
func v(name string) datalog.Var { return datalog.Var{Name: name} }

// w returns a Wildcard term.
func w() datalog.Wildcard { return datalog.Wildcard{} }

// pos returns a positive literal from predicate and args.
func pos(pred string, args ...datalog.Term) datalog.Literal {
	return datalog.Literal{
		Positive: true,
		Atom:     datalog.Atom{Predicate: pred, Args: args},
	}
}

// neg returns a negative literal from predicate and args.
func neg(pred string, args ...datalog.Term) datalog.Literal {
	return datalog.Literal{
		Positive: false,
		Atom:     datalog.Atom{Predicate: pred, Args: args},
	}
}

// rule builds a Rule from head predicate/args and body literals.
func rule(headPred string, headArgs []datalog.Term, body ...datalog.Literal) datalog.Rule {
	return datalog.Rule{
		Head: datalog.Atom{Predicate: headPred, Args: headArgs},
		Body: body,
	}
}

// CallGraphRules returns the system Datalog rules for call graph construction.
// These implement CHA (Class Hierarchy Analysis) and RTA (Rapid Type Analysis).
func CallGraphRules() []datalog.Rule {
	return []datalog.Rule{
		// 1. Direct resolution: CallTarget(call, fn) :- CallCalleeSym(call, sym), FunctionSymbol(sym, fn).
		rule("CallTarget",
			[]datalog.Term{v("call"), v("fn")},
			pos("CallCalleeSym", v("call"), v("sym")),
			pos("FunctionSymbol", v("sym"), v("fn")),
		),

		// 2. Method on concrete class:
		//    CallTarget(call, fn) :- MethodCall(call, recv, name), ExprType(recv, classId),
		//                            ClassDecl(classId, _, _), MethodDecl(classId, name, fn).
		rule("CallTarget",
			[]datalog.Term{v("call"), v("fn")},
			pos("MethodCall", v("call"), v("recv"), v("name")),
			pos("ExprType", v("recv"), v("classId")),
			pos("ClassDecl", v("classId"), w(), w()),
			pos("MethodDecl", v("classId"), v("name"), v("fn")),
		),

		// 3. CHA for interfaces:
		//    CallTarget(call, fn) :- MethodCall(call, recv, name), ExprType(recv, ifaceId),
		//                            InterfaceDecl(ifaceId, _, _), Implements(classId, ifaceId),
		//                            MethodDecl(classId, name, fn).
		rule("CallTarget",
			[]datalog.Term{v("call"), v("fn")},
			pos("MethodCall", v("call"), v("recv"), v("name")),
			pos("ExprType", v("recv"), v("ifaceId")),
			pos("InterfaceDecl", v("ifaceId"), w(), w()),
			pos("Implements", v("classId"), v("ifaceId")),
			pos("MethodDecl", v("classId"), v("name"), v("fn")),
		),

		// 4a. Inheritance (base case): inherit from parent's own methods.
		//    MethodDeclInherited(childId, name, fn) :- Extends(childId, parentId),
		//        MethodDecl(parentId, name, fn), not MethodDeclDirect(childId, name, _).
		rule("MethodDeclInherited",
			[]datalog.Term{v("childId"), v("name"), v("fn")},
			pos("Extends", v("childId"), v("parentId")),
			pos("MethodDecl", v("parentId"), v("name"), v("fn")),
			neg("MethodDeclDirect", v("childId"), v("name"), w()),
		),

		// 4b. Inheritance (recursive): inherit methods parent itself inherited.
		//    MethodDeclInherited(childId, name, fn) :- Extends(childId, parentId),
		//        MethodDeclInherited(parentId, name, fn), not MethodDeclDirect(childId, name, _).
		rule("MethodDeclInherited",
			[]datalog.Term{v("childId"), v("name"), v("fn")},
			pos("Extends", v("childId"), v("parentId")),
			pos("MethodDeclInherited", v("parentId"), v("name"), v("fn")),
			neg("MethodDeclDirect", v("childId"), v("name"), w()),
		),

		// 5. MethodDeclDirect base case:
		//    MethodDeclDirect(classId, name, fn) :- MethodDecl(classId, name, fn), ClassDecl(classId, _, _).
		rule("MethodDeclDirect",
			[]datalog.Term{v("classId"), v("name"), v("fn")},
			pos("MethodDecl", v("classId"), v("name"), v("fn")),
			pos("ClassDecl", v("classId"), w(), w()),
		),

		// 6. Instantiated:
		//    Instantiated(classId) :- NewExpr(_, classId).
		rule("Instantiated",
			[]datalog.Term{v("classId")},
			pos("NewExpr", w(), v("classId")),
		),

		// 7. RTA:
		//    CallTargetRTA(call, fn) :- MethodCall(call, recv, name), ExprType(recv, ifaceId),
		//        InterfaceDecl(ifaceId, _, _), Implements(classId, ifaceId),
		//        Instantiated(classId), MethodDecl(classId, name, fn).
		rule("CallTargetRTA",
			[]datalog.Term{v("call"), v("fn")},
			pos("MethodCall", v("call"), v("recv"), v("name")),
			pos("ExprType", v("recv"), v("ifaceId")),
			pos("InterfaceDecl", v("ifaceId"), w(), w()),
			pos("Implements", v("classId"), v("ifaceId")),
			pos("Instantiated", v("classId")),
			pos("MethodDecl", v("classId"), v("name"), v("fn")),
		),
	}
}
