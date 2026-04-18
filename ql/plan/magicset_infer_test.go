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
	// Issue #112: happy path must report Fallback=false / FallbackReason=nil.
	if inf.Fallback {
		t.Fatalf("expected Fallback=false on happy path, got true (reason=%v)", inf.FallbackReason)
	}
	if inf.FallbackReason != nil {
		t.Fatalf("expected nil FallbackReason on happy path, got %v", inf.FallbackReason)
	}
}

// TestWithMagicSetAuto_FallbackSignalsObservably (issue #112) asserts that
// the silent-fallback path on an unsafe-head augmented program populates
// Fallback / FallbackReason so callers can distinguish "no bindings to
// infer" (Fallback=false, Bindings=nil) from "transform fired and broke"
// (Fallback=true, Bindings=nil).
func TestWithMagicSetAuto_FallbackSignalsObservably(t *testing.T) {
	// Reuse the unsafe-head construction from
	// TestWithMagicSetAuto_UnsafeHeadFallback.
	rules := []datalog.Rule{
		{
			Head: datalog.Atom{Predicate: "P", Args: []datalog.Term{datalog.Var{Name: "x"}}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "Q", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "y"}}}},
				{Positive: true, Atom: datalog.Atom{Predicate: "R", Args: []datalog.Term{datalog.Var{Name: "y"}}}},
			},
		},
		{
			Head: datalog.Atom{Predicate: "R", Args: []datalog.Term{datalog.Var{Name: "z"}}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "Q", Args: []datalog.Term{datalog.Var{Name: "_"}, datalog.Var{Name: "z"}}}},
			},
		},
		{
			Head: datalog.Atom{Predicate: "Q", Args: []datalog.Term{datalog.Var{Name: "a"}, datalog.Var{Name: "b"}}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "Base", Args: []datalog.Term{datalog.Var{Name: "a"}, datalog.Var{Name: "b"}}}},
			},
		},
	}
	prog := &datalog.Program{
		Rules: rules,
		Query: &datalog.Query{
			Select: []datalog.Term{datalog.Var{Name: "x"}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "P", Args: []datalog.Term{datalog.IntConst{Value: 1}}}},
			},
		},
	}

	ep, inf, errs := WithMagicSetAuto(prog, nil)
	if len(errs) != 0 {
		t.Fatalf("expected silent fallback (no errors), got %v", errs)
	}
	if ep == nil {
		t.Fatalf("expected non-nil ExecutionPlan from fallback path")
	}
	if !inf.Fallback {
		t.Fatalf("expected Fallback=true on unsafe-head fallback (caller can't otherwise distinguish from no-bindings-inferred)")
	}
	if inf.FallbackReason == nil {
		t.Fatalf("expected non-nil FallbackReason on fallback")
	}
	if !strings.Contains(inf.FallbackReason.Error(), "unsafe rule") {
		t.Fatalf("expected FallbackReason to mention the unsafe-rule cause, got %q", inf.FallbackReason.Error())
	}
}

// TestWithMagicSetAuto_NoBindingsIsNotFallback (issue #112) asserts that
// the "no inferable bindings" path leaves Fallback=false. Only an actual
// transform-then-fail is a fallback.
func TestWithMagicSetAuto_NoBindingsIsNotFallback(t *testing.T) {
	// Body has no constants, equalities-to-constants, or constant-bearing
	// base literals — no bindings inferable.
	prog := progPathClosure([]datalog.Literal{
		{Positive: true, Atom: datalog.Atom{Predicate: "Path", Args: []datalog.Term{datalog.Var{Name: "a"}, datalog.Var{Name: "b"}}}},
	}, "a", "b")
	_, inf, errs := WithMagicSetAuto(prog, nil)
	if len(errs) != 0 {
		t.Fatalf("plan errors: %v", errs)
	}
	if inf.Fallback {
		t.Fatalf("expected Fallback=false when no bindings are inferable, got true")
	}
	if inf.FallbackReason != nil {
		t.Fatalf("expected nil FallbackReason on no-bindings path, got %v", inf.FallbackReason)
	}
}

