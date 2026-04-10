package eval

import (
	"context"
	"fmt"
	"log"

	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

// DefaultMaxIterations is the default maximum number of fixpoint iterations
// per stratum. If exceeded, a warning is logged but evaluation continues
// with the results computed so far.
const DefaultMaxIterations = 100

// ResultSet holds the query results.
type ResultSet struct {
	Columns []string // column names (from query select)
	Rows    [][]Value
}

// EvalOption configures the evaluator.
type EvalOption func(*evalConfig)

type evalConfig struct {
	maxIterations int
}

// WithMaxIterations sets the maximum number of fixpoint iterations per stratum.
// If the limit is reached, a warning is logged and evaluation proceeds with
// the results computed so far. A value of 0 means no limit.
func WithMaxIterations(n int) EvalOption {
	return func(c *evalConfig) { c.maxIterations = n }
}

// Evaluate executes an ExecutionPlan over base facts and returns results.
func Evaluate(ctx context.Context, execPlan *plan.ExecutionPlan, baseRels map[string]*Relation, opts ...EvalOption) (*ResultSet, error) {
	cfg := evalConfig{maxIterations: DefaultMaxIterations}
	for _, o := range opts {
		o(&cfg)
	}

	// allRels starts with base facts; derived relations are added as we go.
	allRels := make(map[string]*Relation, len(baseRels))
	for k, v := range baseRels {
		allRels[k] = v
	}

	for si, stratum := range execPlan.Strata {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("cancelled before stratum %d: %w", si, err)
		}

		// Ensure head relations exist.
		for _, rule := range stratum.Rules {
			headName := rule.Head.Predicate
			if _, ok := allRels[headName]; !ok {
				allRels[headName] = NewRelation(headName, len(rule.Head.Args))
			}
		}

		// Bootstrap: evaluate each rule once using full relations as source.
		deltaRels := make(map[string]*Relation)
		for _, rule := range stratum.Rules {
			headName := rule.Head.Predicate
			headRel := allRels[headName]

			newTuples := Rule(rule, allRels)
			for _, t := range newTuples {
				if headRel.Add(t) {
					dr, ok := deltaRels[headName]
					if !ok {
						dr = NewRelation(headName, headRel.Arity)
						deltaRels[headName] = dr
					}
					dr.Add(t)
				}
			}
		}

		// Semi-naive fixpoint.
		iteration := 0
		for {
			if err := ctx.Err(); err != nil {
				return nil, fmt.Errorf("cancelled in fixpoint stratum %d: %w", si, err)
			}

			// Check iteration limit.
			if cfg.maxIterations > 0 && iteration >= cfg.maxIterations {
				log.Printf("WARNING: stratum %d reached max iteration limit (%d); results may be incomplete", si, cfg.maxIterations)
				break
			}
			iteration++

			// Check if any delta is non-empty.
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

			nextDelta := make(map[string]*Relation)
			for _, rule := range stratum.Rules {
				headName := rule.Head.Predicate
				headRel := allRels[headName]

				newTuples := RuleDelta(rule, allRels, deltaRels)
				for _, t := range newTuples {
					if headRel.Add(t) {
						dr, ok := nextDelta[headName]
						if !ok {
							dr = NewRelation(headName, headRel.Arity)
							nextDelta[headName] = dr
						}
						dr.Add(t)
					}
				}
			}
			deltaRels = nextDelta
		}

		// Evaluate aggregates after fixpoint.
		for _, agg := range stratum.Aggregates {
			resultRel := Aggregate(agg, allRels)
			allRels[agg.ResultRelation] = resultRel
		}
	}

	// Evaluate the query.
	if execPlan.Query == nil {
		return &ResultSet{}, nil
	}

	return evalQuery(execPlan.Query, allRels), nil
}

// evalQuery evaluates the planned query and returns a ResultSet.
func evalQuery(q *plan.PlannedQuery, allRels map[string]*Relation) *ResultSet {
	initial := []binding{make(binding)}
	bindings := evalJoinSteps(q.JoinOrder, allRels, initial)

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
			rs.Rows = append(rs.Rows, row)
		}
	}
	return rs
}
