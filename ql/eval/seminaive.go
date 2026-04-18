package eval

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

// DefaultMaxIterations is the default maximum number of fixpoint iterations
// per stratum. If exceeded and WithAllowPartial(true) is NOT set, Evaluate
// returns a *IterationCapError (wraps ErrIterationCapExceeded). With
// WithAllowPartial(true), legacy behaviour is restored: a warning is logged
// and evaluation proceeds with the partial results computed so far.
const DefaultMaxIterations = 100

// DefaultMaxBindingsPerRule is the default per-rule cap on intermediate
// binding cardinality during join evaluation. With weak join constraints
// (free variables, low-selectivity predicates) intermediate cardinality can
// reach 100M+ entries (1-2 GB) before any deduplication happens, which OOMs
// the process. The cap is well above legitimate query needs but well below
// the RAM ceiling on a typical workstation. Set 0 via WithMaxBindingsPerRule
// to disable.
const DefaultMaxBindingsPerRule = 5_000_000

// ErrBindingCapExceeded is the sentinel returned (wrapped in a *BindingCapError)
// when a rule's intermediate join cardinality exceeds the configured cap.
// Callers can detect it with errors.Is.
var ErrBindingCapExceeded = errors.New("rule binding cap exceeded")

// BindingCapError gives detail about which rule blew the cap and at what
// step. It wraps ErrBindingCapExceeded so errors.Is works.
type BindingCapError struct {
	Rule        string
	StepIndex   int
	Cap         int
	Cardinality int
}

func (e *BindingCapError) Error() string {
	if e.Rule == "" {
		return fmt.Sprintf("rule binding cap exceeded: cap=%d at join step %d (intermediate cardinality=%d). Increase --max-bindings-per-rule or rewrite the query for better selectivity.", e.Cap, e.StepIndex, e.Cardinality)
	}
	return fmt.Sprintf("rule %q exceeded binding cap of %d at join step %d (intermediate cardinality=%d). Increase --max-bindings-per-rule or rewrite the query for better selectivity.", e.Rule, e.Cap, e.StepIndex, e.Cardinality)
}

func (e *BindingCapError) Unwrap() error { return ErrBindingCapExceeded }

// ErrIterationCapExceeded is the sentinel returned (wrapped in a
// *IterationCapError) when a stratum's semi-naive fixpoint loop hits the
// configured iteration cap without reaching a fixpoint. Callers can detect
// it with errors.Is. Use WithAllowPartial(true) to restore the legacy
// "warn and return partial results" behaviour.
var ErrIterationCapExceeded = errors.New("iteration cap exceeded before fixpoint")

// IterationCapError gives detail about which stratum failed to converge,
// the cap that was hit, the last-iteration delta size (a non-zero value
// proves the fixpoint was still producing new tuples — i.e. the result is
// genuinely incomplete, not just close to convergence), and the head
// predicate of the rule whose delta was largest at the cap. It wraps
// ErrIterationCapExceeded so errors.Is works.
type IterationCapError struct {
	Stratum       int    // index of the stratum that failed to converge
	Rule          string // head predicate of the rule with the largest delta at the cap
	Cap           int    // iteration cap that was hit
	LastDeltaSize int    // total tuples in delta on the iteration the cap fired
}

func (e *IterationCapError) Error() string {
	if e.Rule == "" {
		return fmt.Sprintf("query did not converge in %d iterations (stratum %d, last delta size: %d). Increase --max-iterations or pass --allow-partial to accept incomplete results.", e.Cap, e.Stratum, e.LastDeltaSize)
	}
	return fmt.Sprintf("query did not converge in %d iterations (rule: %s, last delta size: %d). Increase --max-iterations or pass --allow-partial to accept incomplete results.", e.Cap, e.Rule, e.LastDeltaSize)
}

func (e *IterationCapError) Unwrap() error { return ErrIterationCapExceeded }

// joinLimits carries the per-rule binding cap, cancellation context, and
// identifying context down through the join evaluation call chain.
// A nil receiver means no cap and no ctx check.
//
// ctx is checked between join steps and inside the per-binding inner loop of
// applyPositive. Without that, a single Rule()/RuleDelta() call building a
// 10M-binding intermediate could ignore --timeout for many seconds (issue
// #81 follow-up: per-iteration ctx checks at the seminaive level only fire
// between rules, not within them).
type joinLimits struct {
	ctx         context.Context
	maxBindings int    // 0 == unlimited
	ruleName    string // for error messages; may be empty (e.g. final query)
}