// TestWithMagicSetAutoOpts_StrictSurfacesPlanError (issue #112) asserts
// that strict mode returns the underlying planning error rather than
// silently falling back to plain Plan.
//
// Runs over two fixtures: the first is the canonical wildcard-in-body
// unsafe-rule shape; the second is a renamed-identifier variant on the
// same code path (see fixture comment below). The second fixture is a
// smoke check that the assertion isn't hard-coded to predicate names,
// not a structurally-distinct failure mode. A genuinely different
// failure-shape fixture is left as a follow-up (#124 review minor).
func TestWithMagicSetAutoOpts_StrictSurfacesPlanError(t *testing.T) {
	cases := []struct {
		name string
		prog *datalog.Program
	}{
		{
			name: "unsafe_head_via_wildcard_2arg",
			prog: &datalog.Program{
				Rules: []datalog.Rule{
					{
						Head: datalog.Atom{Predicate: "P", Args: []datalog.Term{datalog.Var{Name: "x"}}},
						Body: []datalog.Literal{
							{Positive: true, Atom: datalog.Atom{Predicate: "Q", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "y"}}}},
							{Positive: true, Atom: datalog.Atom{Predicate: "R", Args: []datalog.Term{datalog.Var{Name: "y"}}}},
						},
					},
					{
						Head: datalog.Atom{Predicate: "R", Args: []datalog.Term{datalog.Var{Name: "z"}}},
						Body: []datalog.Literal{
							{Positive: true, Atom: datalog.Atom{Predicate: "Q", Args: []datalog.Term{datalog.Var{Name: "_"}, datalog.Var{Name: "z"}}}},
						},
					},
					{
						Head: datalog.Atom{Predicate: "Q", Args: []datalog.Term{datalog.Var{Name: "a"}, datalog.Var{Name: "b"}}},
						Body: []datalog.Literal{
							{Positive: true, Atom: datalog.Atom{Predicate: "Base", Args: []datalog.Term{datalog.Var{Name: "a"}, datalog.Var{Name: "b"}}}},
						},
					},
				},
				Query: &datalog.Query{
					Select: []datalog.Term{datalog.Var{Name: "x"}},
					Body: []datalog.Literal{
						{Positive: true, Atom: datalog.Atom{Predicate: "P", Args: []datalog.Term{datalog.IntConst{Value: 1}}}},
					},
				},
			},
		},
		{
			// Renamed-identifier variant of the first fixture with a
			// 3-arg base relation tacked on. The unsafe-rule trigger is
			// still a wildcard-bearing body literal feeding a head var
			// (`Mid(_, w)` -> `Leaf(w)`), so this exercises the same
			// isSafe code path as the first case. Kept as a smoke check
			// that the strict-mode assertion isn't hard-coded to
			// predicate names like P/Q/R; not a structurally-distinct
			// failure mode. Finding a genuinely different planner-error
			// shape that's reachable through the magic-set augmented
			// program needs its own investigation — see #124 review.
			name: "unsafe_head_via_wildcard_3arg",
			prog: &datalog.Program{
				Rules: []datalog.Rule{
					{
						Head: datalog.Atom{Predicate: "Top", Args: []datalog.Term{datalog.Var{Name: "k"}}},
						Body: []datalog.Literal{
							{Positive: true, Atom: datalog.Atom{Predicate: "Mid", Args: []datalog.Term{datalog.Var{Name: "k"}, datalog.Var{Name: "u"}}}},
							{Positive: true, Atom: datalog.Atom{Predicate: "Leaf", Args: []datalog.Term{datalog.Var{Name: "u"}}}},
						},
					},
					{
						Head: datalog.Atom{Predicate: "Leaf", Args: []datalog.Term{datalog.Var{Name: "w"}}},
						Body: []datalog.Literal{
							{Positive: true, Atom: datalog.Atom{Predicate: "Mid", Args: []datalog.Term{datalog.Var{Name: "_"}, datalog.Var{Name: "w"}}}},
						},
					},
					{
						Head: datalog.Atom{Predicate: "Mid", Args: []datalog.Term{datalog.Var{Name: "p"}, datalog.Var{Name: "q"}}},
						Body: []datalog.Literal{
							{Positive: true, Atom: datalog.Atom{Predicate: "Triple", Args: []datalog.Term{datalog.Var{Name: "p"}, datalog.Var{Name: "q"}, datalog.Var{Name: "r"}}}},
						},
					},
				},
				Query: &datalog.Query{
					Select: []datalog.Term{datalog.Var{Name: "k"}},
					Body: []datalog.Literal{
						{Positive: true, Atom: datalog.Atom{Predicate: "Top", Args: []datalog.Term{datalog.IntConst{Value: 7}}}},
					},
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ep, _, errs := WithMagicSetAutoOpts(tc.prog, nil, MagicSetOptions{Strict: true})
			if len(errs) == 0 {
				t.Fatalf("expected strict mode to surface planning errors from the augmented program, got none")
			}
			if ep != nil {
				t.Fatalf("expected nil ExecutionPlan in strict failure; got non-nil")
			}
			sawUnsafeHead := false
			for _, e := range errs {
				if strings.Contains(e.Error(), "unsafe rule") {
					sawUnsafeHead = true
					break
				}
			}
			if !sawUnsafeHead {
				t.Fatalf("expected strict-mode error to surface unsafe-rule cause, got: %v", errs)
			}
		})
	}
}

