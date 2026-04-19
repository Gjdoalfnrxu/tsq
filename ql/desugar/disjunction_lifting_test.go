package desugar_test

import (
	"strings"
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
)

// classifyDisj splits the synthetic disjunction rules into per-branch
// IDBs (`_disj_N_l` / `_disj_N_r`) and union projection rules
// (`_disj_N`). After per-branch lifting (#166), every disjunction
// produces 2 of each.
func classifyDisj(prog *datalog.Program) (branch, union []*datalog.Rule) {
	for i := range prog.Rules {
		name := prog.Rules[i].Head.Predicate
		if !strings.HasPrefix(name, "_disj_") {
			continue
		}
		if strings.HasSuffix(name, "_l") || strings.HasSuffix(name, "_r") {
			branch = append(branch, &prog.Rules[i])
		} else {
			union = append(union, &prog.Rules[i])
		}
	}
	return
}

// TestLiftedDisjunction_FiresOnSimpleDisjunction is the basic shape
// check: a single `or` produces 2 per-branch IDBs + 2 union projection
// rules. The caller boundary still sees a single union literal.
func TestLiftedDisjunction_FiresOnSimpleDisjunction(t *testing.T) {
	src := `
class Foo { Foo() { any() } }
predicate p(Foo x) { a(x) or b(x) }
`
	prog := desugarOK(t, parseAndResolve(t, src))
	branch, union := classifyDisj(prog)

	if len(branch) != 2 {
		t.Fatalf("expected 2 per-branch IDB rules, got %d", len(branch))
	}
	if len(union) != 2 {
		t.Fatalf("expected 2 union projection rules, got %d", len(union))
	}
	// Both union rules must have the same head predicate name.
	if union[0].Head.Predicate != union[1].Head.Predicate {
		t.Errorf("union rules disagree on head name: %q vs %q",
			union[0].Head.Predicate, union[1].Head.Predicate)
	}
	// Each union rule body is a single positive literal — the
	// "trivial-IDB projection" shape that P2a class-extent sizing
	// covers without new planner code.
	for _, r := range union {
		if len(r.Body) != 1 || !r.Body[0].Positive {
			t.Errorf("union rule body should be a single positive literal, got %+v", r.Body)
		}
		callee := r.Body[0].Atom.Predicate
		if !strings.HasSuffix(callee, "_l") && !strings.HasSuffix(callee, "_r") {
			t.Errorf("union rule body should call a per-branch IDB, got %q", callee)
		}
	}
}

// TestLiftedDisjunction_PerBranchHeadCarriesFullVars verifies the
// load-bearing #166 invariant: the per-branch IDB head retains its
// full var set (so the planner can size each branch independently),
// while the union head still projects to the shared-vars-only
// intersection seen by the caller.
func TestLiftedDisjunction_PerBranchHeadCarriesFullVars(t *testing.T) {
	src := `
predicate a(int x, int y) { any() }
predicate b(int x, int z) { any() }
predicate test(int x) { a(x, _) or b(x, _) }
`
	prog := desugarOK(t, parseAndResolve(t, src))
	branch, union := classifyDisj(prog)

	if len(branch) != 2 || len(union) != 2 {
		t.Fatalf("expected 2 branch + 2 union, got %d/%d", len(branch), len(union))
	}

	// Union heads carry only `x` (the shared var). `_` is wildcard
	// and must NEVER appear as a positional binding.
	for _, r := range union {
		if len(r.Head.Args) != 1 {
			t.Errorf("union head should have exactly 1 arg (shared `x`), got %d", len(r.Head.Args))
		}
		for _, arg := range r.Head.Args {
			if v, ok := arg.(datalog.Var); ok {
				if v.Name != "x" {
					t.Errorf("union head arg should be `x`, got %q", v.Name)
				}
			}
		}
	}

	// Per-branch heads carry the full visible var set (still excluding
	// the wildcard `_` placeholder — wildcards must never become
	// positional bindings).
	for _, r := range branch {
		for _, arg := range r.Head.Args {
			if v, ok := arg.(datalog.Var); ok && v.Name == "_" {
				t.Errorf("branch head must not contain wildcard `_` as a positional binding (rule %s)",
					r.Head.Predicate)
			}
		}
	}
}

