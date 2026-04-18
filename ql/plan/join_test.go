package plan_test

import (
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

// cmpLit creates a comparison literal.
func cmpLit(op string, left, right string) datalog.Literal {
	return datalog.Literal{
		Positive: true,
		Cmp: &datalog.Comparison{
			Op:    op,
			Left:  datalog.Var{Name: left},
			Right: datalog.Var{Name: right},
		},
	}
}

// aggLit creates an aggregate literal with a result variable.
func aggLit(fn, bodyPred, bodyVar, resultVar string) datalog.Literal {
	return datalog.Literal{
		Positive: true,
		Agg: &datalog.Aggregate{
			Func:      fn,
			Var:       bodyVar,
			TypeName:  "T",
			Body:      []datalog.Literal{posLit(bodyPred, bodyVar)},
			ResultVar: datalog.Var{Name: resultVar},
		},
	}
}

// TestJoinSingleLiteral: one literal → one step.
func TestJoinSingleLiteral(t *testing.T) {
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			rule(atom("P", "x"), posLit("A", "x")),
		},
	}
	ep, errs := plan.Plan(prog, nil)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(ep.Strata) == 0 || len(ep.Strata[0].Rules) == 0 {
		t.Fatal("no strata or rules")
	}
	r := ep.Strata[0].Rules[0]
	if len(r.JoinOrder) != 1 {
		t.Errorf("expected 1 join step, got %d", len(r.JoinOrder))
	}
	if r.JoinOrder[0].Literal.Atom.Predicate != "A" {
		t.Errorf("expected step for A, got %s", r.JoinOrder[0].Literal.Atom.Predicate)
	}
}

// TestJoinTwoLiteralsSharedVar: shared variable guides join order.
func TestJoinTwoLiteralsSharedVar(t *testing.T) {
	// P(x, y) :- A(x), B(x, y).
	// A and B both eligible first; no size hints → default 1000 each → A placed first (first eligible),
	// then B (x is now bound, one var bound).
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			{
				Head: atom("P", "x", "y"),
				Body: []datalog.Literal{posLit("A", "x"), posLit("B", "x", "y")},
			},
		},
	}
	ep, errs := plan.Plan(prog, nil)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	r := ep.Strata[0].Rules[0]
	if len(r.JoinOrder) != 2 {
		t.Errorf("expected 2 join steps, got %d", len(r.JoinOrder))
	}
}

// TestJoinThreeLiteralsGreedy: most-bound-first selection.
func TestJoinThreeLiteralsGreedy(t *testing.T) {
	// P(x, y, z) :- A(x), B(x, y), C(y, z).
	// Start: A eligible (0 bound vars), B eligible (0), C eligible (0).
	// Ties broken by size. All same size → A placed first (stable index).
	// After A: x bound. B has 1 bound var (x), C has 0.
	// B placed. After B: x, y bound. C has 1 bound var (y). C placed.
	hints := map[string]int{"A": 10, "B": 100, "C": 50}
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			{
				Head: atom("P", "x", "y", "z"),
				Body: []datalog.Literal{posLit("A", "x"), posLit("B", "x", "y"), posLit("C", "y", "z")},
			},
		},
	}
	ep, errs := plan.Plan(prog, hints)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	r := ep.Strata[0].Rules[0]
	if len(r.JoinOrder) != 3 {
		t.Fatalf("expected 3 join steps, got %d", len(r.JoinOrder))
	}
	// A should be first (smallest size among tie of 0-bound-var relations).
	if r.JoinOrder[0].Literal.Atom.Predicate != "A" {
		t.Errorf("expected A first (smallest size), got %s", r.JoinOrder[0].Literal.Atom.Predicate)
	}
	// B second.
	if r.JoinOrder[1].Literal.Atom.Predicate != "B" {
		t.Errorf("expected B second, got %s", r.JoinOrder[1].Literal.Atom.Predicate)
	}
	// C last.
	if r.JoinOrder[2].Literal.Atom.Predicate != "C" {
		t.Errorf("expected C last, got %s", r.JoinOrder[2].Literal.Atom.Predicate)
	}
}

