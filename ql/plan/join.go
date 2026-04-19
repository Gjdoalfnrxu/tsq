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

// allBound reports whether every name in vs is present in bound. The empty
// slice (e.g. a constant term) is trivially bound.
func allBound(vs []string, bound map[string]bool) bool {
	for _, v := range vs {
		if v == "_" {
			// Wildcard names should not appear in comparisons; treat as
			// unbound for safety.
			return false
		}
		if !bound[v] {
			return false
		}
	}
	return true
}

// isVarTerm reports whether t is a non-wildcard Datalog variable.
func isVarTerm(t datalog.Term) bool {
	v, ok := t.(datalog.Var)
	return ok && v.Name != "" && v.Name != "_"
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
		leftVars := varsInTerm(lit.Cmp.Left)
		rightVars := varsInTerm(lit.Cmp.Right)
		leftAllBound := allBound(leftVars, bound)
		rightAllBound := allBound(rightVars, bound)
		if leftAllBound && rightAllBound {
			return true
		}
		// Equality var-var: eligible when at least one side is fully bound;
		// applyComparison will propagate the binding to the other side.
		// See PR #145 catalog item #1 and applyComparison in ql/eval.
		if lit.Cmp.Op == "=" {
			if leftAllBound && isVarTerm(lit.Cmp.Right) {
				return true
			}
			if rightAllBound && isVarTerm(lit.Cmp.Left) {
				return true
			}
		}
		return false
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

// pickTinySeed implements the tiny-seed override (issues #98, #109).
//
// Among unplaced eligible literals that qualify as tiny seeds (per
// isTinySeed), it returns the index of the preferred candidate, or -1 if
// there is no qualifying candidate.
//
// Selection rule:
//  1. A hinted candidate (sizeHints[pred] known and > 0) strictly beats
//     an unhinted candidate, regardless of size. This is the issue #109
//     fix: an unhinted EDB with a discriminative constant arg (which
//     qualifies as tiny via the constant-arg branch of isTinySeed) could
//     in reality be 8M rows. We have weaker evidence about its true size
//     than for a relation with a recorded sizeHint, so we must not let
//     the unhinted candidate beat a hinted-tiny one. On main this is
//     masked by the accident that defaultSizeHint (1000) happens to be
//     larger than every hint that passes the tinySeedThreshold gate; that
//     accidental masking is precisely what would silently regress under a
//     refactor that changed defaultUnhintedSize.
//  2. Among same-hinted-status candidates, the smaller effective size
//     wins. Unhinted candidates use defaultUnhintedSize for tiebreak.
//
// defaultUnhintedSize is parameterised so the regression test for #109
// can drive it directly and demonstrate that the hinted-preference rule
// is encoded explicitly rather than emerging from coincidence.
func pickTinySeed(
	body []datalog.Literal,
	placed []bool,
	bound map[string]bool,
	sizeHints map[string]int,
	defaultUnhintedSize int,
) int {
	tinyIdx := -1
	tinySize := 0
	tinyHinted := false
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
		hinted := ok && sz > 0
		if !hinted {
			sz = defaultUnhintedSize
		}
		better := false
		switch {
		case tinyIdx == -1:
			better = true
		case hinted && !tinyHinted:
			// Hinted strictly beats unhinted regardless of size (#109).
			better = true
		case hinted == tinyHinted && sz < tinySize:
			better = true
		}
		if better {
			tinyIdx = i
			tinySize = sz
			tinyHinted = hinted
		}
	}
	return tinyIdx
}

// computeLiveVars fills in the LiveVars field of every step in `steps`.
//
// For step i, LiveVars is the sorted-deduped set of variable names that:
//   - are bound by step i or some earlier step (so they actually exist
//     in the binding when this step finishes), AND
//   - are referenced by steps[i+1..end] OR finalKeep (the rule head
//     vars, or the query's Select vars).
//
// Restricting to "actually bound by now" makes LiveVars precise — it
// reports the vars the projectBindings call will retain, not a
// superset. Vars not yet bound aren't in the binding to drop.
//
// finalKeep is allowed to be nil — the rule body whose head has no live
// vars (e.g. count-only aggregate) projects to empty after the last
// step. Pass an empty slice to mean "drop everything after the last
// step" explicitly; nil and empty are treated identically.
//
// Algorithm: a single right-to-left pass builds the demand set
// (everything still needed at or after the cut), then a left-to-right
// pass tracks the cumulative bound set and intersects.
func computeLiveVars(steps []JoinStep, finalKeep []string) {
	if len(steps) == 0 {
		return
	}
	n := len(steps)
	// demand[i] = vars referenced by steps[i+1..n-1] union finalKeep.
	demand := make([]map[string]bool, n)
	post := map[string]bool{}
	for _, v := range finalKeep {
		if v != "" && v != "_" {
			post[v] = true
		}
	}
	for i := n - 1; i >= 0; i-- {
		// demand[i] = post (snapshot — vars needed AFTER step i).
		d := make(map[string]bool, len(post))
		for v := range post {
			d[v] = true
		}
		demand[i] = d
		for _, v := range varsInLiteral(steps[i].Literal) {
			if v != "" && v != "_" {
				post[v] = true
			}
		}
	}
	// Left-to-right pass: track bound set, intersect with demand.
	bound := map[string]bool{}
	for i := 0; i < n; i++ {
		for _, v := range varsInLiteral(steps[i].Literal) {
			if v != "" && v != "_" {
				bound[v] = true
			}
		}
		// Allocate non-nil empty slice so "projection enabled, keep
		// nothing" is distinguishable from nil ("legacy, no projection").
		live := make([]string, 0, len(demand[i]))
		for v := range demand[i] {
			if bound[v] {
				live = append(live, v)
			}
		}
		steps[i].LiveVars = sortStrings(live)
	}
}

