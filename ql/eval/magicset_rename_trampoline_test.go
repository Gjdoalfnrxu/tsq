package eval

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

// TestEval_RenameTrampolineMagicSet_AvoidsBindingCap is the
// evaluator-level proof for the PR #149 follow-up that addresses
// adversarial-review M3: synthesise base-relation EDB tuples large
// enough that the unrewritten `_disj_N` body trips the binding cap,
// plan with WithMagicSetAutoOpts (which now lifts demand through the
// pure-rename trampoline `mid(x,b) :- _disj_N(x,b)`), and assert the
// rule succeeds post-fix.
//
// Pre-fix behaviour (PR #149 alone): _disj_N's body is NOT rewritten
// (no safe seed could be constructed at the trampoline call site),
// the BigBase1⋈BigBase2 join blows the cap.
//
// Post-fix behaviour (this PR): magic_mid is seeded from the
// grandparent context (SmallExt(a) ⋈ Bridge(a,x,0)), magic__disj_N
// is propagated via MagicSetTransform.propagateBindings, and only
// the demand-restricted slice of BigBase1 is probed — well under the
// cap.
func TestEval_RenameTrampolineMagicSet_AvoidsBindingCap(t *testing.T) {
	// EDB sizing: BigBase1⋈BigBase2 unrestricted is ~bigN*bigN tuples;
	// SmallExt has 2 entries, Bridge restricts the demand to 2 x-values.
	const (
		bigN = 200             // BigBase1, BigBase2 each ~40k tuples
		cap  = 3 * bigN * bigN // 120k cap; pre-fix product would be ~bigN^3 = 8M
	)

	// SmallExt: {1, 2} (a-values, the demand source).
	smallExt := makeRelation("SmallExt", 1, IntVal{V: 1}, IntVal{V: 2})

	// Bridge(a, x, 0): a=1→x=10, a=2→x=20. Constant-bearing base
	// (third col is 0) — qualifies as a grounding atom in
	// bodyContextGroundedVars.
	bridge := makeRelation("Bridge", 3,
		IntVal{V: 1}, IntVal{V: 10}, IntVal{V: 0},
		IntVal{V: 2}, IntVal{V: 20}, IntVal{V: 0},
	)

	// BigBase1(x, m): for every x in [0,bigN), m in [0,bigN). bigN*bigN
	// pairs. With ALL x-values unrestricted the join product against
	// BigBase2(m, b) (also bigN*bigN) generates ~bigN^3 intermediate
	// states — well over the cap.
	bigBase1Vals := make([]Value, 0, bigN*bigN*2)
	for x := 0; x < bigN; x++ {
		for m := 0; m < bigN; m++ {
			bigBase1Vals = append(bigBase1Vals, IntVal{V: int64(x)}, IntVal{V: int64(m)})
		}
	}
	bigBase1 := makeRelation("BigBase1", 2, bigBase1Vals...)

	bigBase2Vals := make([]Value, 0, bigN*bigN*2)
	for m := 0; m < bigN; m++ {
		for b := 0; b < bigN; b++ {
			bigBase2Vals = append(bigBase2Vals, IntVal{V: int64(m)}, IntVal{V: int64(b)})
		}
	}
	bigBase2 := makeRelation("BigBase2", 2, bigBase2Vals...)

	baseRels := map[string]*Relation{
		"SmallExt": smallExt,
		"Bridge":   bridge,
		"BigBase1": bigBase1,
		"BigBase2": bigBase2,
	}

	rules := []datalog.Rule{
		{
			Head: datalog.Atom{Predicate: "top", Args: []datalog.Term{datalog.Var{Name: "b"}}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "SmallExt", Args: []datalog.Term{datalog.Var{Name: "a"}}}},
				{Positive: true, Atom: datalog.Atom{Predicate: "Bridge", Args: []datalog.Term{datalog.Var{Name: "a"}, datalog.Var{Name: "x"}, datalog.IntConst{Value: 0}}}},
				{Positive: true, Atom: datalog.Atom{Predicate: "mid", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "b"}}}},
			},
		},
		{
			Head: datalog.Atom{Predicate: "mid", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "b"}}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "_disj_N", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "b"}}}},
			},
		},
		{
			Head: datalog.Atom{Predicate: "_disj_N", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "b"}}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "BigBase1", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "m"}}}},
				{Positive: true, Atom: datalog.Atom{Predicate: "BigBase2", Args: []datalog.Term{datalog.Var{Name: "m"}, datalog.Var{Name: "b"}}}},
			},
		},
	}
	prog := &datalog.Program{
		Rules: rules,
		Query: &datalog.Query{
			Select: []datalog.Term{datalog.Var{Name: "b"}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "top", Args: []datalog.Term{datalog.Var{Name: "b"}}}},
			},
		},
	}
	hints := map[string]int{
		"SmallExt": smallExt.Len(),
		"Bridge":   bridge.Len(),
		"BigBase1": bigBase1.Len(),
		"BigBase2": bigBase2.Len(),
	}

	// Plan A: with magic-set rewrite (this PR's fix).
	ep, _, errs := plan.WithMagicSetAutoOpts(prog, hints, plan.MagicSetOptions{Strict: true})
	if len(errs) > 0 {
		t.Fatalf("WithMagicSetAutoOpts strict failed: %v", errs)
	}
	rs, err := Evaluate(context.Background(), ep, baseRels, WithMaxBindingsPerRule(cap))
	if err != nil {
		// If we get a binding-cap error here, the magic-set rewrite
		// did NOT bound _disj_N — the fix is not load-bearing.
		var capErr *BindingCapError
		if errors.As(err, &capErr) {
			t.Fatalf("magic-set-rewritten plan still tripped binding cap: %v", err)
		}
		// Some other failure path — surface it raw.
		if strings.Contains(err.Error(), "binding cap") {
			t.Fatalf("magic-set-rewritten plan still tripped binding cap (string match): %v", err)
		}
		t.Fatalf("Evaluate failed unexpectedly: %v", err)
	}
	if len(rs.Rows) == 0 {
		t.Fatalf("expected at least one `top` row from the demand-restricted join, got 0")
	}

	// Plan B (control): plain Plan with no magic-set rewrite. This
	// should hit the binding cap, proving the cap and EDB sizing are
	// load-bearing for the post-fix assertion above.
	plainEP, errs := plan.Plan(prog, hints)
	if len(errs) > 0 {
		t.Fatalf("plain Plan failed: %v", errs)
	}
	_, plainErr := Evaluate(context.Background(), plainEP, baseRels, WithMaxBindingsPerRule(cap))
	if plainErr == nil {
		t.Fatalf("control: expected plain plan to trip binding cap on _disj_N body, but it succeeded — EDB sizing is too small to demonstrate the fix")
	}
	var capErr *BindingCapError
	if !errors.As(plainErr, &capErr) && !strings.Contains(plainErr.Error(), "binding cap") {
		t.Fatalf("control: expected BindingCapError from plain plan, got %v", plainErr)
	}
}
