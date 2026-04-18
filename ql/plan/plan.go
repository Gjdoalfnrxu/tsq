// Package plan implements stratification and join ordering over Datalog programs.
package plan

import (
	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
)

// ExecutionPlan is the output of the planner.
type ExecutionPlan struct {
	Strata []Stratum
	Query  *PlannedQuery // nil if no select clause
}

// Stratum is a set of rules that can be evaluated together (same SCC or dependent group).
type Stratum struct {
	Rules      []PlannedRule
	Aggregates []PlannedAggregate
}

// PlannedRule is a rule with a determined join order.
//
// Body holds the original rule body (in source order) so that the rule can be
// re-planned later with updated size hints — e.g. between strata, once a
// derived (IDB) predicate's true tuple count is known. Without Body the
// evaluator would have no way to recover the literals to reorder; JoinOrder
// alone is the post-greedy result and not equivalent to the input.
type PlannedRule struct {
	Head      datalog.Atom
	Body      []datalog.Literal
	JoinOrder []JoinStep
}

// JoinStep is one step in the join plan.
type JoinStep struct {
	Literal  datalog.Literal // the literal being joined (may be negative)
	JoinCols [][2]int        // pairs of (bodyVar, headVar) positions — for index building
	// IsFilter is true if all variables in Literal are already bound, meaning this step
	// acts as a filter rather than introducing new bindings.
	// Note: IsFilter=true on a negative literal (Literal.Positive==false) means anti-join,
	// not positive membership filter. Callers must check Literal.Positive to distinguish.
	IsFilter bool
}

// PlannedAggregate is an aggregate to evaluate after the stratum fixpoint.
type PlannedAggregate struct {
	ResultRelation string // name of the relation that holds aggregate results
	Agg            datalog.Aggregate
	GroupByVars    []datalog.Var // variables that form the group key
}

// PlannedQuery is the select clause plan.
type PlannedQuery struct {
	Select    []datalog.Term
	JoinOrder []JoinStep
}

// WithMagicSet applies the magic-set transformation to the program using
// the given query bindings, then plans the resulting program. queryBindings
// maps predicate names to bound argument positions.
func WithMagicSet(prog *datalog.Program, sizeHints map[string]int, queryBindings map[string][]int) (*ExecutionPlan, []error) {
	transformed := MagicSetTransform(prog, queryBindings)
	return Plan(transformed, sizeHints)
}

// EstimatorHook is a callback that pre-computes cardinalities for trivially-
// evaluable IDB predicates and writes them into sizeHints (in place). It is
// injected by callers in the eval package so that the planner can run the
// trivial-IDB pre-pass before constructing the final execution plan, without
// the plan package taking a build-time dependency on eval (which would be a
// cycle: eval already imports plan).
//
// Hook contract:
//   - prog and sizeHints are the same values EstimateAndPlan was called with.
//   - maxBindingsPerRule is the per-rule binding cap; the hook MUST honour it
//     so a pathological body cannot OOM the host before planning even starts
//     (issue #130). Pass 0 inside the hook only when the caller of
//     EstimateAndPlan deliberately supplied 0.
//   - The hook may mutate sizeHints in place; it returns the slice of updates
//     applied (for observability) but the canonical state is the mutated map.
//   - Best-effort semantics: failures inside the hook (e.g. cap fires on an
//     individual trivial IDB) MUST be absorbed silently — the IDB falls
//     through to the default hint and the plan proceeds. The hook MUST NOT
//     return an error.
//
// A nil hook is permitted: EstimateAndPlan then degrades to plain Plan with
// whatever sizeHints the caller supplied (useful for tests and for callers
// that have no fact DB to estimate against).
type EstimatorHook func(prog *datalog.Program, sizeHints map[string]int, maxBindingsPerRule int) map[string]int