// TestWithMagicSetAutoOpts_StrictHappyPathUnchanged ensures strict mode is
// transparent on programs whose augmented form plans cleanly.
func TestWithMagicSetAutoOpts_StrictHappyPathUnchanged(t *testing.T) {
	prog := progPathClosure([]datalog.Literal{
		{Positive: true, Atom: datalog.Atom{Predicate: "Path", Args: []datalog.Term{datalog.IntConst{Value: 1}, datalog.Var{Name: "b"}}}},
	}, "b")
	ep, inf, errs := WithMagicSetAutoOpts(prog, nil, MagicSetOptions{Strict: true})
	if len(errs) != 0 {
		t.Fatalf("strict mode must be transparent on plannable inputs; got errors: %v", errs)
	}
	if ep == nil {
		t.Fatalf("expected non-nil ExecutionPlan")
	}
	if inf.Fallback {
		t.Fatalf("expected Fallback=false on happy path under strict mode")
	}
	if len(inf.Bindings) == 0 {
		t.Fatalf("expected bindings to be inferred under strict mode happy path")
	}
}

// TestWithMagicSetAutoOpts_ArityMismatchSamePredDifferentArity pins the
// behaviour of the arity-mismatch guard in WithMagicSetAutoOpts.
//
// Scenario: a single IDB pred P/2 appears twice in the query body with
// different bound-column counts — `P(1, y)` (1 bound position) and
// `P(2, 3)` (2 bound positions). InferQueryBindings records
// bindings[P] from the first occurrence (1 position) but appends a seed
// rule for each occurrence using its own boundCols length, so the second
// seed rule's head arity (magic_P/2) disagrees with the recorded binding
// arity (1). The arity guard in WithMagicSetAutoOpts catches this.
//
// Strict mode must surface the arity-mismatch error; non-strict mode
// must fall back to plain Plan with FallbackReason populated.
func TestWithMagicSetAutoOpts_ArityMismatchSamePredDifferentArity(t *testing.T) {
	mkProg := func() *datalog.Program {
		return &datalog.Program{
			Rules: []datalog.Rule{
				{
					Head: datalog.Atom{Predicate: "P", Args: []datalog.Term{datalog.Var{Name: "a"}, datalog.Var{Name: "b"}}},
					Body: []datalog.Literal{
						{Positive: true, Atom: datalog.Atom{Predicate: "Base", Args: []datalog.Term{datalog.Var{Name: "a"}, datalog.Var{Name: "b"}}}},
					},
				},
			},
			Query: &datalog.Query{
				Select: []datalog.Term{datalog.Var{Name: "y"}},
				Body: []datalog.Literal{
					{Positive: true, Atom: datalog.Atom{Predicate: "P", Args: []datalog.Term{datalog.IntConst{Value: 1}, datalog.Var{Name: "y"}}}},
					{Positive: true, Atom: datalog.Atom{Predicate: "P", Args: []datalog.Term{datalog.IntConst{Value: 2}, datalog.IntConst{Value: 3}}}},
				},
			},
		}
	}

	t.Run("strict_surfaces_arity_mismatch", func(t *testing.T) {
		ep, _, errs := WithMagicSetAutoOpts(mkProg(), nil, MagicSetOptions{Strict: true})
		if ep != nil {
			t.Fatalf("expected nil ExecutionPlan in strict failure; got non-nil")
		}
		if len(errs) == 0 {
			t.Fatalf("expected strict mode to surface arity-mismatch error, got none")
		}
		sawArity := false
		for _, e := range errs {
			if strings.Contains(e.Error(), "arity mismatch") {
				sawArity = true
				break
			}
		}
		if !sawArity {
			t.Fatalf("expected strict-mode error to mention arity mismatch, got: %v", errs)
		}
	})

	t.Run("nonstrict_falls_back_with_reason", func(t *testing.T) {
		ep, inf, errs := WithMagicSetAutoOpts(mkProg(), nil, MagicSetOptions{})
		if len(errs) != 0 {
			t.Fatalf("non-strict mode must swallow planning errors and return a working plan; got errs=%v", errs)
		}
		if ep == nil {
			t.Fatalf("expected non-nil ExecutionPlan from plain-Plan fallback")
		}
		if !inf.Fallback {
			t.Fatalf("expected Fallback=true on arity-mismatch fallback path")
		}
		if inf.FallbackReason == nil {
			t.Fatalf("expected FallbackReason to be populated; got nil")
		}
		if !strings.Contains(inf.FallbackReason.Error(), "arity mismatch") {
			t.Fatalf("expected FallbackReason to mention arity mismatch, got: %v", inf.FallbackReason)
		}
	})
}

