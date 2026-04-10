// Package plan — magic-set transformation for demand-driven evaluation.
package plan

import (
	"fmt"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
)

// MagicSetTransform rewrites a Datalog program so that only relevant tuples
// are derived. queryBindings maps predicate names to lists of argument positions
// that are bound in the top-level query (e.g., {"Path": {0}} means arg 0 of
// Path is bound at query time).
//
// For each IDB rule with bound arguments, the transformation:
//  1. Creates a "magic_<pred>" seed predicate carrying the bound arguments.
//  2. Rewrites each rule for <pred> to include magic_<pred> as an extra body literal.
//  3. Adds propagation rules so magic predicates flow through the dependency chain.
//
// The resulting program can be evaluated by the same semi-naive evaluator — magic
// predicates simply prune irrelevant tuples early.
func MagicSetTransform(prog *datalog.Program, queryBindings map[string][]int) *datalog.Program {
	if len(queryBindings) == 0 {
		return prog
	}

	// Identify IDB predicates (those that appear as rule heads).
	idbPreds := make(map[string]bool)
	for _, rule := range prog.Rules {
		idbPreds[rule.Head.Predicate] = true
	}

	// Only transform predicates that are IDB and have bindings.
	relevantBindings := make(map[string][]int)
	for pred, cols := range queryBindings {
		if idbPreds[pred] && len(cols) > 0 {
			relevantBindings[pred] = cols
		}
	}

	if len(relevantBindings) == 0 {
		return prog
	}

	// Propagate binding info: if rule R derives pred P with bound cols,
	// and R's body references pred Q, determine which cols of Q become bound
	// through variable flow.
	allBindings := propagateBindings(prog.Rules, relevantBindings)

	var newRules []datalog.Rule

	// Generate magic seed facts from query bindings and propagation rules.
	for pred, boundCols := range allBindings {
		magicPred := magicName(pred)

		// For each rule that derives `pred`, add:
		// 1. A modified rule with magic_<pred>(...) in the body.
		// 2. Propagation rules for magic predicates of body atoms.
		for _, rule := range prog.Rules {
			if rule.Head.Predicate != pred {
				continue
			}

			// Build the magic atom for the head: magic_pred(bound_args_from_head).
			magicArgs := make([]datalog.Term, len(boundCols))
			for i, col := range boundCols {
				if col < len(rule.Head.Args) {
					magicArgs[i] = rule.Head.Args[col]
				} else {
					magicArgs[i] = datalog.Wildcard{}
				}
			}
			magicLit := datalog.Literal{
				Positive: true,
				Atom:     datalog.Atom{Predicate: magicPred, Args: magicArgs},
			}

			// Rewritten rule: add magic literal as first body atom.
			rewrittenBody := make([]datalog.Literal, 0, len(rule.Body)+1)
			rewrittenBody = append(rewrittenBody, magicLit)
			rewrittenBody = append(rewrittenBody, rule.Body...)
			newRules = append(newRules, datalog.Rule{
				Head: rule.Head,
				Body: rewrittenBody,
			})

			// Generate magic propagation rules for IDB body predicates.
			// For each body literal that references an IDB pred with bindings,
			// generate: magic_<bodyPred>(bound_args) :- magic_<pred>(head_bound_args), <preceding body lits>.
			for bi, lit := range rule.Body {
				if lit.Cmp != nil || lit.Agg != nil || !lit.Positive {
					continue
				}
				bodyPred := lit.Atom.Predicate
				bodyCols, ok := allBindings[bodyPred]
				if !ok {
					continue
				}

				// Build the magic propagation rule.
				propMagicArgs := make([]datalog.Term, len(bodyCols))
				for i, col := range bodyCols {
					if col < len(lit.Atom.Args) {
						propMagicArgs[i] = lit.Atom.Args[col]
					} else {
						propMagicArgs[i] = datalog.Wildcard{}
					}
				}

				propHead := datalog.Atom{
					Predicate: magicName(bodyPred),
					Args:      propMagicArgs,
				}

				// Body: magic_<pred>(head_bound) + all body literals before this one.
				propBody := make([]datalog.Literal, 0, bi+2)
				propBody = append(propBody, magicLit)
				propBody = append(propBody, rule.Body[:bi]...)

				// Only add if the propagation rule is safe (all head vars bound in body).
				if isSafe(propHead, propBody) {
					newRules = append(newRules, datalog.Rule{
						Head: propHead,
						Body: propBody,
					})
				}
			}
		}
	}

	// Add rules for predicates that are NOT transformed.
	for _, rule := range prog.Rules {
		if _, transformed := allBindings[rule.Head.Predicate]; !transformed {
			newRules = append(newRules, rule)
		}
	}

	return &datalog.Program{
		Rules: newRules,
		Query: prog.Query,
	}
}