// TestLiftedDisjunction_NestedDisjunctionsCompose verifies that two
// nested disjunctions (`a or b or c`) compose correctly: each level of
// disjunction adds its own per-branch lifting, so a 3-way disjunction
// yields 4 branch IDBs + 4 union rules across two _disj_N predicates.
func TestLiftedDisjunction_NestedDisjunctionsCompose(t *testing.T) {
	src := `
predicate a(int x) { any() }
predicate b(int x) { any() }
predicate c(int x) { any() }
predicate test(int x) { a(x) or b(x) or c(x) }
`
	prog := desugarOK(t, parseAndResolve(t, src))
	branch, union := classifyDisj(prog)

	if len(branch) != 4 {
		t.Fatalf("expected 4 per-branch IDB rules (2 per disjunction node), got %d", len(branch))
	}
	if len(union) != 4 {
		t.Fatalf("expected 4 union projection rules (2 per disjunction node), got %d", len(union))
	}
}

// TestLiftedDisjunction_TenBranches is the §6.5 plan gate: a wide
// disjunction (the value-flow shape that motivated #166) lifts to one
// IDB per branch. Each branch can then be sized independently by the
// existing planner machinery without falling back to the 1000-row
// default that breaks join ordering on synthesised unions.
func TestLiftedDisjunction_TenBranches(t *testing.T) {
	// Build a 10-branch disjunction inline.
	branches := []string{}
	for i := 0; i < 10; i++ {
		branches = append(branches, "p(x)")
	}
	src := "predicate p(int x) { any() }\n" +
		"predicate test(int x) { " + strings.Join(branches, " or ") + " }\n"

	prog := desugarOK(t, parseAndResolve(t, src))
	branch, union := classifyDisj(prog)

	// 10 branches → 9 disjunction nodes (right-associated parse) →
	// 9 * 2 = 18 branch rules and 18 union rules. Don't pin the
	// associativity exactly; just assert we got per-branch lifting
	// for every disjunction node.
	if len(branch) < 18 {
		t.Errorf("expected >=18 per-branch IDB rules from 10-way disjunction, got %d", len(branch))
	}
	if len(union) < 18 {
		t.Errorf("expected >=18 union rules from 10-way disjunction, got %d", len(union))
	}
	if len(branch) != len(union) {
		t.Errorf("branch and union rule counts must match, got %d vs %d", len(branch), len(union))
	}
}

// TestLiftedDisjunction_UnderscorePreserved is a regression guard for
// the wildcard handling: per-branch lifting must NOT promote `_` into
// a head binding even though `_` appears in both branches' bodies. If
// this test fails, multiple `_` occurrences in a single branch body
// would be unified — silently filtering rows that would otherwise
// match. (This bug bit during initial PR4 development on the
// react-usestate `find_setstate_updater_calls_other_setstate` golden
// — one row dropped.)
func TestLiftedDisjunction_UnderscorePreserved(t *testing.T) {
	src := `
predicate Edge(int a, int b, int c) { any() }
predicate FunctionContains(int a, int b) { any() }
predicate test(int x, int y) {
    FunctionContains(x, y)
    or
    exists(int mid |
        FunctionContains(x, mid) and
        Edge(mid, _, _) and
        FunctionContains(mid, y)
    )
}
`
	prog := desugarOK(t, parseAndResolve(t, src))
	branch, _ := classifyDisj(prog)
	for _, r := range branch {
		for _, arg := range r.Head.Args {
			if v, ok := arg.(datalog.Var); ok && v.Name == "_" {
				t.Fatalf("rule %s head contains wildcard `_` as a positional binding — would unify body `_`s and silently drop rows",
					r.Head.Predicate)
			}
		}
	}
}

// TestLiftedDisjunction_CallerSeesUnionLiteral verifies the caller
// boundary contract: the calling rule's body still references a single
// `_disj_N(SV...)` literal over the shared vars only. This is what
// keeps the lifting transform a non-breaking change for upstream
// passes (magic-set, demand inference, etc.) — they see the same
// union literal as before, just sized correctly now.
func TestLiftedDisjunction_CallerSeesUnionLiteral(t *testing.T) {
	src := `
class Foo { Foo() { any() } }
predicate p(Foo x) { a(x) or b(x) }
`
	prog := desugarOK(t, parseAndResolve(t, src))
	r := findRuleExact(prog, "p")
	if r == nil {
		t.Fatal("expected rule 'p'")
	}
	var disjCalls int
	for _, lit := range r.Body {
		name := lit.Atom.Predicate
		if !strings.HasPrefix(name, "_disj_") {
			continue
		}
		disjCalls++
		if strings.HasSuffix(name, "_l") || strings.HasSuffix(name, "_r") {
			t.Errorf("caller body must call the union IDB, not a per-branch IDB (got %q)", name)
		}
	}
	if disjCalls != 1 {
		t.Errorf("caller body should reference exactly one _disj union literal, got %d", disjCalls)
	}
}
