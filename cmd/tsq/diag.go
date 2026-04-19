package main

// Diagnostic helpers for the `tsq query` and `tsq check` commands. These
// support the --print-rel-sizes, --dump-plan, and --dump-rewritten-rules
// flags added to help debug planner/eval issues on production fact DBs
// (see the _disj_2 step-2 cap-hit investigation, branch
// investigate/disj2-step2-cap).
//
// Kept in a standalone file so the diagnostic surface can be extended
// (and tested) without continuing to bloat main.go.

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/Gjdoalfnrxu/tsq/extract/db"
	"github.com/Gjdoalfnrxu/tsq/extract/schema"
	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

// printRelSizes writes one line per non-empty fact relation in factDB to w,
// sorted by descending row count. Format: `rel <name> <rowCount>`.
//
// Iteration uses schema.Registry as the source of truth for which relations
// exist (matching buildSizeHints in main.go); db.DB does not expose its
// internal map. Relations with zero rows are suppressed so the output stays
// useful on extracted DBs that only populate a subset of the schema.
func printRelSizes(w io.Writer, factDB *db.DB) {
	type entry struct {
		name string
		rows int
	}
	entries := make([]entry, 0, len(schema.Registry))
	for _, def := range schema.Registry {
		n := factDB.Relation(def.Name).Tuples()
		if n == 0 {
			continue
		}
		entries = append(entries, entry{def.Name, n})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].rows != entries[j].rows {
			return entries[i].rows > entries[j].rows
		}
		return entries[i].name < entries[j].name
	})
	for _, e := range entries {
		fmt.Fprintf(w, "rel %s %d\n", e.name, e.rows)
	}
}

// dumpPlan writes a human-readable rendering of the planned join order for
// every rule (and the top-level select query, if any) to w. For each step we
// annotate each argument as bound (B) or free (F) at the point that step is
// reached. "Bound" means the variable was introduced by an earlier join step
// in the same rule; constants and wildcards are shown verbatim.
func dumpPlan(w io.Writer, ep *plan.ExecutionPlan) {
	if ep == nil {
		fmt.Fprintln(w, "[dump-plan] (nil execution plan)")
		return
	}
	fmt.Fprintln(w, "[dump-plan] planned join orders:")
	for si, st := range ep.Strata {
		fmt.Fprintf(w, "  stratum %d (%d rules)\n", si, len(st.Rules))
		for _, r := range st.Rules {
			fmt.Fprintf(w, "    rule %s :-\n", atomDump(r.Head))
			bound := map[string]bool{}
			for i, step := range r.JoinOrder {
				marker := "+"
				if step.IsFilter {
					marker = "f"
				}
				fmt.Fprintf(w, "      %s[%d] %s\n", marker, i, literalDump(step.Literal, bound))
				// After this step, every variable in the literal becomes bound
				// (positive joins introduce bindings; negative/anti-join
				// literals do not, but for the bound/free annotation on
				// subsequent steps the conservative thing is to mark them
				// bound only when the literal contributes bindings).
				if step.Literal.Positive && step.Literal.Atom.Predicate != "" {
					for _, t := range step.Literal.Atom.Args {
						if v, ok := t.(datalog.Var); ok {
							bound[v.Name] = true
						}
					}
				}
			}
		}
	}
	if ep.Query != nil {
		fmt.Fprintln(w, "  query:")
		bound := map[string]bool{}
		for i, step := range ep.Query.JoinOrder {
			marker := "+"
			if step.IsFilter {
				marker = "f"
			}
			fmt.Fprintf(w, "    %s[%d] %s\n", marker, i, literalDump(step.Literal, bound))
			if step.Literal.Positive && step.Literal.Atom.Predicate != "" {
				for _, t := range step.Literal.Atom.Args {
					if v, ok := t.(datalog.Var); ok {
						bound[v.Name] = true
					}
				}
			}
		}
		sels := make([]string, len(ep.Query.Select))
		for i, t := range ep.Query.Select {
			sels[i] = termDump(t, nil)
		}
		fmt.Fprintf(w, "    select %s\n", strings.Join(sels, ", "))
	}
}

// atomDump renders an atom without bound/free annotation (used for rule
// heads, where every variable is by construction free until the body binds it).
func atomDump(a datalog.Atom) string {
	parts := make([]string, len(a.Args))
	for i, t := range a.Args {
		parts[i] = termDump(t, nil)
	}
	return a.Predicate + "(" + strings.Join(parts, ", ") + ")"
}

// literalDump renders a literal with bound/free annotations on each variable
// argument given the bound-set entering this step.
func literalDump(lit datalog.Literal, bound map[string]bool) string {
	if lit.Cmp != nil {
		s := termDump(lit.Cmp.Left, bound) + " " + lit.Cmp.Op + " " + termDump(lit.Cmp.Right, bound)
		if !lit.Positive {
			return "not(" + s + ")"
		}
		return s
	}
	if lit.Agg != nil {
		// Aggregate sub-goals: dump the function and var only; the inner body
		// has its own join structure that the planner doesn't expose here.
		return fmt.Sprintf("%s(%s %s | ...)", lit.Agg.Func, lit.Agg.TypeName, lit.Agg.Var)
	}
	parts := make([]string, len(lit.Atom.Args))
	for i, t := range lit.Atom.Args {
		parts[i] = termDump(t, bound)
	}
	s := lit.Atom.Predicate + "(" + strings.Join(parts, ", ") + ")"
	if !lit.Positive {
		return "not " + s
	}
	return s
}

// termDump renders a term, annotating Vars with [B] (bound entering this
// step) or [F] (free) when bound is non-nil.
func termDump(t datalog.Term, bound map[string]bool) string {
	switch v := t.(type) {
	case datalog.Var:
		if bound == nil {
			return v.Name
		}
		if bound[v.Name] {
			return v.Name + "[B]"
		}
		return v.Name + "[F]"
	case datalog.IntConst:
		return fmt.Sprintf("%d", v.Value)
	case datalog.StringConst:
		return fmt.Sprintf("%q", v.Value)
	case datalog.Wildcard:
		return "_"
	default:
		return "?"
	}
}

// dumpRewrittenRules prints the program AFTER magic-set rewriting. If
// queryBindings is empty the magic-set transform is a no-op, so this falls
// back to printing the input program unchanged with a header noting that.
//
// Stringification reuses (*datalog.Program).String for fidelity with the
// rest of the toolchain.
func dumpRewrittenRules(w io.Writer, prog *datalog.Program, queryBindings map[string][]int) {
	if prog == nil {
		fmt.Fprintln(w, "[dump-rewritten-rules] (nil program)")
		return
	}
	if len(queryBindings) == 0 {
		fmt.Fprintln(w, "[dump-rewritten-rules] no inferable query bindings; printing input program unchanged:")
		fmt.Fprintln(w, prog.String())
		return
	}
	transformed := plan.MagicSetTransform(prog, queryBindings)
	fmt.Fprintf(w, "[dump-rewritten-rules] magic-set transform applied with bindings=%v\n", queryBindings)
	fmt.Fprintln(w, transformed.String())
}
