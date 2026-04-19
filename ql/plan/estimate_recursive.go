package plan

import (
	"math"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/stats"
)

// RecursiveIDB describes a derived predicate that participates in a
// non-trivial strongly-connected component of the predicate dependency
// graph (or has a self-loop on a single-rule body) — i.e. its
// definition cannot be evaluated by IdentifyTrivialIDBs because at
// least one rule body refers, transitively, back to the head.
//
// The estimator (EstimateRecursiveIDB) splits the rules into two
// disjoint groups:
//
//   - BaseRules: rules whose body contains NO atom referring to any
//     predicate inside the recursive group. These ground the
//     fixpoint and provide the seed cardinality B.
//   - StepRules: rules whose body contains AT LEAST ONE atom referring
//     to a predicate inside the recursive group. These define the
//     recursive expansion and contribute fan-out σ per iteration.
//
// A recursive IDB with no BaseRules is degenerate (the fixpoint is
// either empty or unbounded depending on the rule body's truth at
// iteration 0); the estimator returns SaturatedSizeHint for those
// rather than guess.
type RecursiveIDB struct {
	Name      string
	Arity     int
	BaseRules []datalog.Rule
	StepRules []datalog.Rule
	// SCCMembers is the full set of predicate names in the same SCC
	// as Name (including Name itself). Used by the estimator to
	// classify literals as "recursive references" vs "base / non-
	// recursive IDB references" without re-running SCC analysis.
	SCCMembers map[string]bool
}

// IdentifyRecursiveIDBs returns the set of recursive IDB heads in
// `prog`, one entry per head. Predicates already accepted as trivial
// (per IdentifyTrivialIDBs over the same basePredicates) are excluded:
// trivial IDBs are sized by the existing pre-pass and there is nothing
// for the recursive estimator to add.
//
// Heads with mixed arities across rules are skipped (defensive — the
// rest of the pipeline assumes uniform arity, same convention as
// IdentifyTrivialIDBs).
//
// Output order is the SCC discovery order from tarjanSCCs (which is
// reverse-topological — dependencies first); within an SCC, heads
// appear in name-sorted order so the result is deterministic across
// runs.
func IdentifyRecursiveIDBs(prog *datalog.Program, basePredicates map[string]bool) []RecursiveIDB {
	if prog == nil || len(prog.Rules) == 0 {
		return nil
	}

	// Group rules by head; reject mixed-arity (consistent with
	// IdentifyTrivialIDBs).
	rulesByHead := map[string][]datalog.Rule{}
	arityByHead := map[string]int{}
	multiArity := map[string]bool{}
	for _, rule := range prog.Rules {
		name := rule.Head.Predicate
		arity := len(rule.Head.Args)
		if existing, seen := arityByHead[name]; seen {
			if existing != arity {
				multiArity[name] = true
			}
		} else {
			arityByHead[name] = arity
		}
		rulesByHead[name] = append(rulesByHead[name], rule)
	}

	trivialSet := map[string]bool{}
	for _, t := range IdentifyTrivialIDBs(prog, basePredicates) {
		trivialSet[t.Name] = true
	}

	adj, preds := buildDepGraph(prog.Rules)
	sccs := tarjanSCCs(adj, preds)

	// Collect heads to consider: name appears as a rule head, has
	// uniform arity, is not trivial, is not a base predicate.
	candidate := func(name string) bool {
		if multiArity[name] || basePredicates[name] || trivialSet[name] {
			return false
		}
		_, hasRules := rulesByHead[name]
		return hasRules
	}

	// An SCC is "recursive" if it has more than one member, OR if its
	// single member has a self-loop (a body literal that refers to
	// itself). Single-node SCCs without self-loops are non-recursive
	// and would have been caught by IdentifyTrivialIDBs already if
	// otherwise eligible.
	hasSelfLoop := func(name string) bool {
		for _, e := range adj[name] {
			if e.to == name {
				return true
			}
		}
		return false
	}

	var out []RecursiveIDB
	for _, scc := range sccs {
		members := map[string]bool{}
		for _, p := range scc {
			members[p] = true
		}
		recursive := len(scc) > 1
		if !recursive && len(scc) == 1 && hasSelfLoop(scc[0]) {
			recursive = true
		}
		if !recursive {
			continue
		}
		// Sort members by name for deterministic output. Copy
		// first because sortStrings (join.go) sorts in place,
		// and `scc` is owned by tarjanSCCs's internal state.
		sortedNames := make([]string, len(scc))
		copy(sortedNames, scc)
		sortStrings(sortedNames)
		for _, name := range sortedNames {
			if !candidate(name) {
				continue
			}
			rules := rulesByHead[name]
			var base, step []datalog.Rule
			for _, rule := range rules {
				if ruleBodyTouchesSCC(rule.Body, members) {
					step = append(step, rule)
				} else {
					base = append(base, rule)
				}
			}
			out = append(out, RecursiveIDB{
				Name:       name,
				Arity:      arityByHead[name],
				BaseRules:  base,
				StepRules:  step,
				SCCMembers: members,
			})
		}
	}
	return out
}

