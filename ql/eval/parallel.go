package eval

import (
	"context"
	"fmt"
	"sync"

	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

// firstError returns the first non-nil error from a slice, or nil if all are nil.
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

	i := 0
	for gk, groupRules := range groups {
		wg.Add(1)
		go func(idx int, k string, rs []plan.PlannedRule) {
			defer wg.Done()
			var tuples []Tuple
			for _, rule := range rs {
				// Cooperative cancellation (issue #81): bail early if the
				// outer context is already done. Workers cannot stop a
				// running Rule() mid-call, but they can stop launching the
				// next rule in their group.
				if cerr := ctx.Err(); cerr != nil {
					errs[idx] = fmt.Errorf("parallel bootstrap cancelled at rule %s: %w", rule.Head.Predicate, cerr)
					return
				}
				newTuples, rerr := Rule(rule, allRels, maxBindings)
				if rerr != nil {
					errs[idx] = rerr
					return
				}
				if cerr := ctx.Err(); cerr != nil {
					errs[idx] = fmt.Errorf("parallel bootstrap cancelled after rule %s: %w", rule.Head.Predicate, cerr)
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

	i := 0
	for gk, groupRules := range groups {
		wg.Add(1)
		go func(idx int, k string, rs []plan.PlannedRule) {
			defer wg.Done()
			var tuples []Tuple
			for _, rule := range rs {
				// Cooperative cancellation (issue #81): see parallelBootstrap.
				if cerr := ctx.Err(); cerr != nil {
					errs[idx] = fmt.Errorf("parallel delta cancelled at rule %s: %w", rule.Head.Predicate, cerr)
					return
				}
				newTuples, rerr := RuleDelta(rule, allRels, deltaRels, maxBindings)
				if rerr != nil {
					errs[idx] = rerr
					return
				}
				if cerr := ctx.Err(); cerr != nil {
					errs[idx] = fmt.Errorf("parallel delta cancelled after rule %s: %w", rule.Head.Predicate, cerr)
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
