package eval

import (
	"context"
	"fmt"
	"sort"
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
	"pgregory.net/rapid"
)

// ---------------------------------------------------------------------------
// Naive evaluator — reference oracle for differential testing
// ---------------------------------------------------------------------------

// naiveEvaluate runs a simple fixpoint evaluation without delta tracking.
// It repeatedly applies all rules until no new tuples are derived.
func naiveEvaluate(rules []plan.PlannedRule, baseRels map[string]*Relation) map[string]*Relation {
	rels := make(map[string]*Relation, len(baseRels))
	for k, v := range baseRels {
		// Deep copy base relations so we don't mutate them.
		nr := NewRelation(v.Name, v.Arity)
		for _, t := range v.Tuples() {
			nr.Add(t)
		}
		rels[k] = nr
	}

	// Ensure head relations exist.
	for _, rule := range rules {
		headName := rule.Head.Predicate
		if _, ok := rels[headName]; !ok {
			rels[headName] = NewRelation(headName, len(rule.Head.Args))
		}
	}

	// Iterate to fixpoint.
	for {
		changed := false
		for _, rule := range rules {
			headName := rule.Head.Predicate
			headRel := rels[headName]
			newTuples := Rule(rule, rels)
			for _, t := range newTuples {
				if headRel.Add(t) {
					changed = true
				}
			}
		}
		if !changed {
			break
		}
	}
	return rels
}

// ---------------------------------------------------------------------------
// Generators for random Datalog programs
// ---------------------------------------------------------------------------

// genSchema generates a random set of relation schemas (name → arity).
func genSchema(t *rapid.T) map[string]int {
	nRels := rapid.IntRange(2, 5).Draw(t, "nRels")
	schema := make(map[string]int, nRels)
	for i := 0; i < nRels; i++ {
		name := fmt.Sprintf("r%d", i)
		arity := rapid.IntRange(2, 4).Draw(t, fmt.Sprintf("arity_%s", name))
		schema[name] = arity
	}
	return schema
}

// genFacts generates random initial facts for the given schema.
func genFacts(t *rapid.T, schema map[string]int) map[string]*Relation {
	rels := make(map[string]*Relation)
	for name, arity := range schema {
		rel := NewRelation(name, arity)
		nTuples := rapid.IntRange(5, 20).Draw(t, fmt.Sprintf("ntuples_%s", name))
		for i := 0; i < nTuples; i++ {
			tuple := make(Tuple, arity)
			for j := 0; j < arity; j++ {
				tuple[j] = IntVal{V: int64(rapid.IntRange(0, 9).Draw(t, fmt.Sprintf("val_%s_%d_%d", name, i, j)))}
			}
			rel.Add(tuple)
		}
		rels[name] = rel
	}
	return rels
}

// genRules generates random well-formed Datalog rules over the given schema.
// All generated rules are positive (no negation) and safe (head vars appear in body).
func genRules(t *rapid.T, schema map[string]int) []datalog.Rule {
	relNames := make([]string, 0, len(schema))
	for name := range schema {
		relNames = append(relNames, name)
	}
	sort.Strings(relNames)

	nRules := rapid.IntRange(1, 4).Draw(t, "nRules")
	var rules []datalog.Rule

	for ri := 0; ri < nRules; ri++ {
		// Pick a head relation.
		headIdx := rapid.IntRange(0, len(relNames)-1).Draw(t, fmt.Sprintf("headIdx_%d", ri))
		headName := relNames[headIdx]
		headArity := schema[headName]

		// Generate 1-3 body atoms.
		nBody := rapid.IntRange(1, 3).Draw(t, fmt.Sprintf("nBody_%d", ri))
		var bodyLits []datalog.Literal
		boundVars := make(map[string]bool)
		varPool := []string{"X", "Y", "Z", "W", "V", "U"}

		// Track which vars exist so we can ensure joins.
		for bi := 0; bi < nBody; bi++ {
			bodyRelIdx := rapid.IntRange(0, len(relNames)-1).Draw(t, fmt.Sprintf("bodyRelIdx_%d_%d", ri, bi))
			bodyRelName := relNames[bodyRelIdx]
			bodyArity := schema[bodyRelName]

			args := make([]datalog.Term, bodyArity)
			for ai := 0; ai < bodyArity; ai++ {
				// Use an existing bound var sometimes to create joins.
				if len(boundVars) > 0 && rapid.IntRange(0, 2).Draw(t, fmt.Sprintf("reuse_%d_%d_%d", ri, bi, ai)) == 0 {
					// Pick a random bound var.
					bvList := make([]string, 0, len(boundVars))
					for v := range boundVars {
						bvList = append(bvList, v)
					}
					sort.Strings(bvList)
					idx := rapid.IntRange(0, len(bvList)-1).Draw(t, fmt.Sprintf("bvIdx_%d_%d_%d", ri, bi, ai))
					args[ai] = datalog.Var{Name: bvList[idx]}
				} else {
					// Introduce a new or existing var.
					varIdx := rapid.IntRange(0, len(varPool)-1).Draw(t, fmt.Sprintf("varIdx_%d_%d_%d", ri, bi, ai))
					varName := varPool[varIdx]
					args[ai] = datalog.Var{Name: varName}
					boundVars[varName] = true
				}
			}

			bodyLits = append(bodyLits, datalog.Literal{
				Positive: true,
				Atom:     datalog.Atom{Predicate: bodyRelName, Args: args},
			})
		}

		// Build head args using only bound vars (safety requirement).
		headArgs := make([]datalog.Term, headArity)
		bvList := make([]string, 0, len(boundVars))
		for v := range boundVars {
			bvList = append(bvList, v)
		}
		sort.Strings(bvList)
		for ai := 0; ai < headArity; ai++ {
			idx := rapid.IntRange(0, len(bvList)-1).Draw(t, fmt.Sprintf("headVar_%d_%d", ri, ai))
			headArgs[ai] = datalog.Var{Name: bvList[idx]}
		}

		rules = append(rules, datalog.Rule{
			Head: datalog.Atom{Predicate: headName, Args: headArgs},
			Body: bodyLits,
		})
	}
	return rules
}

