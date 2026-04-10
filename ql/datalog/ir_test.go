package datalog_test

import (
	"strings"
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
)

func TestProgramStringEmpty(t *testing.T) {
	p := &datalog.Program{}
	got := p.String()
	if got != "" {
		t.Errorf("empty program String() = %q, want %q", got, "")
	}
}

func TestProgramStringRule(t *testing.T) {
	p := &datalog.Program{
		Rules: []datalog.Rule{
			{
				Head: datalog.Atom{
					Predicate: "Foo",
					Args:      []datalog.Term{datalog.Var{Name: "this"}},
				},
				Body: []datalog.Literal{
					{Positive: true, Atom: datalog.Atom{
						Predicate: "Bar",
						Args:      []datalog.Term{datalog.Var{Name: "this"}},
					}},
				},
			},
		},
	}
	got := p.String()
	if !strings.Contains(got, "Foo(this)") {
		t.Errorf("missing head: %q", got)
	}
	if !strings.Contains(got, "Bar(this)") {
		t.Errorf("missing body: %q", got)
	}
	if !strings.Contains(got, ":-") {
		t.Errorf("missing :- separator: %q", got)
	}
}

func TestProgramStringNegation(t *testing.T) {
	p := &datalog.Program{
		Rules: []datalog.Rule{
			{
				Head: datalog.Atom{Predicate: "Foo", Args: []datalog.Term{datalog.Var{Name: "this"}}},
				Body: []datalog.Literal{
					{Positive: false, Atom: datalog.Atom{
						Predicate: "Bar",
						Args:      []datalog.Term{datalog.Var{Name: "this"}},
					}},
				},
			},
		},
	}
	got := p.String()
	if !strings.Contains(got, "not Bar(this)") {
		t.Errorf("missing negation: %q", got)
	}
}

func TestProgramStringComparison(t *testing.T) {
	p := &datalog.Program{
		Rules: []datalog.Rule{
			{
				Head: datalog.Atom{Predicate: "P", Args: []datalog.Term{datalog.Var{Name: "x"}}},
				Body: []datalog.Literal{
					{Positive: true, Cmp: &datalog.Comparison{
						Op:    "=",
						Left:  datalog.Var{Name: "x"},
						Right: datalog.IntConst{Value: 42},
					}},
				},
			},
		},
	}
	got := p.String()
	if !strings.Contains(got, "x = 42") {
		t.Errorf("missing comparison: %q", got)
	}
}

func TestProgramStringQuery(t *testing.T) {
	p := &datalog.Program{
		Query: &datalog.Query{
			Select: []datalog.Term{datalog.Var{Name: "x"}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{
					Predicate: "Foo",
					Args:      []datalog.Term{datalog.Var{Name: "x"}},
				}},
			},
		},
	}
	got := p.String()
	if !strings.Contains(got, "?-") {
		t.Errorf("missing query marker: %q", got)
	}
	if !strings.Contains(got, "select x") {
		t.Errorf("missing select: %q", got)
	}
}

func TestTermStringTypes(t *testing.T) {
	tests := []struct {
		name string
		prog *datalog.Program
		want string
	}{
		{
			"IntConst",
			&datalog.Program{Rules: []datalog.Rule{{
				Head: datalog.Atom{Predicate: "P", Args: []datalog.Term{datalog.IntConst{Value: 7}}},
			}}},
			"7",
		},
		{
			"StringConst",
			&datalog.Program{Rules: []datalog.Rule{{
				Head: datalog.Atom{Predicate: "P", Args: []datalog.Term{datalog.StringConst{Value: "hi"}}},
			}}},
			`"hi"`,
		},
		{
			"Wildcard",
			&datalog.Program{Rules: []datalog.Rule{{
				Head: datalog.Atom{Predicate: "P", Args: []datalog.Term{datalog.Wildcard{}}},
			}}},
			"_",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.prog.String()
			if !strings.Contains(got, tt.want) {
				t.Errorf("String() = %q, want to contain %q", got, tt.want)
			}
		})
	}
}

func TestRuleNoBody(t *testing.T) {
	p := &datalog.Program{
		Rules: []datalog.Rule{
			{Head: datalog.Atom{Predicate: "Fact", Args: []datalog.Term{datalog.IntConst{Value: 1}}}},
		},
	}
	got := p.String()
	// A rule with no body should end with a period, no ":-".
	if strings.Contains(got, ":-") {
		t.Errorf("no-body rule should not contain :-: %q", got)
	}
	if !strings.Contains(got, "Fact(1).") {
		t.Errorf("wrong output: %q", got)
	}
}

func TestAggregateString(t *testing.T) {
	p := &datalog.Program{
		Rules: []datalog.Rule{
			{
				Head: datalog.Atom{Predicate: "Count", Args: []datalog.Term{datalog.Var{Name: "n"}}},
				Body: []datalog.Literal{
					{
						Positive: true,
						Agg: &datalog.Aggregate{
							Func:     "count",
							Var:      "x",
							TypeName: "Foo",
							Body: []datalog.Literal{
								{Positive: true, Atom: datalog.Atom{
									Predicate: "Foo",
									Args:      []datalog.Term{datalog.Var{Name: "x"}},
								}},
							},
						},
					},
				},
			},
		},
	}
	got := p.String()
	if !strings.Contains(got, "count") {
		t.Errorf("missing aggregate func: %q", got)
	}
}
