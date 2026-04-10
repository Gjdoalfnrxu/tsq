package plan_test

import (
	"strings"
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

// TestValidateUnsafeHeadVariable: variable in head not in positive body → error.
func TestValidateUnsafeHeadVariable(t *testing.T) {
	// P(x, y) :- A(x).  — y is in head but not in any positive body literal.
	r := datalog.Rule{
		Head: atom("P", "x", "y"),
		Body: []datalog.Literal{posLit("A", "x")},
	}
	errs := plan.ValidateRule(r)
	if len(errs) == 0 {
		t.Fatal("expected error for unsafe head variable, got none")
	}
	found := false
	for _, e := range errs {
		if strings.Contains(e.Error(), "y") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected error mentioning 'y', got: %v", errs)
	}
}

// TestValidateUnsafeNegationVariable: variable in negative literal not in positive body → error.
func TestValidateUnsafeNegationVariable(t *testing.T) {
	// P(x) :- A(x), not B(z).  — z only appears in negation.
	r := datalog.Rule{
		Head: atom("P", "x"),
		Body: []datalog.Literal{posLit("A", "x"), negLit("B", "z")},
	}
	errs := plan.ValidateRule(r)
	if len(errs) == 0 {
		t.Fatal("expected error for unsafe negation variable, got none")
	}
	found := false
	for _, e := range errs {
		if strings.Contains(e.Error(), "z") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected error mentioning 'z', got: %v", errs)
	}
}

// TestValidateValidRule: properly safe rule → no errors.
func TestValidateValidRule(t *testing.T) {
	// P(x, y) :- A(x, y), not B(x).
	r := datalog.Rule{
		Head: atom("P", "x", "y"),
		Body: []datalog.Literal{
			{Positive: true, Atom: atom("A", "x", "y")},
			negLit("B", "x"),
		},
	}
	errs := plan.ValidateRule(r)
	if len(errs) != 0 {
		t.Errorf("expected no errors for safe rule, got: %v", errs)
	}
}

// TestValidateFactRule: head with no body → head vars must appear in body (empty body → error if vars present).
func TestValidateFactRuleNoBody(t *testing.T) {
	// P(x) :- (no body) — x is unsafe.
	r := datalog.Rule{
		Head: atom("P", "x"),
		Body: nil,
	}
	errs := plan.ValidateRule(r)
	if len(errs) == 0 {
		t.Fatal("expected error for head variable with no body, got none")
	}
}

// TestValidateGroundFact: P() :- (no body) — nullary head, no vars → valid.
func TestValidateGroundFact(t *testing.T) {
	r := datalog.Rule{
		Head: datalog.Atom{Predicate: "P", Args: nil},
		Body: nil,
	}
	errs := plan.ValidateRule(r)
	if len(errs) != 0 {
		t.Errorf("expected no errors for ground fact, got: %v", errs)
	}
}