// magicName returns the magic predicate name for a given predicate.
func magicName(pred string) string {
	return fmt.Sprintf("magic_%s", pred)
}

// propagateBindings computes the transitive closure of binding information
// through the rule dependency graph.
func propagateBindings(rules []datalog.Rule, initial map[string][]int) map[string][]int {
	bindings := make(map[string][]int)
	for k, v := range initial {
		bindings[k] = v
	}

	// Fixed-point iteration to propagate bindings.
	changed := true
	for changed {
		changed = false
		for _, rule := range rules {
			headPred := rule.Head.Predicate
			headCols, ok := bindings[headPred]
			if !ok {
				continue
			}

			// Map bound head columns to variable names.
			boundVars := make(map[string]bool)
			for _, col := range headCols {
				if col < len(rule.Head.Args) {
					if v, isVar := rule.Head.Args[col].(datalog.Var); isVar {
						boundVars[v.Name] = true
					}
				}
			}

			// Propagate through body: variables bound by the head + preceding literals.
			currentBound := make(map[string]bool)
			for k, v := range boundVars {
				currentBound[k] = v
			}

			for _, lit := range rule.Body {
				if lit.Cmp != nil || lit.Agg != nil || !lit.Positive {
					// Constants/comparisons can bind variables too, but skip for simplicity.
					continue
				}

				bodyPred := lit.Atom.Predicate
				// Determine which columns of bodyPred are bound.
				var boundBodyCols []int
				for i, arg := range lit.Atom.Args {
					switch a := arg.(type) {
					case datalog.Var:
						if currentBound[a.Name] {
							boundBodyCols = append(boundBodyCols, i)
						}
					case datalog.IntConst, datalog.StringConst:
						boundBodyCols = append(boundBodyCols, i)
					}
				}

				// If this body pred has new bindings, record them.
				if len(boundBodyCols) > 0 {
					existing, exists := bindings[bodyPred]
					if !exists || !sameIntSlice(existing, boundBodyCols) {
						// Merge: take the intersection if both exist, or set if new.
						if !exists {
							bindings[bodyPred] = boundBodyCols
							changed = true
						}
					}
				}

				// All variables in this literal become bound for subsequent literals.
				for _, arg := range lit.Atom.Args {
					if v, isVar := arg.(datalog.Var); isVar && v.Name != "_" {
						currentBound[v.Name] = true
					}
				}
			}
		}
	}

	return bindings
}

// isSafe checks if all variables in the head appear in the body.
func isSafe(head datalog.Atom, body []datalog.Literal) bool {
	bodyVars := make(map[string]bool)
	for _, lit := range body {
		if lit.Cmp != nil {
			collectTermVars(lit.Cmp.Left, bodyVars)
			collectTermVars(lit.Cmp.Right, bodyVars)
			continue
		}
		if !lit.Positive {
			continue // negated literals don't bind
		}
		for _, arg := range lit.Atom.Args {
			collectTermVars(arg, bodyVars)
		}
	}

	for _, arg := range head.Args {
		if v, ok := arg.(datalog.Var); ok && v.Name != "_" {
			if !bodyVars[v.Name] {
				return false
			}
		}
	}
	return true
}

func collectTermVars(t datalog.Term, vars map[string]bool) {
	if v, ok := t.(datalog.Var); ok && v.Name != "_" {
		vars[v.Name] = true
	}
}

func sameIntSlice(a, b []int) bool {
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
