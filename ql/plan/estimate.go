package plan

import (
	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
)

// TrivialIDB describes a derived (IDB) predicate whose definition is
// "trivially evaluable" before main planning: every rule defining the
// predicate has a body composed only of base (EDB) predicates and
// comparisons, with no negation, no aggregates, and no references to other
// IDBs. Such predicates can be evaluated up-front using only base relations,
// giving the planner real cardinality numbers instead of the
// defaultSizeHint=1000 fallback.
//
// This is the fix for the co-stratified case of issue #88: when a tiny IDB
// seed (e.g. isUseStateSetterCall, 7 tuples) and the explody rule that uses
// it (e.g. setStateUpdaterCallsFn) land in the same stratum, the prior
// "between-strata refresh" never fires for the seed before the explody rule
// is planned, so the planner picks a Cartesian-heavy join order. Pre-computing
// trivial IDBs gives the planner accurate seed sizes from the very first
// Plan() call.
type TrivialIDB struct {
	// Name is the predicate name.
	Name string
	// Arity is the head arity for all rules defining this predicate.
	// All rules defining a TrivialIDB must share the same arity (else it
	// is excluded from the trivial set).
	Arity int
	// Rules are the original Datalog rules defining this predicate.
	// Multiple rules represent a union (a disjunction in QL source).
	Rules []datalog.Rule
}

// IdentifyTrivialIDBs returns the set of derived predicates in prog whose
// definitions are evaluable using only base predicates and other already-
// trivial IDBs. The result is in topological order: each TrivialIDB depends
// only on basePredicates and on TrivialIDBs that appear earlier in the slice.
//
// A rule body is admissible if every literal is either a comparison or a
// positive atom whose predicate is in basePredicates OR is the head of
// another trivial-eligible IDB. Negation, aggregates, and recursion (direct
// or indirect) all disqualify the head predicate. Predicates defined with
// inconsistent arities across rules are excluded (defensive — the rest of
// the pipeline assumes uniform arity).
//
// The transitive closure matters for the React-bridge case in issue #88:
// `isUseStateSetterCall(c) :- CallCalleeSym(c,sym), isUseStateSetterSym(sym).`
// references another IDB (`isUseStateSetterSym`), but that one IS trivial,
// so once its size is known, `isUseStateSetterCall` becomes evaluable too.
// Without the closure we'd cap out at 1-hop trivials and the planner would
// still mis-score the actual seed predicate of the explody rule.
//
// `basePredicates` is the set of names that are EDB (base) relations — i.e.
// supplied by the fact DB. The fixed-point iteration runs to convergence
// (each pass admits at least one more IDB or terminates).
func IdentifyTrivialIDBs(prog *datalog.Program, basePredicates map[string]bool) []TrivialIDB {
	if prog == nil || len(prog.Rules) == 0 {
		return nil
	}

	// Group rules by head predicate name. Track arity per name; if a name
	// appears with multiple arities we drop it (defensive).
	rulesByHead := map[string][]datalog.Rule{}
	arityByHead := map[string]int{}
	multiArity := map[string]bool{}
	headOrder := []string{} // first-seen order for stable output
	for _, rule := range prog.Rules {
		name := rule.Head.Predicate
		arity := len(rule.Head.Args)
		if existing, seen := arityByHead[name]; seen {
			if existing != arity {
				multiArity[name] = true
			}
		} else {
			arityByHead[name] = arity
			headOrder = append(headOrder, name)
		}
		rulesByHead[name] = append(rulesByHead[name], rule)
	}

	// Fixed-point closure: a head is trivial in pass N if every body literal
	// is a comparison, a positive atom on a base predicate, or a positive
	// atom on a head already accepted as trivial in passes 1..N-1.
	// Convergence is guaranteed: each pass either adds a head to `accepted`
	// or doesn't, and the set is bounded by the head count.
	accepted := map[string]bool{}
	out := []TrivialIDB{}
	for {
		progress := false
		for _, name := range headOrder {
			if accepted[name] || multiArity[name] || basePredicates[name] {
				continue
			}
			rules := rulesByHead[name]
			ok := true
			for _, rule := range rules {
				if !ruleBodyIsTrivial(rule.Body, basePredicates, accepted, name) {
					ok = false
					break
				}
			}
			if !ok {
				continue
			}
			accepted[name] = true
			out = append(out, TrivialIDB{
				Name:  name,
				Arity: arityByHead[name],
				Rules: rules,
			})
			progress = true
		}
		if !progress {
			break
		}
	}
	return out
}

