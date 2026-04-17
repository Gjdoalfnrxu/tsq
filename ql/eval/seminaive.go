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

// joinLimits carries the per-rule binding cap and identifying context
// down through the join evaluation call chain. A nil receiver means no cap.
type joinLimits struct {
	maxBindings int    // 0 == unlimited
	ruleName    string // for error messages; may be empty (e.g. final query)
}

func (l *joinLimits) check(stepIndex, n int) error {
	if l == nil || l.maxBindings <= 0 {
		return nil
	}
	if n > l.maxBindings {
		return &BindingCapError{Rule: l.ruleName, StepIndex: stepIndex, Cap: l.maxBindings, Cardinality: n}
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

	for si, stratum := range execPlan.Strata {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("cancelled before stratum %d: %w", si, err)
		}

		// Ensure head relations exist.
		for _, rule := range stratum.Rules {
			headName := rule.Head.Predicate
			headArity := len(rule.Head.Args)
			hk := relKey(headName, headArity)
			if _, ok := allRels[hk]; !ok {
				allRels[hk] = NewRelation(headName, headArity)
			}
		}

		// Bootstrap: evaluate each rule once using full relations as source.
		var deltaRels map[string]*Relation
		if cfg.parallel {
			var perr error
			deltaRels, perr = parallelBootstrap(stratum.Rules, allRels, cfg.maxBindingsPerRule)
			if perr != nil {
				return nil, perr
			}
		} else {
			deltaRels = make(map[string]*Relation)
			for _, rule := range stratum.Rules {
				headName := rule.Head.Predicate
				headArity := len(rule.Head.Args)
				hk := relKey(headName, headArity)
				headRel := allRels[hk]

				newTuples, rerr := Rule(rule, allRels, cfg.maxBindingsPerRule)
				if rerr != nil {
					return nil, rerr
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
				return nil, fmt.Errorf("cancelled in fixpoint stratum %d: %w", si, err)
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
				deltaRels, perr = parallelDelta(stratum.Rules, allRels, deltaRels, cfg.maxBindingsPerRule)
				if perr != nil {
					return nil, perr
				}
			} else {
				nextDelta := make(map[string]*Relation)
				for _, rule := range stratum.Rules {
					headName := rule.Head.Predicate
					headArity := len(rule.Head.Args)
					hk := relKey(headName, headArity)
					headRel := allRels[hk]

					newTuples, rerr := RuleDelta(rule, allRels, deltaRels, cfg.maxBindingsPerRule)
					if rerr != nil {
						return nil, rerr
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
			resultRel, aerr := Aggregate(agg, allRels, cfg.maxBindingsPerRule)
			if aerr != nil {
				return nil, aerr
			}
			allRels[relKey(resultRel.Name, resultRel.Arity)] = resultRel
		}
	}

	// Evaluate the query.
	if execPlan.Query == nil {
		return &ResultSet{}, nil
	}

	return evalQuery(execPlan.Query, allRels, cfg.maxBindingsPerRule)
}

// evalQuery evaluates the planned query and returns a ResultSet.
func evalQuery(q *plan.PlannedQuery, allRels map[string]*Relation, maxBindings int) (*ResultSet, error) {
	initial := []binding{make(binding)}
	limits := &joinLimits{maxBindings: maxBindings, ruleName: "<query>"}
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
