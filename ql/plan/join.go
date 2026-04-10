package plan

import (
	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
)

const defaultSizeHint = 1000

// varsInTerm returns variable names referenced by a term.
func varsInTerm(t datalog.Term) []string {
	if v, ok := t.(datalog.Var); ok {
		return []string{v.Name}
	}
	return nil
}

// varsInLiteral returns all variable names referenced in a literal.
func varsInLiteral(lit datalog.Literal) []string {
	var vs []string
	if lit.Cmp != nil {
		vs = append(vs, varsInTerm(lit.Cmp.Left)...)
		vs = append(vs, varsInTerm(lit.Cmp.Right)...)
		return vs
	}
	if lit.Agg != nil {
		// Aggregate result variable is bound after placement.
		if lit.Agg.ResultVar.Name != "" {
			vs = append(vs, lit.Agg.ResultVar.Name)
		}
		return vs
	}
	for _, arg := range lit.Atom.Args {
		vs = append(vs, varsInTerm(arg)...)
	}
	return vs
}

// isEligible returns true if lit can be placed given the currently bound variables.
func isEligible(lit datalog.Literal, bound map[string]bool) bool {
	if lit.Cmp != nil {
		// Comparison is eligible when both operands are bound (or are constants).
		leftVars := varsInTerm(lit.Cmp.Left)
		rightVars := varsInTerm(lit.Cmp.Right)
		for _, v := range leftVars {
			if !bound[v] {
				return false
			}
		}
		for _, v := range rightVars {
			if !bound[v] {
				return false
			}
		}
		return true
	}
	if lit.Agg != nil {
		// Aggregate: eligible when all body literal vars used in the outer rule are bound.
		// For now, aggregates are always eligible (their body is self-contained).
		return true
	}
	if !lit.Positive {
		// Negative literal: eligible only when all its variables are already bound.
		for _, v := range varsInLiteral(lit) {
			if !bound[v] {
				return false
			}
		}
		return true
	}
	return true // positive atoms are always eligible
}

// scoreLiteral returns a score for ordering: lower score = placed earlier.
// We want most-bound-first and smallest relation first.
func scoreLiteral(lit datalog.Literal, bound map[string]bool, sizeHints map[string]int) (boundCount int, size int) {
	vars := varsInLiteral(lit)
	for _, v := range vars {
		if bound[v] {
			boundCount++
		}
	}
	// Higher bound count = better (placed earlier), so negate for "lower = earlier" convention.
	// We return (negBound, size) and pick minimum.
	relName := ""
	if lit.Atom.Predicate != "" {
		relName = lit.Atom.Predicate
	}
	sz, ok := sizeHints[relName]
	if !ok || sz <= 0 {
		sz = defaultSizeHint
	}
	return -boundCount, sz
}

// orderJoins implements greedy join ordering for a rule body.
func orderJoins(body []datalog.Literal, sizeHints map[string]int) []JoinStep {
	if len(body) == 0 {
		return nil
	}

	bound := map[string]bool{}
	placed := make([]bool, len(body))
	steps := make([]JoinStep, 0, len(body))

	for len(steps) < len(body) {
		bestIdx := -1
		bestNegBound := 0
		bestSize := 0

		for i, lit := range body {
			if placed[i] {
				continue
			}
			if !isEligible(lit, bound) {
				continue
			}
			negBound, size := scoreLiteral(lit, bound, sizeHints)
			if bestIdx == -1 || negBound < bestNegBound || (negBound == bestNegBound && size < bestSize) {
				bestIdx = i
				bestNegBound = negBound
				bestSize = size
			}
		}

		if bestIdx == -1 {
			// No eligible literal found — this shouldn't happen for safe rules,
			// but handle gracefully by placing the first unplaced literal.
			for i, p := range placed {
				if !p {
					bestIdx = i
					break
				}
			}
		}

		lit := body[bestIdx]
		placed[bestIdx] = true

		// Determine if this is a filter step (all vars already bound).
		vars := varsInLiteral(lit)
		allBound := true
		for _, v := range vars {
			if !bound[v] {
				allBound = false
				break
			}
		}

		step := JoinStep{
			Literal:  lit,
			IsFilter: allBound && lit.Cmp == nil, // filters are non-comparison steps where all vars are bound
		}

		// Mark newly bound variables.
		for _, v := range vars {
			bound[v] = true
		}

		steps = append(steps, step)
	}

	return steps
}
