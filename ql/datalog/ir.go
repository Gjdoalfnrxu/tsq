// Package datalog defines the intermediate representation produced by the desugarer.
package datalog

import (
	"fmt"
	"strings"
)

// Program is a complete Datalog program.
type Program struct {
	Rules []Rule
	Query *Query
}

// Rule is a Datalog rule: Head :- Body.
type Rule struct {
	Head Atom
	Body []Literal
}

// Query is the top-level query (from a QL select clause).
type Query struct {
	Select []Term // expressions to output
	Body   []Literal
}

// Atom is a predicate application.
type Atom struct {
	Predicate string
	Args      []Term
}

// Literal is a possibly-negated atom or comparison.
type Literal struct {
	Positive bool
	Atom     Atom
	Cmp      *Comparison // non-nil for comparison literals
	Agg      *Aggregate  // non-nil for aggregate sub-goals
}

// Comparison is a comparison constraint.
type Comparison struct {
	Op    string // "=", "!=", "<", ">", "<=", ">="
	Left  Term
	Right Term
}

// Aggregate is an aggregation sub-goal.
type Aggregate struct {
	Func      string // "count", "min", "max", "sum", "avg"
	Var       string // the aggregated variable
	TypeName  string // the declared type of the var
	Body      []Literal
	Expr      Term // what is aggregated (for min/max/sum/avg)
	ResultVar Var  // the fresh variable that holds the aggregate result
}

// Term is a Datalog term (variable, constant, or wildcard).
type Term interface{ termNode() }

type Var struct{ Name string }
type IntConst struct{ Value int64 }
type StringConst struct{ Value string }
type Wildcard struct{}

func (Var) termNode()         {}
func (IntConst) termNode()    {}
func (StringConst) termNode() {}
func (Wildcard) termNode()    {}

// String returns a readable Datalog representation for debugging.
func (p *Program) String() string {
	var b strings.Builder
	for _, r := range p.Rules {
		b.WriteString(ruleString(r))
		b.WriteByte('\n')
	}
	if p.Query != nil {
		b.WriteString(queryString(p.Query))
		b.WriteByte('\n')
	}
	return b.String()
}

func ruleString(r Rule) string {
	head := atomString(r.Head)
	if len(r.Body) == 0 {
		return head + "."
	}
	parts := make([]string, len(r.Body))
	for i, lit := range r.Body {
		parts[i] = literalString(lit)
	}
	return head + " :- " + strings.Join(parts, ", ") + "."
}

func queryString(q *Query) string {
	parts := make([]string, len(q.Body))
	for i, lit := range q.Body {
		parts[i] = literalString(lit)
	}
	selParts := make([]string, len(q.Select))
	for i, t := range q.Select {
		selParts[i] = termString(t)
	}
	body := strings.Join(parts, ", ")
	sel := strings.Join(selParts, ", ")
	if body == "" {
		return "?- select " + sel + "."
	}
	return "?- " + body + " select " + sel + "."
}

func atomString(a Atom) string {
	if len(a.Args) == 0 {
		return a.Predicate + "()"
	}
	parts := make([]string, len(a.Args))
	for i, t := range a.Args {
		parts[i] = termString(t)
	}
	return a.Predicate + "(" + strings.Join(parts, ", ") + ")"
}

func literalString(lit Literal) string {
	if lit.Cmp != nil {
		s := termString(lit.Cmp.Left) + " " + lit.Cmp.Op + " " + termString(lit.Cmp.Right)
		if !lit.Positive {
			return "not(" + s + ")"
		}
		return s
	}
	if lit.Agg != nil {
		return aggregateString(lit.Agg)
	}
	s := atomString(lit.Atom)
	if !lit.Positive {
		return "not " + s
	}
	return s
}

func aggregateString(a *Aggregate) string {
	parts := make([]string, len(a.Body))
	for i, lit := range a.Body {
		parts[i] = literalString(lit)
	}
	body := strings.Join(parts, ", ")
	result := ""
	if a.ResultVar.Name != "" {
		result = a.ResultVar.Name + " = "
	}
	if a.Expr != nil {
		return fmt.Sprintf("%s%s(%s %s | %s | %s)", result, a.Func, a.TypeName, a.Var, body, termString(a.Expr))
	}
	return fmt.Sprintf("%s%s(%s %s | %s)", result, a.Func, a.TypeName, a.Var, body)
}

func termString(t Term) string {
	switch v := t.(type) {
	case Var:
		return v.Name
	case IntConst:
		return fmt.Sprintf("%d", v.Value)
	case StringConst:
		return fmt.Sprintf("%q", v.Value)
	case Wildcard:
		return "_"
	default:
		return "?"
	}
}