func (l *joinLimits) check(stepIndex, n int) error {
	if l == nil {
		return nil
	}
	if l.ctx != nil {
		if err := l.ctx.Err(); err != nil {
			return fmt.Errorf("rule %q cancelled at join step %d: %w", l.ruleName, stepIndex, err)
		}
	}
	if l.maxBindings <= 0 {
		return nil
	}
	if n > l.maxBindings {
		return &BindingCapError{Rule: l.ruleName, StepIndex: stepIndex, Cap: l.maxBindings, Cardinality: n}
	}
	return nil
}

// ctxErr returns a wrapped ctx error if the limits' context is cancelled,
// or nil otherwise. Used inside the per-binding inner loops of applyPositive
// where checking ctx every binding would be too expensive — callers throttle
// to every Nth iteration.
func (l *joinLimits) ctxErr(stepIndex int) error {
	if l == nil || l.ctx == nil {
		return nil
	}
	if err := l.ctx.Err(); err != nil {
		return fmt.Errorf("rule %q cancelled at join step %d: %w", l.ruleName, stepIndex, err)
	}
	return nil
}

// ResultSet holds the query results.
type ResultSet struct {
	Columns []string // column names (from query select)
	Rows    [][]Value
}

// Option configures the evaluator.
type Option func(*evalConfig)

type evalConfig struct {
	maxIterations      int
	maxBindingsPerRule int
	parallel           bool
	allowPartial       bool
	// sizeHints is the planner's relation→tuple-count map. When non-nil,
	// Evaluate refreshes it after each stratum's fixpoint converges (using
	// the materialised tuple counts of the head predicates produced in that
	// stratum) and re-plans every subsequent stratum's join order with the
	// updated hints. This fixes the IDB-default-1000 misestimate that caused
	// Cartesian-heavy join orders for queries whose seed predicate is a tiny
	// derived relation. See issue #88.
	sizeHints map[string]int
	// materialisedExtents holds class-extent IDBs that were
	// pre-materialised before planning (P2a). Each entry is keyed by
	// relKey(name, arity) and the corresponding *Relation is folded into
	// allRels at evaluation start, so downstream rules see the extent as
	// a base-like relation. Because plan.EstimateAndPlanWithExtents
	// strips the materialised rules from the program before stratification,
	// no rule in execPlan should have a head matching one of these names —
	// but Evaluate defensively skips rules whose head DOES collide so a
	// double-injection cannot duplicate work.
	materialisedExtents map[string]*Relation
}

// WithMaxIterations sets the maximum number of fixpoint iterations per stratum.
// If the limit is reached and WithAllowPartial(true) is NOT set, Evaluate
// returns a *IterationCapError (wraps ErrIterationCapExceeded). With
// WithAllowPartial(true), legacy behaviour applies: a warning is logged and
// evaluation proceeds with the partial results computed so far. A value of
// 0 means no limit.
func WithMaxIterations(n int) Option {
	return func(c *evalConfig) { c.maxIterations = n }
}

// WithAllowPartial restores the legacy behaviour for the iteration cap:
// when the cap is hit, log a warning and return partial results instead of
// returning an error. Default is false (return error). This option does NOT
// affect the binding cap, which always errors.
func WithAllowPartial(allow bool) Option {
	return func(c *evalConfig) { c.allowPartial = allow }
}

// WithMaxBindingsPerRule sets the per-rule cap on intermediate join binding
// cardinality. If a rule's intermediate cardinality exceeds the cap during
// evaluation, Evaluate returns a *BindingCapError (wraps ErrBindingCapExceeded).
// A value of 0 disables the cap.
func WithMaxBindingsPerRule(n int) Option {
	return func(c *evalConfig) { c.maxBindingsPerRule = n }
}

// WithParallel enables parallel evaluation of independent rules within
// a stratum's fixpoint iteration. Rules with different head predicates
// are evaluated concurrently.
func WithParallel() Option {
	return func(c *evalConfig) { c.parallel = true }
}

// WithSizeHints provides the planner's relation→tuple-count map to the
// evaluator. When supplied, Evaluate refreshes the map after each stratum's
// fixpoint converges with the actual tuple counts of derived predicates
// produced in that stratum, then re-plans every later stratum (and the final
// query) using the refreshed hints. This is the fix for issue #88 — without
// it, derived (IDB) predicates fall through to defaultSizeHint=1000 and the
// planner mis-orders joins seeded by tiny derived relations.
//
// Pass the same map handed to plan.Plan; the evaluator mutates it in place.
// Callers that do not need adaptive replanning can omit this option, in
// which case behaviour is unchanged.
func WithSizeHints(hints map[string]int) Option {
	return func(c *evalConfig) { c.sizeHints = hints }
}