// ---------------------------------------------------------------------------
// Helper: run program through the planner and semi-naive evaluator
// ---------------------------------------------------------------------------

func runSemiNaive(rules []datalog.Rule, baseRels map[string]*Relation) (map[string]*Relation, error) {
	prog := &datalog.Program{Rules: rules}
	execPlan, errs := plan.Plan(prog, nil)
	if len(errs) > 0 {
		return nil, errs[0]
	}

	// Evaluate stratum by stratum using the real evaluator.
	allRels := make(map[string]*Relation, len(baseRels))
	for k, v := range baseRels {
		// Deep copy so we don't mutate originals.
		nr := NewRelation(v.Name, v.Arity)
		for _, t := range v.Tuples() {
			nr.Add(t)
		}
		allRels[k] = nr
	}

	_, err := Evaluate(context.Background(), execPlan, allRels)
	if err != nil {
		return nil, err
	}

	// Re-run to get the full relation map (Evaluate only returns query results).
	// We need to replicate the evaluation loop to capture all derived relations.
	allRels2 := make(map[string]*Relation, len(baseRels))
	for k, v := range baseRels {
		nr := NewRelation(v.Name, v.Arity)
		for _, t := range v.Tuples() {
			nr.Add(t)
		}
		allRels2[k] = nr
	}

	for _, stratum := range execPlan.Strata {
		for _, rule := range stratum.Rules {
			headName := rule.Head.Predicate
			if _, ok := allRels2[headName]; !ok {
				allRels2[headName] = NewRelation(headName, len(rule.Head.Args))
			}
		}

		deltaRels := make(map[string]*Relation)
		for _, rule := range stratum.Rules {
			headName := rule.Head.Predicate
			headRel := allRels2[headName]
			newTuples := Rule(rule, allRels2)
			for _, t := range newTuples {
				if headRel.Add(t) {
					dr, ok := deltaRels[headName]
					if !ok {
						dr = NewRelation(headName, headRel.Arity)
						deltaRels[headName] = dr
					}
					dr.Add(t)
				}
			}
		}

		for {
			anyDelta := false
			for _, dr := range deltaRels {
				if dr.Len() > 0 {
					anyDelta = true
					break
				}
			}
			if !anyDelta {
				break
			}

			nextDelta := make(map[string]*Relation)
			for _, rule := range stratum.Rules {
				headName := rule.Head.Predicate
				headRel := allRels2[headName]
				newTuples := RuleDelta(rule, allRels2, deltaRels)
				for _, t := range newTuples {
					if headRel.Add(t) {
						dr, ok := nextDelta[headName]
						if !ok {
							dr = NewRelation(headName, headRel.Arity)
							nextDelta[headName] = dr
						}
						dr.Add(t)
					}
				}
			}
			deltaRels = nextDelta
		}
	}

	return allRels2, nil
}

