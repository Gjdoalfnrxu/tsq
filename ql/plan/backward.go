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
//
// Interaction with P2a (materialised class extents). Rules that the
// estimator hook materialises as eager class extents are stripped from
// the program by EstimateAndPlanWithExtents BEFORE Plan() (and therefore
// InferBackwardDemand) sees it. As a result those predicates are absent
// from idbArity here and are treated as base relations for demand
// purposes — exactly what callers want, since the materialised relation
// is supplied to the evaluator as a base-like input. Their cardinality
// still enters via sizeHints, so they can still serve as small-extent
// grounding atoms in callers' bodies even though they themselves are
// not demand-inferred.
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
//
// Tuning notes (Adversarial-review Finding 5 on PR #143):
//   - The 5000 ceiling sits well above P2b's sampling estimator's
//     typical error band, so a sampled-down value (e.g. true 7000
//     reported as 4900) tipping into "small" is bounded — at worst the
//     planner pre-binds vars the greedy scorer would also have
//     preferred. It does not produce an unsafe plan.
//   - This constant is co-tuned with tinySeedThreshold (32) and
//     defaultSizeHint (1000): SmallExtentThreshold > defaultSizeHint
//     is intentional so an unhinted IDB does NOT spuriously qualify as
//     a small extent (defaultSizeHint = 1000 < 5000 means we'd over-
//     classify; this is why isSmallExtent requires the hint to be
//     KNOWN — see implementation).
//   - Boundary semantics are inclusive: sz <= SmallExtentThreshold.
//     See isSmallExtent and the boundary tests at 4999/5000/5001 in
//     backward_test.go.
const SmallExtentThreshold = 5000

// LargeArityOneExtentThreshold is the upper bound on a predicate's
// sizeHint for an arity-1 IDB literal to count as a grounder for its
// single var.
//
// Rationale (disj2-round2 / PR #158): per-tuple iteration of an
// arity-1 IDB extent is cheap regardless of size — it's a single-column
// scan. An arity-1 literal whose hint sits in the 5k–500k range (real
// class extents like UseStateSetterCall on mastodon-shape corpora) is
// the SOLE source of demand for downstream synth `_disj_*` predicates
// in setStateUpdater-style queries. Refusing to ground at 5001 because
// the generic SmallExtentThreshold caps at 5000 leaves the synth-disj
// with empty demand and forces the planner into a 5-atom cross-product
// cap-hit. Allowing arity-1 grounding up to a much higher ceiling
// avoids that failure mode without touching the multi-arity case
// (where wider tuples make per-tuple scoring matter).
//
// Saturated hints (eval.SaturatedSizeHint = 1<<30 → genuinely huge or
// MaterialiseClassExtents-failed) deliberately stay above this ceiling
// and continue to drop demand. The fix targets known-mid-sized arity-1
// extents only; unknowable-size literals remain untreated.
const LargeArityOneExtentThreshold = 1_000_000

// LowFanoutThreshold is the upper bound on per-driver fan-out
// (RowCount / NDV) for a column to qualify as a "low-fanout" join key
// in stats-aware grounding (Phase B PR4). When a body literal binds a
// low-fanout column — by a constant or by sharing a variable with the
// already-bound prefix — probing that column yields a small set of
// matches per driver tuple, which means the literal's remaining vars
// can be treated as bound for backward-demand purposes even when the
// relation as a whole is large (above SmallExtentThreshold).
//
// Set to 10: a per-probe lookup returning ≤ 10 matches is small enough
// that any downstream literal sharing those bound vars sees a
// per-driver workload comparable to grounding through a small extent.
// Higher values would over-ground (start treating big fan-out columns
// as grounders); lower values would miss the FK-shape case where a
// child column has 1 parent (LocalFlow.dstSym → LocalFlow row count =
// NDV ≈ row count).
//
// Plan §3.2: "isSmallExtent(pred, hints) || isLowFanoutCol(pred, col,
// stats) where the latter fires when a literal binds a column whose
// NDV/RowCount ratio means each driver tuple selects O(1) matches."
const LowFanoutThreshold = 10

