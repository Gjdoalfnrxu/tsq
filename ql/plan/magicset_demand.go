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
	return InferRuleBodyDemandBindingsWithClassExtents(prog, idbPreds, sizeHints, nil)
}

// InferRuleBodyDemandBindingsWithClassExtents is the disj2-round3
// entry point. classExtentNames carries the set of materialised
// class-extent base predicate names — those names are treated as
// grounders for any var they bind, regardless of size hint, when
// computing backward demand and constructing the magic-set seed
// bodies. nil classExtentNames degrades to the
// InferRuleBodyDemandBindings behaviour.
func InferRuleBodyDemandBindingsWithClassExtents(
	prog *datalog.Program,
	idbPreds map[string]bool,
	sizeHints map[string]int,
	classExtentNames map[string]bool,
) (map[string][]int, []datalog.Rule) {
	return inferRuleBodyDemandBindings(prog, idbPreds, sizeHints, classExtentNames, nil)
}

// InferRuleBodyDemandBindingsWithStats is the Phase B PR4 entry point.
// Identical to InferRuleBodyDemandBindingsWithClassExtents but
// additionally consults an EDB statistics sidecar lookup, threaded
// through to the underlying backward-demand inference for low-fanout
// grounding. nil lookup degrades to the WithClassExtents behaviour
// byte-identically.
func InferRuleBodyDemandBindingsWithStats(
	prog *datalog.Program,
	idbPreds map[string]bool,
	sizeHints map[string]int,
	classExtentNames map[string]bool,
	lookup StatsLookup,
) (map[string][]int, []datalog.Rule) {
	return inferRuleBodyDemandBindings(prog, idbPreds, sizeHints, classExtentNames, lookup)
}

func inferRuleBodyDemandBindings(
	prog *datalog.Program,
	idbPreds map[string]bool,
	sizeHints map[string]int,
	classExtentNames map[string]bool,
	lookup StatsLookup,
) (map[string][]int, []datalog.Rule) {
	if prog == nil || len(prog.Rules) == 0 {
		return nil, nil
	}
	demand := inferBackwardDemand(prog, sizeHints, classExtentNames, lookup)
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

		predSeeds, parentBindings, parentSeeds := buildDemandSeedsForPredWithParents(prog, pred, cols, sizeHints, demand)
		if len(predSeeds) == 0 && len(parentSeeds) == 0 {
			continue
		}
		bindings[pred] = append([]int(nil), cols...)
		seeds = append(seeds, predSeeds...)
		// Fold in any parent-pred bindings/seeds discovered while
		// chasing demand back through pure-rename trampolines (the
		// `functionContainsStar(fn,node) :- _disj_2(fn,node)` shape
		// from Mastodon). Without this, a synth pred whose only
		// caller is a rename rule has no preceding grounding context
		// to construct a safe seed body, so the rewrite drops on the
		// floor — the bug PR #149 originally aimed to fix but missed
		// (run_005 still cap-hit identically).
		for parentPred, parentCols := range parentBindings {
			if existing, ok := bindings[parentPred]; ok {
				bindings[parentPred] = unionSortedCols(existing, parentCols)
			} else {
				bindings[parentPred] = append([]int(nil), parentCols...)
			}
		}
		seeds = append(seeds, parentSeeds...)
	}

	if len(bindings) == 0 {
		return nil, nil
	}
	return bindings, seeds
}

// predHasQueryBinding returns true iff prog.Query.Body contains a
// positive atom for pred whose `cols` positions are ALL bound by query
// context (constants, equalities, or preceding base atoms with shared
// vars). When true, the existing InferQueryBindings pipeline will emit
// the seed rules; we don't want to double up.
//
// Adversarial-review F1 on PR #149: the previous version returned true
// on any positive occurrence of pred regardless of binding. That makes
// the demand-seed path silently bail for queries like `_disj_2(c, y)`
// where neither var is grounded by query context — InferQueryBindings
// also produces no seed, and the magic rewrite drops on the floor. We
// must verify cols ⊆ query-context-bound-cols at the occurrence; only
// then can we trust the query-binding pipeline to handle it.
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
	queryRule := datalog.Rule{Head: datalog.Atom{}, Body: prog.Query.Body}
	// nil lookup is deliberate (Phase B PR4): this function gates
	// whether to skip emitting a demand-seed in favour of the
	// InferQueryBindings pipeline. InferQueryBindings does NOT consult
	// the stats sidecar — it grounds only via constants, equalities,
	// and preceding base atoms with shared vars. If we passed a
	// non-nil lookup here, the stats-driven grounding could mark a
	// pred as "query-bound", we'd skip emitting the demand-seed, and
	// InferQueryBindings would also produce no seed (it can't see the
	// stats grounding), leaving the magic predicate seedless and
	// triggering silent fallback to plain Plan. Keep the contracts
	// aligned by mirroring InferQueryBindings's grounding view here.
	ctxBoundVars := bodyContextGroundedVars(queryRule, nil, map[string]bool{}, arity1BaseGroundedIDBs(prog), nil, nil)
	for _, lit := range prog.Query.Body {
		if lit.Cmp != nil || lit.Agg != nil || !lit.Positive {
			continue
		}
		if lit.Atom.Predicate != pred {
			continue
		}
		// Match: the query references pred. Verify the demanded cols
		// are actually grounded at this occurrence before stepping
		// aside for InferQueryBindings.
		boundCols := literalBoundCols(lit, ctxBoundVars)
		if containsAllInts(boundCols, cols) {
			return true
		}
	}
	return false
}