// sortStrings returns the input slice sorted in place. Small slices, so
// insertion sort avoids importing the sort package and matches the style
// of sortUniqueInts in backward.go.
func sortStrings(xs []string) []string {
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && xs[j-1] > xs[j]; j-- {
			xs[j-1], xs[j] = xs[j], xs[j-1]
		}
	}
	return xs
}

// headVars returns the variable names referenced by an Atom's args, in
// stable order, deduplicated. Used as finalKeep for rule bodies.
func headVars(a datalog.Atom) []string {
	seen := map[string]bool{}
	var out []string
	for _, arg := range a.Args {
		if v, ok := arg.(datalog.Var); ok && v.Name != "_" && !seen[v.Name] {
			seen[v.Name] = true
			out = append(out, v.Name)
		}
	}
	return out
}

// selectVars returns the variable names referenced by query Select terms.
func selectVars(sel []datalog.Term) []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range sel {
		if v, ok := t.(datalog.Var); ok && v.Name != "_" && !seen[v.Name] {
			seen[v.Name] = true
			out = append(out, v.Name)
		}
	}
	return out
}

// magicPredPrefix is the predicate-name prefix used by MagicSetTransform
// for synthesised seed/propagation predicates. Anchoring literals with
// this prefix to slot 0 (see pickMagicAnchor) is the disj2-round6 fix
// for the unseeded-propagation cap-hit on `magic__disj_28` —
// magic_<pred> literals are the seed-driven demand source and must
// drive the join, never act as a tail-end filter against a base-only
// cross product.
const magicPredPrefix = "magic_"

// isMagicLiteral reports whether lit is a positive atom whose predicate
// name carries the `magic_` prefix. Comparisons, aggregates, and
// negative literals are excluded — only positive atom drivers qualify
// for the round-6 anchor.
func isMagicLiteral(lit datalog.Literal) bool {
	if !lit.Positive || lit.Cmp != nil || lit.Agg != nil {
		return false
	}
	p := lit.Atom.Predicate
	return len(p) > len(magicPredPrefix) && p[:len(magicPredPrefix)] == magicPredPrefix
}

// pickMagicAnchor returns the index of the next unplaced positive
// literal whose predicate is a magic-set seed predicate (name prefixed
// with `magic_`), or -1 if none qualifies. It runs BEFORE the
// tiny-seed override and the standard greedy scorer in
// orderJoins/orderJoinsWithDemandAndIDB. See `magicPredPrefix` for the
// full rationale.
//
// Selection rule among multiple magic literals: the FIRST unplaced one
// in body order wins. This is body-order-stable (matters for tests
// that pin plans) and is the order MagicSetTransform itself uses when
// constructing rewritten bodies (magic head-prereq first, then
// preceding original body lits).
//
// Eligibility: positive atoms are always eligible (isEligible returns
// true unconditionally), so this function only consults `placed`.
func pickMagicAnchor(body []datalog.Literal, placed []bool) int {
	for i, lit := range body {
		if placed[i] {
			continue
		}
		if isMagicLiteral(lit) {
			return i
		}
	}
	return -1
}

// orderJoins implements greedy join ordering for a rule body.
//
// Selection rule per slot:
//  1. Magic-anchor (disj2-round6): any unplaced `magic_<pred>` positive
//     literal wins immediately. See pickMagicAnchor / magicPredPrefix.
//  2. Otherwise, prefer any "tiny seed" (see isTinySeed) — issue #98
//     defensive heuristic against missing/wrong sizeHints. Among
//     multiple tiny candidates, the one with the smaller known
//     sizeHint wins (with constants tiebreaking over no-constants).
//  3. Otherwise fall back to normal cost scoring: most-bound-first,
//     then smallest-relation-first.
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

		// Pass 0 — magic-anchor override (disj2-round6).
		if magicIdx := pickMagicAnchor(body, placed); magicIdx != -1 {
			bestIdx = magicIdx
		} else if tinyIdx := pickTinySeed(body, placed, bound, sizeHints, defaultSizeHint); tinyIdx != -1 {
			// Pass 1 — tiny-seed override (issue #98 + #109). See pickTinySeed.
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
