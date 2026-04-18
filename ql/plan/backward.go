// Package plan — native backward demand inference (P3a).
//
// P3a closes the "backward binding direction" gap that was historically
// worked around by wrapping dataflow-shaped queries in a BackwardTracker
// bridge class (#121, PR #133). The roadmap's Configuration-surface verdict
// is explicit: Configuration classes are a legible user-facing idiom, but
// they must not be load-bearing for performance — the planner itself should
// figure out binding direction from rule-body shape plus sizeHints.
//
// The core idea: given a rule body, a "demand set" is the set of variables
// whose values are known to be bound before the rule is evaluated. Two
// sources of demand:
//
//  1. Head demand. When rule R's head predicate is always consumed by
//     another rule R' with a constant or small-extent grounding on R's
//     head argument positions, those positions are demand-bound inside R.
//     This propagates the magic-set idea natively into the planner
//     without the magic-set program rewrite: the planner orders R's body
//     as if those head vars were already bound.
//
//  2. Body-internal demand. A small-extent literal (sizeHint <=
//     smallExtentThreshold) that shares a variable with a large-extent
//     literal "grounds" the large literal in the obvious
//     sideways-information-passing sense. This is what the existing
//     tiny-seed heuristic already captures one step ahead; backward
//     inference extends it to multi-hop chains.
//
// The inference is conservative by design: when we cannot prove a
// position is demand-bound, we do not add it to the demand set, and the
// planner falls through to normal greedy scoring. This preserves all
// current plans as a lower bound — P3a can only strictly improve seed
// selection, never regress it.
package plan

import (
	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
)

// SmallExtentThreshold is the upper bound on a predicate's sizeHint for
// it to count as a "small extent" whose presence in a rule body can
// back-propagate demand to other literals. It is deliberately more
// generous than tinySeedThreshold (32) because backward inference
// tolerates a larger seed — the risk of mis-classifying a 500-row
// predicate as "small" is just that we pre-bind variables the planner
// would probably have preferred anyway. The constant mirrors the
// smallIDBThreshold from the roadmap's Phase 3a section.
const SmallExtentThreshold = 5000

// DemandMap records, per predicate name, the argument positions whose
// values are known to be bound at rule-evaluation time because every
// caller of this predicate grounds them. Keys are predicate names;
// values are sorted, deduplicated slices of column indices.
//
// Conservative semantics: a position is in the demand set iff EVERY
// rule body that references this predicate (as a positive atom)
// effectively binds that column — either by a constant, by a shared
// variable with a small-extent literal, or by a variable that was
// demand-bound in the caller's own head. If any caller fails to
// bind, the position drops out of the set ("all-callers" intersect).
// This is the classic magic-set adornment requirement: if we plan R's
// body assuming position K is bound but some caller doesn't bind it,
// we get an incorrect plan. The intersect keeps the inference sound.
type DemandMap map[string][]int

