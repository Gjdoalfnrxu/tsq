package plan

import (
	"fmt"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
)

// ValidateRule checks safety conditions for a single Datalog rule.
// Every variable in the head must appear in at least one positive body literal.
// Every variable in a negative literal must appear in a positive literal in the same body.
func ValidateRule(rule datalog.Rule) []error {
	var errs []error

	// Collect variables bound by positive body literals and aggregate result vars.
	positiveVars := map[string]bool{}
	for _, lit := range rule.Body {
		if lit.Cmp != nil {
			continue
		}
		if lit.Agg != nil {
			// The aggregate result variable is bound by the aggregate.
			if lit.Agg.ResultVar.Name != "" {
				positiveVars[lit.Agg.ResultVar.Name] = true
			}
			continue
		}
		if lit.Positive {
			for _, arg := range lit.Atom.Args {
				if v, ok := arg.(datalog.Var); ok {
					positiveVars[v.Name] = true
				}
			}
		}
	}

	// Check head variables.
	for _, arg := range rule.Head.Args {
		if v, ok := arg.(datalog.Var); ok {
			if !positiveVars[v.Name] {
				errs = append(errs, fmt.Errorf("unsafe rule: head variable %q does not appear in any positive body literal (predicate %s)", v.Name, rule.Head.Predicate))
			}
		}
	}

	// Check negative literal variables.
	for _, lit := range rule.Body {
		if lit.Cmp != nil || lit.Agg != nil {
			continue
		}
		if !lit.Positive {
			for _, arg := range lit.Atom.Args {
				if v, ok := arg.(datalog.Var); ok {
					if !positiveVars[v.Name] {
						errs = append(errs, fmt.Errorf("unsafe rule: variable %q in negative literal %s does not appear in any positive body literal", v.Name, lit.Atom.Predicate))
					}
				}
			}
		}
	}

	return errs
}
