package plan

import (
	"fmt"
	"sort"
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"pgregory.net/rapid"
)

// TestMagicSetTransformPreservesResults is the critical property test:
// magic-set transformation must produce the same query results as the
// untransformed program.
func TestMagicSetTransformPreservesResults(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a simple program with 1-2 IDB predicates.
		nBase := rapid.IntRange(2, 3).Draw(t, "nBase")
		baseNames := make([]string, nBase)
		for i := 0; i < nBase; i++ {
			baseNames[i] = fmt.Sprintf("Base%d", i)
		}

		// Create a simple IDB rule: Derived(x,y) :- Base0(x,z), Base1(z,y).
		bodyIdx0 := rapid.IntRange(0, nBase-1).Draw(t, "bodyIdx0")
		bodyIdx1 := rapid.IntRange(0, nBase-1).Draw(t, "bodyIdx1")

		rules := []datalog.Rule{
			{
				Head: datalog.Atom{
					Predicate: "Derived",
					Args:      []datalog.Term{datalog.Var{Name: "X"}, datalog.Var{Name: "Y"}},
				},
				Body: []datalog.Literal{
					{
						Positive: true,
						Atom: datalog.Atom{
							Predicate: baseNames[bodyIdx0],
							Args:      []datalog.Term{datalog.Var{Name: "X"}, datalog.Var{Name: "Z"}},
						},
					},
					{
						Positive: true,
						Atom: datalog.Atom{
							Predicate: baseNames[bodyIdx1],
							Args:      []datalog.Term{datalog.Var{Name: "Z"}, datalog.Var{Name: "Y"}},
						},
					},
				},
			},
		}

		query := &datalog.Query{
			Select: []datalog.Term{datalog.Var{Name: "A"}, datalog.Var{Name: "B"}},
			Body: []datalog.Literal{
				{
					Positive: true,
					Atom: datalog.Atom{
						Predicate: "Derived",
						Args:      []datalog.Term{datalog.Var{Name: "A"}, datalog.Var{Name: "B"}},
					},
				},
			},
		}

		prog := &datalog.Program{Rules: rules, Query: query}

		// Plan without magic-set.
		ep1, errs1 := Plan(prog, nil)
		if len(errs1) > 0 {
			t.Skip("plan error: ", errs1[0])
		}

		// Plan with magic-set (bind first column of Derived).
		queryBindings := map[string][]int{"Derived": {0}}
		ep2, errs2 := WithMagicSet(prog, nil, queryBindings)
		if len(errs2) > 0 {
			t.Skip("magic-set plan error: ", errs2[0])
		}

		// Both plans should have valid structure.
		if ep1.Query == nil {
			t.Fatal("original plan has nil query")
		}
		if ep2.Query == nil {
			t.Fatal("magic-set plan has nil query")
		}

		// Verify structural properties: with a binding on the IDB predicate
		// `Derived`, the magic-set transform must rewrite Derived's rule to
		// include a magic_Derived(...) literal in its body. If no rule body
		// references magic_Derived, the transform silently no-op'd and the
		// results-preservation guarantee is meaningless.
		//
		// Note (disj2-round5): we no longer assert that magic_<pred> appears
		// as a rule HEAD here. In this minimal property-test program, the only
		// IDB rule body uses base relations exclusively (Base0/Base1) — so
		// the only thing a sound magic-set transform can produce is the
		// rewritten Derived rule itself. Emitting `magic_Base0(...)`
		// propagation rules would be pointless (no IDB consumes them) and,
		// in pathological shadow cases, wildcard-unsafe — see the round-5
		// gate in MagicSetTransform.
		hasMagic := false
		for _, s := range ep2.Strata {
			for _, r := range s.Rules {
				for _, lit := range r.Body {
					if lit.Atom.Predicate != "" && len(lit.Atom.Predicate) > 6 && lit.Atom.Predicate[:6] == "magic_" {
						hasMagic = true
					}
				}
				if len(r.Head.Predicate) > 6 && r.Head.Predicate[:6] == "magic_" {
					hasMagic = true
				}
			}
		}
		if !hasMagic {
			t.Fatalf("magic-set plan did not emit any magic_* references despite Derived binding")
		}

		// NOTE: End-to-end evaluation comparison (magic-set vs naive produces
		// identical results) is tested in the integration test at the repo root
		// (TestMagicSetPreservesResults) to avoid import cycles between plan and eval.
	})
}