// WithMaterialisedClassExtents injects pre-materialised class-extent
// relations into Evaluate. Each entry must already be relKey-keyed
// ("<name>/<arity>"); the canonical producer is
// MakeMaterialisingEstimatorHook, which writes into a sink map the
// caller then hands here.
//
// Semantics: the supplied relations are added to allRels at evaluation
// start, so any rule body literal that references one of those names
// resolves to the materialised tuples instead of an empty relation.
// Rules whose HEAD matches a materialised extent name+arity are skipped
// during stratum bootstrap and fixpoint — defensively, since
// EstimateAndPlanWithExtents already strips them from the program. The
// double-guard means a misuse (passing materialised relations without
// having stripped the rules) silently degrades to "no work duplicated"
// rather than "extent gets unioned with itself and re-deduped at every
// iteration".
//
// nil or empty map is a no-op.
func WithMaterialisedClassExtents(rels map[string]*Relation) Option {
	return func(c *evalConfig) { c.materialisedExtents = rels }
}

// Evaluate executes an ExecutionPlan over base facts and returns results.
func Evaluate(ctx context.Context, execPlan *plan.ExecutionPlan, baseRels map[string]*Relation, opts ...Option) (*ResultSet, error) {
	cfg := evalConfig{
		maxIterations:      DefaultMaxIterations,
		maxBindingsPerRule: DefaultMaxBindingsPerRule,
	}
	for _, o := range opts {
		o(&cfg)
	}

	// allRels starts with base facts; derived relations are added as we go.
	// The map is keyed by (name, arity) via relKey() to ensure that a
	// rule head whose arity differs from a base relation of the same name
	// (the QL bridge class characteristic predicate case) does NOT shadow
	// the base relation. See ql/eval/relkey.go for the rationale.
	allRels := keyRels(baseRels)

	// P2a: fold pre-materialised class extents into allRels so downstream
	// rules see them as base-like. Keys are already relKey'd by the
	// MakeMaterialisingEstimatorHook contract — we don't re-key here so
	// that any drift in keying convention surfaces as a missed lookup
	// (visible) rather than a silent double-write to two different keys.
	// A class-extent name that collides with an existing base relation
	// of the same arity overwrites the base entry — but in practice this
	// can't happen because base relations come from the schema and class
	// extents come from `class C { ... }` declarations whose names are
	// distinct; the desugarer's arity-1 class char-pred would only
	// collide with an arity-1 base relation, which the bridge does not
	// have.
	skipHeads := make(map[string]bool, len(cfg.materialisedExtents))
	for k, rel := range cfg.materialisedExtents {
		if rel == nil {
			continue
		}
		allRels[k] = rel
		skipHeads[k] = true
	}

	for si, stratum := range execPlan.Strata {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("evaluation cancelled before stratum %d: %w", si, err)
		}

		// Ensure head relations exist (skipping materialised class
		// extents — their relation is already in allRels and creating
		// a fresh empty NewRelation would clobber the materialised
		// tuples).
		for _, rule := range stratum.Rules {
			headName := rule.Head.Predicate
			headArity := len(rule.Head.Args)
			hk := relKey(headName, headArity)
			if skipHeads[hk] {
				continue
			}
			if _, ok := allRels[hk]; !ok {
				allRels[hk] = NewRelation(headName, headArity)
			}
		}

		// Filter out rules whose head is a materialised class extent
		// (P2a defensive guard; planner already strips these). We do
		// this per-stratum rather than once up front so the iteration
		// order over execPlan.Strata is preserved exactly.
		activeRules := stratum.Rules
		if len(skipHeads) > 0 {
			activeRules = make([]plan.PlannedRule, 0, len(stratum.Rules))
			for _, rule := range stratum.Rules {
				hk := relKey(rule.Head.Predicate, len(rule.Head.Args))
				if skipHeads[hk] {
					continue
				}
				activeRules = append(activeRules, rule)
			}
		}

		// Bootstrap: evaluate each rule once using full relations as source.
		var deltaRels map[string]*Relation
		if cfg.parallel {
			var perr error
			deltaRels, perr = parallelBootstrap(ctx, activeRules, allRels, cfg.maxBindingsPerRule)
			if perr != nil {
				if errors.Is(perr, context.Canceled) || errors.Is(perr, context.DeadlineExceeded) {
					return nil, fmt.Errorf("evaluation cancelled at stratum %d, %w", si, perr)
				}
				return nil, perr
			}
		} else {
			deltaRels = make(map[string]*Relation)
			for _, rule := range activeRules {
				headName := rule.Head.Predicate
				headArity := len(rule.Head.Args)
				hk := relKey(headName, headArity)
				headRel := allRels[hk]

				newTuples, rerr := Rule(ctx, rule, allRels, cfg.maxBindingsPerRule)
				if rerr != nil {
					// Add stratum context to ctx-cancellation errors so operators
					// can localise WHERE the cancellation hit, not just WHICH rule.
					if errors.Is(rerr, context.Canceled) || errors.Is(rerr, context.DeadlineExceeded) {
						return nil, fmt.Errorf("evaluation cancelled at stratum %d, bootstrap %w", si, rerr)
					}
					return nil, rerr
				}
				// Per-rule cancellation check (issue #81): a single rule's
				// Rule()/RuleDelta() call may itself be slow on large inputs, so
				// we honor ctx as soon as it returns rather than only at the
				// next iteration boundary.
				if cerr := ctx.Err(); cerr != nil {
					return nil, fmt.Errorf("evaluation cancelled at stratum %d, bootstrap rule %s: %w", si, headName, cerr)
				}
				for _, t := range newTuples {
					if headRel.Add(t) {
						dr, ok := deltaRels[hk]
						if !ok {
							dr = NewRelation(headName, headRel.Arity)
							deltaRels[hk] = dr
						}
						dr.Add(t)
					}
				}
			}
		}

		// Semi-naive fixpoint.
		iteration := 0
		for {
			if err := ctx.Err(); err != nil {
				return nil, fmt.Errorf("evaluation cancelled at stratum %d, iteration %d: %w", si, iteration, err)
			}

			// Check if any delta is non-empty. If the fixpoint has already
			// converged (no new tuples produced last iteration) we exit
			// cleanly — even if iteration count happens to equal the cap.
			anyDelta := false
			for _, dr := range deltaRels {
				if dr.Len() > 0 {
					anyDelta = true
					break
				}
			}
			if !anyDelta {
				break
			}

			// Check iteration limit. If hit and !allowPartial, return error
			// (issue #79). Compute the delta size and dominant rule first so
			// the caller has actionable diagnostics. The dominant rule is the
			// one whose delta is largest at the cap — the most likely culprit.
			if cfg.maxIterations > 0 && iteration >= cfg.maxIterations {
				totalDelta := 0
				dominantKey := ""
				dominantSize := -1
				for k, dr := range deltaRels {
					n := dr.Len()
					totalDelta += n
					if n > dominantSize {
						dominantSize = n
						dominantKey = k
					}
				}
				dominantName := dominantKey
				if dr, ok := deltaRels[dominantKey]; ok && dr != nil {
					dominantName = dr.Name
				}
				if !cfg.allowPartial {
					return nil, &IterationCapError{
						Stratum:       si,
						Rule:          dominantName,
						Cap:           cfg.maxIterations,
						LastDeltaSize: totalDelta,
					}
				}
				log.Printf("WARNING: stratum %d reached max iteration limit (%d); results may be incomplete (last delta size: %d, dominant rule: %s)", si, cfg.maxIterations, totalDelta, dominantName)
				break
			}
			iteration++

			if cfg.parallel {
				var perr error
				deltaRels, perr = parallelDelta(ctx, activeRules, allRels, deltaRels, cfg.maxBindingsPerRule)
				if perr != nil {
					if errors.Is(perr, context.Canceled) || errors.Is(perr, context.DeadlineExceeded) {
						return nil, fmt.Errorf("evaluation cancelled at stratum %d, iteration %d, %w", si, iteration, perr)
					}
					return nil, perr
				}
				// Per-iteration cancellation check on the parallel path —
				// parallelDelta returns the first per-rule error (which may
				// itself be a wrapped ctx error from a worker), but we also
				// re-check here in case workers all completed successfully on a
				// stale-but-not-yet-cancelled context boundary.
				if cerr := ctx.Err(); cerr != nil {
					return nil, fmt.Errorf("evaluation cancelled at stratum %d, iteration %d: %w", si, iteration, cerr)
				}
			} else {
				nextDelta := make(map[string]*Relation)
				for _, rule := range activeRules {
					headName := rule.Head.Predicate
					headArity := len(rule.Head.Args)
					hk := relKey(headName, headArity)
					headRel := allRels[hk]

					newTuples, rerr := RuleDelta(ctx, rule, allRels, deltaRels, cfg.maxBindingsPerRule)
					if rerr != nil {
						if errors.Is(rerr, context.Canceled) || errors.Is(rerr, context.DeadlineExceeded) {
							return nil, fmt.Errorf("evaluation cancelled at stratum %d, iteration %d, %w", si, iteration, rerr)
						}
						return nil, rerr
					}
					// Per-rule cancellation check (issue #81). A single
					// RuleDelta call on a large delta or wide join can take
					// many seconds; checking ctx after each rule (rather than
					// only at the top of the next iteration) is what makes
					// --timeout actually responsive on long strata.
					if cerr := ctx.Err(); cerr != nil {
						return nil, fmt.Errorf("evaluation cancelled at stratum %d, iteration %d, rule %s: %w", si, iteration, headName, cerr)
					}
					for _, t := range newTuples {
						if headRel.Add(t) {
							dr, ok := nextDelta[hk]
							if !ok {
								dr = NewRelation(headName, headRel.Arity)
								nextDelta[hk] = dr
							}
							dr.Add(t)
						}
					}
				}
				deltaRels = nextDelta
			}
		}

		// Evaluate aggregates after fixpoint.
		for _, agg := range stratum.Aggregates {
			resultRel, aerr := Aggregate(ctx, agg, allRels, cfg.maxBindingsPerRule)
			if aerr != nil {
				return nil, aerr
			}
			allRels[relKey(resultRel.Name, resultRel.Arity)] = resultRel
		}

		// Issue #88: refresh size hints with derived-relation cardinalities
		// produced in this stratum, then re-plan every subsequent stratum
		// (and the final query) so they pick join orders with real numbers
		// instead of defaultSizeHint=1000 for IDB predicates. Strata that
		// have already executed are not re-planned (their work is done).
		if cfg.sizeHints != nil {
			for _, rule := range stratum.Rules {
				name := rule.Head.Predicate
				arity := len(rule.Head.Args)
				if rel, ok := allRels[relKey(name, arity)]; ok && rel != nil {
					n := rel.Len()
					// Only update if the new value is larger or the key is
					// absent. We never shrink hints below an existing base
					// count for a predicate of the same name (defensive — a
					// bridge IDB and an EDB sharing a name would be a bug
					// upstream, but if it ever happens we don't want to
					// silently zero out the base count).
					if cur, exists := cfg.sizeHints[name]; !exists || n > cur {
						cfg.sizeHints[name] = n
					}
				}
			}
			// Re-plan strata si+1..end and the final query.
			//
			// P3a: thread the demand map captured at initial Plan() time so
			// demand-driven seed choice is preserved across the refresh.
			// Demand is intentionally NOT recomputed here — re-running
			// InferBackwardDemand mid-fixpoint would risk flapping (a hint
			// crossing SmallExtentThreshold mid-evaluation could add or
			// drop demand positions, producing a different plan than the
			// one Plan() returned). Stable demand across the fixpoint
			// matches the doc-stated property on RePlanStratumWithDemand.
			for j := si + 1; j < len(execPlan.Strata); j++ {
				plan.RePlanStratumWithDemand(&execPlan.Strata[j], cfg.sizeHints, execPlan.Demand)
			}
			if execPlan.Query != nil {
				plan.RePlanQuery(execPlan.Query, cfg.sizeHints)
			}
		}
	}

	// Evaluate the query.
	if execPlan.Query == nil {
		return &ResultSet{}, nil
	}

	return evalQuery(ctx, execPlan.Query, allRels, cfg.maxBindingsPerRule)
}

