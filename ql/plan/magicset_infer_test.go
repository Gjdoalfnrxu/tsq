package plan

import (
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