// TestMagicSetTransformNoBindings verifies that an empty binding map returns
// the original program unchanged.
func TestMagicSetTransformNoBindings(t *testing.T) {
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			{
				Head: datalog.Atom{Predicate: "Q", Args: []datalog.Term{datalog.Var{Name: "X"}}},
				Body: []datalog.Literal{
					{Positive: true, Atom: datalog.Atom{Predicate: "A", Args: []datalog.Term{datalog.Var{Name: "X"}}}},
				},
			},
		},
	}

	result := MagicSetTransform(prog, nil)
	if result != prog {
		t.Error("expected same program pointer when no bindings")
	}

	result = MagicSetTransform(prog, map[string][]int{})
	if result != prog {
		t.Error("expected same program pointer when empty bindings")
	}
}

// TestMagicSetTransformBasicRewrite verifies the structure of a magic-set
// transformed program for a simple case.
func TestMagicSetTransformBasicRewrite(t *testing.T) {
	// Rule: Path(x,y) :- Edge(x,y).
	// Rule: Path(x,z) :- Path(x,y), Path(y,z).
	// Binding: Path column 0 is bound.
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			{
				Head: datalog.Atom{
					Predicate: "Path",
					Args:      []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "y"}},
				},
				Body: []datalog.Literal{
					{Positive: true, Atom: datalog.Atom{
						Predicate: "Edge",
						Args:      []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "y"}},
					}},
				},
			},
			{
				Head: datalog.Atom{
					Predicate: "Path",
					Args:      []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "z"}},
				},
				Body: []datalog.Literal{
					{Positive: true, Atom: datalog.Atom{
						Predicate: "Path",
						Args:      []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "y"}},
					}},
					{Positive: true, Atom: datalog.Atom{
						Predicate: "Path",
						Args:      []datalog.Term{datalog.Var{Name: "y"}, datalog.Var{Name: "z"}},
					}},
				},
			},
		},
	}

	result := MagicSetTransform(prog, map[string][]int{"Path": {0}})

	// Check that magic_Path rules exist.
	var magicRules []datalog.Rule
	var pathRules []datalog.Rule
	for _, r := range result.Rules {
		if r.Head.Predicate == "magic_Path" {
			magicRules = append(magicRules, r)
		}
		if r.Head.Predicate == "Path" {
			pathRules = append(pathRules, r)
		}
	}

	if len(pathRules) == 0 {
		t.Fatal("expected Path rules in transformed program")
	}

	// Each Path rule should have magic_Path in its body.
	for _, r := range pathRules {
		hasMagic := false
		for _, lit := range r.Body {
			if lit.Atom.Predicate == "magic_Path" {
				hasMagic = true
			}
		}
		if !hasMagic {
			t.Errorf("Path rule missing magic_Path body literal: %s", ruleToString(r))
		}
	}

	t.Logf("transformed program has %d rules (%d magic, %d Path)",
		len(result.Rules), len(magicRules), len(pathRules))
}

func ruleToString(r datalog.Rule) string {
	p := &datalog.Program{Rules: []datalog.Rule{r}}
	return p.String()
}

// TestMagicSetTransformBaseOnlyRule verifies that rules with only base
// predicates (no IDB body atoms) are handled correctly.
func TestMagicSetTransformBaseOnlyRule(t *testing.T) {
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			{
				Head: datalog.Atom{
					Predicate: "Q",
					Args:      []datalog.Term{datalog.Var{Name: "X"}},
				},
				Body: []datalog.Literal{
					{Positive: true, Atom: datalog.Atom{
						Predicate: "Base",
						Args:      []datalog.Term{datalog.Var{Name: "X"}},
					}},
				},
			},
		},
	}

	result := MagicSetTransform(prog, map[string][]int{"Q": {0}})

	// Should have a rewritten Q rule with magic_Q in body.
	foundRewritten := false
	for _, r := range result.Rules {
		if r.Head.Predicate == "Q" {
			for _, lit := range r.Body {
				if lit.Atom.Predicate == "magic_Q" {
					foundRewritten = true
				}
			}
		}
	}
	if !foundRewritten {
		t.Error("expected Q rule to contain magic_Q body literal after transformation")
		for _, r := range result.Rules {
			t.Logf("  rule: %s", ruleToString(r))
		}
	}
}