// InferBackwardDemand walks prog's rules and returns the demand map
// that the planner should apply when ordering each rule's body.
//
// Algorithm (fixed-point):
//
//  1. Initialise demand[name] = "all positions" for every IDB head.
//     This is the "maximally bound" starting point — the intersect
//     can only shrink it.
//  2. For each rule R and each positive atom L in R.Body referring to
//     an IDB predicate P, compute which columns of L are bound by R's
//     context (constants, shared vars with tiny-hinted literals,
//     shared vars with R's own demand-bound head args). Intersect
//     demand[P] with that column set.
//  3. Repeat until no demand set changes.
//
// The fixed point exists and converges because each iteration either
// shrinks at least one demand set or terminates. Worst case is
// O(|Rules| * |Preds| * |BodyLits|) passes before convergence, bounded
// by the total number of column positions across all heads.
//
// sizeHints drives the "small extent" classification for
// body-internal sideways passing. Nil hints → every literal is treated
// as unknown-size, which degenerates to "constant-only" grounding.
func InferBackwardDemand(prog *datalog.Program, sizeHints map[string]int) DemandMap {
	if prog == nil || len(prog.Rules) == 0 {
		return DemandMap{}
	}
	if sizeHints == nil {
		sizeHints = map[string]int{}
	}

	// Group IDB head arities. If a predicate is defined with mixed
	// arities (shouldn't happen after desugar, but be defensive), we
	// skip backward inference for it — the name/arity ambiguity from
	// the roadmap's audit-#3 finding would give unsafe results.
	idbArity := map[string]int{}
	mixedArity := map[string]bool{}
	for _, r := range prog.Rules {
		name := r.Head.Predicate
		arity := len(r.Head.Args)
		if existing, seen := idbArity[name]; seen {
			if existing != arity {
				mixedArity[name] = true
			}
		} else {
			idbArity[name] = arity
		}
	}

	// Initialise: every IDB predicate starts "fully demanded" (all
	// columns assumed bound). Intersection shrinks this over time.
	// Sentinel nil slice means "unknown / top" — an unset name in the
	// map means "no caller observed yet", which is distinct from
	// "observed with zero bound cols". We use a sentinel map
	// (initialised[name] = true) to distinguish.
	demand := DemandMap{}
	initialised := map[string]bool{}

	changed := true
	// Bound iterations defensively: each pass only ever shrinks demand
	// sets. With P positions summed across all IDBs, at most P+1
	// passes before we stabilise. Add a buffer just in case of
	// implementation slip.
	maxIter := 1
	for name := range idbArity {
		maxIter += idbArity[name] + 1
	}
	if maxIter < 8 {
		maxIter = 8
	}

	iter := 0
	for changed && iter < maxIter {
		changed = false
		iter++

		// The query body is a first-class caller of any IDB it references.
		// Treating it as such tightens the all-callers intersect: a column
		// the rule callers happen to bind but the query does not is NOT
		// safely demand-bound at planning time. Adversarial-review
		// Finding 1 on PR #143.
		//
		// The query has no head, so headBoundVars is empty. We synthesise
		// a Rule with an empty head and the query body so the same
		// bodyContextGroundedVars / literalBoundCols / intersect path
		// reused below applies uniformly.
		if prog.Query != nil && len(prog.Query.Body) > 0 {
			queryRule := datalog.Rule{
				Head: datalog.Atom{},
				Body: prog.Query.Body,
			}
			queryHeadBound := map[string]bool{}
			ctxBoundVars := bodyContextGroundedVars(queryRule, sizeHints, queryHeadBound)
			for _, lit := range prog.Query.Body {
				if lit.Cmp != nil || lit.Agg != nil || !lit.Positive {
					continue
				}
				pred := lit.Atom.Predicate
				if _, isIDB := idbArity[pred]; !isIDB {
					continue
				}
				if mixedArity[pred] {
					continue
				}
				observed := literalBoundCols(lit, ctxBoundVars)
				if !initialised[pred] {
					demand[pred] = append([]int(nil), observed...)
					initialised[pred] = true
					changed = true
					continue
				}
				prev := demand[pred]
				next := intersectSortedCols(prev, observed)
				if !sameCols(prev, next) {
					demand[pred] = next
					changed = true
				}
			}
		}

		for _, rule := range prog.Rules {
			// Compute which of this rule's own head vars are demand-bound.
			// On the first pass, treat them as unbound (nothing proven
			// yet). On subsequent passes use the current demand map.
			headDemandCols := demand[rule.Head.Predicate]
			headBoundVars := map[string]bool{}
			for _, col := range headDemandCols {
				if col < 0 || col >= len(rule.Head.Args) {
					continue
				}
				if v, ok := rule.Head.Args[col].(datalog.Var); ok && v.Name != "_" {
					headBoundVars[v.Name] = true
				}
			}

			// Walk the body and determine, for each positive-atom literal
			// over an IDB predicate, which of its columns are bound by
			// rule context. Then intersect into demand[pred].
			//
			// "Bound by context" here means: bound by constants in the
			// atom itself, by `var = const` comparisons anywhere in the
			// body, by a shared variable with a known-small positive
			// atom (sizeHint <= SmallExtentThreshold), or by a shared
			// variable with a head arg that is already demand-bound.
			ctxBoundVars := bodyContextGroundedVars(rule, sizeHints, headBoundVars)

			for _, lit := range rule.Body {
				if lit.Cmp != nil || lit.Agg != nil || !lit.Positive {
					continue
				}
				pred := lit.Atom.Predicate
				if _, isIDB := idbArity[pred]; !isIDB {
					continue
				}
				if mixedArity[pred] {
					continue
				}
				observed := literalBoundCols(lit, ctxBoundVars)
				if !initialised[pred] {
					demand[pred] = append([]int(nil), observed...)
					initialised[pred] = true
					// Always flag changed on first initialisation: even if
					// observed is empty, a downstream rule whose body
					// references `pred` in a chain might now see a
					// different head-demand propagation path. Conservative
					// re-iteration beats missing a fixed-point update.
					changed = true
					continue
				}
				prev := demand[pred]
				next := intersectSortedCols(prev, observed)
				if !sameCols(prev, next) {
					demand[pred] = next
					changed = true
				}
			}
		}
	}

	// Filter: a predicate that was never observed as a body atom (only
	// ever defined as a head) has no caller-imposed demand. We drop it
	// from the map so callers can distinguish "no demand" (not in map)
	// from "demand is the empty set" (in map, zero cols) — the latter
	// means "observed, but no column reliably bound", which is still
	// useful signal to tests.
	for name := range idbArity {
		if !initialised[name] {
			delete(demand, name)
		}
	}
	return demand
}