// arity1BaseGroundedIDBs returns the set of IDB predicate names whose
// every defining rule has a single head var (arity 1) and a body
// composed entirely of non-IDB (base/extensional) positive atoms or
// comparisons — i.e. the structural shape of a class-extent helper
// (concrete-charPred-style: `Pred(this) :- Base1(this,_), Base2(_,_)`).
//
// Such predicates are safe to treat as grounders for their single
// column at any size up to LargeArityOneExtentThreshold: per-tuple
// iteration is a single-column scan and the body is non-recursive so
// the tuple count is bounded by the underlying base relations.
//
// This is the gating set for the disj2-round2 fix in
// bodyContextGroundedVars. Conservative on purpose:
//   - mixed-arity heads → excluded (mirror of mixedArity skip in
//     InferBackwardDemand, prevents the audit-#3 name/arity hazard)
//   - any IDB-on-IDB body atom → excluded (recursion or IDB-stacking
//     means the size hint is the load-bearing signal we should not
//     bypass)
//   - aggregates / negation / empty body → excluded
func arity1BaseGroundedIDBs(prog *datalog.Program) map[string]bool {
	out := map[string]bool{}
	if prog == nil || len(prog.Rules) == 0 {
		return out
	}
	// First pass: collect IDB names so we can check body atoms.
	idb := map[string]bool{}
	for _, r := range prog.Rules {
		idb[r.Head.Predicate] = true
	}
	// Group rules by head and arity-check.
	type ruleSet struct {
		ok    bool
		rules []datalog.Rule
	}
	heads := map[string]*ruleSet{}
	for _, r := range prog.Rules {
		rs, ok := heads[r.Head.Predicate]
		if !ok {
			rs = &ruleSet{ok: true}
			heads[r.Head.Predicate] = rs
		}
		if len(r.Head.Args) != 1 {
			rs.ok = false
		}
		rs.rules = append(rs.rules, r)
	}
	for name, rs := range heads {
		if !rs.ok {
			continue
		}
		allOK := true
		for _, r := range rs.rules {
			if len(r.Body) == 0 {
				allOK = false
				break
			}
			for _, lit := range r.Body {
				if lit.Cmp != nil {
					continue
				}
				if lit.Agg != nil || !lit.Positive {
					allOK = false
					break
				}
				if idb[lit.Atom.Predicate] {
					allOK = false
					break
				}
			}
			if !allOK {
				break
			}
		}
		if allOK {
			out[name] = true
		}
	}
	return out
}

// isLargeArity1Grounder returns true if `pred` is an arity-1 base-grounded
// IDB AND its size hint is within LargeArityOneExtentThreshold (or
// unknown — treat as plausibly small for grounding purposes since the
// alternative is dropping demand and producing a cross-product plan).
// Saturated hints (>= LargeArityOneExtentThreshold, in practice 1<<30)
// deliberately do NOT qualify; we only relax the gate for mid-sized
// extents.
func isLargeArity1Grounder(pred string, sizeHints map[string]int, large map[string]bool) bool {
	if !large[pred] {
		return false
	}
	sz, ok := sizeHints[pred]
	if !ok {
		// No hint: treat as eligible — refusing to ground here is what
		// triggers the disj2-round2 cap-hit in the SaturatedSizeHint
		// sibling case where the pre-pass overwrote the hint with the
		// saturated marker. Caller bears responsibility for not over-
		// committing on truly-huge extents (the threshold above guards
		// the saturated marker explicitly).
		return true
	}
	return sz > 0 && sz <= LargeArityOneExtentThreshold
}

