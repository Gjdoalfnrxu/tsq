package eval

import (
	"context"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

// EstimateNonRecursiveIDBSizes pre-computes the cardinality of every
// "trivially evaluable" derived predicate in prog (see plan.IdentifyTrivialIDBs)
// using the supplied base relations, and writes each computed count into
// sizeHints (in place). The returned map mirrors the writes, so callers that
// want a separate copy can use it.
//
// Why this exists: the planner's greedy join ordering uses sizeHints to score
// literals. Without entries for derived predicates, IDB literals fall through
// to defaultSizeHint=1000 (ql/plan/join.go). For queries whose ideal seed is
// a tiny derived predicate (e.g. isUseStateSetterCall, 7 tuples in the React
// bridge), the planner picks a Cartesian-heavy order based on the
// 1000-tuple lie and OOMs (issue #88). The previous fix re-planned BETWEEN
// strata, but co-stratified rules — where the seed and the explody rule are
// in the same SCC — never benefit because the seed is not yet materialised
// when its sibling rule is planned. This pre-pass is the missing piece.
//
// Restrictions on what is "trivial": rules with only positive base-predicate
// atoms and comparisons — no negation, no aggregates, no recursion, no
// references to other IDBs. Predicates not meeting this bar are left at the
// default hint and will benefit from the existing between-strata refresh
// only if they happen to be in a later stratum.
//
// Update semantics: an existing sizeHints entry is overwritten only when the
// computed value is strictly larger (defensive — a manually-supplied small
// base count for a name colliding with an IDB head must not be silently
// shrunk; see the parallel rule in seminaive.go's between-strata refresh).
//
// Errors during evaluation of an individual trivial IDB (e.g. binding cap)
// are silently absorbed — the pre-pass is best-effort. If a "trivial" rule
// itself OOMs we'd rather degrade to the default hint than fail compilation.
//
// maxBindingsPerRule is the per-rule binding cap applied to each trivial IDB
// evaluation. Without it the pre-pass can fully materialise an N×M join just
// to count head facts and OOM (issue #130 — mastodon corpus blew 30 GB RSS
// inside this function on setStateUpdaterCallsFn). Pass 0 to disable the cap
// (legacy behaviour, not recommended on real corpora). When the cap fires the
// failed/break branch below treats the IDB as "could not estimate" and the
// default hint applies — exactly the right semantics for a best-effort pass.
//
// Returns the slice of (name, computed-size) updates actually applied, for
// observability/testing.
func EstimateNonRecursiveIDBSizes(prog *datalog.Program, baseRels map[string]*Relation, sizeHints map[string]int, maxBindingsPerRule int) map[string]int {
	if sizeHints == nil {
		sizeHints = map[string]int{}
	}
	if prog == nil {
		return map[string]int{}
	}

	basePreds := make(map[string]bool, len(baseRels))
	for _, rel := range baseRels {
		if rel == nil {
			continue
		}
		basePreds[rel.Name] = true
	}

	// trivials is in topological order: each entry depends only on base
	// predicates and on earlier entries. We materialise as we go and stash
	// each result back into `keyed` so subsequent rules can reference it.
	trivials := plan.IdentifyTrivialIDBs(prog, basePreds)
	updates := make(map[string]int, len(trivials))

	keyed := keyRels(baseRels)

	for _, t := range trivials {
		head := NewRelation(t.Name, t.Arity)
		failed := false
		for _, rule := range t.Rules {
			planned := plan.SingleRule(rule, sizeHints)
			// Apply the user-supplied binding cap so a pathological body
			// (cross-product before a selective join) cannot eat all RAM
			// here. On cap-exceeded the err branch below treats the IDB as
			// unestimatable and falls through to the default hint. Issue
			// #130: passing 0 here meant pre-pass evaluation was unbounded
			// and OOMed on real corpora before the cap could ever fire on
			// the main eval pass.
			tuples, err := Rule(context.Background(), planned, keyed, maxBindingsPerRule)
			if err != nil {
				// Best-effort: skip this IDB entirely on any error so we
				// don't half-populate hints. The default hint will apply
				// and the between-strata refresh in Evaluate will catch up
				// (for non-co-stratified cases at least).
				failed = true
				break
			}
			for _, tup := range tuples {
				head.Add(tup)
			}
		}
		if failed {
			continue
		}
		// Make this IDB visible to subsequent trivial rules that reference
		// it. keyRels uses (name,arity) so this won't shadow a base relation
		// of a different arity. Topological order guarantees no later
		// trivial depends on something not yet in `keyed`.
		keyed[relKey(t.Name, t.Arity)] = head

		n := head.Len()
		if cur, exists := sizeHints[t.Name]; !exists || n > cur {
			sizeHints[t.Name] = n
		}
		updates[t.Name] = n
	}

	return updates
}
