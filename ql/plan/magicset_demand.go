// Package plan — demand-driven magic-set augmentation for synthesised
// predicates (the `_disj_N`, `_neg_N` shapes emitted by the desugarer).
//
// Background. The original `WithMagicSetAuto` path infers magic-set
// bindings only from `prog.Query` — it walks the query body, looks for
// constants / equalities / preceding base-relation literals that ground
// IDB-literal arguments, and emits seed rules. It never looks at the
// rule bodies that CALL an IDB. That's fine for a flat query like
// `from Path(1, b)` but blind for the desugared shape:
//
//	Caller(c) :- ClassExt(c), _disj_2(c, _).         // small ClassExt
//	_disj_2(c, y) :- A(c, y).                        // base join branch 1
//	_disj_2(c, y) :- B(c, m), C(m, n), D(n, y).      // 4-atom branch 2
//
// Here `_disj_2` has no query-side binding (the query may just want
// `Caller(c)`). But every CALL site of `_disj_2` binds head column 0
// (via `ClassExt(c)`, a small extent). The native rule-body backward
// inference (`InferBackwardDemand`, P3a) already records this fact in
// its `DemandMap`. P3a uses it only to bias the greedy join planner's
// scoring inside `_disj_2`'s body — it does NOT push that demand into
// the program rewrite, so at evaluation time `_disj_2` still computes
// every tuple of B⋈C⋈D before the cap fires.
//
// This file closes that gap. When the demand map says "_disj_2 column
// 0 is bound at every call site", we synthesise:
//
//  1. A magic-set binding entry `{"_disj_2": [0]}`, fed into
//     `MagicSetTransform` so `_disj_2`'s rules get rewritten to
//     `_disj_2(c, y) :- magic__disj_2(c), <body>`.
//
//  2. A demand-seed rule per caller, of the form
//     `magic__disj_2(c) :- <caller's grounding context for c>.`
//     Without this seed, the rewritten `_disj_2` produces no tuples
//     (the magic predicate would be empty).
//
// The seed body is the caller's preceding/grounding literals, NOT the
// caller's whole body. We need just enough to ground the demanded head
// vars at evaluation time — typically the small-extent atom that gave
// rise to the demand observation in the first place. Including the
// full caller body would risk re-introducing the very cardinality
// blowup we're trying to bound.
//
// Conservative-by-design (mirroring the rest of the magic-set
// machinery): if we cannot construct a safe, side-effect-free seed
// for a demanded predicate, we drop that predicate from the rewrite
// set rather than emit a broken seed. The non-strict
// `WithMagicSetAutoOpts` path still falls back to plain `Plan` on
// any subsequent planning error.

package plan

import (
	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
)

// InferRuleBodyDemandBindings walks `prog`'s rules, computes the
// backward demand map (per `InferBackwardDemand`), and converts it
// into a magic-set bindings map plus the seed rules required to make
// the magic predicates produce tuples at evaluation time from caller
// context.
//
// idbPreds is the set of predicate names eligible for rewriting (rule
// heads). Any demand entry referencing a non-IDB name is skipped.
//
// Returns:
//   - bindings: predicate → bound column positions, suitable for
//     MagicSetTransform. Only includes predicates for which we could
//     construct AT LEAST ONE safe demand seed.
//   - seedRules: the demand-seed rules to append to the augmented
//     program after MagicSetTransform.
//
// When the demand map is empty or no safe seed could be constructed
// for any demanded predicate, returns (nil, nil) — caller should then
// skip the rule-body magic-set augmentation entirely.
func InferRuleBodyDemandBindings(
	prog *datalog.Program,
	idbPreds map[string]bool,
	sizeHints map[string]int,
) (map[string][]int, []datalog.Rule) {
	if prog == nil || len(prog.Rules) == 0 {
		return nil, nil
	}
	demand := InferBackwardDemand(prog, sizeHints)
	if len(demand) == 0 {
		return nil, nil
	}

	bindings := map[string][]int{}
	var seeds []datalog.Rule

	// For each predicate with non-empty demand that's also a rewritable
	// IDB, walk every rule body that calls it and emit a seed per call
	// site. The seed body is the caller's grounding context: those
	// preceding body literals that establish bindings for the demanded
	// head vars.
	for pred, cols := range demand {
		if len(cols) == 0 {
			continue
		}
		if !idbPreds[pred] {
			continue
		}
		// Restrict to synthesised desugar shapes (`_disj_*`, `_neg_*`).
		// These are the predicates the desugarer emits as cardinality-
		// dangerous joins where backward demand is the load-bearing
		// fix. Hand-written IDBs have well-tested join orderings under
		// the existing planner, and rewriting them risks introducing
		// new stratification edges (e.g. recursive negation cycles via
		// taint-style helper predicates) that flip a previously-plain-
		// planned program into "augmented program is unplannable" and
		// fall back to plain Plan with a noisy warning. Conservative
		// scope: synth-name only. Broaden later once measured.
		if !isSynthDesugarName(pred) {
			continue
		}
		// A predicate already covered by query-binding inference is
		// handled by the existing InferQueryBindings path. We only
		// fire here when the demand source is a RULE body. Detect
		// this by checking the query body: if the pred appears with
		// any constant / equality-grounded / base-grounded arg there,
		// skip — the query-binding path will (or already did) emit a
		// seed for it.
		if predHasQueryBinding(prog, pred, cols) {
			continue
		}

		predSeeds := buildDemandSeedsForPred(prog, pred, cols, sizeHints)
		if len(predSeeds) == 0 {
			continue
		}
		bindings[pred] = append([]int(nil), cols...)
		seeds = append(seeds, predSeeds...)
	}

	if len(bindings) == 0 {
		return nil, nil
	}
	return bindings, seeds
}