// relToSortedKeys returns sorted tuple keys for a relation (for comparison).
func relToSortedKeys(r *Relation) []string {
	if r == nil {
		return nil
	}
	keys := make([]string, 0, r.Len())
	for _, t := range r.Tuples() {
		keys = append(keys, tupleKey(t))
	}
	sort.Strings(keys)
	return keys
}

// ---------------------------------------------------------------------------
// Property 1: Semi-naive == Naive (differential oracle)
// ---------------------------------------------------------------------------

func TestPropertySemiNaiveEqualsNaive(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		schema := genSchema(t)
		facts := genFacts(t, schema)
		rules := genRules(t, schema)

		// Plan rules for both evaluators.
		prog := &datalog.Program{Rules: rules}
		execPlan, errs := plan.Plan(prog, nil)
		if len(errs) > 0 {
			t.Skip("invalid program: ", errs[0])
		}

		// Collect all planned rules.
		var plannedRules []plan.PlannedRule
		for _, stratum := range execPlan.Strata {
			plannedRules = append(plannedRules, stratum.Rules...)
		}

		// Run naive.
		naiveRels := naiveEvaluate(plannedRules, facts)

		// Run semi-naive.
		semiRels, err := runSemiNaive(rules, facts)
		if err != nil {
			t.Fatalf("semi-naive evaluation failed: %v", err)
		}

		// Compare all relations present in either result.
		allNames := make(map[string]bool)
		for k := range naiveRels {
			allNames[k] = true
		}
		for k := range semiRels {
			allNames[k] = true
		}

		for name := range allNames {
			naiveKeys := relToSortedKeys(naiveRels[name])
			semiKeys := relToSortedKeys(semiRels[name])

			if len(naiveKeys) != len(semiKeys) {
				t.Errorf("relation %s: naive has %d tuples, semi-naive has %d\nnaive:  %v\nsemi:   %v\nrules:  %s",
					name, len(naiveKeys), len(semiKeys), naiveKeys, semiKeys, prog.String())
				continue
			}
			for i := range naiveKeys {
				if naiveKeys[i] != semiKeys[i] {
					t.Errorf("relation %s: mismatch at tuple %d\nnaive:  %v\nsemi:   %v\nrules:  %s",
						name, i, naiveKeys, semiKeys, prog.String())
					break
				}
			}
		}
	})
}

// ---------------------------------------------------------------------------
// Property 2: Monotonicity — adding facts never removes results
// ---------------------------------------------------------------------------

func TestPropertyMonotonicity(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		schema := genSchema(t)
		facts1 := genFacts(t, schema)
		rules := genRules(t, schema)

		prog := &datalog.Program{Rules: rules}
		execPlan, errs := plan.Plan(prog, nil)
		if len(errs) > 0 {
			t.Skip("invalid program: ", errs[0])
		}

		var plannedRules []plan.PlannedRule
		for _, stratum := range execPlan.Strata {
			plannedRules = append(plannedRules, stratum.Rules...)
		}

		// Evaluate with original facts.
		rels1 := naiveEvaluate(plannedRules, facts1)

		// Create extended facts (superset of facts1).
		facts2 := make(map[string]*Relation)
		for name, rel := range facts1 {
			nr := NewRelation(name, rel.Arity)
			for _, t := range rel.Tuples() {
				nr.Add(t)
			}
			// Add extra random tuples.
			nExtra := rapid.IntRange(1, 10).Draw(t, fmt.Sprintf("nExtra_%s", name))
			for i := 0; i < nExtra; i++ {
				tuple := make(Tuple, rel.Arity)
				for j := 0; j < rel.Arity; j++ {
					tuple[j] = IntVal{V: int64(rapid.IntRange(0, 9).Draw(t, fmt.Sprintf("extra_%s_%d_%d", name, i, j)))}
				}
				nr.Add(tuple)
			}
			facts2[name] = nr
		}

		// Evaluate with extended facts.
		rels2 := naiveEvaluate(plannedRules, facts2)

		// Assert: every tuple in rels1 is also in rels2 (monotonicity).
		for name, rel1 := range rels1 {
			rel2 := rels2[name]
			if rel2 == nil {
				if rel1.Len() > 0 {
					t.Errorf("relation %s: had %d tuples with fewer facts, missing entirely with more facts", name, rel1.Len())
				}
				continue
			}
			for _, tuple := range rel1.Tuples() {
				if !rel2.Contains(tuple) {
					t.Errorf("relation %s: monotonicity violated — tuple %v present with fewer facts but absent with more\nrules: %s",
						name, tuple, prog.String())
				}
			}
		}
	})
}