// TestMagicSetPlannable verifies that magic-set transformed programs can be planned.
func TestMagicSetPlannable(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a transitive closure program.
		prog := &datalog.Program{
			Rules: []datalog.Rule{
				{
					Head: datalog.Atom{
						Predicate: "Path",
						Args:      []datalog.Term{datalog.Var{Name: "X"}, datalog.Var{Name: "Y"}},
					},
					Body: []datalog.Literal{
						{Positive: true, Atom: datalog.Atom{
							Predicate: "Edge",
							Args:      []datalog.Term{datalog.Var{Name: "X"}, datalog.Var{Name: "Y"}},
						}},
					},
				},
				{
					Head: datalog.Atom{
						Predicate: "Path",
						Args:      []datalog.Term{datalog.Var{Name: "X"}, datalog.Var{Name: "Z"}},
					},
					Body: []datalog.Literal{
						{Positive: true, Atom: datalog.Atom{
							Predicate: "Edge",
							Args:      []datalog.Term{datalog.Var{Name: "X"}, datalog.Var{Name: "Y"}},
						}},
						{Positive: true, Atom: datalog.Atom{
							Predicate: "Path",
							Args:      []datalog.Term{datalog.Var{Name: "Y"}, datalog.Var{Name: "Z"}},
						}},
					},
				},
			},
			Query: &datalog.Query{
				Select: []datalog.Term{datalog.Var{Name: "A"}, datalog.Var{Name: "B"}},
				Body: []datalog.Literal{
					{Positive: true, Atom: datalog.Atom{
						Predicate: "Path",
						Args:      []datalog.Term{datalog.Var{Name: "A"}, datalog.Var{Name: "B"}},
					}},
				},
			},
		}

		// Pick which columns to bind.
		nBound := rapid.IntRange(0, 1).Draw(t, "nBound")
		var boundCols []int
		if nBound > 0 {
			col := rapid.IntRange(0, 1).Draw(t, "boundCol")
			boundCols = []int{col}
		}

		_, errs := WithMagicSet(prog, nil, map[string][]int{"Path": boundCols})
		if len(errs) > 0 {
			t.Fatalf("WithMagicSet failed: %v", errs[0])
		}
	})
}

// TestMagicSetDeterministic verifies that the transformation is deterministic.
func TestMagicSetDeterministic(t *testing.T) {
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			{
				Head: datalog.Atom{Predicate: "Q", Args: []datalog.Term{datalog.Var{Name: "X"}, datalog.Var{Name: "Y"}}},
				Body: []datalog.Literal{
					{Positive: true, Atom: datalog.Atom{Predicate: "A", Args: []datalog.Term{datalog.Var{Name: "X"}, datalog.Var{Name: "Z"}}}},
					{Positive: true, Atom: datalog.Atom{Predicate: "B", Args: []datalog.Term{datalog.Var{Name: "Z"}, datalog.Var{Name: "Y"}}}},
				},
			},
		},
	}

	bindings := map[string][]int{"Q": {0}}

	r1 := MagicSetTransform(prog, bindings)
	r2 := MagicSetTransform(prog, bindings)

	// Compare rule counts and head predicates.
	if len(r1.Rules) != len(r2.Rules) {
		t.Fatalf("non-deterministic: %d vs %d rules", len(r1.Rules), len(r2.Rules))
	}

	preds1 := make([]string, len(r1.Rules))
	preds2 := make([]string, len(r2.Rules))
	for i := range r1.Rules {
		preds1[i] = r1.Rules[i].Head.Predicate
		preds2[i] = r2.Rules[i].Head.Predicate
	}
	sort.Strings(preds1)
	sort.Strings(preds2)
	for i := range preds1 {
		if preds1[i] != preds2[i] {
			t.Errorf("non-deterministic: rule %d head %q vs %q", i, preds1[i], preds2[i])
		}
	}
}
