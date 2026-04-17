package plan

import (
	"strings"
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
)

// helper: build a minimal program with a transitive-closure-style IDB (Path)
// and a single base relation (Edge), then attach the supplied query body.
func progPathClosure(queryBody []datalog.Literal, selectVars ...string) *datalog.Program {
	rules := []datalog.Rule{
		{
			Head: datalog.Atom{Predicate: "Path", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "y"}}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "Edge", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "y"}}}},
			},
		},
		{
			Head: datalog.Atom{Predicate: "Path", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "z"}}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "Edge", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "y"}}}},
				{Positive: true, Atom: datalog.Atom{Predicate: "Path", Args: []datalog.Term{datalog.Var{Name: "y"}, datalog.Var{Name: "z"}}}},
			},
		},
	}
	sel := make([]datalog.Term, len(selectVars))
	for i, v := range selectVars {
		sel[i] = datalog.Var{Name: v}
	}
	return &datalog.Program{
		Rules: rules,
		Query: &datalog.Query{Select: sel, Body: queryBody},
	}
}

func TestInferQueryBindings_ConstantInIDBLiteral(t *testing.T) {
	// from Path(1, b)  — first column is a constant, so binding {0} must be inferred.
	prog := progPathClosure([]datalog.Literal{
		{Positive: true, Atom: datalog.Atom{Predicate: "Path", Args: []datalog.Term{datalog.IntConst{Value: 1}, datalog.Var{Name: "b"}}}},
	}, "b")
	idb := IDBPredicates(prog)
	inf := InferQueryBindings(prog, idb)

	cols, ok := inf.Bindings["Path"]
	if !ok {
		t.Fatalf("expected Path in bindings, got %v", inf.Bindings)
	}
	if len(cols) != 1 || cols[0] != 0 {
		t.Fatalf("expected Path bound at col 0, got %v", cols)
	}
	if len(inf.SeedRules) != 1 {
		t.Fatalf("expected 1 seed rule, got %d", len(inf.SeedRules))
	}
	if inf.SeedRules[0].Head.Predicate != "magic_Path" {
		t.Fatalf("expected magic_Path seed head, got %q", inf.SeedRules[0].Head.Predicate)
	}
}

func TestInferQueryBindings_EqualityToConst(t *testing.T) {
	// from Path(a, b), where a = 1
	prog := progPathClosure([]datalog.Literal{
		{Positive: true, Atom: datalog.Atom{Predicate: "Path", Args: []datalog.Term{datalog.Var{Name: "a"}, datalog.Var{Name: "b"}}}},
		{Positive: true, Cmp: &datalog.Comparison{Op: "=", Left: datalog.Var{Name: "a"}, Right: datalog.IntConst{Value: 1}}},
	}, "b")
	idb := IDBPredicates(prog)
	inf := InferQueryBindings(prog, idb)

	cols, ok := inf.Bindings["Path"]
	if !ok || len(cols) != 1 || cols[0] != 0 {
		t.Fatalf("expected Path bound at col 0 via a=1, got %v", inf.Bindings)
	}
}

func TestInferQueryBindings_NoConstantsFallback(t *testing.T) {
	// from Path(a, b)  — no binding inferable, magic-set transform should be skipped.
	prog := progPathClosure([]datalog.Literal{
		{Positive: true, Atom: datalog.Atom{Predicate: "Path", Args: []datalog.Term{datalog.Var{Name: "a"}, datalog.Var{Name: "b"}}}},
	}, "a", "b")
	idb := IDBPredicates(prog)
	inf := InferQueryBindings(prog, idb)
	if len(inf.Bindings) != 0 {
		t.Fatalf("expected no bindings inferred, got %v", inf.Bindings)
	}
	if len(inf.SeedRules) != 0 {
		t.Fatalf("expected no seed rules, got %d", len(inf.SeedRules))
	}

	// WithMagicSetAuto must fall through to plain Plan with no error.
	ep, _, errs := WithMagicSetAuto(prog, nil)
	if len(errs) != 0 {
		t.Fatalf("plan errors: %v", errs)
	}
	// Sanity: produced rules should not include any magic_* predicates.
	for _, s := range ep.Strata {
		for _, r := range s.Rules {
			if len(r.Head.Predicate) > 6 && r.Head.Predicate[:6] == "magic_" {
				t.Fatalf("expected no magic_* rules in fallback plan, found %s", r.Head.Predicate)
			}
		}
	}
}