// ruleBodyTouchesSCC returns true if any positive or negative atom in
// body references a predicate inside `members`. Comparisons and
// aggregate sub-bodies are walked transitively (an aggregate over a
// recursive predicate is itself recursive in the dependency graph).
func ruleBodyTouchesSCC(body []datalog.Literal, members map[string]bool) bool {
	for _, lit := range body {
		if lit.Cmp != nil {
			continue
		}
		if lit.Agg != nil {
			if ruleBodyTouchesSCC(lit.Agg.Body, members) {
				return true
			}
			continue
		}
		if members[lit.Atom.Predicate] {
			return true
		}
	}
	return false
}

// StatsLookup is the minimal contract the recursive estimator needs
// from the EDB statistics sidecar. It is defined here (rather than as
// a re-export of *stats.Schema) so tests can supply hand-built
// fixtures without constructing a full Schema, and so the planner
// stays decoupled from the on-disk format.
//
// All methods return (value, ok=false) when the requested datum is
// absent — a missing relation, an out-of-range column, or a stats
// implementation that simply doesn't track that datum. The estimator
// treats any false return as "default-stats mode, refuse to estimate
// past the fixpoint seed."
type StatsLookup interface {
	RowCount(rel string) (int64, bool)
	NDV(rel string, col int) (int64, bool)
}

// SchemaStatsLookup adapts a *stats.Schema to StatsLookup. A nil
// schema yields a lookup whose every method returns (0, false), which
// drives the estimator into default-stats mode — preserving the
// "estimate hook is no-op when no sidecar is loaded" contract.
func SchemaStatsLookup(s *stats.Schema) StatsLookup {
	return schemaStatsLookup{s: s}
}

type schemaStatsLookup struct {
	s *stats.Schema
}

func (l schemaStatsLookup) RowCount(rel string) (int64, bool) {
	rs := l.s.Lookup(rel)
	if rs == nil {
		return 0, false
	}
	return rs.RowCount, true
}

func (l schemaStatsLookup) NDV(rel string, col int) (int64, bool) {
	rs := l.s.Lookup(rel)
	if rs == nil {
		return 0, false
	}
	if col < 0 || col >= len(rs.Cols) {
		return 0, false
	}
	c := rs.Cols[col]
	// NDV of zero is a legitimate value (empty column) but is also
	// what an unpopulated ColStats zero-value looks like. The
	// distinction matters: a zero NDV on a non-empty relation
	// signals stats are missing for this column, not that the
	// column is empty. Cross-check against RowCount: when RowCount
	// > 0 and NDV == 0 the entry is uninitialised, signal absent.
	if c.NDV == 0 && rs.RowCount > 0 {
		return 0, false
	}
	return c.NDV, true
}

// SaturatedSizeHint is the planner-side mirror of eval.SaturatedSizeHint
// (the "definitely huge, exact unknown" ceiling). They MUST stay in sync;
// a build-time check in eval (saturated_size_hint_test.go) asserts
// equality so this package doesn't have to import eval.
const SaturatedSizeHint = 1 << 30

// recursiveFixpointMaxIterations bounds the σ-iteration loop in
// EstimateRecursiveIDB. Plan §4.2 cites "≤ 5 rounds in practice." We
// cap at this value and break early on convergence (|σ_{n+1} - σ_n| <
// recursiveFixpointTolerance).
const recursiveFixpointMaxIterations = 5

// recursiveFixpointTolerance is the convergence threshold for the
// σ-iteration loop. Matches the "0.05" cited in plan §4.3.
const recursiveFixpointTolerance = 0.05