// MaterialisingEstimatorHook is the P2a-extended estimator contract: in
// addition to writing trivial-IDB cardinalities into sizeHints (the same
// job EstimatorHook does), it returns a set of class-extent head
// predicate names that the hook materialised eagerly. The actual relation
// objects live opaquely behind the hook and are handed to the evaluator
// via a separate channel (see eval.WithMaterialisedClassExtents). The
// planner only needs the names so it can mark those rules as
// already-evaluated and skip planning their bodies.
//
// The hook mutates sizeHints in place (same contract as EstimatorHook);
// there is no separate updates return value because the planner has no
// use for one — every call site would throw it away.
//
// The returned name set is keyed by predicate NAME (not name+arity)
// because class extents are always 1-arity; the eval-side hook owner is
// the source of truth for the actual *Relation values and arity-keys
// them internally via relKey().
type MaterialisingEstimatorHook func(prog *datalog.Program, sizeHints map[string]int, maxBindingsPerRule int) (materialisedExtents map[string]bool)

// Func is the planning entry point used by EstimateAndPlan after the
// pre-pass has populated sizeHints. The default is plan.Plan; callers that
// want magic-set rewriting pass plan.WithMagicSetAutoOpts (wrapped to match
// this signature). Keeping this pluggable lets EstimateAndPlan stay the
// single estimate-then-plan entry point regardless of which planner variant
// is in use, without EstimateAndPlan needing to know about magic-set options.
type Func func(prog *datalog.Program, sizeHints map[string]int) (*ExecutionPlan, []error)

// EstimateAndPlan is the single estimate-then-plan entry point. It owns the
// order: stratify (implicit, via planFn) → identify trivial IDBs and estimate
// their sizes via the eval-supplied hook → plan everything once with full
// hints. Replaces the prior two-pass ceremony in cmd/tsq's compileAndEval
// (cheap-plan → estimate → re-plan-every-stratum), eliminating a re-entrancy
// hazard between the two passes (#88-era trust-channel for IDB hints).
//
// Why a single entry point: the prior compileAndEval flow first called
// plan.Plan with base-only hints, then ran EstimateNonRecursiveIDBSizes,
// then called RePlanStratum/RePlanQuery to swap in the refreshed hints. That
// produced two distinct planning passes that had to agree on stratification,
// magic-set inference, and binding propagation. Folding to one pass means
// magic-set inference (and any future hint-aware planner pass) sees the
// IDB cardinalities from the very first call — no out-of-band trust channel
// needed to bridge the two passes.
//
// Arguments:
//   - prog: the post-desugar, post-MergeSystemRules program to plan.
//   - sizeHints: caller-supplied hints (typically built from base relation
//     tuple counts). May be nil. Mutated in place by the estimator hook so
//     the post-call map carries the trivial-IDB sizes too.
//   - maxBindingsPerRule: binding cap forwarded to the estimator hook
//     (issue #130 / PR #132 — must NOT be lost when migrating callers).
//   - estimator: see EstimatorHook docs. May be nil to skip the pre-pass.
//   - planFn: the planner to invoke once hints are populated. Pass plan.Plan
//     for the no-magic-set path; pass a closure over WithMagicSetAutoOpts
//     for the magic-set path.
//
// The between-strata refresh in eval.Evaluate (RePlanStratum/RePlanQuery
// after each stratum's fixpoint) is preserved untouched — it handles the
// case where a non-trivial (recursive) IDB's true size becomes known
// mid-evaluation, which the estimator hook cannot pre-compute by definition.
// EstimateAndPlan only collapses the redundant outer two-pass ceremony.
func EstimateAndPlan(
	prog *datalog.Program,
	sizeHints map[string]int,
	maxBindingsPerRule int,
	estimator EstimatorHook,
	planFn Func,
) (*ExecutionPlan, []error) {
	return EstimateAndPlanWithExtents(prog, sizeHints, maxBindingsPerRule, estimator, nil, planFn)
}