// isMaterialisedClassExtentGrounder returns true if `pred` is in the
// caller-supplied class-extent name set. This is the disj2-round3 fix:
// when EstimateAndPlanWithExtents materialises a class-extent rule and
// strips it from the planning program, the structural detector
// `arity1BaseGroundedIDBs` cannot find the rule any more (it's gone)
// and the consuming rule's body literal looks like a base atom whose
// only signal is its sizeHint. For materialised class extents the
// extent has ALREADY been computed once, lives in RAM as a base-like
// relation, and per-tuple iteration of its single column is cheap
// regardless of size — so it is safe to ground its arg vars at any size,
// including the SaturatedSizeHint marker (the materialising hook is the
// authoritative signal that the extent was successfully built; size
// hint can be unrelated).
//
// classExtentNames may be nil; nil means "no materialised extents
// declared" and degrades to the PR #158 behaviour (arity-1 structural
// detector only).
func isMaterialisedClassExtentGrounder(pred string, classExtentNames map[string]bool) bool {
	return classExtentNames[pred]
}

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
	return InferBackwardDemandWithClassExtents(prog, sizeHints, nil)
}

// InferBackwardDemandWithStats is the Phase B PR4 entry point that
// additionally honours an EDB statistics sidecar lookup. The stats
// drive the low-fanout grounding heuristic
// (bodyContextGroundedVars + isLowFanoutCol): a body literal that
// binds a low-fanout column (per-driver fan-out ≤ LowFanoutThreshold)
// grounds its remaining vars even when the relation is too large for
// the SmallExtentThreshold or arity-1 paths.
//
// nil lookup degrades to InferBackwardDemandWithClassExtents
// behaviour byte-identically (default-stats mode per plan §3.4).
func InferBackwardDemandWithStats(
	prog *datalog.Program,
	sizeHints map[string]int,
	classExtentNames map[string]bool,
	lookup StatsLookup,
) DemandMap {
	return inferBackwardDemand(prog, sizeHints, classExtentNames, lookup)
}

// InferBackwardDemandWithClassExtents is the disj2-round3 entry point
// that additionally honours a caller-supplied set of materialised
// class-extent base names. Those names are treated as grounders for any
// var they bind in any rule body, regardless of size hint or the
// structural arity-1 detector. See isMaterialisedClassExtentGrounder
// for rationale.
//
// The plain InferBackwardDemand wrapper passes nil, preserving its
// pre-disj2-round3 behaviour.
func InferBackwardDemandWithClassExtents(
	prog *datalog.Program,
	sizeHints map[string]int,
	classExtentNames map[string]bool,
) DemandMap {
	return inferBackwardDemand(prog, sizeHints, classExtentNames, nil)
}