// predHasQueryBinding returns true iff prog.Query.Body contains a
// positive atom for pred whose `cols` positions are all bound by query
// context (constants, equalities, or preceding base atoms with shared
// vars). When true, the existing InferQueryBindings pipeline will emit
// the seed rules; we don't want to double up.
//
// Conservative: false positives here just mean we DON'T emit a
// demand-seed and let the query-binding path handle it (correct).
// False negatives mean we emit a demand-seed in addition to the
// query-binding seed — also safe, the magic predicate just gets seeded
// from two sources.
func predHasQueryBinding(prog *datalog.Program, pred string, cols []int) bool {
	if prog.Query == nil || len(prog.Query.Body) == 0 {
		return false
	}
	for _, lit := range prog.Query.Body {
		if lit.Cmp != nil || lit.Agg != nil || !lit.Positive {
			continue
		}
		if lit.Atom.Predicate != pred {
			continue
		}
		// Match: the query references pred. Trust InferQueryBindings
		// to produce the seed; we step aside.
		return true
	}
	return false
}

// buildDemandSeedsForPred constructs one demand-seed rule per call
// site of pred in prog.Rules.
//
// For each rule R that has `pred(args...)` as a positive body literal
// at index i:
//   - The seed head is `magic_pred(args[c0], args[c1], ...)` for c
//     in the demand cols.
//   - The seed body is the subset of R.Body[0..i-1] needed to ground
//     the head args. Specifically, we include literals that
//     transitively bind any head-arg variable.
//
// Skip a call site (silently — it just doesn't produce a seed) if:
//   - the head args at the demanded positions aren't variables
//     (constants are already-bound, no seed needed for those args
//     specifically, but we still need the others — for now we skip
//     mixed-shape sites);
//   - the resulting seed wouldn't be safe (head vars not all bound
//     by the constructed body).
//
// This mirrors the structural conservatism of InferQueryBindings.
func buildDemandSeedsForPred(
	prog *datalog.Program,
	pred string,
	cols []int,
	sizeHints map[string]int,
) []datalog.Rule {
	var seeds []datalog.Rule
	magicPred := magicName(pred)

	for _, rule := range prog.Rules {
		// Skip self-recursion on pred — a rule whose head IS pred
		// can't seed itself in a useful way (it would create a magic
		// rule for the recursive case that depends on the very
		// predicate we're trying to bound).
		if rule.Head.Predicate == pred {
			continue
		}
		for i, lit := range rule.Body {
			if lit.Cmp != nil || lit.Agg != nil || !lit.Positive {
				continue
			}
			if lit.Atom.Predicate != pred {
				continue
			}
			if len(lit.Atom.Args) < maxColIndex(cols)+1 {
				continue
			}

			// Build the seed head args: pull from the call-site's atom
			// at the demanded column positions. If a position is a
			// constant, it lifts directly into the magic head as a
			// constant — no body needed for that column. If it's a
			// variable, we need the body to bind it.
			seedHeadArgs := make([]datalog.Term, len(cols))
			demandedVars := map[string]bool{}
			malformed := false
			for k, col := range cols {
				if col < 0 || col >= len(lit.Atom.Args) {
					malformed = true
					break
				}
				arg := lit.Atom.Args[col]
				seedHeadArgs[k] = arg
				if v, ok := arg.(datalog.Var); ok && v.Name != "_" {
					demandedVars[v.Name] = true
				}
			}
			if malformed {
				continue
			}

			// Construct the seed body: caller's preceding literals
			// (j < i), preserving order, but we keep only literals
			// that contribute to grounding the demanded vars. A
			// literal qualifies if it shares a variable with the
			// demanded set OR it is a comparison / small-extent atom
			// that grounds a transitively-shared variable. To keep
			// the algorithm simple and conservative, we include ALL
			// preceding positive atoms over base relations (non-IDB)
			// plus comparisons — these are the cheap, side-effect-
			// free literals the desugarer typically emits as the
			// "guard" before a synth-disj call. Including a preceding
			// IDB atom would risk re-introducing the very cardinality
			// blowup we're trying to bound, so we drop those.
			var seedBody []datalog.Literal
			idb := IDBPredicates(prog)
			for j := 0; j < i; j++ {
				prev := rule.Body[j]
				if prev.Agg != nil {
					continue
				}
				if prev.Cmp != nil {
					seedBody = append(seedBody, prev)
					continue
				}
				if !prev.Positive {
					// Negative literals require all their vars bound;
					// we can include them only if all their vars are
					// already in the bound set built so far. Keep it
					// simple — drop them.
					continue
				}
				// Positive atom: include if it's a base relation
				// (not an IDB head), OR if it's an IDB head whose
				// hint marks it as small-extent (safe to evaluate as
				// part of the seed).
				bodyPred := prev.Atom.Predicate
				if idb[bodyPred] && !isSmallExtent(bodyPred, sizeHints) {
					// Risk re-introducing the blowup; drop.
					continue
				}
				seedBody = append(seedBody, prev)
			}

			seedHead := datalog.Atom{Predicate: magicPred, Args: seedHeadArgs}
			if !isSafe(seedHead, seedBody) {
				continue
			}
			seeds = append(seeds, datalog.Rule{Head: seedHead, Body: seedBody})
		}
	}
	return seeds
}