// --- Issue #121 Phase A: rule-body binding inference --------------------------------

// progBackwardConfigShape constructs a program shaped like the BackwardTracker
// pattern: a small `isSink` IDB followed by a recursive `flowsTo` IDB followed
// by an `isSource` IDB. This is the load-bearing case that motivated rule-body
// binding inference (issue #121).
func progBackwardConfigShape() *datalog.Program {
	rules := []datalog.Rule{
		// Edge(x, y) is the base relation
		// flowsTo(s, t) :- Edge(s, t).
		{
			Head: datalog.Atom{Predicate: "flowsTo", Args: []datalog.Term{datalog.Var{Name: "s"}, datalog.Var{Name: "t"}}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "Edge", Args: []datalog.Term{datalog.Var{Name: "s"}, datalog.Var{Name: "t"}}}},
			},
		},
		// flowsTo(s, t) :- Edge(s, m), flowsTo(m, t).
		{
			Head: datalog.Atom{Predicate: "flowsTo", Args: []datalog.Term{datalog.Var{Name: "s"}, datalog.Var{Name: "t"}}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "Edge", Args: []datalog.Term{datalog.Var{Name: "s"}, datalog.Var{Name: "m"}}}},
				{Positive: true, Atom: datalog.Atom{Predicate: "flowsTo", Args: []datalog.Term{datalog.Var{Name: "m"}, datalog.Var{Name: "t"}}}},
			},
		},
		// isSink(t) :- Sink(t).  -- IDB seeded by a base 'Sink'
		{
			Head: datalog.Atom{Predicate: "isSink", Args: []datalog.Term{datalog.Var{Name: "t"}}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "Sink", Args: []datalog.Term{datalog.Var{Name: "t"}}}},
			},
		},
		// hasFlowTo(s, t) :- isSink(t), flowsTo(s, t).
		{
			Head: datalog.Atom{Predicate: "hasFlowTo", Args: []datalog.Term{datalog.Var{Name: "s"}, datalog.Var{Name: "t"}}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "isSink", Args: []datalog.Term{datalog.Var{Name: "t"}}}},
				{Positive: true, Atom: datalog.Atom{Predicate: "flowsTo", Args: []datalog.Term{datalog.Var{Name: "s"}, datalog.Var{Name: "t"}}}},
			},
		},
	}
	return &datalog.Program{
		Rules: rules,
		Query: &datalog.Query{
			Select: []datalog.Term{datalog.Var{Name: "s"}, datalog.Var{Name: "t"}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "hasFlowTo", Args: []datalog.Term{datalog.Var{Name: "s"}, datalog.Var{Name: "t"}}}},
			},
		},
	}
}

func TestInferRuleBindings_BackwardConfigurationShape(t *testing.T) {
	prog := progBackwardConfigShape()
	idb := IDBPredicates(prog)
	// Mark isSink as small (well below smallIDBThreshold).
	hints := map[string]int{"isSink": 5}
	got := InferRuleBindings(prog, idb, hints)
	cols, ok := got["flowsTo"]
	if !ok {
		t.Fatalf("expected rule-binding for flowsTo (sink-bound), got: %v", got)
	}
	// `t` is bound by isSink(t); flowsTo(s, t) → second arg is bound.
	if len(cols) != 1 || cols[0] != 1 {
		t.Fatalf("expected flowsTo binding [1] (sink position), got %v", cols)
	}
}