// containsAllInts returns true iff every element of need appears in
// haystack. haystack is assumed sorted-unique; need need not be.
func containsAllInts(haystack, need []int) bool {
	if len(need) == 0 {
		return true
	}
	set := make(map[int]bool, len(haystack))
	for _, v := range haystack {
		set[v] = true
	}
	for _, v := range need {
		if !set[v] {
			return false
		}
	}
	return true
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
	// Hoisted out of the per-literal hot loop below: the (name, arity) IDB
	// head index is a function of `prog` only and was previously rebuilt
	// for every preceding-literal scan. Compute once per call.
	idbByArity := idbHeadByArity(prog)

	// disj2-round5: arity-keyed call-site match. The desugarer's
	// auto-emitted arity-1 class-extent helpers (PR #146) — e.g.
	// `VarDecl(this) :- VarDecl(this, _, _, _).` — register a NEW
	// IDB head named "VarDecl" at arity 1. The underlying base
	// relation `VarDecl/4` keeps the same name. When backward demand
	// is inferred for the arity-1 IDB head (`demand[VarDecl] = [0]`
	// from arity-1 grounding callers), this loop must NOT match the
	// arity-4 base usages of the same name as call sites — those are
	// not calls to the IDB.
	//
	// Concrete failure mode (Mastodon `setStateUpdaterCallsOtherSetStateThroughContext`):
	// without arity disambiguation, a body literal like
	// `VarDecl(_, sym, srcExpr, _)` (arity 4, position 0 wildcard) was
	// matched as a "call" to `VarDecl/1` and produced the seed
	// `magic_VarDecl(_) :- ...` with a wildcard head var. `isSafe` lets
	// the wildcard through but `validate.ValidateRule` rejects it
	// ("head variable \"_\" does not appear in any positive body literal"),
	// causing `WithMagicSetAutoOpts` to fall back to plain Plan. With
	// no magic-set rewrite, every IDB in the program runs to completion
	// against the full base — including unrelated siblings like
	// `setStateUpdaterCallsFn` — and one of them caps out.
	//
	// Same family as PR #156's preceding-literal IDB-shadow fix; that
	// PR keyed the body-IDB lookup by (name, arity), but the call-site
	// match itself remained name-only. This restricts the call-site
	// match to literals whose arity equals the IDB head arity.
	predArities := map[int]bool{}
	for _, r := range prog.Rules {
		if r.Head.Predicate == pred {
			predArities[len(r.Head.Args)] = true
		}
	}

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
			// Arity-keyed call-site match (round-5). If the program
			// has any IDB head for `pred`, only literals at one of
			// those arities are call sites; literals at other arities
			// are base-relation usages of a colliding name.
			if len(predArities) > 0 && !predArities[len(lit.Atom.Args)] {
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
			// disj2-round5 belt-and-braces: skip the call site if any
			// demanded position is a wildcard at the call. `isSafe`'s
			// `_`-exemption (magicset.go:isSafe) lets a wildcard head
			// arg through, but `validate.ValidateRule` (validate.go)
			// rejects it as an unbound head var. The transformed
			// program then fails to plan and `WithMagicSetAutoOpts`
			// silently falls back to plain Plan with a warning. Drop
			// the seed up front rather than emit one we know will be
			// rejected downstream.
			wildcardAtDemandedPos := false
			for _, arg := range seedHeadArgs {
				if v, ok := arg.(datalog.Var); ok && v.Name == "_" {
					wildcardAtDemandedPos = true
					break
				}
			}
			if wildcardAtDemandedPos {
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
			// Build an IDB head index keyed by (name, arity). Name-only
			// keying conflates the desugarer's auto-emitted arity-1
			// class-extent helpers (e.g. `CallArg(this) :- CallArg(this,_,_).`,
			// emitted for any `@callarg`-typed parameter under PR #146) with
			// the underlying arity-N base relation of the same name. Without
			// arity disambiguation, a preceding `CallArg(c, 0, argFn)` (the
			// arity-3 base atom) gets dropped here as "an unhinted IDB,"
			// silently zeroing out the only literal that would have grounded
			// `argFn` in the magic-set seed body. Result: isSafe rejects the
			// seed (head var not body-bound), no seed is emitted, the synth-
			// disj rewrite drops on the floor, and `_disj_2` blows the
			// binding cap on its 5-atom join.
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
				// (no IDB rule head matches BOTH name AND arity), OR
				// if it IS an IDB head at the matching arity but the
				// hint marks it small-extent (safe to evaluate inside
				// the seed). Arity-keyed: see comment above.
				bodyPred := prev.Atom.Predicate
				bodyArity := len(prev.Atom.Args)
				if idbByArity[predArity{bodyPred, bodyArity}] && !isSmallExtent(bodyPred, sizeHints) {
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

// buildDemandSeedsForPredWithParents extends buildDemandSeedsForPred
// to traverse pure-rename trampoline rules upward when a direct seed
// cannot be safely constructed at the call site.
//
// Motivation (Mastodon `_disj_2` / PR #149 follow-up). The desugarer
// can emit a chain like:
//
//	functionContainsStar(fn, node) :- FunctionContains(fn, node).
//	functionContainsStar(fn, node) :- _disj_2(fn, node).
//
//	_disj_2(fn, node) :- ...big base join...
//
// `_disj_2`'s only call site is the rename rule, which has zero
// preceding literals. `buildDemandSeedsForPred` therefore can't ground
// the demanded `fn` and emits no seeds — `_disj_2` stays unbound at
// runtime and blows the binding cap.
//
// The grounding `fn` actually exists at the GRAND-caller of
// `functionContainsStar`, e.g. `setStateUpdaterCallsFn` whose body has
// `isUseStateSetterCall(c) and CallArg(c,0,argFn) and
// functionContainsStar(argFn, innerCall)`. P3a's `InferBackwardDemand`
// already records this — `demand[functionContainsStar] = [0]`. We
// just have to lift it into the magic-set bindings.
//
// Algorithm (single hop only — extend later if needed):
//
//  1. Direct: try `buildDemandSeedsForPred(prog, pred, cols, ...)`.
//     If it returns at least one seed, we're done — return it with
//     empty parent maps.
//  2. Rename traversal: for each call site of `pred` that is a "pure
//     rename" (rule whose body is exactly the single positive
//     `pred(...)` literal, and whose head shares variables with that
//     literal at the demanded positions), the rule's HEAD is the
//     parent. Look up `demand[parent]`. If it covers the renamed
//     positions, mark the parent for magic-set inclusion (bindings)
//     and emit seeds for `magic_<parent>` from ITS callers using the
//     same `buildDemandSeedsForPred` logic — but keyed on the parent
//     name so the magic-set transform's `propagateBindings` chains
//     `magic_<parent>` → `magic_<pred>` automatically.
//
// Returning parents in a separate map (rather than recursing into
// `InferRuleBodyDemandBindings`) keeps the scope-and-shape filtering
// (synth-only) untouched — parents are typically NON-synth IDBs like
// `functionContainsStar`, which the synth-only filter would otherwise
// reject.
//
// Bound on traversal depth: 1 hop. Multi-hop rename chains are rare
// in practice (the desugarer doesn't synthesise them) and a single
// hop covers the Mastodon shape. Generalising to N-hops would require
// cycle detection and is left for a follow-up if measurement shows
// it matters.
func buildDemandSeedsForPredWithParents(
	prog *datalog.Program,
	pred string,
	cols []int,
	sizeHints map[string]int,
	demand DemandMap,
) (predSeeds []datalog.Rule, parentBindings map[string][]int, parentSeeds []datalog.Rule) {
	predSeeds = buildDemandSeedsForPred(prog, pred, cols, sizeHints)
	if len(predSeeds) > 0 {
		// Direct seeds already cover the demanded positions; no need
		// to chase parents.
		return predSeeds, nil, nil
	}

	parentBindings = map[string][]int{}
	seenParent := map[string]bool{}

	for _, rule := range prog.Rules {
		if rule.Head.Predicate == pred {
			continue
		}
		// A rule qualifies as a "rename trampoline" for pred iff its
		// body is a single positive atom over pred. Comparisons or
		// extra literals disqualify — those would require more
		// careful per-position grounding analysis than we attempt
		// here.
		if len(rule.Body) != 1 {
			continue
		}
		only := rule.Body[0]
		if only.Cmp != nil || only.Agg != nil || !only.Positive {
			continue
		}
		if only.Atom.Predicate != pred {
			continue
		}
		if len(only.Atom.Args) < maxColIndex(cols)+1 {
			continue
		}

		// Map the demanded body-atom column positions to head
		// positions via variable correspondence. If body arg
		// `cols[k]` is a var that also appears in the head at
		// position `hPos`, then demand on body col cols[k] lifts to
		// demand on head col hPos.
		var headCols []int
		mappedAll := true
		for _, col := range cols {
			arg := only.Atom.Args[col]
			v, ok := arg.(datalog.Var)
			if !ok || v.Name == "_" {
				// Constant body arg or wildcard — no mapping needed
				// (the constant is already-bound at the trampoline
				// itself). We skip without disqualifying.
				continue
			}
			hPos := -1
			for hi, ha := range rule.Head.Args {
				if hv, ok := ha.(datalog.Var); ok && hv.Name == v.Name {
					hPos = hi
					break
				}
			}
			if hPos < 0 {
				mappedAll = false
				break
			}
			headCols = append(headCols, hPos)
		}
		if !mappedAll || len(headCols) == 0 {
			continue
		}

		parent := rule.Head.Predicate
		if seenParent[parent] {
			continue
		}
		// Only honour parents that the demand pass already proves
		// have demand on AT LEAST the head positions we need. This
		// is the load-bearing soundness check — we never invent
		// demand the analysis didn't already validate.
		parentDemand, hasParentDemand := demand[parent]
		if !hasParentDemand {
			continue
		}
		if !subsetSortedCols(sortUniqueInts(headCols), parentDemand) {
			continue
		}

		// Build seeds for the parent's magic predicate from ITS
		// callers' grounding context.
		grandSeeds := buildDemandSeedsForPred(prog, parent, sortUniqueInts(headCols), sizeHints)
		if len(grandSeeds) == 0 {
			continue
		}
		seenParent[parent] = true
		if existing, ok := parentBindings[parent]; ok {
			parentBindings[parent] = unionSortedCols(existing, headCols)
		} else {
			parentBindings[parent] = sortUniqueInts(headCols)
		}
		parentSeeds = append(parentSeeds, grandSeeds...)
	}

	if len(parentBindings) == 0 {
		return nil, nil, nil
	}
	return nil, parentBindings, parentSeeds
}

// subsetSortedCols returns true iff every element of `sub` is in
// `super`. Both must be sorted-unique slices.
func subsetSortedCols(sub, super []int) bool {
	if len(sub) == 0 {
		return true
	}
	i := 0
	for _, x := range super {
		if x == sub[i] {
			i++
			if i == len(sub) {
				return true
			}
		}
	}
	return i == len(sub)
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

// predArity is a (predicate-name, arity) key used to disambiguate IDB
// heads from base relations of the same name. The desugarer can emit
// auto-generated arity-1 class-extent helper rules (e.g.
// `CallArg(this) :- CallArg(this,_,_).` from PR #146's class-typed
// parameter injection) over the same name as an existing arity-N base
// relation. Name-only IDB lookups silently shadow the base relation,
// causing legitimate base atoms to be rejected as "unhinted IDBs" in
// seed-body construction. Keying by (name, arity) is sound because
// the desugarer never collides arities for genuinely-the-same predicate.
type predArity struct {
	name  string
	arity int
}

// idbHeadByArity returns the set of (name, arity) tuples that appear
// as rule heads in prog. Used by buildDemandSeedsForPred to filter
// preceding body literals at the correct arity, so an arity-3 base atom
// does not get dropped because an arity-1 IDB exists with the same
// predicate name. Mirrors the (name, arity) keying that
// InferBackwardDemand applies to its mixedArity guard, applied here at
// the seed-body construction site.
func idbHeadByArity(prog *datalog.Program) map[predArity]bool {
	out := map[predArity]bool{}
	if prog == nil {
		return out
	}
	for _, r := range prog.Rules {
		out[predArity{r.Head.Predicate, len(r.Head.Args)}] = true
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
