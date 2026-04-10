package eval

import (
	"sync"

	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

// parallelBootstrap evaluates rules concurrently, grouping by head predicate.
// Rules with different head predicates run in parallel; rules with the same
// head predicate run sequentially within their group to avoid data races on
// the shared Relation.
func parallelBootstrap(rules []plan.PlannedRule, allRels map[string]*Relation) map[string]*Relation {
	// Group rules by head predicate.
	groups := groupByHead(rules)

	// Collect results per group, then merge.
	type groupResult struct {
		pred   string
		tuples []Tuple
	}

	results := make([]groupResult, len(groups))
	var wg sync.WaitGroup

	i := 0
	for pred, groupRules := range groups {
		wg.Add(1)
		go func(idx int, p string, rs []plan.PlannedRule) {
			defer wg.Done()
			var tuples []Tuple
			// Take a read-only snapshot of allRels for this group.
			// Each group reads from the shared allRels (safe: read-only at this point).
			for _, rule := range rs {
				newTuples := Rule(rule, allRels)
				tuples = append(tuples, newTuples...)
			}
			results[idx] = groupResult{pred: p, tuples: tuples}
		}(i, pred, groupRules)
		i++
	}

	wg.Wait()

	// Merge results into delta relations.
	deltaRels := make(map[string]*Relation)
	for _, gr := range results {
		headRel := allRels[gr.pred]
		for _, t := range gr.tuples {
			if headRel.Add(t) {
				dr, ok := deltaRels[gr.pred]
				if !ok {
					dr = NewRelation(gr.pred, headRel.Arity)
					deltaRels[gr.pred] = dr
				}
				dr.Add(t)
			}
		}
	}
	return deltaRels
}

// parallelDelta evaluates delta rules concurrently, grouping by head predicate.
func parallelDelta(rules []plan.PlannedRule, allRels map[string]*Relation, deltaRels map[string]*Relation) map[string]*Relation {
	groups := groupByHead(rules)

	type groupResult struct {
		pred   string
		tuples []Tuple
	}

	results := make([]groupResult, len(groups))
	var wg sync.WaitGroup

	i := 0
	for pred, groupRules := range groups {
		wg.Add(1)
		go func(idx int, p string, rs []plan.PlannedRule) {
			defer wg.Done()
			var tuples []Tuple
			for _, rule := range rs {
				newTuples := RuleDelta(rule, allRels, deltaRels)
				tuples = append(tuples, newTuples...)
			}
			results[idx] = groupResult{pred: p, tuples: tuples}
		}(i, pred, groupRules)
		i++
	}

	wg.Wait()

	nextDelta := make(map[string]*Relation)
	for _, gr := range results {
		headRel := allRels[gr.pred]
		for _, t := range gr.tuples {
			if headRel.Add(t) {
				dr, ok := nextDelta[gr.pred]
				if !ok {
					dr = NewRelation(gr.pred, headRel.Arity)
					nextDelta[gr.pred] = dr
				}
				dr.Add(t)
			}
		}
	}
	return nextDelta
}

// groupByHead groups planned rules by their head predicate name.
func groupByHead(rules []plan.PlannedRule) map[string][]plan.PlannedRule {
	groups := make(map[string][]plan.PlannedRule)
	for _, rule := range rules {
		pred := rule.Head.Predicate
		groups[pred] = append(groups[pred], rule)
	}
	return groups
}