// recursiveGeometricThreshold is the σ value above which the
// geometric series form is considered numerically unstable and the
// estimator falls to the domain-ceiling form instead. Plan §4.3
// rationale: at σ near 1 the series grows without numerical bound and
// the recursion saturates fast in practice — better to use the
// ceiling early.
const recursiveGeometricThreshold = 0.95

// EstimateRecursiveIDB computes a size hint for one recursive IDB
// using selectivity-composition + bounded fixpoint (plan §4.2-4.3).
//
// Inputs:
//   - idb: the recursive IDB to size, as produced by
//     IdentifyRecursiveIDBs.
//   - baseSize: the estimated cardinality of the base case
//     (sum-over-BaseRules). Computed by the caller via the existing
//     P2b sampler (see eval.SampleJoinCardinality) — recursive
//     estimation is a planner-side computation but the base sampler
//     is in the eval package, so we accept the result here.
//   - lookup: the stats sidecar accessor. May be nil — treated as
//     default-stats mode and forces SaturatedSizeHint.
//
// Returns the size hint as int64 (the planner's sizeHints map is
// int-keyed but several intermediate products overflow int32; the
// caller saturates to int + SaturatedSizeHint at the integration
// site).
//
// Soundness contract (plan §4.5): the returned estimate is always
// >= the true fixpoint cardinality. Over-estimation only de-prioritises
// the recursive IDB as a join seed — the safe direction. Under-
// estimation would seed the recursion and cause the cap-hit blow-up
// pattern this estimator exists to prevent.
func EstimateRecursiveIDB(idb RecursiveIDB, baseSize int64, lookup StatsLookup) int64 {
	// Default-stats / no-base-rules → refuse to estimate past
	// SaturatedSizeHint (plan §3.4: "the estimator MUST detect
	// default-stats mode and refuse to estimate beyond depth 1").
	if lookup == nil || len(idb.BaseRules) == 0 {
		return SaturatedSizeHint
	}
	if baseSize <= 0 {
		// A genuinely empty base case yields an empty fixpoint
		// (no seed for the recursion to chain from). Returning 0
		// would let the planner pick this IDB as the cheapest
		// seed and Cartesian-explode downstream if any subsequent
		// sample turns out non-zero. Use SaturatedSizeHint to
		// stay sound-for-ordering (plan §4.5).
		return SaturatedSizeHint
	}
	if len(idb.StepRules) == 0 {
		// Recursive group identified but this particular head
		// only has base rules in it — its size IS the base size.
		// (Can happen when another SCC member references this
		// head but this head doesn't reference back; both end up
		// in the same SCC by mutual reachability.)
		return baseSize
	}

	// σ — total per-iteration fan-out across all step rules.
	// Multiple step rules represent a union of recursive disjuncts
	// (e.g. mayResolveTo's seven step kinds). Each contributes
	// independently to the next-iteration size, so σ is the SUM
	// of per-rule selectivities — not the max or mean. Sum is the
	// sound-for-ordering choice: under-summing would let one
	// cheap disjunct mask the explosive one, exactly the failure
	// mode (multi-rule head defaults to 1000) this estimator
	// exists to fix.
	//
	// The σ-iteration loop is a placeholder for future extensions
	// that consult |IDB|/NDV(IDB, mid) — the σ formula in plan §4.2.
	// In the current implementation σ is a pure function of base-
	// rel stats and the rule body shape, so it converges in one
	// pass; the loop and tolerance check are kept so the formula
	// can be extended without restructuring the call site.
	sigma := 0.0
	for i := 0; i < recursiveFixpointMaxIterations; i++ {
		next := 0.0
		anyStepProduced := false
		for _, rule := range idb.StepRules {
			s, ok := composeStepSelectivity(rule, idb.SCCMembers, lookup)
			if !ok {
				// At least one step rule had insufficient stats
				// to compose σ — degrade to default-stats mode
				// for the whole IDB. Mixing real σ for some
				// rules with a guess for others is the worst
				// of both worlds.
				return SaturatedSizeHint
			}
			anyStepProduced = true
			next += s
		}
		if !anyStepProduced {
			return SaturatedSizeHint
		}
		if math.Abs(next-sigma) < recursiveFixpointTolerance {
			sigma = next
			break
		}
		sigma = next
	}

	if sigma < recursiveGeometricThreshold {
		// Closed-form geometric series:
		//   |IDB| ≈ B / (1 - σ)
		// Sound upper bound on the fixpoint when σ is the per-
		// step expansion factor (textbook transitive-closure
		// cost model).
		denom := 1.0 - sigma
		// denom > 0.05 here by the threshold.
		est := float64(baseSize) / denom
		return saturate(est)
	}

	// σ ≥ threshold: fan-out positive. Use the finite-domain
	// ceiling — for a head of arity K, the ceiling is the product
	// of the column-domain sizes (number of distinct values that
	// could appear in each head position). We approximate by
	// using SaturatedSizeHint as the universal ceiling: the head
	// columns' precise NDV bounds are not available without
	// per-rule head→body var tracing, which is PR4-scope.
	//
	// This is intentionally conservative — any IDB with σ ≥ 0.95
	// is treated as "definitely huge, exact unknown," which is
	// the same posture the existing cap-hit branch uses.
	return SaturatedSizeHint
}

