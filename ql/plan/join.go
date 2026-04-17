package plan

import (
	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
)

const defaultSizeHint = 1000

// tinySeedThreshold is the cardinality threshold (inclusive) below which a
// candidate literal is treated as a "tiny seed" — eligible to win the next
// join slot regardless of normal cost-model scoring.
//
// Rationale (issue #98): the planner's normal scoring picks the lowest
// (-boundCount, sizeHint) tuple. When sizeHints are missing or wrong (the
// failure mode that produced the setState OOM in issue #96), a literal that
// is in fact extremely selective can lose to a literal with a slightly-better
// reported size and end up placed last, producing Cartesian-shaped
// intermediates. The tiny-seed override is a defensive complement to issue
// #88's pre-pass / between-strata refresh: even if the pre-pass misses a
// derived predicate's true size, this catches obviously-tiny literals.
//
// The 32 threshold is generous enough to cover seed predicates (typically
// single-digit tuple counts) while still being well below any plausible
// per-probe blow-up factor.
const tinySeedThreshold = 32

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

// hasConstantArg returns true if any argument of the literal's atom is a
// constant (not a variable or wildcard). Constants on probe-by-key columns
// are strong evidence the literal will produce few output tuples.
func hasConstantArg(lit datalog.Literal) bool {
	if lit.Cmp != nil || lit.Agg != nil {
		return false
	}
	for _, arg := range lit.Atom.Args {
		switch arg.(type) {
		case datalog.IntConst, datalog.StringConst:
			return true
		}
	}
	return false
}

// hasSharedVar returns true if the literal references at least one variable
// in the bound set.
func hasSharedVar(lit datalog.Literal, bound map[string]bool) bool {
	for _, v := range varsInLiteral(lit) {
		if bound[v] {
			return true
		}
	}
	return false
}

// isTinySeed reports whether lit is a "tiny seed" candidate that should win
// the next join slot regardless of its position in normal cost scoring.
//
// A literal qualifies as a tiny seed if BOTH:
//   - We have evidence its expected output is small. Evidence comes in three
//     flavours, in decreasing order of confidence:
//   - (a) sizeHint is known and ≤ tinySeedThreshold;
//   - (b) the literal has at least one constant argument (probing by a
//     specific key gives few results in practice);
//   - (c) it has shared variables with the current bound prefix AND a
//     known sizeHint that is ≤ tinySeedThreshold (the per-probe case).
//
// The strict anti-false-positive case is the one called out in issue #98:
// an IDB with NO sizeHint AND NO shared vars (and no constants) must NOT be
// classified as tiny — we have no evidence at all and would risk picking a
// truly large relation as the seed.
//
// Comparisons, negative literals, and aggregates never qualify (they are
// either filters or have their own placement constraints).
func isTinySeed(lit datalog.Literal, bound map[string]bool, sizeHints map[string]int) bool {
	if lit.Cmp != nil || lit.Agg != nil {
		return false
	}
	if !lit.Positive {
		return false
	}
	relName := lit.Atom.Predicate
	sz, hintKnown := sizeHints[relName]
	if hintKnown && sz > 0 && sz <= tinySeedThreshold {
		// (a) Direct evidence: known and tiny.
		return true
	}
	// Without a known tiny hint, require at least one of:
	//   - a constant argument (evidence the output is point-keyed), or
	//   - a shared bound var (evidence the output is per-probe bounded).
	// Either of these alone is sufficient when the hint is missing entirely
	// — the missing-hint case is the whole defensive point of issue #98.
	if !hintKnown || sz <= 0 {
		if hasConstantArg(lit) {
			return true
		}
		// Shared-var-only with no hint at all: ambiguous. Per the issue's
		// anti-false-positive rule we still need *some* evidence the output
		// is bounded. A shared bound var means probing rather than a full
		// scan; combined with no contradictory size info we treat it as
		// tiny — this is the per-probe case mentioned in the issue.
		if hasSharedVar(lit, bound) {
			return true
		}
		return false
	}
	// Hint known but larger than threshold: only qualify if we have BOTH
	// evidence the access is point-keyed AND the relative size is not huge.
	// We do NOT fire here — large hints should fall through to normal
	// scoring (which already prefers smaller relations).
	return false
}

// orderJoins implements greedy join ordering for a rule body.
//
// Selection rule per slot:
//  1. Among eligible candidates, prefer any "tiny seed" (see isTinySeed)
//     — this is the issue #98 defensive heuristic against missing/wrong
//     sizeHints. Among multiple tiny candidates, the one with the smaller
//     known sizeHint wins (with constants tiebreaking over no-constants).
//  2. Otherwise fall back to normal cost scoring: most-bound-first, then
//     smallest-relation-first.
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

		// Pass 1 — tiny-seed override (issue #98). Pick the smallest tiny
		// candidate by known sizeHint (defaultSizeHint when unknown) so that
		// e.g. a known-7 literal beats an unknown-but-with-constants literal.
		tinyIdx := -1
		tinySize := 0
		for i, lit := range body {
			if placed[i] {
				continue
			}
			if !isEligible(lit, bound) {
				continue
			}
			if !isTinySeed(lit, bound, sizeHints) {
				continue
			}
			sz, ok := sizeHints[lit.Atom.Predicate]
			if !ok || sz <= 0 {
				sz = defaultSizeHint
			}
			if tinyIdx == -1 || sz < tinySize {
				tinyIdx = i
				tinySize = sz
			}
		}
		if tinyIdx != -1 {
			bestIdx = tinyIdx
		} else {
			// Pass 2 — fallback to standard greedy scoring.
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