// isSynthDesugarName returns true for predicate names emitted by
// `ql/desugar.freshSynthName` for the disjunction / negation
// helper-IDB shapes. The desugarer uses fixed prefixes (`_disj_`,
// `_neg_`) for these synth predicates; restricting demand-driven
// magic-set augmentation to those names keeps the scope tight to
// the cardinality-dangerous case (Mastodon `_disj_2`) while
// avoiding accidental rewrites on hand-written IDBs.
func isSynthDesugarName(pred string) bool {
	const disjPrefix = "_disj_"
	const negPrefix = "_neg_"
	return (len(pred) > len(disjPrefix) && pred[:len(disjPrefix)] == disjPrefix) ||
		(len(pred) > len(negPrefix) && pred[:len(negPrefix)] == negPrefix)
}

func maxColIndex(cols []int) int {
	m := 0
	for _, c := range cols {
		if c > m {
			m = c
		}
	}
	return m
}

// MergeBindings merges two magic-set binding maps. When the same
// predicate appears in both with different column sets, the union is
// taken (both call sites' bindings are valid demand evidence; the
// rewrite simply needs to bind ANY of them at runtime). The result
// is a fresh map; inputs are not mutated.
func MergeBindings(a, b map[string][]int) map[string][]int {
	out := map[string][]int{}
	for k, v := range a {
		out[k] = append([]int(nil), v...)
	}
	for k, v := range b {
		if existing, ok := out[k]; ok {
			out[k] = unionSortedCols(existing, v)
		} else {
			out[k] = append([]int(nil), v...)
		}
	}
	return out
}

func unionSortedCols(a, b []int) []int {
	seen := map[int]bool{}
	for _, x := range a {
		seen[x] = true
	}
	for _, x := range b {
		seen[x] = true
	}
	out := make([]int, 0, len(seen))
	for x := range seen {
		out = append(out, x)
	}
	return sortUniqueInts(out)
}