func TestInferRuleBindings_NoMagicWhenSourceAndSinkBothLarge(t *testing.T) {
	prog := progBackwardConfigShape()
	idb := IDBPredicates(prog)
	// No size hints at all → no IDB qualifies as small → no rule bindings.
	got := InferRuleBindings(prog, idb, nil)
	if len(got) != 0 {
		t.Fatalf("expected no rule bindings without small-IDB hints, got: %v", got)
	}
	// Same when hints exceed the threshold.
	got = InferRuleBindings(prog, idb, map[string]int{"isSink": 1_000_000})
	if len(got) != 0 {
		t.Fatalf("expected no rule bindings when isSink is large, got: %v", got)
	}
}

func TestInferRuleBindings_DoesNotFireOnTransitiveClosurePath(t *testing.T) {
	// The `Path(x,z) :- Edge(x,y), Path(y,z)` shape from earlier tests
	// must not record a rule binding even though `Edge` (a base) precedes
	// `Path` in the body. The smallIDBSeen gate prevents this.
	prog := progPathClosure(nil, "x", "y")
	idb := IDBPredicates(prog)
	got := InferRuleBindings(prog, idb, map[string]int{"Path": 5})
	if len(got) != 0 {
		t.Fatalf("expected no rule bindings on plain transitive closure, got: %v", got)
	}
}

func TestBuildRuleSeedRules_ConfigurationShapeProducesSeed(t *testing.T) {
	prog := progBackwardConfigShape()
	idb := IDBPredicates(prog)
	hints := map[string]int{"isSink": 5}
	bindings := InferRuleBindings(prog, idb, hints)
	seeds := BuildRuleSeedRules(prog, idb, hints, bindings)
	if len(seeds) == 0 {
		t.Fatalf("expected at least one seed rule for flowsTo, got none")
	}
	found := false
	for _, s := range seeds {
		if s.Head.Predicate == "magic_flowsTo" {
			found = true
			if len(s.Head.Args) != 1 {
				t.Errorf("expected magic_flowsTo seed of arity 1, got %d", len(s.Head.Args))
			}
		}
	}
	if !found {
		t.Errorf("no magic_flowsTo seed rule emitted; got: %v", seeds)
	}
}

func TestFilterRuleBindingsAvoidingWildcards_DropsDirectWildcardCollision(t *testing.T) {
	// Construct a program where a rule body literal P(_, y) collides with a
	// proposed binding {P: [0]}.
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			{
				Head: datalog.Atom{Predicate: "Q", Args: []datalog.Term{datalog.Var{Name: "y"}}},
				Body: []datalog.Literal{
					{Positive: true, Atom: datalog.Atom{Predicate: "P", Args: []datalog.Term{datalog.Wildcard{}, datalog.Var{Name: "y"}}}},
				},
			},
		},
	}
	in := map[string][]int{"P": {0}}
	out := filterRuleBindingsAvoidingWildcards(prog, in)
	if _, ok := out["P"]; ok {
		t.Fatalf("expected P binding to be filtered out due to wildcard at col 0, got: %v", out)
	}
}