// ---------------------------------------------------------------------------
// Property 3: Join commutativity — body permutations yield same results
// ---------------------------------------------------------------------------

func TestPropertyJoinCommutativity(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		schema := genSchema(t)
		facts := genFacts(t, schema)

		relNames := make([]string, 0, len(schema))
		for name := range schema {
			relNames = append(relNames, name)
		}
		sort.Strings(relNames)

		// Generate a single rule with 2-3 body atoms.
		nBody := rapid.IntRange(2, 3).Draw(t, "nBody")
		var bodyLits []datalog.Literal
		boundVars := make(map[string]bool)
		varPool := []string{"X", "Y", "Z", "W", "V"}

		for bi := 0; bi < nBody; bi++ {
			bodyRelIdx := rapid.IntRange(0, len(relNames)-1).Draw(t, fmt.Sprintf("bodyRelIdx_%d", bi))
			bodyRelName := relNames[bodyRelIdx]
			bodyArity := schema[bodyRelName]

			args := make([]datalog.Term, bodyArity)
			for ai := 0; ai < bodyArity; ai++ {
				if len(boundVars) > 0 && rapid.IntRange(0, 2).Draw(t, fmt.Sprintf("reuse_%d_%d", bi, ai)) == 0 {
					bvList := make([]string, 0, len(boundVars))
					for v := range boundVars {
						bvList = append(bvList, v)
					}
					sort.Strings(bvList)
					idx := rapid.IntRange(0, len(bvList)-1).Draw(t, fmt.Sprintf("bvIdx_%d_%d", bi, ai))
					args[ai] = datalog.Var{Name: bvList[idx]}
				} else {
					varIdx := rapid.IntRange(0, len(varPool)-1).Draw(t, fmt.Sprintf("varIdx_%d_%d", bi, ai))
					varName := varPool[varIdx]
					args[ai] = datalog.Var{Name: varName}
					boundVars[varName] = true
				}
			}
			bodyLits = append(bodyLits, datalog.Literal{
				Positive: true,
				Atom:     datalog.Atom{Predicate: bodyRelName, Args: args},
			})
		}

		// Head: pick a relation, use bound vars.
		headIdx := rapid.IntRange(0, len(relNames)-1).Draw(t, "headIdx")
		headName := relNames[headIdx]
		headArity := schema[headName]
		bvList := make([]string, 0, len(boundVars))
		for v := range boundVars {
			bvList = append(bvList, v)
		}
		sort.Strings(bvList)
		headArgs := make([]datalog.Term, headArity)
		for ai := 0; ai < headArity; ai++ {
			idx := rapid.IntRange(0, len(bvList)-1).Draw(t, fmt.Sprintf("headVar_%d", ai))
			headArgs[ai] = datalog.Var{Name: bvList[idx]}
		}
		headAtom := datalog.Atom{Predicate: headName, Args: headArgs}

		// Evaluate with original body order.
		rule1 := datalog.Rule{Head: headAtom, Body: bodyLits}
		prog1 := &datalog.Program{Rules: []datalog.Rule{rule1}}
		ep1, errs := plan.Plan(prog1, nil)
		if len(errs) > 0 {
			t.Skip("invalid program: ", errs[0])
		}
		var planned1 []plan.PlannedRule
		for _, s := range ep1.Strata {
			planned1 = append(planned1, s.Rules...)
		}
		rels1 := naiveEvaluate(planned1, facts)

		// Evaluate with reversed body order.
		reversedBody := make([]datalog.Literal, len(bodyLits))
		for i, lit := range bodyLits {
			reversedBody[len(bodyLits)-1-i] = lit
		}
		rule2 := datalog.Rule{Head: headAtom, Body: reversedBody}
		prog2 := &datalog.Program{Rules: []datalog.Rule{rule2}}
		ep2, errs := plan.Plan(prog2, nil)
		if len(errs) > 0 {
			t.Skip("invalid program after permutation: ", errs[0])
		}
		var planned2 []plan.PlannedRule
		for _, s := range ep2.Strata {
			planned2 = append(planned2, s.Rules...)
		}
		rels2 := naiveEvaluate(planned2, facts)

		// Compare the head relation.
		keys1 := relToSortedKeys(rels1[headName])
		keys2 := relToSortedKeys(rels2[headName])

		if len(keys1) != len(keys2) {
			t.Errorf("join commutativity violated for %s: order1 has %d tuples, order2 has %d\nrule1: %s\nrule2: %s",
				headName, len(keys1), len(keys2), prog1.String(), prog2.String())
			return
		}
		for i := range keys1 {
			if keys1[i] != keys2[i] {
				t.Errorf("join commutativity violated for %s at tuple %d\norder1: %v\norder2: %v",
					headName, i, keys1, keys2)
				break
			}
		}
	})
}

