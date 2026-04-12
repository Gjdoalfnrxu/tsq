package eval

import (
	"sync"

	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

// parallelBootstrap evaluates rules concurrently, grouping by head predicate.
// Rules with different head predicates run in parallel; rules with the same
// head predicate run sequentially within their group to avoid data races on
// the shared Relation. Grouping is by (name, arity) — same-name different-arity
// heads are independent and merge into independent relations.
func parallelBootstrap(rules []plan.PlannedRule, allRels map[string]*Relation) map[string]*Relation {
	groups := groupByHead(rules)

	type groupResult struct {
		key    string // (name, arity) key
		name   string
		tuples []Tuple
	}

	results := make([]groupResult, len(groups))
	var wg sync.WaitGroup

	i := 0
	for gk, groupRules := range groups {
		wg.Add(1)
		go func(idx int, k string, rs []plan.PlannedRule) {
			defer wg.Done()
			var tuples []Tuple
			for _, rule := range rs {
				newTuples := Rule(rule, allRels)
				tuples = append(tuples, newTuples...)
			}
			results[idx] = groupResult{key: k, name: rs[0].Head.Predicate, tuples: tuples}
		}(i, gk, groupRules)
		i++
	}

	wg.Wait()

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
	return deltaRels
}

// parallelDelta evaluates delta rules concurrently, grouping by head (name, arity).
func parallelDelta(rules []plan.PlannedRule, allRels map[string]*Relation, deltaRels map[string]*Relation) map[string]*Relation {
	groups := groupByHead(rules)

	type groupResult struct {
		key    string
		name   string
		tuples []Tuple
	}

	results := make([]groupResult, len(groups))
	var wg sync.WaitGroup

	i := 0
	for gk, groupRules := range groups {
		wg.Add(1)
		go func(idx int, k string, rs []plan.PlannedRule) {
			defer wg.Done()
			var tuples []Tuple
			for _, rule := range rs {
				newTuples := RuleDelta(rule, allRels, deltaRels)
				tuples = append(tuples, newTuples...)
			}
			results[idx] = groupResult{key: k, name: rs[0].Head.Predicate, tuples: tuples}
		}(i, gk, groupRules)
		i++
	}

	wg.Wait()

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
	return nextDelta
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
