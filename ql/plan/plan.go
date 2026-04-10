// Package plan implements stratification and join ordering over Datalog programs.
package plan

import (
	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
)

// ExecutionPlan is the output of the planner.
type ExecutionPlan struct {
	Strata []Stratum
	Query  *PlannedQuery // nil if no select clause
}

// Stratum is a set of rules that can be evaluated together (same SCC or dependent group).
type Stratum struct {
	Rules      []PlannedRule
	Aggregates []PlannedAggregate
}

// PlannedRule is a rule with a determined join order.
type PlannedRule struct {
	Head      datalog.Atom
	JoinOrder []JoinStep
}

// JoinStep is one step in the join plan.
type JoinStep struct {
	Literal  datalog.Literal // the literal being joined (may be negative)
	JoinCols [][2]int        // pairs of (bodyVar, headVar) positions — for index building
	// IsFilter is true if all variables in Literal are already bound, meaning this step
	// acts as a filter rather than introducing new bindings.
	// Note: IsFilter=true on a negative literal (Literal.Positive==false) means anti-join,
	// not positive membership filter. Callers must check Literal.Positive to distinguish.
	IsFilter bool
}

// PlannedAggregate is an aggregate to evaluate after the stratum fixpoint.
type PlannedAggregate struct {
	ResultRelation string // name of the relation that holds aggregate results
	Agg            datalog.Aggregate
	GroupByVars    []datalog.Var // variables that form the group key
}

// PlannedQuery is the select clause plan.
type PlannedQuery struct {
	Select    []datalog.Term
	JoinOrder []JoinStep
}

// WithMagicSet applies the magic-set transformation to the program using
// the given query bindings, then plans the resulting program. queryBindings
// maps predicate names to bound argument positions.
func WithMagicSet(prog *datalog.Program, sizeHints map[string]int, queryBindings map[string][]int) (*ExecutionPlan, []error) {
	transformed := MagicSetTransform(prog, queryBindings)
	return Plan(transformed, sizeHints)
}

// Plan produces an ExecutionPlan from a Datalog program.
// sizeHints maps relation names to estimated tuple counts (for join ordering).
// Unknown relations default to 1000.
func Plan(prog *datalog.Program, sizeHints map[string]int) (*ExecutionPlan, []error) {
	// Validate all rules first.
	var errs []error
	for _, rule := range prog.Rules {
		if ruleErrs := ValidateRule(rule); len(ruleErrs) > 0 {
			errs = append(errs, ruleErrs...)
		}
	}
	if len(errs) > 0 {
		return nil, errs
	}

	// Build dependency graph and stratify.
	strata, stratErrs := stratify(prog.Rules)
	if len(stratErrs) > 0 {
		return nil, stratErrs
	}

	if sizeHints == nil {
		sizeHints = map[string]int{}
	}

	ep := &ExecutionPlan{}
	for _, stratum := range strata {
		ps := Stratum{}
		for _, rule := range stratum {
			order := orderJoins(rule.Body, sizeHints)
			ps.Rules = append(ps.Rules, PlannedRule{
				Head:      rule.Head,
				JoinOrder: order,
			})
			// Collect aggregates from rule body.
			for _, lit := range rule.Body {
				if lit.Agg != nil {
					ps.Aggregates = append(ps.Aggregates, PlannedAggregate{
						ResultRelation: lit.Agg.ResultVar.Name,
						Agg:            *lit.Agg,
						GroupByVars:    collectGroupByVars(rule, lit.Agg),
					})
				}
			}
		}
		ep.Strata = append(ep.Strata, ps)
	}

	// Plan the query.
	if prog.Query != nil {
		order := orderJoins(prog.Query.Body, sizeHints)
		ep.Query = &PlannedQuery{
			Select:    prog.Query.Select,
			JoinOrder: order,
		}
	}

	return ep, nil
}

// collectGroupByVars returns the head variables that are not the aggregate result variable.
func collectGroupByVars(rule datalog.Rule, agg *datalog.Aggregate) []datalog.Var {
	aggResultName := agg.ResultVar.Name
	var groupBy []datalog.Var
	seen := map[string]bool{}
	for _, arg := range rule.Head.Args {
		if v, ok := arg.(datalog.Var); ok {
			if v.Name != aggResultName && !seen[v.Name] {
				groupBy = append(groupBy, v)
				seen[v.Name] = true
			}
		}
	}
	return groupBy
}