// composeStepSelectivity computes the σ contribution of one step
// rule body. The step rule shape (per plan §4.1) is:
//
//	head(...) :- step1(...), step2(...), ..., recRef(..., midVar, ...).
//
// where exactly one body literal references the recursive head (or
// another SCC member). σ for this rule is the expected number of
// surviving head-tuples per driver tuple in the recursive reference
// — bounded by the product of (RowCount/NDV) ratios for the non-
// recursive joins that bind output variables.
//
// We compute a conservative upper bound for σ:
//
//	σ ≤ Π_{lit ∈ non-recursive body lits} (RowCount(lit) / NDV(lit, joinCol))
//
// where joinCol is the column of `lit` participating in a join with
// any other body literal (or with the head). Without per-literal
// join-key analysis we use column 0 as the proxy join column — sound
// for the typical "first column is the pk-like id" shape every tsq
// EDB relation follows.
//
// Returns (σ, true) on success. Returns (0, false) when any
// non-recursive body literal lacks RowCount or NDV stats — the
// caller treats false as "fall back to SaturatedSizeHint."
func composeStepSelectivity(rule datalog.Rule, sccMembers map[string]bool, lookup StatsLookup) (float64, bool) {
	sigma := 1.0
	sawNonRecursive := false
	for _, lit := range rule.Body {
		if lit.Cmp != nil {
			continue
		}
		if lit.Agg != nil {
			// Aggregates inside a recursive step would cross
			// stratification boundaries and are not supported
			// by this estimator. Bail to default-stats mode.
			return 0, false
		}
		name := lit.Atom.Predicate
		if sccMembers[name] {
			// Skip the recursive reference — it contributes
			// |IDB|/NDV(IDB, mid), which we cannot evaluate
			// here without circular reasoning. Bound by
			// fan-out only.
			continue
		}
		rowCount, ok := lookup.RowCount(name)
		if !ok {
			return 0, false
		}
		ndv, ok := lookup.NDV(name, 0)
		if !ok || ndv == 0 {
			// NDV of zero on a populated relation would imply
			// every row has the same join-column value — a
			// fan-out of RowCount per driver tuple. That's
			// possible in principle but indistinguishable
			// from "stats missing" in the current schema, so
			// bail.
			return 0, false
		}
		sawNonRecursive = true
		// Per-driver fan-out from this literal: RowCount/NDV
		// is the average matches per distinct join-column
		// value. A negative literal contributes at most a
		// factor of 1 (it filters, never multiplies); skip
		// its multiplicative contribution.
		if !lit.Positive {
			continue
		}
		sigma *= float64(rowCount) / float64(ndv)
	}
	if !sawNonRecursive {
		// A step body that has only the recursive reference (no
		// joining literals at all) would give σ = 1 trivially,
		// which under-estimates if the recursive reference itself
		// can fan out. Refuse — let the caller fall back.
		return 0, false
	}
	return sigma, true
}

// saturate converts a float estimate to int64, clamping at
// SaturatedSizeHint and rejecting NaN/Inf. The clamp matches the
// existing "definitely huge, exact unknown" ceiling so all estimator
// branches contribute hints from the same numerical range.
func saturate(v float64) int64 {
	if math.IsNaN(v) || math.IsInf(v, 0) || v > float64(SaturatedSizeHint) {
		return SaturatedSizeHint
	}
	if v < 1 {
		return 1
	}
	return int64(v)
}
