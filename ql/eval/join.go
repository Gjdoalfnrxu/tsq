package eval

import (
	"errors"

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
// Returns all result tuples for the rule head. If maxBindings > 0 and the
// intermediate join cardinality exceeds it during evaluation, Rule returns
// a *BindingCapError (wraps ErrBindingCapExceeded) and stops early to
// prevent OOM (issue #80).
func Rule(rule plan.PlannedRule, rels map[string]*Relation, maxBindings int) ([]Tuple, error) {
	initial := []binding{make(binding)}
	limits := &joinLimits{maxBindings: maxBindings, ruleName: rule.Head.Predicate}
	bindings, err := evalJoinSteps(rule.JoinOrder, rels, initial, limits)
	if err != nil {
		return nil, err
	}
	return projectHead(rule.Head, bindings), nil
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
func RuleDelta(rule plan.PlannedRule, rels map[string]*Relation, deltaRels map[string]*Relation, maxBindings int) ([]Tuple, error) {
	seen := make(map[string]struct{})
	var results []Tuple
	limits := &joinLimits{maxBindings: maxBindings, ruleName: rule.Head.Predicate}

	for di, step := range rule.JoinOrder {
		// Only positive atom steps with a delta can be "delta variants".
		if step.Literal.Cmp != nil || step.Literal.Agg != nil {
			continue
		}
		if !step.Literal.Positive {
			continue
		}
		pred := step.Literal.Atom.Predicate
		arity := len(step.Literal.Atom.Args)
		delta, hasDelta := deltaRels[relKey(pred, arity)]
		if !hasDelta || delta.Len() == 0 {
			continue
		}

		// Evaluate the join sequence with delta substitution only at step di.
		initial := []binding{make(binding)}
		bindings, err := evalJoinStepsWithDelta(rule.JoinOrder, rels, deltaRels, di, delta, initial, limits)
		if err != nil {
			return nil, err
		}
		tuples := projectHead(rule.Head, bindings)
		for _, t := range tuples {
			k := tupleKey(t)
			if _, exists := seen[k]; !exists {
				seen[k] = struct{}{}
				results = append(results, t)
			}
		}
	}
	return results, nil
}

// evalJoinSteps processes a sequence of JoinSteps, starting from the given
// bindings, and returns all final bindings. limits may be nil for unlimited.
func evalJoinSteps(steps []plan.JoinStep, rels map[string]*Relation, initial []binding, limits *joinLimits) ([]binding, error) {
	current := initial
	for i, step := range steps {
		if len(current) == 0 {
			return nil, nil
		}
		next, err := applyStep(step, rels, current, limits)
		if err != nil {
			// Annotate with the step index where the cap was hit.
			var bce *BindingCapError
			if errors.As(err, &bce) && bce.StepIndex == 0 {
				bce.StepIndex = i
			}
			return nil, err
		}
		current = next
		if err := limits.check(i, len(current)); err != nil {
			return nil, err
		}
	}
	return current, nil
}

// evalJoinStepsWithDelta processes a sequence of JoinSteps like evalJoinSteps,
// but at step deltaIdx it substitutes the relation for deltaRel (the delta
// relation for that predicate). All other steps use the full relations in rels.
// This ensures only one literal position is delta-substituted per variant,
// which is required for correct semi-naive evaluation.
func evalJoinStepsWithDelta(steps []plan.JoinStep, rels map[string]*Relation, deltaRels map[string]*Relation, deltaIdx int, deltaRel *Relation, initial []binding, limits *joinLimits) ([]binding, error) {
	current := initial
	for i, step := range steps {
		if len(current) == 0 {
			return nil, nil
		}
		var (
			next []binding
			err  error
		)
		if i == deltaIdx {
			// Use the delta relation for this step only.
			// Build a merged map where only this literal's (name, arity)
			// slot is replaced. We must key by arity, not name, so a delta
			// for `Foo/1` does not shadow `Foo/3` and vice versa.
			pred := step.Literal.Atom.Predicate
			arity := len(step.Literal.Atom.Args)
			merged := make(map[string]*Relation, len(rels)+1)
			for k, v := range rels {
				merged[k] = v
			}
			merged[relKey(pred, arity)] = deltaRel
			next, err = applyStep(step, merged, current, limits)
		} else {
			next, err = applyStep(step, rels, current, limits)
		}
		if err != nil {
			var bce *BindingCapError
			if errors.As(err, &bce) && bce.StepIndex == 0 {
				bce.StepIndex = i
			}
			return nil, err
		}
		current = next
		if err := limits.check(i, len(current)); err != nil {
			return nil, err
		}
	}
	return current, nil
}

// applyStep applies a single JoinStep to the current set of bindings.
// limits may be nil for unlimited evaluation.
func applyStep(step plan.JoinStep, rels map[string]*Relation, bindings []binding, limits *joinLimits) ([]binding, error) {
	lit := step.Literal

	// Comparison filter.
	if lit.Cmp != nil {
		return applyComparison(lit.Cmp, bindings), nil
	}

	// Aggregate sub-goal (handled at stratum level; skip here).
	if lit.Agg != nil {
		return bindings, nil
	}

	// Builtin predicate — evaluate procedurally.
	if IsBuiltin(lit.Atom.Predicate) {
		if lit.Positive {
			return ApplyBuiltin(lit.Atom, bindings), nil
		}
		// Negated builtin: keep bindings where the builtin produces no results.
		var out []binding
		for _, b := range bindings {
			result := ApplyBuiltin(lit.Atom, []binding{b})
			if len(result) == 0 {
				out = append(out, b)
			}
		}
		return out, nil
	}

	if lit.Positive {
		return applyPositive(lit.Atom, rels, bindings, limits)
	}
	// Negative (anti-join). Anti-joins only filter (output ≤ input), so they
	// can't grow cardinality past the cap; no limit threading needed.
	return applyNegative(lit.Atom, rels, bindings), nil
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
// The relation lookup is keyed by (predicate, arity) so a 1-arity literal
// like `C(this)` cannot accidentally probe a 3-arity base relation `C`
// of the same name.
func applyPositive(atom datalog.Atom, rels map[string]*Relation, bindings []binding, limits *joinLimits) ([]binding, error) {
	rel, ok := rels[relKey(atom.Predicate, len(atom.Args))]
	if !ok || rel == nil || rel.Len() == 0 {
		return nil, nil
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
			// Index.Lookup keys are canonical (partialKey over sorted cols),
			// and applyPositive builds boundCols in ascending order by
			// iterating atom.Args left-to-right, so a hit here IS a match.
			// The full-equality re-check that used to live here is dead
			// work — see TestPartialKeyCanonicality_* and
			// TestIndexLookupAgreement_* in partialkey_canonicality_test.go.
			//
			// Defensive arity guard only: if the relation contains a tuple
			// shorter than expected, skip rather than panic. Should be
			// impossible given Relation.Add's arity-mismatch panic, but
			// kept as a belt-and-braces check.
			if len(boundCols) > 0 && boundCols[len(boundCols)-1] >= len(t) {
				continue
			}

			// Extend binding with free variables.
			nb := b.clone()
			consistent := true
			for _, fv := range freeVars {
				if fv.col < len(t) {
					if existing, ok := nb[fv.name]; ok {
						// Variable already bound (from earlier column in same atom).
						// Must be equal for the binding to be consistent.
						eq, err := Compare("=", existing, t[fv.col])
						if err != nil || !eq {
							consistent = false
							break
						}
					} else {
						nb[fv.name] = t[fv.col]
					}
				}
			}
			if !consistent {
				continue
			}
			out = append(out, nb)
			// Early cap check inside the inner loop. Without this, a single
			// blown literal can still allocate gigabytes of bindings before
			// the per-step check at the call site fires (issue #80).
			if limits != nil && limits.maxBindings > 0 && len(out) > limits.maxBindings {
				return nil, &BindingCapError{
					Rule:        limits.ruleName,
					Cap:         limits.maxBindings,
					Cardinality: len(out),
				}
			}
		}
	}
	return out, nil
}

// applyNegative filters bindings by requiring NO matching tuple exists (anti-join).
// Lookup is keyed by (predicate, arity) — see applyPositive for the rationale.
func applyNegative(atom datalog.Atom, rels map[string]*Relation, bindings []binding) []binding {
	rel, ok := rels[relKey(atom.Predicate, len(atom.Args))]

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