// bodyContextGroundedVars returns the set of variable names that the
// planner can assume are bound when starting to order `rule`'s body.
// Sources:
//   - Head arguments the caller-side demand pass has proven bound
//     (passed in via headBoundVars).
//   - Variables equated to constants by `var = const` comparisons
//     anywhere in the body.
//   - Variables appearing in a positive atom that (a) has a
//     constant-tagged column (existing ground-by-lookup heuristic) or
//     (b) is a known-small extent literal per sizeHints. Small-extent
//     grounding is the body-internal sideways-information-passing
//     case: once we seed on the small literal, every var it mentions
//     is bound before the large literals are probed.
//
// The function is used for two distinct purposes: inside
// InferBackwardDemand (to compute per-body-literal observed bindings
// for intersection) and inside orderJoinsWithDemand (to inject the
// same context into the greedy planner so it doesn't double-count).
// Keeping the logic in one place avoids drift.
func bodyContextGroundedVars(
	rule datalog.Rule,
	sizeHints map[string]int,
	headBoundVars map[string]bool,
) map[string]bool {
	bound := map[string]bool{}
	for v := range headBoundVars {
		bound[v] = true
	}
	// Pass 1: const-equality comparisons.
	for _, lit := range rule.Body {
		if lit.Cmp == nil || lit.Cmp.Op != "=" {
			continue
		}
		if v, ok := lit.Cmp.Left.(datalog.Var); ok && isConstTerm(lit.Cmp.Right) && v.Name != "_" {
			bound[v.Name] = true
		}
		if v, ok := lit.Cmp.Right.(datalog.Var); ok && isConstTerm(lit.Cmp.Left) && v.Name != "_" {
			bound[v.Name] = true
		}
	}
	// Pass 2: small-extent and constant-bearing atoms.
	// We iterate to a fixed point so that "small atom A binds x; atom
	// B sharing x is then treated as probed, binding its other vars if
	// IT is also a small extent" works transitively.
	for {
		progress := false
		for _, lit := range rule.Body {
			if lit.Cmp != nil || lit.Agg != nil || !lit.Positive {
				continue
			}
			// Small extent: every var in the atom becomes bound after
			// seeding.
			if isSmallExtent(lit.Atom.Predicate, sizeHints) {
				for _, arg := range lit.Atom.Args {
					if v, ok := arg.(datalog.Var); ok && v.Name != "_" {
						if !bound[v.Name] {
							bound[v.Name] = true
							progress = true
						}
					}
				}
				continue
			}
			// Constant-bearing atom with at least one shared bound var:
			// the per-probe lookup grounds the remaining vars (existing
			// tiny-seed heuristic, applied here so backward inference
			// sees the same world the planner will).
			if hasConstTerm(lit.Atom.Args) {
				shared := false
				for _, arg := range lit.Atom.Args {
					if v, ok := arg.(datalog.Var); ok && v.Name != "_" && bound[v.Name] {
						shared = true
						break
					}
				}
				if shared {
					for _, arg := range lit.Atom.Args {
						if v, ok := arg.(datalog.Var); ok && v.Name != "_" {
							if !bound[v.Name] {
								bound[v.Name] = true
								progress = true
							}
						}
					}
				}
			}
		}
		if !progress {
			break
		}
	}
	return bound
}

