package plan

import (
	"fmt"
	"strings"

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

	// Treat synthetic arithmetic pseudo-variables (arith(...)) as bound —
	// they represent computed values from the desugarer.
	for _, lit := range rule.Body {
		if lit.Cmp == nil {
			continue
		}
		for _, side := range []datalog.Term{lit.Cmp.Left, lit.Cmp.Right} {
			if v, ok := side.(datalog.Var); ok && strings.HasPrefix(v.Name, "arith(") {
				positiveVars[v.Name] = true
			}
		}
	}

	// Propagate bindings through equality comparisons: if one side of an
	// equality is already bound, the other side becomes bound too. Iterate
	// until no new bindings are found (handles transitive chains like a=b, b=c).
	changed := true
	for changed {
		changed = false
		for _, lit := range rule.Body {
			if lit.Cmp == nil || lit.Cmp.Op != "=" {
				continue
			}
			lv, lok := lit.Cmp.Left.(datalog.Var)
			rv, rok := lit.Cmp.Right.(datalog.Var)
			// Also treat constants as "bound".
			lBound := (lok && positiveVars[lv.Name]) || !lok
			rBound := (rok && positiveVars[rv.Name]) || !rok
			if lBound && rok && !positiveVars[rv.Name] {
				positiveVars[rv.Name] = true
				changed = true
			}
			if rBound && lok && !positiveVars[lv.Name] {
				positiveVars[lv.Name] = true
				changed = true
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
