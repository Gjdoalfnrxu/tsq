package eval

import (
	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

// binding is a partial assignment of Datalog variable names to Values.
type binding map[string]Value

// clone makes a shallow copy of a binding.
func (b binding) clone() binding {
	nb := make(binding, len(b))
	for k, v := range b {
		nb[k] = v
	}
	return nb
}

// lookupTerm resolves a Term against current bindings.
// Returns (value, true) if fully resolved, or (nil, false) if the term is an
// unbound variable.
func lookupTerm(t datalog.Term, b binding) (Value, bool) {
	switch tv := t.(type) {
	case datalog.Var:
		if tv.Name == "_" {
			// Wildcard — always unbound (caller handles)
			return nil, false
		}
		v, ok := b[tv.Name]
		return v, ok
	case datalog.IntConst:
		return IntVal{V: tv.Value}, true
	case datalog.StringConst:
		return StrVal{V: tv.Value}, true
	case datalog.Wildcard:
		return nil, false
	default:
		return nil, false
	}
}

// Rule evaluates a single PlannedRule against the given relations.
// Returns all result tuples for the rule head.
func Rule(rule plan.PlannedRule, rels map[string]*Relation) []Tuple {
	initial := []binding{make(binding)}
	bindings := evalJoinSteps(rule.JoinOrder, rels, initial)
	return projectHead(rule.Head, bindings)
}

// RuleDelta evaluates a rule in semi-naive mode.
// It generates one variant per body literal, substituting ONLY that literal's
// position with the delta relation (new tuples only). Results are unioned
// and returned.
//
// The substitution is position-aware: for a rule like
//
//	Path(x,z) :- Path(x,y), Path(y,z)
//
// when di=0, only the first Path literal uses delta; the second uses full.
// When di=1, only the second Path literal uses delta; the first uses full.
// This avoids the delta×delta over-counting that a global predicate replacement
// would produce.
func RuleDelta(rule plan.PlannedRule, rels map[string]*Relation, deltaRels map[string]*Relation) []Tuple {
	seen := make(map[string]struct{})
	var results []Tuple

	for di, step := range rule.JoinOrder {
		// Only positive atom steps with a delta can be "delta variants".
		if step.Literal.Cmp != nil || step.Literal.Agg != nil {
			continue
		}
		if !step.Literal.Positive {
			continue
		}
		pred := step.Literal.Atom.Predicate
		delta, hasDelta := deltaRels[pred]
		if !hasDelta || delta.Len() == 0 {
			continue
		}

		// Evaluate the join sequence with delta substitution only at step di.
		initial := []binding{make(binding)}
		bindings := evalJoinStepsWithDelta(rule.JoinOrder, rels, deltaRels, di, delta, initial)
		tuples := projectHead(rule.Head, bindings)
		for _, t := range tuples {
			k := tupleKey(t)
			if _, exists := seen[k]; !exists {
				seen[k] = struct{}{}
				results = append(results, t)
			}
		}
	}
	return results
}

// evalJoinSteps processes a sequence of JoinSteps, starting from the given
// bindings, and returns all final bindings.
func evalJoinSteps(steps []plan.JoinStep, rels map[string]*Relation, initial []binding) []binding {
	current := initial
	for _, step := range steps {
		if len(current) == 0 {
			return nil
		}
		current = applyStep(step, rels, current)
	}
	return current
}

// evalJoinStepsWithDelta processes a sequence of JoinSteps like evalJoinSteps,
// but at step deltaIdx it substitutes the relation for deltaRel (the delta
// relation for that predicate). All other steps use the full relations in rels.
// This ensures only one literal position is delta-substituted per variant,
// which is required for correct semi-naive evaluation.
func evalJoinStepsWithDelta(steps []plan.JoinStep, rels map[string]*Relation, deltaRels map[string]*Relation, deltaIdx int, deltaRel *Relation, initial []binding) []binding {
	current := initial
	for i, step := range steps {
		if len(current) == 0 {
			return nil
		}
		if i == deltaIdx {
			// Use the delta relation for this step only.
			// Build a merged map where only this literal's predicate is replaced.
			pred := step.Literal.Atom.Predicate
			merged := make(map[string]*Relation, len(rels)+1)
			for k, v := range rels {
				merged[k] = v
			}
			merged[pred] = deltaRel
			current = applyStep(step, merged, current)
		} else {
			current = applyStep(step, rels, current)
		}
	}
	return current
}

// applyStep applies a single JoinStep to the current set of bindings.
func applyStep(step plan.JoinStep, rels map[string]*Relation, bindings []binding) []binding {
	lit := step.Literal

	// Comparison filter.
	if lit.Cmp != nil {
		return applyComparison(lit.Cmp, bindings)
	}

	// Aggregate sub-goal (handled at stratum level; skip here).
	if lit.Agg != nil {
		return bindings
	}

	if lit.Positive {
		return applyPositive(lit.Atom, rels, bindings)
	}
	// Negative (anti-join).
	return applyNegative(lit.Atom, rels, bindings)
}

// applyComparison filters bindings by evaluating the comparison against each.
func applyComparison(cmp *datalog.Comparison, bindings []binding) []binding {
	var out []binding
	for _, b := range bindings {
		lv, lok := lookupTerm(cmp.Left, b)
		rv, rok := lookupTerm(cmp.Right, b)
		if !lok || !rok {
			// Unbound variable in comparison — skip (shouldn't happen with valid plans).
			continue
		}
		ok, err := Compare(cmp.Op, lv, rv)
		if err == nil && ok {
			out = append(out, b)
		}
	}
	return out
}

// applyPositive extends bindings by probing the named relation.
func applyPositive(atom datalog.Atom, rels map[string]*Relation, bindings []binding) []binding {
	rel, ok := rels[atom.Predicate]
	if !ok || rel == nil || rel.Len() == 0 {
		return nil
	}

	var out []binding
	for _, b := range bindings {
		// Determine which columns are bound and which are free variables.
		boundCols := make([]int, 0, len(atom.Args))
		boundVals := make([]Value, 0, len(atom.Args))
		freeVars := make([]struct {
			name string
			col  int
		}, 0, len(atom.Args))

		for i, arg := range atom.Args {
			if v, ok := lookupTerm(arg, b); ok {
				boundCols = append(boundCols, i)
				boundVals = append(boundVals, v)
			} else if vv, isVar := arg.(datalog.Var); isVar && vv.Name != "_" {
				freeVars = append(freeVars, struct {
					name string
					col  int
				}{vv.Name, i})
			}
			// Wildcards are ignored.
		}

		// Use index if we have bound columns.
		var matchingIdxs []int
		if len(boundCols) > 0 {
			idx := rel.Index(boundCols)
			matchingIdxs = idx.Lookup(boundVals)
		} else {
			// Full scan.
			matchingIdxs = make([]int, len(rel.Tuples()))
			for i := range rel.Tuples() {
				matchingIdxs[i] = i
			}
		}

		tuples := rel.Tuples()
		for _, ti := range matchingIdxs {
			t := tuples[ti]
			// Verify bound columns match (index lookup already filters, but
			// for multi-col with partial hash we re-check).
			match := true
			for j, col := range boundCols {
				if col >= len(t) {
					match = false
					break
				}
				eq, err := Compare("=", t[col], boundVals[j])
				if err != nil || !eq {
					match = false
					break
				}
			}
			if !match {
				continue
			}

			// Extend binding with free variables.
			nb := b.clone()
			for _, fv := range freeVars {
				if fv.col < len(t) {
					nb[fv.name] = t[fv.col]
				}
			}
			out = append(out, nb)
		}
	}
	return out
}

// applyNegative filters bindings by requiring NO matching tuple exists (anti-join).
func applyNegative(atom datalog.Atom, rels map[string]*Relation, bindings []binding) []binding {
	rel, ok := rels[atom.Predicate]

	var out []binding
	for _, b := range bindings {
		if !ok || rel == nil || rel.Len() == 0 {
			// No relation = no matching tuples = anti-join succeeds.
			out = append(out, b)
			continue
		}

		// Determine bound columns.
		boundCols := make([]int, 0, len(atom.Args))
		boundVals := make([]Value, 0, len(atom.Args))
		for i, arg := range atom.Args {
			if v, ok := lookupTerm(arg, b); ok {
				boundCols = append(boundCols, i)
				boundVals = append(boundVals, v)
			}
		}

		var matchingIdxs []int
		if len(boundCols) > 0 {
			idx := rel.Index(boundCols)
			matchingIdxs = idx.Lookup(boundVals)
		} else {
			// No bound columns — any tuple would match.
			if rel.Len() > 0 {
				// Match found → anti-join fails.
				continue
			}
			out = append(out, b)
			continue
		}

		// Verify matches exist.
		hasMatch := false
		tuples := rel.Tuples()
		for _, ti := range matchingIdxs {
			t := tuples[ti]
			match := true
			for j, col := range boundCols {
				if col >= len(t) {
					match = false
					break
				}
				eq, err := Compare("=", t[col], boundVals[j])
				if err != nil || !eq {
					match = false
					break
				}
			}
			if match {
				hasMatch = true
				break
			}
		}

		if !hasMatch {
			out = append(out, b)
		}
	}
	return out
}

// projectHead builds head tuples from fully-resolved bindings.
func projectHead(head datalog.Atom, bindings []binding) []Tuple {
	var out []Tuple
	for _, b := range bindings {
		t := make(Tuple, len(head.Args))
		valid := true
		for i, arg := range head.Args {
			v, ok := lookupTerm(arg, b)
			if !ok {
				// Unbound head variable — this binding is incomplete.
				valid = false
				break
			}
			t[i] = v
		}
		if valid {
			out = append(out, t)
		}
	}
	return out
}