// literalBoundCols returns the sorted, deduplicated list of column
// indices of lit.Atom whose argument is either a constant or a
// variable in ctxBound.
func literalBoundCols(lit datalog.Literal, ctxBound map[string]bool) []int {
	if lit.Atom.Predicate == "" {
		return nil
	}
	seen := map[int]bool{}
	var cols []int
	for i, arg := range lit.Atom.Args {
		switch a := arg.(type) {
		case datalog.IntConst, datalog.StringConst:
			if !seen[i] {
				cols = append(cols, i)
				seen[i] = true
			}
		case datalog.Var:
			if a.Name == "_" {
				continue
			}
			if ctxBound[a.Name] && !seen[i] {
				cols = append(cols, i)
				seen[i] = true
			}
		}
	}
	return sortUniqueInts(cols)
}

// isSmallExtent returns true if sizeHints[pred] is known and within
// the small-extent threshold.
func isSmallExtent(pred string, sizeHints map[string]int) bool {
	sz, ok := sizeHints[pred]
	if !ok {
		return false
	}
	return sz > 0 && sz <= SmallExtentThreshold
}

// intersectSortedCols returns the sorted intersection of two
// sorted-unique integer slices.
func intersectSortedCols(a, b []int) []int {
	if len(a) == 0 || len(b) == 0 {
		return nil
	}
	out := make([]int, 0, min(len(a), len(b)))
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			out = append(out, a[i])
			i++
			j++
		case a[i] < b[j]:
			i++
		default:
			j++
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// sameCols returns true if two sorted-unique column slices are equal.
func sameCols(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sortUniqueInts(xs []int) []int {
	if len(xs) < 2 {
		return xs
	}
	// Insertion sort — small slices, avoid importing sort.
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && xs[j-1] > xs[j]; j-- {
			xs[j-1], xs[j] = xs[j], xs[j-1]
		}
	}
	// Dedupe in place.
	out := xs[:1]
	for i := 1; i < len(xs); i++ {
		if xs[i] != out[len(out)-1] {
			out = append(out, xs[i])
		}
	}
	return out
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// orderJoinsWithDemand is the demand-aware form of orderJoins. It
// pre-binds the variables implied by head-demand and body-context
// grounding before running greedy placement. Callers that don't want
// demand inference pass an empty head and nil demand — the result is
// identical to orderJoins(body, sizeHints).
//
// When `headDemand` names positions of `head` that the caller has
// proven bound, the corresponding variables are added to the initial
// bound set. This biases the greedy scorer toward placing literals
// that share those variables first (boundCount > 0 wins), without
// requiring a magic-set program rewrite.
//
// Safety: we do NOT mark filter steps based on demand-prebinding
// alone. A literal is only a filter if ALL its vars are actually
// bound at evaluation time. Demand-prebinding is a planner hint, not
// a runtime contract — the evaluator's binding map is still driven
// by the actual join order. So we track two sets: `plannerBound`
// (what the scorer sees, includes demand) and `runtimeBound` (what
// the evaluator will actually have, only grown by placed literals).
// IsFilter is set from runtimeBound, preserving evaluator semantics.
func orderJoinsWithDemand(
	head datalog.Atom,
	body []datalog.Literal,
	sizeHints map[string]int,
	headDemand []int,
) []JoinStep {
	if len(body) == 0 {
		return nil
	}

	plannerBound := map[string]bool{}
	runtimeBound := map[string]bool{}
	for _, col := range headDemand {
		if col < 0 || col >= len(head.Args) {
			continue
		}
		if v, ok := head.Args[col].(datalog.Var); ok && v.Name != "_" {
			plannerBound[v.Name] = true
			// NOTE: headDemand vars are NOT added to runtimeBound.
			// At evaluation time they are bound by magic-set seeds or
			// (in the post-P3a world) by the calling rule's join
			// context. The greedy planner uses them for scoring, but
			// the evaluator treats each body-level var as introduced
			// by its first-placed atom. This is consistent with how
			// orderJoins currently handles a purely flat body.
		}
	}

	placed := make([]bool, len(body))
	steps := make([]JoinStep, 0, len(body))

	for len(steps) < len(body) {
		bestIdx := -1
		bestNegBound := 0
		bestSize := 0

		// Eligibility is checked against runtimeBound only: a comparison or
		// negative literal cannot be placed based on demand-prebinding
		// because at evaluation time its vars are not yet bound. Scoring
		// uses plannerBound so demand-prebinding biases which POSITIVE
		// literal wins the slot (boundCount dominance), but never
		// promotes a literal's eligibility.
		//
		// pickTinySeed receives runtimeBound (NOT plannerBound). The
		// "shared bound var" branch of isTinySeed promotes an unhinted
		// IDB to tiny-seed status if it shares a var with the current
		// bound prefix; that promotion would lead the evaluator to
		// full-scan the IDB. Demand-prebound vars are not bound at
		// runtime — only placed-literal vars are — so feeding plannerBound
		// here would let head-demand alone promote a large unhinted IDB
		// to seed and contradict the conservatism the surrounding
		// plannerBound/runtimeBound split exists to enforce. Adversarial
		// review Finding 2 on PR #143. The "head-demand biases scoring"
		// promise is preserved at scoreLiteral below (which keeps
		// plannerBound).
		tinyIdx := pickTinySeed(body, placed, runtimeBound, sizeHints, defaultSizeHint)
		if tinyIdx != -1 && isEligible(body[tinyIdx], runtimeBound) {
			bestIdx = tinyIdx
		} else {
			for i, lit := range body {
				if placed[i] {
					continue
				}
				if !isEligible(lit, runtimeBound) {
					continue
				}
				negBound, size := scoreLiteral(lit, plannerBound, sizeHints)
				if bestIdx == -1 || negBound < bestNegBound || (negBound == bestNegBound && size < bestSize) {
					bestIdx = i
					bestNegBound = negBound
					bestSize = size
				}
			}
		}

		if bestIdx == -1 {
			for i, p := range placed {
				if !p {
					bestIdx = i
					break
				}
			}
		}

		lit := body[bestIdx]
		placed[bestIdx] = true

		vars := varsInLiteral(lit)
		allBound := true
		for _, v := range vars {
			if !runtimeBound[v] {
				allBound = false
				break
			}
		}
		step := JoinStep{
			Literal:  lit,
			IsFilter: allBound && lit.Cmp == nil,
		}
		for _, v := range vars {
			plannerBound[v] = true
			runtimeBound[v] = true
		}
		steps = append(steps, step)
	}
	return steps
}