// TestJoinSizeHintsTieBreaking: smaller relation placed first when bound counts tie.
func TestJoinSizeHintsTieBreaking(t *testing.T) {
	// P(x) :- BigRel(x), SmallRel(x).
	// Both have x unbound initially; SmallRel has smaller hint → placed first.
	hints := map[string]int{"BigRel": 9000, "SmallRel": 5}
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			{
				Head: atom("P", "x"),
				Body: []datalog.Literal{posLit("BigRel", "x"), posLit("SmallRel", "x")},
			},
		},
	}
	ep, errs := plan.Plan(prog, hints)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	r := ep.Strata[0].Rules[0]
	if len(r.JoinOrder) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(r.JoinOrder))
	}
	if r.JoinOrder[0].Literal.Atom.Predicate != "SmallRel" {
		t.Errorf("expected SmallRel first (smaller size), got %s", r.JoinOrder[0].Literal.Atom.Predicate)
	}
}

// TestJoinComparisonPlacedAfterOperandsBound.
func TestJoinComparisonPlacedAfterOperandsBound(t *testing.T) {
	// P(x, y) :- A(x), B(y), x < y.
	// Comparison x < y only eligible after both x and y are bound.
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			{
				Head: atom("P", "x", "y"),
				Body: []datalog.Literal{posLit("A", "x"), posLit("B", "y"), cmpLit("<", "x", "y")},
			},
		},
	}
	ep, errs := plan.Plan(prog, nil)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	r := ep.Strata[0].Rules[0]
	if len(r.JoinOrder) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(r.JoinOrder))
	}
	last := r.JoinOrder[2]
	if last.Literal.Cmp == nil {
		t.Errorf("expected comparison as last step, got atom %s", last.Literal.Atom.Predicate)
	}
}

// TestJoinOrderAggregateAfterPositives: aggregate literal is placed after positive literals.
func TestJoinOrderAggregateAfterPositives(t *testing.T) {
	// P(x, total) :- A(x), count<y : T(y)>(total).
	// The positive literal A(x) should come before the aggregate literal.
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			{
				Head: atom("P", "x", "total"),
				Body: []datalog.Literal{posLit("A", "x"), aggLit("count", "T", "y", "total")},
			},
		},
	}
	ep, errs := plan.Plan(prog, nil)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	r := ep.Strata[0].Rules[0]
	if len(r.JoinOrder) != 2 {
		t.Fatalf("expected 2 join steps, got %d", len(r.JoinOrder))
	}
	// Positive literal A should come first.
	if r.JoinOrder[0].Literal.Atom.Predicate != "A" {
		t.Errorf("expected A first, got %s", r.JoinOrder[0].Literal.Atom.Predicate)
	}
	// Aggregate literal should come second.
	if r.JoinOrder[1].Literal.Agg == nil {
		t.Errorf("expected aggregate as second step")
	}
}

// posLitConst creates a positive literal whose first arg is a string
// constant, with the rest being variables. Used by tiny-seed tests that need
// to model "literal grounded by a constant on an indexed col."
func posLitConst(pred string, constVal string, vars ...string) datalog.Literal {
	args := make([]datalog.Term, 0, 1+len(vars))
	args = append(args, datalog.StringConst{Value: constVal})
	for _, v := range vars {
		args = append(args, datalog.Var{Name: v})
	}
	return datalog.Literal{
		Positive: true,
		Atom:     datalog.Atom{Predicate: pred, Args: args},
	}
}