// evalQuery evaluates the planned query and returns a ResultSet.
func evalQuery(ctx context.Context, q *plan.PlannedQuery, allRels map[string]*Relation, maxBindings int) (*ResultSet, error) {
	initial := []binding{make(binding)}
	limits := &joinLimits{ctx: ctx, maxBindings: maxBindings, ruleName: "<query>"}
	bindings, err := evalJoinSteps(q.JoinOrder, allRels, initial, limits)
	if err != nil {
		return nil, err
	}

	rs := &ResultSet{}

	// Build column names from select terms.
	for i, sel := range q.Select {
		switch sv := sel.(type) {
		case interface{ Name() string }:
			rs.Columns = append(rs.Columns, sv.Name())
		default:
			rs.Columns = append(rs.Columns, fmt.Sprintf("col%d", i))
		}
	}
	if len(rs.Columns) == 0 {
		for i := range q.Select {
			rs.Columns = append(rs.Columns, fmt.Sprintf("col%d", i))
		}
	}

	seen := make(map[string]struct{})
	for _, b := range bindings {
		row := make([]Value, len(q.Select))
		valid := true
		for i, sel := range q.Select {
			v, ok := lookupTerm(sel, b)
			if !ok {
				valid = false
				break
			}
			row[i] = v
		}
		if valid {
			key := tupleKey(Tuple(row))
			if _, dup := seen[key]; !dup {
				seen[key] = struct{}{}
				rs.Rows = append(rs.Rows, row)
			}
		}
	}
	return rs, nil
}
