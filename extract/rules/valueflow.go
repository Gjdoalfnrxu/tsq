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
// query.
//
// # Plan deviation — rule, not walker post-pass
//
// The plan §1.2 sketches ParamBinding as a "post-pass after the main walker
// run" because it needs `CallTarget` to be settled. In tsq's architecture,
// `CallTarget` is itself a system Datalog rule (extract/rules/callgraph.go),
// not an extractor output. Implementing ParamBinding as a walker post-pass
// would either duplicate CallTarget resolution or require running Datalog
// inside the extractor. Modelling it as a rule here is the same pattern as
// InterFlow / FlowStar / ParamToReturn — same rows, same shape, same
// carve-outs as the plan.
//
// # Carve-outs (NOT modelled in Phase A — silent skip)
//
//   - Spread args `f(...rest)`           — `not CallArgSpread(call, idx)`.
//   - Rest params `function f(...args)`  — `not ParameterRest(fn, idx)`.
//   - Destructured params `function f({a,b}, [x,y])` — `not
//     ParameterDestructured(fn, idx)`. The walker emits a single Parameter
//     row for the slot with the pattern source text as the synthesised
//     "name" (so the symbol id is bogus); the negation prevents that bogus
//     symbol leaking into ParamBinding. Per-bound-name expansion is deferred
//     to Phase C.
//
// CallTargetRTA is intentionally NOT consumed here — RTA-resolved targets
// are noisier (one call site → many candidate fns) and the budget gate in
// valueflow_budget_test.go widens to `CallTarget ∪ CallTargetRTA` to verify
// the multiplicative blow-up source stays within 5x of CallArg.
//
// Rule shape:
//
//	ParamBinding(fn, idx, paramSym, argExpr) :-
//	    ( CallTarget(call, fn) ; CallTargetRTA(call, fn) ),
//	    CallArg(call, idx, argExpr),
//	    not CallArgSpread(call, idx),
//	    Parameter(fn, idx, _, _, paramSym, _),
//	    not ParameterRest(fn, idx),
//	    not ParameterDestructured(fn, idx).
func ValueFlowRules() []datalog.Rule {
	body := func(targetRel string) []datalog.Literal {
		return []datalog.Literal{
			pos(targetRel, v("call"), v("fn")),
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
			neg("ParameterDestructured", v("fn"), v("idx")),
		}
	}
	head := []datalog.Term{v("fn"), v("idx"), v("paramSym"), v("argExpr")}
	return []datalog.Rule{
		// ParamBinding via direct CallTarget (callgraph.go rule 1).
		rule("ParamBinding", head, body("CallTarget")...),
		// ParamBinding via RTA-resolved CallTarget (callgraph.go rule 4).
		// Budget gate (valueflow_budget_test.go) bounds the multiplicative
		// blow-up.
		rule("ParamBinding", head, body("CallTargetRTA")...),
	}
}