// TestWithMagicSetAuto_RuleBindingActivationOnIDBHints (issue #121 Phase A.1)
// is the load-bearing activation test for the planner-pipeline re-order.
//
// On the first planning pass `sizeHints` only contains base-relation counts
// (CLI's buildSizeHints), so InferRuleBindings cannot identify any small-IDB
// body literal and produces zero rule bindings — the resulting plan has no
// `magic_*` seed rules. Only after eval.EstimateNonRecursiveIDBSizes
// populates `sizeHints` with real IDB cardinalities can rule-body inference
// fire on Configuration-shaped queries (BackwardTracker pattern).
//
// This test simulates the two-pass pipeline (cmd/tsq/main.go's
// compileAndEval): first call WithMagicSetAutoOpts with base-only hints and
// assert no magic-set seed rule fires; then add a small IDB hint (as
// EstimateNonRecursiveIDBSizes would) and re-call WithMagicSetAutoOpts —
// asserting that the magic_flowsTo seed rule now appears in the plan.
//
// If the cmd/tsq/main.go re-order regresses (someone removes the
// post-estimate replan), the cmd/tsq integration coverage will catch the
// behavioural regression; this test pins the planner-side contract that
// the second pass produces a different plan when IDB hints arrive.
func TestWithMagicSetAuto_RuleBindingActivationOnIDBHints(t *testing.T) {
	prog := progBackwardConfigShape()

	// First pass: base-only hints (Edge has tuples, but no IDB sized).
	// InferRuleBindings sees no small-IDB literal, no rule bindings emerge.
	baseOnlyHints := map[string]int{"Edge": 100, "Sink": 5}
	ep1, inf1, errs1 := WithMagicSetAutoOpts(prog, baseOnlyHints, MagicSetOptions{})
	if len(errs1) > 0 {
		t.Fatalf("first-pass plan errors: %v", errs1)
	}
	if ep1 == nil {
		t.Fatalf("first-pass plan is nil")
	}
	if _, present := inf1.Bindings["flowsTo"]; present {
		t.Fatalf("first-pass: did not expect flowsTo binding without IDB hint, got: %v", inf1.Bindings)
	}
	if planContainsMagicSeed(ep1, "magic_flowsTo") {
		t.Fatalf("first-pass: did not expect magic_flowsTo seed rule in plan without IDB hint")
	}

	// Second pass: trusted IDB hint for `isSink` is now provided via
	// MagicSetOptions.IDBSizeHints (as cmd/tsq/main.go does after
	// EstimateNonRecursiveIDBSizes). Rule-body inference can now
	// identify isSink(t) as small-IDB-bound, propagate the binding
	// into flowsTo(s, t)'s second arg, and emit a magic_flowsTo seed
	// rule. Note that we deliberately do NOT add `isSink` to the
	// base-only hints map: that path is filtered by the base-shadow
	// guard. The IDBSizeHints channel is the soundness-preserving
	// route for IDB cardinalities.
	ep2, inf2, errs2 := WithMagicSetAutoOpts(prog, baseOnlyHints, MagicSetOptions{
		IDBSizeHints: map[string]int{"isSink": 5},
	})
	if len(errs2) > 0 {
		t.Fatalf("second-pass plan errors: %v", errs2)
	}
	if ep2 == nil {
		t.Fatalf("second-pass plan is nil")
	}
	cols, present := inf2.Bindings["flowsTo"]
	if !present {
		t.Fatalf("second-pass: expected flowsTo binding once isSink is sized, got: %v", inf2.Bindings)
	}
	if len(cols) != 1 || cols[0] != 1 {
		t.Fatalf("second-pass: expected flowsTo bound at col 1 (sink position), got %v", cols)
	}
	if !planContainsMagicSeed(ep2, "magic_flowsTo") {
		t.Fatalf("second-pass: expected magic_flowsTo seed rule in plan after IDB hint")
	}
}

// progCustomStepConfigShape mirrors progBackwardConfigShape but
// substitutes the `flowsTo` IDB with a custom binary `step` IDB whose
// body walks a `Contains` base relation (transitive closure). This is
// the structural analogue of dataflow tracking — the load-bearing
// claim of issue #121 Phase A.2 is that the magic-set inference is
// agnostic to which binary relation the abstract `step` predicate
// expands to.
//
// Schema:
//   - base relations: `Contains(parent, child)`, `Sink(t)`
//   - IDB rules:
//     step(s, t) :- Contains(s, t).
//     step(s, t) :- Contains(s, m), step(m, t).
//     isSink(t) :- Sink(t).
//     hasFlowTo(s, t) :- isSink(t), step(s, t).
//
// Same shape as progBackwardConfigShape with `flowsTo`→`step`,
// `Edge`→`Contains`. If the rule-body binding inference works *only*
// on the literal name `flowsTo`, this test fails — proving the
// inference is name-agnostic and any binary `step` Configuration
// works (Phase A.2 design intent).
func progCustomStepConfigShape() *datalog.Program {
	rules := []datalog.Rule{
		{
			Head: datalog.Atom{Predicate: "step", Args: []datalog.Term{datalog.Var{Name: "s"}, datalog.Var{Name: "t"}}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "Contains", Args: []datalog.Term{datalog.Var{Name: "s"}, datalog.Var{Name: "t"}}}},
			},
		},
		{
			Head: datalog.Atom{Predicate: "step", Args: []datalog.Term{datalog.Var{Name: "s"}, datalog.Var{Name: "t"}}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "Contains", Args: []datalog.Term{datalog.Var{Name: "s"}, datalog.Var{Name: "m"}}}},
				{Positive: true, Atom: datalog.Atom{Predicate: "step", Args: []datalog.Term{datalog.Var{Name: "m"}, datalog.Var{Name: "t"}}}},
			},
		},
		{
			Head: datalog.Atom{Predicate: "isSink", Args: []datalog.Term{datalog.Var{Name: "t"}}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "Sink", Args: []datalog.Term{datalog.Var{Name: "t"}}}},
			},
		},
		{
			Head: datalog.Atom{Predicate: "hasFlowTo", Args: []datalog.Term{datalog.Var{Name: "s"}, datalog.Var{Name: "t"}}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "isSink", Args: []datalog.Term{datalog.Var{Name: "t"}}}},
				{Positive: true, Atom: datalog.Atom{Predicate: "step", Args: []datalog.Term{datalog.Var{Name: "s"}, datalog.Var{Name: "t"}}}},
			},
		},
	}
	return &datalog.Program{
		Rules: rules,
		Query: &datalog.Query{
			Select: []datalog.Term{datalog.Var{Name: "s"}, datalog.Var{Name: "t"}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "hasFlowTo", Args: []datalog.Term{datalog.Var{Name: "s"}, datalog.Var{Name: "t"}}}},
			},
		},
	}
}