// TestJoinTinySeedWinsOverSharedVarLargerLiteral is the load-bearing
// mutation-killable test for issue #98.
//
// The heuristic must override standard greedy scoring. Standard scoring
// prefers more-bound-vars first (-boundCount), then smaller-relation. So the
// discriminating case is: a tiny known-size literal with NO shared vars
// (negBound=0) versus a larger literal that DOES have shared vars
// (negBound=-1). Standard picks the larger one because of the bound-count
// preference; the tiny-seed heuristic flips it.
//
// Body shape:
//
//	P(c, f, n) :- TinySeed(f), Big(f, n), TinyIDB(c).
//	    TinySeed: size 3   — wins slot 0 (smallest, both tiny by hint).
//	    Big:      size 200 — shares f with TinySeed (slot 1 candidate).
//	    TinyIDB:  size 7   — NO shared vars (slot 1 candidate).
//
// At slot 1 (after TinySeed bound f):
//   - Big:     (negBound=-1, size=200) — standard prefers (more bound vars).
//   - TinyIDB: (negBound=0,  size=7)   — standard puts last.
//
// Without the heuristic, slot 1 = Big and slot 2 = TinyIDB. With the
// heuristic, TinyIDB qualifies as tiny (sizeHint=7 ≤ tinySeedThreshold=32)
// and wins slot 1.
//
// Mutation kill: disabling the tiny-seed pass causes Big to win slot 1 and
// TinyIDB to land at slot 2 — `JoinOrder[1] == "TinyIDB"` then fails.
func TestJoinTinySeedWinsOverSharedVarLargerLiteral(t *testing.T) {
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			{
				Head: atom("P", "c", "f", "n"),
				Body: []datalog.Literal{
					posLit("TinySeed", "f"),
					posLit("Big", "f", "n"),
					posLit("TinyIDB", "c"),
				},
			},
		},
	}
	hints := map[string]int{"TinySeed": 3, "Big": 200, "TinyIDB": 7}
	ep, errs := plan.Plan(prog, hints)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	r := ep.Strata[0].Rules[0]
	if len(r.JoinOrder) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(r.JoinOrder))
	}
	got := []string{
		r.JoinOrder[0].Literal.Atom.Predicate,
		r.JoinOrder[1].Literal.Atom.Predicate,
		r.JoinOrder[2].Literal.Atom.Predicate,
	}
	// Slot 0: TinySeed (size 3) wins on both standard and heuristic.
	if got[0] != "TinySeed" {
		t.Fatalf("slot 0: expected TinySeed (size 3), got %s; full order: %v", got[0], got)
	}
	// Slot 1 (load-bearing): TinyIDB must win despite no shared vars.
	if got[1] != "TinyIDB" {
		t.Errorf("slot 1: expected TinyIDB (tiny-seed override beating Big's shared-var bonus), got %s; full order: %v",
			got[1], got)
	}
	// Slot 2: Big lands last.
	if got[2] != "Big" {
		t.Errorf("slot 2: expected Big, got %s; full order: %v", got[2], got)
	}
}

// TestJoinTinySeedNoSizeHintNoSharedVarNotPicked is the anti-false-positive
// guard from issue #98. A literal with NO sizeHint AND NO shared variables
// AND no constant args must NOT be classified as tiny — we have no evidence
// the output is small. If we treated unhinted literals as tiny by default
// we would invert the bug: instead of placing actually-large IDBs last we
// would place actually-large IDBs first.
//
// Setup: at slot 1, after Seed binds x, the candidates are:
//   - Big(x, y): shared var x, has known sizeHint=200 (NOT tiny by hint).
//     Standard scoring: (negBound=-1, size=200).
//   - UnknownIDB(y): NO shared vars with the prefix, NO sizeHint, NO
//     constants. Standard scoring: (negBound=0, size=1000).
//
// Standard scoring picks Big at slot 1 (better bound count). The tiny-seed
// heuristic must NOT classify UnknownIDB as tiny — otherwise it would
// override the bound-count preference and pick UnknownIDB (the canonical
// false positive). UnknownIDB must end up at slot 2.
//
// Mutation: replacing the no-hint branch of isTinySeed with `return true`
// (i.e. dropping the anti-false-positive guard) would classify UnknownIDB
// as tiny, and it would win slot 1 over Big — this assertion then fails.
func TestJoinTinySeedNoSizeHintNoSharedVarNotPicked(t *testing.T) {
	// P(x, y) :- Seed(x), Big(x, y), UnknownIDB(z).
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			{
				Head: atom("P", "x", "y"),
				Body: []datalog.Literal{
					posLit("Seed", "x"),
					posLit("Big", "x", "y"),
					posLit("UnknownIDB", "z"),
				},
			},
		},
	}
	// Seed=10 wins slot 0 (tiny). Big=200 (NOT tiny by hint).
	// UnknownIDB has no hint and no shared vars at slot 1.
	hints := map[string]int{"Seed": 10, "Big": 200}
	ep, errs := plan.Plan(prog, hints)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	r := ep.Strata[0].Rules[0]
	if len(r.JoinOrder) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(r.JoinOrder))
	}
	got := []string{
		r.JoinOrder[0].Literal.Atom.Predicate,
		r.JoinOrder[1].Literal.Atom.Predicate,
		r.JoinOrder[2].Literal.Atom.Predicate,
	}
	if got[0] != "Seed" {
		t.Fatalf("slot 0: expected Seed (size 10, tiny), got %s; full order: %v", got[0], got)
	}
	if got[1] != "Big" {
		t.Errorf("slot 1: expected Big (shared var, hint 200) — UnknownIDB must NOT be falsely classified tiny; got %s; full order: %v",
			got[1], got)
	}
	if got[2] != "UnknownIDB" {
		t.Errorf("slot 2: expected UnknownIDB last; got %s; full order: %v", got[2], got)
	}
}