// inferBackwardDemand is the unified implementation. The two public
// wrappers (InferBackwardDemandWithClassExtents and
// InferBackwardDemandWithStats) differ only in whether they pass a
// non-nil StatsLookup.
func inferBackwardDemand(
	prog *datalog.Program,
	sizeHints map[string]int,
	classExtentNames map[string]bool,
	lookup StatsLookup,
) DemandMap {
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
	largeArity1IDBs := arity1BaseGroundedIDBs(prog)

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

	// Per-iteration recomputation. The original in-place intersect
	// design had a soundness gap: when caller R's head demand grew
	// LATER in the fixed point (e.g. because the query/grandparent
	// pass had not yet observed R's own demand on iter 0), the early
	// intersect of R's body literal demand with the empty initial
	// observation became permanent — `intersect([], [0]) == []` and
	// no subsequent iteration could grow it back.
	//
	// Concretely (Mastodon `_disj_2` failure mode): the rename rule
	// `functionContainsStar :- _disj_2` was visited on iter 1 with
	// `demand[functionContainsStar] = []` (still uninitialised from
	// the rule passes the rename appears before in source order),
	// observed `_disj_2 -> []` and initialised `demand[_disj_2]=[]`.
	// Iter 2: setStateUpdaterCallsFn raised
	// `demand[functionContainsStar]` to [0], the rename re-ran and
	// now observed `_disj_2 -> [0]`, but `intersect([],[0]) = []`
	// stuck. Result: `_disj_2` looked unbound to the magic-set
	// rewrite even though there's a real rename-trampoline path.
	//
	// Fix: each pass computes a fresh `observation` map by walking
	// every caller (query + every rule body) and intersecting their
	// per-pass observed columns. Head context for each rule is taken
	// from `prevDemand` (the previous iteration's result), so as
	// prevDemand grows monotonically across iterations, observation
	// grows monotonically too. We assign newDemand := observation
	// directly (no cross-iteration intersect) — the all-callers
	// intersect is fully captured WITHIN each pass.
	prevDemand := DemandMap{}
	initialised := map[string]bool{}

	maxIter := 1
	for name := range idbArity {
		maxIter += idbArity[name] + 1
	}
	if maxIter < 8 {
		maxIter = 8
	}

	for iter := 0; iter < maxIter; iter++ {
		observation := map[string][]int{}
		observed := map[string]bool{}
		recordObservation := func(pred string, cols []int) {
			if !observed[pred] {
				observation[pred] = append([]int(nil), cols...)
				observed[pred] = true
				return
			}
			observation[pred] = intersectSortedCols(observation[pred], cols)
		}

		// The query body is a first-class caller of any IDB it
		// references. Adversarial-review Finding 1 on PR #143.
		if prog.Query != nil && len(prog.Query.Body) > 0 {
			queryRule := datalog.Rule{Head: datalog.Atom{}, Body: prog.Query.Body}
			ctxBoundVars := bodyContextGroundedVars(queryRule, sizeHints, map[string]bool{}, largeArity1IDBs, classExtentNames, lookup)
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
				recordObservation(pred, literalBoundCols(lit, ctxBoundVars))
			}
		}

		for _, rule := range prog.Rules {
			// Use PREVIOUS iteration's demand for head-context
			// computation. As prevDemand grows monotonically,
			// growing head demand re-flows through this rule's body
			// observations on the next pass.
			headDemandCols := prevDemand[rule.Head.Predicate]
			headBoundVars := map[string]bool{}
			for _, col := range headDemandCols {
				if col < 0 || col >= len(rule.Head.Args) {
					continue
				}
				if v, ok := rule.Head.Args[col].(datalog.Var); ok && v.Name != "_" {
					headBoundVars[v.Name] = true
				}
			}
			ctxBoundVars := bodyContextGroundedVars(rule, sizeHints, headBoundVars, largeArity1IDBs, classExtentNames, lookup)

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
				recordObservation(pred, literalBoundCols(lit, ctxBoundVars))
			}
		}

		// newDemand := observation (no intersect with prevDemand —
		// each pass's observation already contains the full
		// all-callers intersect for this pass).
		newDemand := DemandMap{}
		newInitialised := map[string]bool{}
		for pred, cols := range observation {
			newDemand[pred] = cols
			newInitialised[pred] = true
		}
		// Carry over previously-initialised preds that weren't
		// observed this pass (defensive — shouldn't occur in
		// practice since callers don't change between iterations).
		for pred := range initialised {
			if _, ok := newInitialised[pred]; !ok {
				newDemand[pred] = prevDemand[pred]
				newInitialised[pred] = true
			}
		}

		converged := len(newInitialised) == len(initialised)
		if converged {
			for pred, cols := range newDemand {
				if !sameCols(cols, prevDemand[pred]) {
					converged = false
					break
				}
			}
		}

		prevDemand = newDemand
		initialised = newInitialised
		if converged {
			break
		}
	}

	// Filter: a predicate that was never observed as a body atom (only
	// ever defined as a head) has no caller-imposed demand. We drop it
	// from the map so callers can distinguish "no demand" (not in map)
	// from "demand is the empty set" (in map, zero cols) — the latter
	// means "observed, but no column reliably bound", which is still
	// useful signal to tests.
	demand := DemandMap{}
	for pred, cols := range prevDemand {
		if initialised[pred] {
			demand[pred] = cols
		}
	}
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
	largeArity1IDBs map[string]bool,
	classExtentNames map[string]bool,
	lookup StatsLookup,
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
			// seeding. Arity-1 base-grounded IDB extents (class-extent
			// helpers like `UseStateSetterCall(c) :- CallCalleeSym(c,_),
			// ImportBinding(_,_,_)`) qualify too even when their hint
			// exceeds SmallExtentThreshold — see disj2-round2 / PR #158
			// rationale on LargeArityOneExtentThreshold.
			// Materialised class extents are always arity-1 (the
			// MaterialisingEstimatorHook contract — see plan.go). Restrict
			// the relaxation to arity-1 occurrences so a name collision
			// with an arity-N base relation (defensive case — desugarer
			// shouldn't emit one but a hand-written predicate could)
			// cannot over-ground the wider literal's vars.
			isMatExt := len(lit.Atom.Args) == 1 &&
				isMaterialisedClassExtentGrounder(lit.Atom.Predicate, classExtentNames)
			if isSmallExtent(lit.Atom.Predicate, sizeHints) ||
				isLargeArity1Grounder(lit.Atom.Predicate, sizeHints, largeArity1IDBs) ||
				isMatExt {
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
					continue
				}
			}
			// Phase B PR4: stats-aware low-fanout grounding. When the
			// literal binds a low-fanout column (per-driver fan-out ≤
			// LowFanoutThreshold per the EDB stats sidecar), the
			// per-probe lookup yields O(1) matches and the literal's
			// remaining vars can be treated as bound — even when the
			// relation is too large for the SmallExtentThreshold or
			// arity-1 paths above.
			//
			// "Binds" means: the column carries either a constant or a
			// variable already in `bound`. We compute the set of bound
			// columns per literal and check each against the stats
			// lookup; if ANY bound column is low-fanout, the whole
			// literal grounds.
			//
			// nil lookup degrades to no-op — preserving byte-identical
			// behaviour when no sidecar is loaded (default-stats mode
			// per plan §3.4).
			if lookup != nil {
				if anyBoundColIsLowFanout(lit, bound, lookup) {
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

// isLowFanoutCol returns true when the per-driver fan-out for `pred`'s
// `col` (RowCount / NDV) is at most LowFanoutThreshold per the stats
// sidecar. A nil lookup, missing relation, or missing column returns
// false (default-stats mode — refuse to ground without evidence).
//
// Plan §3.2 rationale: "isSmallExtent(pred, hints) ||
// isLowFanoutCol(pred, col, stats) where the latter fires when a
// literal binds a column whose NDV/RowCount ratio means each driver
// tuple selects O(1) matches." The fan-out RowCount/NDV is the inverse
// of NDV/RowCount and the more direct quantity for "matches per
// distinct join-key value."
//
// NDV == 0 on a non-empty relation is treated as absent (matches
// SchemaStatsLookup.NDV semantics — the zero-value ColStats is
// indistinguishable from "stats intentionally absent for this
// column"). NDV >= RowCount is sound (fan-out ≤ 1, qualifies); the
// inequality direction is the FK-shape case (e.g. Contains.child).
func isLowFanoutCol(pred string, col int, lookup StatsLookup) bool {
	if lookup == nil {
		return false
	}
	rowCount, ok := lookup.RowCount(pred)
	if !ok || rowCount <= 0 {
		return false
	}
	ndv, ok := lookup.NDV(pred, col)
	if !ok || ndv <= 0 {
		return false
	}
	// fan-out = rowCount / ndv. Use integer arithmetic to avoid
	// float boundary surprises at the threshold edge: rowCount <=
	// ndv * LowFanoutThreshold is equivalent to rowCount/ndv <=
	// LowFanoutThreshold for positive values.
	return rowCount <= ndv*int64(LowFanoutThreshold)
}

// anyBoundColIsLowFanout reports whether ANY column position of
// lit.Atom that carries a constant or an already-bound variable is
// low-fanout per the stats lookup. This is the gate condition for the
// stats-aware grounding path in bodyContextGroundedVars: a single
// low-fanout bound column is enough to make the per-driver probe
// O(1), which lets the planner treat the rest of the literal's vars
// as bound for backward demand.
//
// Comparisons, aggregates, and negative literals are filtered by the
// caller (this is only invoked on positive atoms).
func anyBoundColIsLowFanout(lit datalog.Literal, bound map[string]bool, lookup StatsLookup) bool {
	pred := lit.Atom.Predicate
	if pred == "" {
		return false
	}
	for i, arg := range lit.Atom.Args {
		switch a := arg.(type) {
		case datalog.IntConst, datalog.StringConst:
			if isLowFanoutCol(pred, i, lookup) {
				return true
			}
		case datalog.Var:
			if a.Name == "_" {
				continue
			}
			if !bound[a.Name] {
				continue
			}
			if isLowFanoutCol(pred, i, lookup) {
				return true
			}
		}
	}
	return false
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
	return orderJoinsWithDemandAndIDB(head, body, sizeHints, headDemand, nil)
}

// orderJoinsWithDemandAndIDB extends orderJoinsWithDemand with awareness
// of per-IDB demand bindings: for any positive body literal that calls
// an IDB whose demand map names bound positions, the planner penalises
// scheduling that literal until the corresponding variables are
// runtime-bound. This is the disj2-round4 fix.
//
// Concretely, after magic-set rewriting an IDB call P(x,y) with
// demand[P]=[0] will be evaluated as `magic_P(x), P(x,y)` — i.e. the
// magic seed prefilters x. If we schedule P(x,y) before x is bound by
// some grounder in the body, we lose that benefit and the underlying
// recursive evaluation iterates the entire predicate. The fix tells
// the greedy scorer "defer this literal until x is runtime-bound" by
// inflating its effective size hint to SaturatedSizeHint when the
// precondition is unmet. Once a later step grounds x, the literal
// becomes attractively-cheap again at its real size.
//
// Only POSITIVE atom literals on IDBs with non-empty demand are
// affected. Comparisons, negatives, aggregates, base relations, and
// IDBs with empty demand keep their current scoring. The penalty does
// NOT apply to the rule's OWN head predicate (that would forbid
// recursive self-calls from being scheduled at all).
//
// idbDemand may be nil; nil degrades exactly to orderJoinsWithDemand's
// pre-round4 behaviour.
func orderJoinsWithDemandAndIDB(
	head datalog.Atom,
	body []datalog.Literal,
	sizeHints map[string]int,
	headDemand []int,
	idbDemand DemandMap,
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
		// Tiny-seed override is suppressed for IDB literals whose demand
		// preconditions aren't met yet — a pickTinySeed promotion via the
		// shared-bound-var branch would otherwise schedule a demand-IDB
		// before its bound vars are grounded, defeating the round4 fix.
		// We pass placedView equal to placed but block IDB-demand-deferred
		// candidates by marking them temporarily placed for this lookup.
		// disj2-round6: magic-pred anchor. Any unplaced positive literal
		// whose predicate carries the `magic_` prefix MUST win the next
		// slot regardless of normal cost scoring (boundCount /
		// sizeHint / tiny-seed). Magic predicates are the seed-driven
		// demand source after `MagicSetTransform`: their extension is
		// either empty (rule produces nothing — correct) or non-empty
		// (the only values worth driving the join with — correct).
		// Placing them late lets the planner cross-product the
		// preceding base/IDB literals before pruning, which is the
		// pathology behind the round-6 cap-hit on
		// `magic__disj_28` (VarDecl ⋈ DestructureField on no shared
		// vars, ~10M tuples, hits the 5M cap before
		// `magic_contextDestructureBinding` ever filters).
		//
		// Eligibility: positive atoms are always eligible (isEligible
		// returns true unconditionally for positive non-comparison
		// literals), so the anchor pass need only check `placed`.
		// Multiple magic literals in one body (rare but possible —
		// e.g. a propagation rule built from a body where the
		// preceding lits already include a magic prereq) are
		// scheduled in body order; subsequent ones become filters once
		// runtimeBound covers their args.
		if magicIdx := pickMagicAnchor(body, placed); magicIdx != -1 {
			bestIdx = magicIdx
		} else {
			var tinyMask []bool
			if hasIDBDemand(body, idbDemand) {
				tinyMask = make([]bool, len(body))
				copy(tinyMask, placed)
				for i, lit := range body {
					if tinyMask[i] {
						continue
					}
					if isIDBCallDeferred(lit, head.Predicate, idbDemand, runtimeBound, sizeHints) {
						tinyMask[i] = true
					}
				}
			} else {
				tinyMask = placed
			}
			tinyIdx := pickTinySeed(body, tinyMask, runtimeBound, sizeHints, defaultSizeHint)
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
					// Round4: defer IDB literals whose demand requires
					// positions not yet runtime-bound. Inflate their size to
					// SaturatedSizeHint so any other eligible candidate wins;
					// when a later step binds the required vars they regain
					// their true cost and become competitive again.
					if isIDBCallDeferred(lit, head.Predicate, idbDemand, runtimeBound, sizeHints) {
						size = idbDeferredPenalty
					}
					if bestIdx == -1 || negBound < bestNegBound || (negBound == bestNegBound && size < bestSize) {
						bestIdx = i
						bestNegBound = negBound
						bestSize = size
					}
				}
			}
		} // end of magic-anchor else

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

// idbDeferredPenalty is the cost-model penalty assigned to an IDB
// literal whose demand bindings are not yet runtime-bound. It must
// dominate any plausible real size hint while staying below int
// overflow; aligning with eval.SaturatedSizeHint (1<<30) is the
// natural choice — both signal "treat as effectively unbounded".
const idbDeferredPenalty = 1 << 30

// hasIDBDemand returns true if any literal in body refers to a
// predicate present in idbDemand with a non-empty bound-position list.
// Used as a cheap pre-check to skip the per-step deferral computation
// when no demand applies (the common case).
func hasIDBDemand(body []datalog.Literal, idbDemand DemandMap) bool {
	if len(idbDemand) == 0 {
		return false
	}
	for _, lit := range body {
		if !lit.Positive || lit.Cmp != nil || lit.Agg != nil {
			continue
		}
		if cols, ok := idbDemand[lit.Atom.Predicate]; ok && len(cols) > 0 {
			return true
		}
	}
	return false
}

// isIDBCallDeferred reports whether lit is a positive call to an IDB
// whose demand map names argument positions that are NOT yet bound in
// runtimeBound. Such a call should be deferred until a later step
// grounds the required vars — but only when:
//
//   - The literal's size hint is "large" (≥ SmallExtentThreshold) or
//     unhinted (defaultSizeHint). A genuinely small IDB call (e.g.
//     a tiny class extent like TaintSink at size 7) is cheap to
//     schedule even with free args; deferring it would suppress the
//     existing tiny-seed heuristic that depends on small-hinted IDBs
//     winning slot 0. Round4's target is the recursive / big-IDB
//     shape (functionContainsStar), not small lookup helpers.
//
//   - The predicate is NOT the rule's own head (recursive self-call).
//     A literal that is a recursive self-call (e.g. `Path :- Path,
//     Edge`) is NEVER deferred — that would forbid scheduling the
//     recursive case at all, breaking fixpoint convergence.
//
// Comparisons, negatives, aggregates, and base relations always return
// false (idbDemand has no entry, or is gated out above).
func isIDBCallDeferred(
	lit datalog.Literal,
	selfHead string,
	idbDemand DemandMap,
	runtimeBound map[string]bool,
	sizeHints map[string]int,
) bool {
	if !lit.Positive || lit.Cmp != nil || lit.Agg != nil {
		return false
	}
	pred := lit.Atom.Predicate
	if pred == "" || pred == selfHead {
		return false
	}
	cols, ok := idbDemand[pred]
	if !ok || len(cols) == 0 {
		return false
	}
	// Size-gate: only defer literals that would be expensive to
	// scan unbound. A known-small IDB hint exempts the literal from
	// deferral — its full scan is cheap.
	if sz, hinted := sizeHints[pred]; hinted && sz > 0 && sz <= SmallExtentThreshold {
		return false
	}
	for _, col := range cols {
		if col < 0 || col >= len(lit.Atom.Args) {
			// Arity mismatch — treat as not-deferred to avoid
			// silently breaking malformed but otherwise-eligible plans.
			return false
		}
		v, isVar := lit.Atom.Args[col].(datalog.Var)
		if !isVar || v.Name == "" || v.Name == "_" {
			// Constant or wildcard at the demand position satisfies
			// the magic-set seed without runtime binding.
			continue
		}
		if !runtimeBound[v.Name] {
			return true
		}
	}
	return false
}