// TestIssue121_PlannerPicksBackwardBindingForCustomStep verifies the
// load-bearing claim of issue #121 Phase A.2 (Option A): the magic-set
// rule-body binding inference works for ANY binary `step` IDB, not
// just the dataflow-default `flowsTo`. This is what lets
// `BackwardTracker` subclasses override `step` (e.g. with
// `functionContainsStar` for structural containment-walks like
// `SetStateUpdaterTracker` in bridge/tsq_react.qll) and still benefit
// from the magic-set rewrite.
//
// Asserts:
//   - first pass (base-only hints): no `magic_step` seed rule emitted.
//   - second pass (IDBSizeHints["isSink"]=5 supplied as if from
//     EstimateNonRecursiveIDBSizes): `magic_step` seed rule appears,
//     `step` is bound at column 1 (sink position).
//
// If this test fails (e.g. inference only fires on the literal name
// `flowsTo`), the Phase A.2 generalization is broken and any
// Configuration that overrides `step` will silently OOM on real
// workloads.
func TestIssue121_PlannerPicksBackwardBindingForCustomStep(t *testing.T) {
	prog := progCustomStepConfigShape()

	// First pass — no IDB hints, just bases. Expect no magic-set firing.
	baseOnlyHints := map[string]int{"Contains": 100, "Sink": 5}
	ep1, inf1, errs1 := WithMagicSetAutoOpts(prog, baseOnlyHints, MagicSetOptions{})
	if len(errs1) > 0 {
		t.Fatalf("first-pass plan errors: %v", errs1)
	}
	if _, present := inf1.Bindings["step"]; present {
		t.Fatalf("first-pass: did not expect `step` binding without IDB hint, got: %v", inf1.Bindings)
	}
	if planContainsMagicSeed(ep1, "magic_step") {
		t.Fatalf("first-pass: did not expect magic_step seed rule in plan without IDB hint")
	}

	// Second pass — supply `isSink` cardinality via the trusted IDB-hint
	// channel. This mirrors the cmd/tsq/main.go re-plan path post
	// EstimateNonRecursiveIDBSizes.
	ep2, inf2, errs2 := WithMagicSetAutoOpts(prog, baseOnlyHints, MagicSetOptions{
		IDBSizeHints: map[string]int{"isSink": 5},
	})
	if len(errs2) > 0 {
		t.Fatalf("second-pass plan errors: %v", errs2)
	}
	cols, present := inf2.Bindings["step"]
	if !present {
		t.Fatalf("second-pass: expected `step` binding once isSink is sized, got: %v", inf2.Bindings)
	}
	if len(cols) != 1 || cols[0] != 1 {
		t.Fatalf("second-pass: expected `step` bound at col 1 (sink position), got %v", cols)
	}
	if !planContainsMagicSeed(ep2, "magic_step") {
		t.Fatalf("second-pass: expected magic_step seed rule in plan after IDB hint — magic-set inference must be agnostic to step-relation name")
	}
}

// planContainsMagicSeed returns true iff some stratum in ep contains a
// rule whose head predicate equals magicHead.
func planContainsMagicSeed(ep *ExecutionPlan, magicHead string) bool {
	if ep == nil {
		return false
	}
	for _, s := range ep.Strata {
		for _, r := range s.Rules {
			if r.Head.Predicate == magicHead {
				return true
			}
		}
	}
	return false
}