// EstimateAndPlanWithExtents is the P2a-extended entry point. When
// matExtHook is non-nil it is preferred over estimator: the hook may
// pre-materialise tagged class-extent rules (rule.ClassExtent == true,
// body matches IsClassExtentBody) and return their head names. Those
// rules are then stripped from the program before planning so the
// planner treats them as base relations supplied externally to Evaluate.
// The eval side is responsible for actually injecting the *Relation
// objects via WithMaterialisedClassExtents.
//
// Filtering semantics: only rules whose head name appears in the
// returned materialisedExtents set AND whose ClassExtent flag is true
// AND whose head arity is 1 are removed. A rule that is tagged but
// arity > 1 (defensive — desugarer only emits arity-1 char preds) or
// whose head appears in materialisedExtents but is NOT tagged
// (defensive — name collision between a class extent and a hand-written
// predicate) is left in place so the evaluator still computes it.
//
// estimator is invoked AFTER materialisation if matExtHook is supplied,
// so it sees the post-materialisation program (without the stripped
// rules) — which mirrors what the planner will see. Both hooks
// contribute to sizeHints; the matExtHook's updates overwrite estimator's
// for the same key per the same "only-grow" semantics that
// EstimateNonRecursiveIDBSizes already documents.
func EstimateAndPlanWithExtents(
	prog *datalog.Program,
	sizeHints map[string]int,
	maxBindingsPerRule int,
	estimator EstimatorHook,
	matExtHook MaterialisingEstimatorHook,
	planFn Func,
) (*ExecutionPlan, []error) {
	if planFn == nil {
		planFn = Plan
	}
	if sizeHints == nil {
		sizeHints = map[string]int{}
	}

	// P2a: class-extent materialisation. Run the materialising hook first
	// so the trivial-IDB pre-pass below sees the (already-materialised)
	// extents as base-like relations and doesn't re-evaluate them.
	var materialisedExtents map[string]bool
	if matExtHook != nil {
		materialisedExtents = matExtHook(prog, sizeHints, maxBindingsPerRule)
	}
	if estimator != nil {
		_ = estimator(prog, sizeHints, maxBindingsPerRule)
	}

	// Strip materialised class-extent rules from the program so the
	// planner treats them as externally-supplied base relations. The
	// stratifier will then see those head names as undefined (and
	// dependents will reference them as if they were base preds, which is
	// exactly what they are once Evaluate injects them). Uses a defensive
	// filter — see function-doc semantics.
	planProg := prog
	if len(materialisedExtents) > 0 {
		filtered := make([]datalog.Rule, 0, len(prog.Rules))
		for _, rule := range prog.Rules {
			if rule.ClassExtent && len(rule.Head.Args) == 1 && materialisedExtents[rule.Head.Predicate] {
				continue
			}
			filtered = append(filtered, rule)
		}
		// Only rebuild the Program struct if we actually filtered
		// anything — the common case (no materialisation) skips an
		// allocation.
		if len(filtered) != len(prog.Rules) {
			planProg = &datalog.Program{Rules: filtered, Query: prog.Query}
		}
	}

	return planFn(planProg, sizeHints)
}

// Plan produces an ExecutionPlan from a Datalog program.
// sizeHints maps relation names to estimated tuple counts (for join ordering).
// Unknown relations default to 1000.
func Plan(prog *datalog.Program, sizeHints map[string]int) (*ExecutionPlan, []error) {
	// Validate all rules first.
	var errs []error
	for _, rule := range prog.Rules {
		if ruleErrs := ValidateRule(rule); len(ruleErrs) > 0 {
			errs = append(errs, ruleErrs...)
		}
	}
	if len(errs) > 0 {
		return nil, errs
	}

	// Build dependency graph and stratify.
	strata, stratErrs := stratify(prog.Rules)
	if len(stratErrs) > 0 {
		return nil, stratErrs
	}

	if sizeHints == nil {
		sizeHints = map[string]int{}
	}

	// P3a: compute per-predicate backward-demand map before ordering any
	// rule body. This is a pure function of the program and sizeHints —
	// no side effects, no evaluator dependency. When demand is empty
	// (e.g. no IDB is caller-grounded) orderJoinsWithDemand degrades to
	// the same behaviour as orderJoins, preserving all prior plans as
	// a lower bound.
	demand := InferBackwardDemand(prog, sizeHints)

	ep := &ExecutionPlan{}
	for _, stratum := range strata {
		ps := Stratum{}
		for _, rule := range stratum {
			headDemand := demand[rule.Head.Predicate]
			order := orderJoinsWithDemand(rule.Head, rule.Body, sizeHints, headDemand)
			ps.Rules = append(ps.Rules, PlannedRule{
				Head:      rule.Head,
				Body:      rule.Body,
				JoinOrder: order,
			})
			// Collect aggregates from rule body.
			for _, lit := range rule.Body {
				if lit.Agg != nil {
					ps.Aggregates = append(ps.Aggregates, PlannedAggregate{
						ResultRelation: lit.Agg.ResultVar.Name,
						Agg:            *lit.Agg,
						GroupByVars:    collectGroupByVars(rule, lit.Agg),
					})
				}
			}
		}
		ep.Strata = append(ep.Strata, ps)
	}

	// Plan the query.
	if prog.Query != nil {
		order := orderJoins(prog.Query.Body, sizeHints)
		ep.Query = &PlannedQuery{
			Select:    prog.Query.Select,
			JoinOrder: order,
		}
	}

	return ep, nil
}