// ---------------------------------------------------------------------------
// Property 4: Stratification correctness — negated predicates in lower strata
// ---------------------------------------------------------------------------

func TestPropertyStratificationCorrectness(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a program with known stratification structure.
		// base predicates: b0, b1 (no rules, just facts)
		// derived predicates: d0 depends positively on b0, b1
		// derived predicates: d1 depends negatively on d0 (must be in higher stratum)

		// We generate a chain of dependencies with negation to test stratification.
		nBase := rapid.IntRange(2, 3).Draw(t, "nBase")
		nDerived := rapid.IntRange(1, 3).Draw(t, "nDerived")

		baseNames := make([]string, nBase)
		for i := 0; i < nBase; i++ {
			baseNames[i] = fmt.Sprintf("base%d", i)
		}

		derivedNames := make([]string, nDerived)
		for i := 0; i < nDerived; i++ {
			derivedNames[i] = fmt.Sprintf("derived%d", i)
		}

		var rules []datalog.Rule

		// First derived predicate: positive dependency on base predicates.
		{
			bodyIdx := rapid.IntRange(0, nBase-1).Draw(t, "firstBodyBase")
			bodyName := baseNames[bodyIdx]
			rules = append(rules, datalog.Rule{
				Head: datalog.Atom{
					Predicate: derivedNames[0],
					Args:      []datalog.Term{datalog.Var{Name: "X"}, datalog.Var{Name: "Y"}},
				},
				Body: []datalog.Literal{{
					Positive: true,
					Atom: datalog.Atom{
						Predicate: bodyName,
						Args:      []datalog.Term{datalog.Var{Name: "X"}, datalog.Var{Name: "Y"}},
					},
				}},
			})
		}

		// Additional derived predicates: each depends negatively on the previous one,
		// and positively on a base to ensure safety.
		for i := 1; i < nDerived; i++ {
			bodyBaseIdx := rapid.IntRange(0, nBase-1).Draw(t, fmt.Sprintf("bodyBase_%d", i))
			rules = append(rules, datalog.Rule{
				Head: datalog.Atom{
					Predicate: derivedNames[i],
					Args:      []datalog.Term{datalog.Var{Name: "X"}, datalog.Var{Name: "Y"}},
				},
				Body: []datalog.Literal{
					{
						Positive: true,
						Atom: datalog.Atom{
							Predicate: baseNames[bodyBaseIdx],
							Args:      []datalog.Term{datalog.Var{Name: "X"}, datalog.Var{Name: "Y"}},
						},
					},
					{
						Positive: false, // negation
						Atom: datalog.Atom{
							Predicate: derivedNames[i-1],
							Args:      []datalog.Term{datalog.Var{Name: "X"}, datalog.Var{Name: "Y"}},
						},
					},
				},
			})
		}

		// Plan and check stratification.
		prog := &datalog.Program{Rules: rules}
		execPlan, errs := plan.Plan(prog, nil)
		if len(errs) > 0 {
			t.Skip("unstratifiable program: ", errs[0])
		}

		// Build predicate → stratum index mapping.
		predStratum := make(map[string]int)
		for si, stratum := range execPlan.Strata {
			for _, rule := range stratum.Rules {
				predStratum[rule.Head.Predicate] = si
			}
		}

		// Verify: for every rule with a negated body literal, the negated predicate
		// must be in a strictly earlier stratum (lower index) than the head.
		for _, rule := range rules {
			headName := rule.Head.Predicate
			headStratum, headOk := predStratum[headName]
			if !headOk {
				continue // base predicate, not derived
			}

			for _, lit := range rule.Body {
				if lit.Cmp != nil || lit.Agg != nil {
					continue
				}
				if !lit.Positive {
					negPred := lit.Atom.Predicate
					negStratum, negOk := predStratum[negPred]
					if !negOk {
						// Base predicate — implicitly in stratum -1 (before all derived), fine.
						continue
					}
					if negStratum >= headStratum {
						t.Errorf("stratification error: negated predicate %s (stratum %d) should be in strictly lower stratum than head %s (stratum %d)\nrules: %s",
							negPred, negStratum, headName, headStratum, prog.String())
					}
				}
			}
		}
	})
}
