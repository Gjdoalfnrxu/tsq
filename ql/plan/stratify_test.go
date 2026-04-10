package plan_test

import (
	"strings"
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

func atom(pred string, vars ...string) datalog.Atom {
	args := make([]datalog.Term, len(vars))
	for i, v := range vars {
		args[i] = datalog.Var{Name: v}
	}
	return datalog.Atom{Predicate: pred, Args: args}
}

func posLit(pred string, vars ...string) datalog.Literal {
	return datalog.Literal{Positive: true, Atom: atom(pred, vars...)}
}

func negLit(pred string, vars ...string) datalog.Literal {
	return datalog.Literal{Positive: false, Atom: atom(pred, vars...)}
}

func rule(head datalog.Atom, body ...datalog.Literal) datalog.Rule {
	return datalog.Rule{Head: head, Body: body}
}

// TestStratifySinglePredicate: one predicate with a single recursive rule → 1 stratum.
func TestStratifySinglePredicate(t *testing.T) {
	// R(x) :- R(x).  — positive self-recursion, fine for stratification.
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			rule(atom("R", "x"), posLit("R", "x")),
		},
	}
	ep, errs := plan.Plan(prog, nil)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(ep.Strata) != 1 {
		t.Errorf("expected 1 stratum, got %d", len(ep.Strata))
	}
}

// TestStratifyTwoPredicates: P uses Q → Q in earlier stratum.
func TestStratifyTwoPredicates(t *testing.T) {
	// Q(x) :- base(x).
	// P(x) :- Q(x).
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			rule(atom("Q", "x"), posLit("base", "x")),
			rule(atom("P", "x"), posLit("Q", "x")),
		},
	}
	ep, errs := plan.Plan(prog, nil)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	// P must come after Q — at least 2 strata, or same stratum but P's rule after Q's.
	// Since P depends on Q (positive), they can be in same stratum or P later.
	// We just check both rules appear.
	total := 0
	for _, s := range ep.Strata {
		total += len(s.Rules)
	}
	if total != 2 {
		t.Errorf("expected 2 rules total across strata, got %d", total)
	}
	// Q must not be in a later stratum than P.
	qStratum := -1
	pStratum := -1
	for i, s := range ep.Strata {
		for _, r := range s.Rules {
			if r.Head.Predicate == "Q" {
				qStratum = i
			}
			if r.Head.Predicate == "P" {
				pStratum = i
			}
		}
	}
	if qStratum > pStratum {
		t.Errorf("Q (stratum %d) should not be after P (stratum %d)", qStratum, pStratum)
	}
}

// TestStratifyMutualRecursion: P↔Q positive → same stratum.
func TestStratifyMutualRecursion(t *testing.T) {
	// P(x) :- Q(x).
	// Q(x) :- P(x).
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			rule(atom("P", "x"), posLit("Q", "x")),
			rule(atom("Q", "x"), posLit("P", "x")),
		},
	}
	ep, errs := plan.Plan(prog, nil)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	// Both in the same stratum.
	if len(ep.Strata) != 1 {
		t.Errorf("expected 1 stratum for mutual recursion, got %d", len(ep.Strata))
	}
}

// TestStratifyNegation: P uses not Q → Q's stratum < P's stratum.
func TestStratifyNegation(t *testing.T) {
	// Q(x) :- base(x).
	// P(x) :- base(x), not Q(x).
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			rule(atom("Q", "x"), posLit("base", "x")),
			rule(atom("P", "x"), posLit("base", "x"), negLit("Q", "x")),
		},
	}
	ep, errs := plan.Plan(prog, nil)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	qStratum := -1
	pStratum := -1
	for i, s := range ep.Strata {
		for _, r := range s.Rules {
			if r.Head.Predicate == "Q" {
				qStratum = i
			}
			if r.Head.Predicate == "P" {
				pStratum = i
			}
		}
	}
	if qStratum == -1 || pStratum == -1 {
		t.Fatalf("could not find Q (stratum %d) or P (stratum %d)", qStratum, pStratum)
	}
	if qStratum >= pStratum {
		t.Errorf("Q (stratum %d) must be strictly before P (stratum %d) due to negation", qStratum, pStratum)
	}
}

// TestStratifyNegationWithinSCC: P uses not P → error.
func TestStratifyNegationWithinSCC(t *testing.T) {
	// P(x) :- base(x), not P(x).  — recursive negation
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			rule(atom("P", "x"), posLit("base", "x"), negLit("P", "x")),
		},
	}
	_, errs := plan.Plan(prog, nil)
	if len(errs) == 0 {
		t.Fatal("expected stratification error for recursive negation, got none")
	}
	found := false
	for _, e := range errs {
		if strings.Contains(e.Error(), "unstratifiable") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'unstratifiable' error, got: %v", errs)
	}
}

// TestStratifyMutualNegation: P uses not Q and Q uses not P → error (unstratifiable).
func TestStratifyMutualNegation(t *testing.T) {
	// P(x) :- base(x), not Q(x).
	// Q(x) :- base(x), not P(x).
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			rule(atom("P", "x"), posLit("base", "x"), negLit("Q", "x")),
			rule(atom("Q", "x"), posLit("base", "x"), negLit("P", "x")),
		},
	}
	_, errs := plan.Plan(prog, nil)
	if len(errs) == 0 {
		t.Fatal("expected stratification error for mutual negation, got none")
	}
}

// TestStratifyAggregateTreatedLikeNegation: aggregate body predicate → strict stratum ordering.
func TestStratifyAggregateTreatedLikeNegation(t *testing.T) {
	// Q(x) :- base(x).
	// P(n) :- count(x | Q(x)) = n.
	aggLit := datalog.Literal{
		Positive: true,
		Agg: &datalog.Aggregate{
			Func:     "count",
			Var:      "x",
			TypeName: "T",
			Body:     []datalog.Literal{posLit("Q", "x")},
			ResultVar: datalog.Var{Name: "_v1"},
		},
	}
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			rule(atom("Q", "x"), posLit("base", "x")),
			{
				Head: atom("P", "_v1"),
				Body: []datalog.Literal{aggLit},
			},
		},
	}
	ep, errs := plan.Plan(prog, nil)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	qStratum := -1
	pStratum := -1
	for i, s := range ep.Strata {
		for _, r := range s.Rules {
			if r.Head.Predicate == "Q" {
				qStratum = i
			}
			if r.Head.Predicate == "P" {
				pStratum = i
			}
		}
	}
	if qStratum == -1 || pStratum == -1 {
		t.Fatalf("could not find Q or P in strata")
	}
	if qStratum >= pStratum {
		t.Errorf("Q (stratum %d) must be strictly before P (stratum %d) due to aggregate", qStratum, pStratum)
	}
}

// TestStratifyBaseFacts: predicates only in bodies → effectively stratum 0 (no rules for them).
func TestStratifyBaseFacts(t *testing.T) {
	// P(x) :- base(x).  — 'base' is a fact relation with no rules.
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			rule(atom("P", "x"), posLit("base", "x")),
		},
	}
	ep, errs := plan.Plan(prog, nil)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	// Only P has a rule — 'base' has no rules so it doesn't appear in any stratum.
	for _, s := range ep.Strata {
		for _, r := range s.Rules {
			if r.Head.Predicate == "base" {
				t.Error("'base' should not appear as a rule head in any stratum")
			}
		}
	}
}