func TestInferQueryBindings_SkipsPositionalIDBOnlyBinding(t *testing.T) {
	// from PathA(a, b), Path(a, c)  — a is bound only by a preceding IDB
	// literal; conservative inference must NOT mark it as bound for Path.
	// We declare both PathA and Path as IDBs by adding rules for both.
	rules := []datalog.Rule{
		{
			Head: datalog.Atom{Predicate: "PathA", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "y"}}},
			Body: []datalog.Literal{{Positive: true, Atom: datalog.Atom{Predicate: "Edge", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "y"}}}}},
		},
		{
			Head: datalog.Atom{Predicate: "Path", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "y"}}},
			Body: []datalog.Literal{{Positive: true, Atom: datalog.Atom{Predicate: "Edge", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "y"}}}}},
		},
	}
	prog := &datalog.Program{
		Rules: rules,
		Query: &datalog.Query{
			Select: []datalog.Term{datalog.Var{Name: "c"}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "PathA", Args: []datalog.Term{datalog.Var{Name: "a"}, datalog.Var{Name: "b"}}}},
				{Positive: true, Atom: datalog.Atom{Predicate: "Path", Args: []datalog.Term{datalog.Var{Name: "a"}, datalog.Var{Name: "c"}}}},
			},
		},
	}
	idb := IDBPredicates(prog)
	inf := InferQueryBindings(prog, idb)
	if cols, ok := inf.Bindings["Path"]; ok {
		t.Fatalf("conservative inference should NOT bind Path from IDB-only positional binding, got %v", cols)
	}
}

func TestInferQueryBindings_BaseLiteralWithConstantBindsVar(t *testing.T) {
	// from Edge(1, m), Path(m, b)  — m is grounded by a base lookup that
	// itself contains a constant; this should bind Path's col 0.
	prog := progPathClosure([]datalog.Literal{
		{Positive: true, Atom: datalog.Atom{Predicate: "Edge", Args: []datalog.Term{datalog.IntConst{Value: 1}, datalog.Var{Name: "m"}}}},
		{Positive: true, Atom: datalog.Atom{Predicate: "Path", Args: []datalog.Term{datalog.Var{Name: "m"}, datalog.Var{Name: "b"}}}},
	}, "b")
	idb := IDBPredicates(prog)
	inf := InferQueryBindings(prog, idb)
	cols, ok := inf.Bindings["Path"]
	if !ok || len(cols) != 1 || cols[0] != 0 {
		t.Fatalf("expected Path bound at col 0 via base-lookup grounding, got %v", inf.Bindings)
	}
}