// RePlanStratum recomputes the JoinOrder of every rule in the given stratum
// using the supplied sizeHints. It mutates the stratum in place. Aggregates
// and head atoms are left untouched. Use this after a prior stratum's fixpoint
// has materialised a derived relation, so that subsequent strata are planned
// with that relation's true cardinality instead of defaultSizeHint.
//
// If a rule's Body is nil (i.e. the stratum was constructed by code that did
// not populate Body — pre-#88 callers) the rule is skipped so behaviour is
// unchanged for legacy callers.
//
// Does NOT recompute backward demand — the between-strata refresh does not
// know about other strata's rules. For demand-aware replanning use
// RePlanStratumWithDemand, which takes a pre-computed demand map.
func RePlanStratum(s *Stratum, sizeHints map[string]int) {
	if s == nil {
		return
	}
	if sizeHints == nil {
		sizeHints = map[string]int{}
	}
	for i := range s.Rules {
		body := s.Rules[i].Body
		if body == nil {
			continue
		}
		s.Rules[i].JoinOrder = orderJoins(body, sizeHints)
	}
}

// RePlanStratumWithDemand is RePlanStratum plus backward-demand awareness.
// Callers that hold a previously-computed DemandMap (typically the one
// returned by InferBackwardDemand at initial Plan time, carried through
// Evaluate) can use this form to preserve the demand-driven seed choice
// across between-strata refreshes.
//
// Note that demand is NOT recomputed here: refreshing it between strata
// would risk flapping (a size hint that crosses a threshold mid-evaluation
// could add or drop positions, producing a different plan than the initial
// one). Stable demand across the fixpoint keeps seminaive convergence
// analysis straightforward.
func RePlanStratumWithDemand(s *Stratum, sizeHints map[string]int, demand DemandMap) {
	if s == nil {
		return
	}
	if sizeHints == nil {
		sizeHints = map[string]int{}
	}
	for i := range s.Rules {
		body := s.Rules[i].Body
		if body == nil {
			continue
		}
		headDemand := demand[s.Rules[i].Head.Predicate]
		s.Rules[i].JoinOrder = orderJoinsWithDemand(s.Rules[i].Head, body, sizeHints, headDemand)
	}
}

// RePlanQuery recomputes the JoinOrder of the planned query with updated
// sizeHints. The query body is reconstructed from the existing JoinOrder
// (since we kept the literals there). Unlike rules, queries have no separate
// Body field — the JoinOrder literals ARE the body in some order. Reordering
// is invariant to input order because orderJoins is greedy on the literal set.
func RePlanQuery(q *PlannedQuery, sizeHints map[string]int) {
	if q == nil {
		return
	}
	if sizeHints == nil {
		sizeHints = map[string]int{}
	}
	body := make([]datalog.Literal, len(q.JoinOrder))
	for i, step := range q.JoinOrder {
		body[i] = step.Literal
	}
	q.JoinOrder = orderJoins(body, sizeHints)
}

// collectGroupByVars returns the head variables that are not the aggregate result variable.
func collectGroupByVars(rule datalog.Rule, agg *datalog.Aggregate) []datalog.Var {
	aggResultName := agg.ResultVar.Name
	var groupBy []datalog.Var
	seen := map[string]bool{}
	for _, arg := range rule.Head.Args {
		if v, ok := arg.(datalog.Var); ok {
			if v.Name != aggResultName && !seen[v.Name] {
				groupBy = append(groupBy, v)
				seen[v.Name] = true
			}
		}
	}
	return groupBy
}