// TestJoinTinySeedConstantArgWinsWithoutHint exercises the constant-arg
// branch of isTinySeed: a literal with a constant on an indexed col is
// strong evidence the output is tiny, even without any sizeHint.
//
// Setup: TypedThing("specific", x) competes against Big (size 100). Without
// the constant-arg branch, TypedThing scores as defaultSizeHint=1000 and
// loses to Big. With it, TypedThing wins as a tiny seed.
//
// Mutation kill: deleting the `hasConstantArg(lit)` clause from isTinySeed
// causes TypedThing to lose and this test fails.
func TestJoinTinySeedConstantArgWinsWithoutHint(t *testing.T) {
	// P(x) :- Big(x), TypedThing("specific", x).
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			{
				Head: atom("P", "x"),
				Body: []datalog.Literal{
					posLit("Big", "x"),
					posLitConst("TypedThing", "specific", "x"),
				},
			},
		},
	}
	hints := map[string]int{"Big": 100} // TypedThing intentionally unhinted.
	ep, errs := plan.Plan(prog, hints)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	r := ep.Strata[0].Rules[0]
	if r.JoinOrder[0].Literal.Atom.Predicate != "TypedThing" {
		t.Errorf("expected TypedThing first (constant-arg tiny seed), got %s",
			r.JoinOrder[0].Literal.Atom.Predicate)
	}
}

// TestJoinHintedTinyBeatsUnhintedConstantArg is the regression for issue
// #109: when both a hinted-tiny relation and an unhinted-but-constant-arg
// relation qualify as "tiny seed" candidates for the same slot, the one
// backed by an explicit size hint must win.
//
// Setup matches the issue body exactly:
//
//	P(x) :- Big("k", x), Tiny(x).
//	    Big:  no sizeHint, has a constant arg ("k"). Could in reality be
//	          an 8M-row EDB with a discriminative constant.
//	    Tiny: sizeHint=5 — genuinely tiny.
//
// Both qualify as tiny seeds (Big via constant-arg branch, Tiny via known
// hint ≤ threshold). Tiny must win slot 0; Big must land at slot 1.
//
// On main this passes only by accident: the tiebreak compares
// `sz < tinySize` and substitutes defaultSizeHint=1000 for unhinted, so
// hinted=5 < 1000 wins by coincidence. A future refactor changing that
// fallback (or an unhinted relation that really is huge) would regress.
// The fix: prefer hinted candidates explicitly. This test pins that
// preference even when the unhinted candidate's effective size would be
// smaller.
func TestJoinHintedTinyBeatsUnhintedConstantArg(t *testing.T) {
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			{
				Head: atom("P", "x"),
				Body: []datalog.Literal{
					posLitConst("Big", "k", "x"),
					posLit("Tiny", "x"),
				},
			},
		},
	}
	hints := map[string]int{"Tiny": 5} // Big intentionally unhinted.
	ep, errs := plan.Plan(prog, hints)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	r := ep.Strata[0].Rules[0]
	if len(r.JoinOrder) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(r.JoinOrder))
	}
	got := []string{
		r.JoinOrder[0].Literal.Atom.Predicate,
		r.JoinOrder[1].Literal.Atom.Predicate,
	}
	if got[0] != "Tiny" {
		t.Errorf("slot 0: expected Tiny (hinted=5) to beat Big (unhinted, constant arg); got %s; full order: %v",
			got[0], got)
	}
	if got[1] != "Big" {
		t.Errorf("slot 1: expected Big to land last; got %s; full order: %v", got[1], got)
	}
}

// TestJoinNegativeLiteralPlacedAfterVarsBound.
func TestJoinNegativeLiteralPlacedAfterVarsBound(t *testing.T) {
	// P(x) :- A(x), not B(x).
	// not B(x) only eligible after x is bound.
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			{
				Head: atom("P", "x"),
				Body: []datalog.Literal{posLit("A", "x"), negLit("B", "x")},
			},
		},
	}
	ep, errs := plan.Plan(prog, nil)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	r := ep.Strata[0].Rules[0]
	if len(r.JoinOrder) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(r.JoinOrder))
	}
	if r.JoinOrder[0].Literal.Atom.Predicate != "A" {
		t.Errorf("expected A first, got %s", r.JoinOrder[0].Literal.Atom.Predicate)
	}
	if r.JoinOrder[1].Literal.Positive {
		t.Errorf("expected last step to be negative literal")
	}
}