// TestWithMagicSetAuto_StratificationFallback exercises the safety net for
// adversarial-review MAJOR 3: a query body with a negated literal preceding
// the IDB literal copies that negation into the magic-seed body, which can
// introduce a new neg-edge into a recursive component and break
// stratification. Plain Plan succeeds (the program's rules alone are
// stratifiable); the magic-augmented program is not. WithMagicSetAuto must
// detect this and fall back to plain Plan rather than surfacing the error.
func TestWithMagicSetAuto_StratificationFallback(t *testing.T) {
	// Rules:
	//   P(x,y) :- Edge(x,y).
	//   P(x,z) :- Edge(x,y), P(y,z).
	//   Bar(z) :- P(z,z).
	// Plain dep graph: Bar -> P (pos), P -> P (pos via self-recursion).
	// Stratifiable (no neg cycle).
	rules := []datalog.Rule{
		{
			Head: datalog.Atom{Predicate: "P", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "y"}}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "Edge", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "y"}}}},
			},
		},
		{
			Head: datalog.Atom{Predicate: "P", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "z"}}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "Edge", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "y"}}}},
				{Positive: true, Atom: datalog.Atom{Predicate: "P", Args: []datalog.Term{datalog.Var{Name: "y"}, datalog.Var{Name: "z"}}}},
			},
		},
		{
			Head: datalog.Atom{Predicate: "Bar", Args: []datalog.Term{datalog.Var{Name: "z"}}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "P", Args: []datalog.Term{datalog.Var{Name: "z"}, datalog.Var{Name: "z"}}}},
			},
		},
	}
	// Query: from Edge(1, m), not Bar(m), P(m, b)
	// Magic-seed for P would be: magic_P(m) :- Edge(1, m), not Bar(m).
	// That introduces magic_P -> Bar (neg). Augmented P also gains
	// P -> magic_P (pos) from the rewrite. Cycle:
	//   P -> magic_P -> Bar -> P     contains a negative edge => unstratifiable.
	prog := &datalog.Program{
		Rules: rules,
		Query: &datalog.Query{
			Select: []datalog.Term{datalog.Var{Name: "b"}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "Edge", Args: []datalog.Term{datalog.IntConst{Value: 1}, datalog.Var{Name: "m"}}}},
				{Positive: false, Atom: datalog.Atom{Predicate: "Bar", Args: []datalog.Term{datalog.Var{Name: "m"}}}},
				{Positive: true, Atom: datalog.Atom{Predicate: "P", Args: []datalog.Term{datalog.Var{Name: "m"}, datalog.Var{Name: "b"}}}},
			},
		},
	}

	// Sanity: plain Plan must succeed (preconditions of the regression test).
	if _, errs := Plan(prog, nil); len(errs) > 0 {
		t.Fatalf("plain Plan unexpectedly failed: %v", errs)
	}

	ep, inf, errs := WithMagicSetAuto(prog, nil)
	if len(errs) > 0 {
		t.Fatalf("WithMagicSetAuto must fall back to plain Plan when augmented program is unstratifiable, got errors: %v", errs)
	}
	if ep == nil {
		t.Fatalf("expected non-nil ExecutionPlan from fallback path")
	}
	// On fallback, Bindings must be reset to empty so callers don't claim the
	// transform fired.
	if len(inf.Bindings) != 0 {
		t.Fatalf("on stratification fallback, expected empty Bindings, got %v", inf.Bindings)
	}
	// Plan must NOT contain magic_* rules (we fell back to the original
	// program).
	for _, s := range ep.Strata {
		for _, r := range s.Rules {
			if len(r.Head.Predicate) > 6 && r.Head.Predicate[:6] == "magic_" {
				t.Fatalf("fallback plan unexpectedly contains magic_* rule: %s", r.Head.Predicate)
			}
		}
	}
}

