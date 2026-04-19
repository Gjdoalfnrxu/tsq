package rules

import (
	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
)

// MayResolveToRules returns the system Datalog rules for the value-flow
// Phase C PR4 recursive `MayResolveTo` closure (see
// docs/design/valueflow-phase-c-plan.md §1.2).
//
// `MayResolveTo(v, s)` is the transitive closure of single-step value-flow
// edges (PR3's `FlowStep`) starting from value-source expressions
// (`ExprValueSource`). It models "the runtime value of expression `v` may
// be the value produced at expression `s`".
//
// The closure is the first PR in Phase C that exercises actual recursion
// in the Datalog evaluator. Three load-bearing pieces from earlier work
// converge here:
//
//  1. Phase B's recursive-IDB cardinality estimator
//     (ql/eval/estimate_recursive.go) sizes the IDB up-front so the planner
//     picks demand-bound joins instead of materialising the full closure.
//  2. The magic-set rewrite (Phase B PR4/PR5) propagates bound arguments
//     through the recursion, so a call-site query like
//     `MayResolveTo(?known, _)` seeds the closure backwards from the use
//     site rather than enumerating every value source.
//  3. The #166 disjunction-poisoning fix (Phase B) plus the per-branch
//     IDB-head lifting that PR2/PR3 already use for `LocalFlowStep` and
//     `InterFlowStep` keep the recursive body free of inline `or`s.
//
// # Path erasure (PR4 vs plan §1.2)
//
// Plan §1.2 sketches the closure with an access-path column:
//
//	mayResolveTo(v, s, p) :- ExprValueSource(v, s), p = "".
//	mayResolveTo(v, s, p) :- flowStep(v, mid, p1),
//	                         mayResolveTo(mid, s, p2),
//	                         pathCompose(p1, p2, p).
//
// PR4 ships path-erased (arity-2). Path sensitivity and `pathCompose` is
// the entire scope of PR5. Path erasure is acknowledged over-approximation
// per the §4.3 refutation-tool contract — PR5 narrows but never adds.
//
// # No diagnostic relation
//
// Plan §5.2 sketched a `MayResolveToCapHit(query)` diagnostic relation
// emitted on iteration-cap fire. The existing seminaive evaluator already
// surfaces an `*IterationCapError` (ql/eval/seminaive.go) with the
// stratum / rule / cap / last-delta-size on hard cap-hit; for PR4 that's
// sufficient signalling. The dedicated diagnostic relation is deferred to
// PR7's CI-perf-gate work where it belongs alongside the cain-nas bench
// alerting wiring.
//
// # Iteration cap source
//
// `DefaultMaxIterations = 100` in ql/eval/seminaive.go. Plan §5.2 quoted
// 50 as the suggested default; the existing infrastructure runs at 100
// and PR4 does NOT introduce a per-predicate override. Honest accounting:
// if Mastodon shows >1% cap-hit rate at 100 iterations, the budget gate
// in plan §5.2 fires and PR7 either lifts the cap or files a planner
// issue. Not a PR4 concern.
//
// # Edge direction (PR4 vs plan §1.2 wording)
//
// Plan §1.2 sketches the closure as
//
//	mayResolveTo(v, s) :- flowStep(v, mid), mayResolveTo(mid, s).
//
// That wording assumes `flowStep(downstream, upstream)` (a "comes from"
// edge). PR2's `LocalFlowStep` and PR3's `InterFlowStep` instead emit
// `(from, to)` as `(source-expression, use-site-expression)` — the
// natural direction of value flow at the AST level
// (`const x = init; use(x)` produces `LocalFlowStep(init, useRef)`).
// The recursive rule below walks backwards along that edge:
//
//	MayResolveTo(v, s) :- FlowStep(mid, v), MayResolveTo(mid, s).
//
// Reads as: "v may resolve to s if some upstream expression mid does,
// and there is a FlowStep edge from mid forward to v." Same closure,
// edge-direction-corrected for the as-shipped `FlowStep(source, use)`
// orientation. The plan doc §1.2 will be updated alongside PR5's path
// version to match — until then this comment is the authoritative
// statement of the predicate's direction contract.
//
// # Two-rule shape (no inline disjunction)
//
// Both rules below share the head `MayResolveTo(v, s)`. The seminaive
// evaluator unions them into one IDB. This is the same multiple-rule
// union shape PR2's `localFlowStepUnion` and PR3's `interFlowStepUnion`
// use to dodge #166 — never an inline `(A or B)` literal disjunction.
func MayResolveToRules() []datalog.Rule {
	return []datalog.Rule{
		// Base case: every value-source expression resolves to itself.
		// MayResolveTo(v, s) :- ExprValueSource(v, s).
		// ExprValueSource emits identity rows (expr == sourceExpr) so the
		// base case seeds (s, s) for every value-source-kind expression.
		rule("MayResolveTo",
			[]datalog.Term{v("v"), v("s")},
			pos("ExprValueSource", v("v"), v("s")),
		),

		// Recursive case: walk one FlowStep edge backwards.
		// MayResolveTo(v, s) :- FlowStep(mid, v), MayResolveTo(mid, s).
		//
		// FlowStep(from=source-expr, to=use-expr) is PR3's
		// `LocalFlowStep ∪ InterFlowStep` union. To go from a known v
		// (use site) toward a source s, we look for some mid that has
		// already been resolved AND has a FlowStep edge forward to v.
		//
		// The MayResolveTo literal is in the join's binding-extension
		// position (mid is bound by FlowStep first), so magic-set rewrite
		// against a bound-v query can push the v binding into FlowStep's
		// `to` column and prune the recursive call's input.
		rule("MayResolveTo",
			[]datalog.Term{v("v"), v("s")},
			pos("FlowStep", v("mid"), v("v")),
			pos("MayResolveTo", v("mid"), v("s")),
		),
	}
}
