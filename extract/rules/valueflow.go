package rules

import (
	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
)

// ValueFlowRules returns the system Datalog rules for value-flow Phase A
// (see docs/design/valueflow-phase-a-plan.md §1.2).
//
// Phase A introduces three grounded base relations consumed by the
// non-recursive `mayResolveTo` predicate:
//
//   - ExprValueSource(expr, sourceExpr)  — emitted directly by the walker
//   - AssignExpr(lhsSym, rhsExpr)        — emitted directly by the walker
//   - ParamBinding(fn, paramIdx, paramSym, argExpr) — derived here
//
// ParamBinding materialises the join CallTarget ⨝ CallArg ⨝ Parameter so the
// non-recursive mayResolveTo's param-bind branch consumes a single
// already-bound base predicate instead of re-deriving the 3-table join per
// query. CallTarget itself is a system-derived relation (callgraph.go), so
// ParamBinding cannot be emitted at extraction time.
//
// Carve-outs (NOT modelled in v1 — silent skip):
//   - Spread args `f(...rest)`     — would require array-shape model.
//   - Rest params `function f(...args)` — same.
//
// Both are deferred to Phase C. This is enforced in the rule shape: spread
// argument call sites have a CallArgSpread row; the negation `not
// CallArgSpread(call, idx)` is included to prevent emitting bogus
// ParamBinding tuples for spread positions where the syntactic argument
// expression does not bind 1:1 to a single parameter.
//
// Rule shape:
//
//	ParamBinding(fn, idx, paramSym, argExpr) :-
//	    CallTarget(call, fn),
//	    CallArg(call, idx, argExpr),
//	    not CallArgSpread(call, idx),
//	    Parameter(fn, idx, _, _, paramSym, _),
//	    not ParameterRest(fn, idx).
func ValueFlowRules() []datalog.Rule {
	return []datalog.Rule{
		// ParamBinding via direct CallTarget (callgraph.go rule 1).
		rule("ParamBinding",
			[]datalog.Term{v("fn"), v("idx"), v("paramSym"), v("argExpr")},
			pos("CallTarget", v("call"), v("fn")),
			mustNamedLiteral("CallArg", map[string]datalog.Term{
				"call":    v("call"),
				"idx":     v("idx"),
				"argNode": v("argExpr"),
			}),
			neg("CallArgSpread", v("call"), v("idx")),
			mustNamedLiteral("Parameter", map[string]datalog.Term{
				"fn":  v("fn"),
				"idx": v("idx"),
				"sym": v("paramSym"),
			}),
			neg("ParameterRest", v("fn"), v("idx")),
		),
	}
}