// TestWithMagicSetAuto_UnsafeHeadFallback exercises issue #99: the magic-set
// transform can emit a propagation rule whose head contains a variable named
// "_" (the desugared wildcard). isSafe inside magicset.go treats "_" as
// always-bound and emits the rule, but the post-transform ValidateRule pass
// (called from Plan) rejects it as an unsafe head variable. WithMagicSetAuto
// must catch that planning error and fall back to plain Plan rather than
// surfacing it.
//
// This is a unit-level regression guard for PR #95's broad
// `if len(errs) > 0 { fall back }` clause in magicset_infer.go. Narrowing the
// fallback (e.g. `if errors.Is(err, ErrStratification)`) would cause
// find_dangerous_jsx.ql to silently regress; this test fails in that case.
//
// Construction: two IDB rules sharing predicate Q. The first binds Q col 0
// (via the query's bound P col 0 -> x -> Q(x,y)). The second uses Q with a
// wildcard `_` in col 0. propagateBindings stamps Q.boundCols=[0] from the
// first occurrence; MagicSetTransform's propagation-rule generation for the
// second occurrence emits magic_Q(_) :- magic_R(z), which is unsafe.
func TestWithMagicSetAuto_UnsafeHeadFallback(t *testing.T) {
	rules := []datalog.Rule{
		// P(x) :- Q(x, y), R(y).  -- binds Q col 0 from query-bound x.
		{
			Head: datalog.Atom{Predicate: "P", Args: []datalog.Term{datalog.Var{Name: "x"}}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "Q", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "y"}}}},
				{Positive: true, Atom: datalog.Atom{Predicate: "R", Args: []datalog.Term{datalog.Var{Name: "y"}}}},
			},
		},
		// R(z) :- Q(_, z).  -- uses Q with `_` in col 0; magic_Q(_) leaks.
		{
			Head: datalog.Atom{Predicate: "R", Args: []datalog.Term{datalog.Var{Name: "z"}}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "Q", Args: []datalog.Term{datalog.Var{Name: "_"}, datalog.Var{Name: "z"}}}},
			},
		},
		// Q(a, b) :- Base(a, b).  -- Q is IDB.
		{
			Head: datalog.Atom{Predicate: "Q", Args: []datalog.Term{datalog.Var{Name: "a"}, datalog.Var{Name: "b"}}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "Base", Args: []datalog.Term{datalog.Var{Name: "a"}, datalog.Var{Name: "b"}}}},
			},
		},
	}
	// Query: from P(1).
	prog := &datalog.Program{
		Rules: rules,
		Query: &datalog.Query{
			Select: []datalog.Term{datalog.Var{Name: "x"}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "P", Args: []datalog.Term{datalog.IntConst{Value: 1}}}},
			},
		},
	}

	// Sanity: the original program plans cleanly (the unsafe rule is purely a
	// product of the magic-set transform, not the input).
	if _, errs := Plan(prog, nil); len(errs) > 0 {
		t.Fatalf("plain Plan unexpectedly failed on the original program: %v", errs)
	}

	// Sanity: the transform itself, applied directly, produces a program that
	// fails to plan (this proves the test is actually exercising the unsafe-
	// head failure mode rather than some other path).
	idb := IDBPredicates(prog)
	inf := InferQueryBindings(prog, idb)
	if len(inf.Bindings) == 0 {
		t.Fatalf("precondition: expected bindings to be inferred for query body")
	}
	transformed := MagicSetTransform(prog, inf.Bindings)
	if len(inf.SeedRules) > 0 {
		augmented := make([]datalog.Rule, 0, len(transformed.Rules)+len(inf.SeedRules))
		augmented = append(augmented, transformed.Rules...)
		augmented = append(augmented, inf.SeedRules...)
		transformed = &datalog.Program{Rules: augmented, Query: transformed.Query}
	}
	if _, errs := Plan(transformed, nil); len(errs) == 0 {
		t.Fatalf("precondition: expected augmented program to fail planning with unsafe-head error; got none")
	} else {
		// Confirm the failure mode is specifically an unsafe head variable.
		sawUnsafeHead := false
		for _, e := range errs {
			if strings.Contains(e.Error(), "unsafe rule: head variable") {
				sawUnsafeHead = true
				break
			}
		}
		if !sawUnsafeHead {
			t.Fatalf("precondition: expected unsafe-head error from augmented program, got: %v", errs)
		}
	}

	// Actual assertion: WithMagicSetAuto must absorb the planning error and
	// fall back to plain Plan.
	ep, infOut, errs := WithMagicSetAuto(prog, nil)
	if len(errs) != 0 {
		t.Fatalf("WithMagicSetAuto must fall back on unsafe-head failure, got errors: %v", errs)
	}
	if ep == nil {
		t.Fatalf("expected non-nil ExecutionPlan from fallback path")
	}
	// Belt-and-braces: fallback must signal by returning empty Bindings.
	if len(infOut.Bindings) != 0 {
		t.Fatalf("on unsafe-head fallback, expected empty Bindings (signal), got %v", infOut.Bindings)
	}
	// Plan must NOT contain magic_* rules (proves we fell back, not that the
	// transform somehow succeeded).
	for _, s := range ep.Strata {
		for _, r := range s.Rules {
			if len(r.Head.Predicate) > 6 && r.Head.Predicate[:6] == "magic_" {
				t.Fatalf("fallback plan unexpectedly contains magic_* rule: %s", r.Head.Predicate)
			}
		}
	}
}

func TestWithMagicSetAuto_AppliesTransformWhenInferable(t *testing.T) {
	prog := progPathClosure([]datalog.Literal{
		{Positive: true, Atom: datalog.Atom{Predicate: "Path", Args: []datalog.Term{datalog.IntConst{Value: 1}, datalog.Var{Name: "b"}}}},
	}, "b")
	ep, inf, errs := WithMagicSetAuto(prog, nil)
	if len(errs) != 0 {
		t.Fatalf("plan errors: %v", errs)
	}
	if len(inf.Bindings) == 0 {
		t.Fatalf("expected bindings to be inferred")
	}
	// Plan must contain at least one magic_* rule.
	hasMagic := false
	for _, s := range ep.Strata {
		for _, r := range s.Rules {
			if len(r.Head.Predicate) > 6 && r.Head.Predicate[:6] == "magic_" {
				hasMagic = true
			}
		}
	}
	if !hasMagic {
		t.Fatalf("expected magic_* rule in plan after WithMagicSetAuto with inferable bindings")
	}
}