// IsClassExtentBody returns true if the rule body matches the structural
// shape of a QL class extent that is safe to materialise once and treat as
// a base relation. The shape is intentionally narrow:
//
//   - At least one positive atom (so the head is grounded by something).
//   - Every literal is either:
//     a) a positive atom whose predicate is in basePredicates, OR
//     b) a positive atom whose predicate is in materialisedExtents
//     (an already-materialised class extent — supports abstract-class
//     union rules and shallow class chains), OR
//     c) a comparison literal.
//   - No negation, no aggregates, no recursion.
//
// The intent (per P2a of the planner roadmap) is to reject rules that
// would force eager materialisation of multiple large extents — e.g. a
// class whose body is `Foo(this) :- Bar(this), Baz(this), Quux(this)` over
// three large IDB extents would NOT match (only one positive atom over a
// known materialisable name). This keeps the materialisation cost bounded
// at "one scan over the largest body relation per class extent".
//
// Callers should additionally check rule.ClassExtent — the structural
// match is necessary but the desugarer's tag is the authoritative signal
// that the rule originated from a class declaration. A rule that looks
// extent-shaped but is e.g. a hand-written user predicate is not eligible
// for the eager-materialise path because the cost trade-off (eager
// evaluation regardless of whether the predicate is referenced) is
// class-extent specific.
//
// `materialisedExtents` is the set of class-extent head names already
// promoted to base-like status by an earlier pass; pass nil if no extents
// have been promoted yet (the first iteration of the materialisation
// fixed-point sees only base predicates).
func IsClassExtentBody(body []datalog.Literal, basePredicates, materialisedExtents map[string]bool) bool {
	if len(body) == 0 {
		// An empty body would make the head universally true at arity 0,
		// which is degenerate. Treat as ineligible — there is nothing to
		// materialise from.
		return false
	}
	hasPositiveAtom := false
	for _, lit := range body {
		if lit.Cmp != nil {
			continue
		}
		if lit.Agg != nil {
			return false
		}
		if !lit.Positive {
			return false
		}
		dep := lit.Atom.Predicate
		if basePredicates[dep] || materialisedExtents[dep] {
			hasPositiveAtom = true
			continue
		}
		return false
	}
	return hasPositiveAtom
}

// SingleRule plans one rule against the given size hints and returns a
// PlannedRule (with Body retained for re-planning). Exposed so callers can
// produce planned rules outside of the full Plan() pipeline — primarily for
// the trivial-IDB pre-pass in ql/eval, which wants to evaluate individual
// rules before main stratification has run.
//
// Naming note: not "Rule" (clashes with eval.Rule and is type-confusing
// alongside datalog.Rule) and not "PlanRule" (revive flags as stutter).
func SingleRule(rule datalog.Rule, sizeHints map[string]int) PlannedRule {
	if sizeHints == nil {
		sizeHints = map[string]int{}
	}
	return PlannedRule{
		Head:      rule.Head,
		Body:      rule.Body,
		JoinOrder: orderJoins(rule.Body, sizeHints),
	}
}

// ruleBodyIsTrivial returns true if every literal in body is either a
// comparison or a positive atom whose predicate is in basePredicates or in
// the previously-accepted trivial-IDB set. Negation, aggregates, and
// references to predicates not yet accepted (including self-recursion) all
// disqualify the rule for this pass; the caller iterates until convergence.
func ruleBodyIsTrivial(body []datalog.Literal, basePredicates, acceptedTrivials map[string]bool, headName string) bool {
	for _, lit := range body {
		if lit.Cmp != nil {
			// Pure comparison constraint — fine, no predicate dependency.
			continue
		}
		if lit.Agg != nil {
			// Aggregate sub-goal — disqualified. Aggregates can be
			// evaluated only after their underlying stratum is materialised
			// and they introduce a different evaluation path than Rule().
			return false
		}
		if !lit.Positive {
			// Negative literal — disqualified. Anti-joins require the
			// referenced relation to be fully materialised (closed-world
			// assumption); pre-pass evaluation would be ill-defined for
			// non-base negated predicates and risky even for base ones if
			// it interacts with stratification.
			return false
		}
		dep := lit.Atom.Predicate
		if dep == headName {
			// Self-recursive — definitely not trivial.
			return false
		}
		if basePredicates[dep] {
			continue
		}
		if acceptedTrivials[dep] {
			continue
		}
		// References an IDB that is not (yet) accepted as trivial.
		return false
	}
	return true
}
