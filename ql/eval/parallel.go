package eval

import (
	"context"
	"fmt"
	"sync"

	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

// firstError returns the first non-nil error from a slice, or nil if all are nil.
//
// This returns the positionally-first non-nil error, NOT the temporally-first.
// Temporal ordering is provided separately by the (childCtx, cancelOnce)
// pattern in parallelBootstrap/parallelDelta: the first failing worker
// cancels the shared child ctx, which causes sibling workers to bail with a
// ctx-error variant. The positional-first error is then a deterministic,
// reproducible representative of the failure, while the cancellation gives
// us the bounded latency on errored runs.
func firstError(errs []error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

// parallelBootstrap evaluates rules concurrently, grouping by head predicate.
// Rules with different head predicates run in parallel; rules with the same
// head predicate run sequentially within their group to avoid data races on
// the shared Relation. Grouping is by (name, arity) — same-name different-arity
// heads are independent and merge into independent relations.
func parallelBootstrap(ctx context.Context, rules []plan.PlannedRule, allRels map[string]*Relation, maxBindings int) (map[string]*Relation, error) {
	groups := groupByHead(rules)

	type groupResult struct {
		key    string // (name, arity) key
		name   string
		tuples []Tuple
	}

	results := make([]groupResult, len(groups))
	errs := make([]error, len(groups))
	var wg sync.WaitGroup

	// Sibling cancellation: derive a child ctx that the first failing worker
	// cancels. Without this, a worker that hits BindingCapError after 1s
	// would block wg.Wait() for the slowest sibling Rule() (potentially 30s)
	// before returning. With it, siblings observe ctx.Done() at their next
	// throttled check and bail in milliseconds.
	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	i := 0
	for gk, groupRules := range groups {
		wg.Add(1)
		go func(idx int, k string, rs []plan.PlannedRule) {
			defer wg.Done()
			var tuples []Tuple
			for _, rule := range rs {
				// Cooperative cancellation (issue #81): bail early if the
				// shared child context is done — either the outer caller
				// cancelled, or a sibling worker errored and we want to
				// stop launching new work in this group.
				if cerr := childCtx.Err(); cerr != nil {
					errs[idx] = fmt.Errorf("parallel bootstrap cancelled at rule %s: %w", rule.Head.Predicate, cerr)
					cancel()
					return
				}
				newTuples, rerr := Rule(childCtx, rule, allRels, maxBindings)
				if rerr != nil {
					errs[idx] = rerr
					// Cancel siblings: first error wins, others observe via childCtx.
					cancel()
					return
				}
				if cerr := childCtx.Err(); cerr != nil {
					errs[idx] = fmt.Errorf("parallel bootstrap cancelled after rule %s: %w", rule.Head.Predicate, cerr)
					cancel()
					return
				}
				tuples = append(tuples, newTuples...)
			}
			results[idx] = groupResult{key: k, name: rs[0].Head.Predicate, tuples: tuples}
		}(i, gk, groupRules)
		i++
	}

	wg.Wait()

	if err := firstError(errs); err != nil {
		return nil, err
	}

	deltaRels := make(map[string]*Relation)
	for _, gr := range results {
		headRel := allRels[gr.key]
		for _, t := range gr.tuples {
			if headRel.Add(t) {
				dr, ok := deltaRels[gr.key]
				if !ok {
					dr = NewRelation(gr.name, headRel.Arity)
					deltaRels[gr.key] = dr
				}
				dr.Add(t)
			}
		}
	}
	return deltaRels, nil
}

// parallelDelta evaluates delta rules concurrently, grouping by head (name, arity).
func parallelDelta(ctx context.Context, rules []plan.PlannedRule, allRels map[string]*Relation, deltaRels map[string]*Relation, maxBindings int) (map[string]*Relation, error) {
	groups := groupByHead(rules)

	type groupResult struct {
		key    string
		name   string
		tuples []Tuple
	}

	results := make([]groupResult, len(groups))
	errs := make([]error, len(groups))
	var wg sync.WaitGroup

	// Sibling cancellation — see parallelBootstrap for the rationale.
	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	i := 0
	for gk, groupRules := range groups {
		wg.Add(1)
		go func(idx int, k string, rs []plan.PlannedRule) {
			defer wg.Done()
			var tuples []Tuple
			for _, rule := range rs {
				// Cooperative cancellation (issue #81): see parallelBootstrap.
				if cerr := childCtx.Err(); cerr != nil {
					errs[idx] = fmt.Errorf("parallel delta cancelled at rule %s: %w", rule.Head.Predicate, cerr)
					cancel()
					return
				}
				newTuples, rerr := RuleDelta(childCtx, rule, allRels, deltaRels, maxBindings)
				if rerr != nil {
					errs[idx] = rerr
					cancel()
					return
				}
				if cerr := childCtx.Err(); cerr != nil {
					errs[idx] = fmt.Errorf("parallel delta cancelled after rule %s: %w", rule.Head.Predicate, cerr)
					cancel()
					return
				}
				tuples = append(tuples, newTuples...)
			}
			results[idx] = groupResult{key: k, name: rs[0].Head.Predicate, tuples: tuples}
		}(i, gk, groupRules)
		i++
	}

	wg.Wait()

	if err := firstError(errs); err != nil {
		return nil, err
	}

	nextDelta := make(map[string]*Relation)
	for _, gr := range results {
		headRel := allRels[gr.key]
		for _, t := range gr.tuples {
			if headRel.Add(t) {
				dr, ok := nextDelta[gr.key]
				if !ok {
					dr = NewRelation(gr.name, headRel.Arity)
					nextDelta[gr.key] = dr
				}
				dr.Add(t)
			}
		}
	}
	return nextDelta, nil
}

// groupByHead groups planned rules by their head (name, arity) key.
// Two rules with the same head name but different arities form separate
// groups, so the eval engine never conflates them.
func groupByHead(rules []plan.PlannedRule) map[string][]plan.PlannedRule {
	groups := make(map[string][]plan.PlannedRule)
	for _, rule := range rules {
		k := relKey(rule.Head.Predicate, len(rule.Head.Args))
		groups[k] = append(groups[k], rule)
	}
	return groups
}
