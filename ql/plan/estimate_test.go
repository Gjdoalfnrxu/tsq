package plan_test

import (
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

// TestIdentifyTrivialIDBsBasePredsOnly: a rule whose body is only base atoms
// is identified as trivial.
func TestIdentifyTrivialIDBsBasePredsOnly(t *testing.T) {
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			rule(atom("Q", "x"), posLit("A", "x"), posLit("B", "x")),
		},
	}
	base := map[string]bool{"A": true, "B": true}
	got := plan.IdentifyTrivialIDBs(prog, base)
	if len(got) != 1 || got[0].Name != "Q" {
		t.Fatalf("expected [Q], got %+v", got)
	}
	if got[0].Arity != 1 {
		t.Errorf("arity: want 1, got %d", got[0].Arity)
	}
	if len(got[0].Rules) != 1 {
		t.Errorf("rules: want 1, got %d", len(got[0].Rules))
	}
}

// TestIdentifyTrivialIDBsTransitive: an IDB that depends on another IDB is
// trivial iff that dependency is itself trivial. Topological order.
func TestIdentifyTrivialIDBsTransitive(t *testing.T) {
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			// L1 :- A      (trivial level 1)
			rule(atom("L1", "x"), posLit("A", "x")),
			// L2 :- L1, B  (trivial level 2 — depends on L1)
			rule(atom("L2", "x"), posLit("L1", "x"), posLit("B", "x")),
			// L3 :- L2     (trivial level 3 — depends on L2)
			rule(atom("L3", "x"), posLit("L2", "x")),
		},
	}
	base := map[string]bool{"A": true, "B": true}
	got := plan.IdentifyTrivialIDBs(prog, base)
	if len(got) != 3 {
		t.Fatalf("want 3 trivials, got %d (%+v)", len(got), got)
	}
	// Topological order is required by the eval-side estimator.
	wantOrder := []string{"L1", "L2", "L3"}
	for i, w := range wantOrder {
		if got[i].Name != w {
			t.Errorf("position %d: want %s, got %s (full: %+v)", i, w, got[i].Name, got)
		}
	}
}

// TestIdentifyTrivialIDBsNegationDisqualifies: a rule with a negative body
// literal is NOT trivial, even if the negated predicate is base.
func TestIdentifyTrivialIDBsNegationDisqualifies(t *testing.T) {
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			rule(atom("Q", "x"), posLit("A", "x"), negLit("B", "x")),
		},
	}
	base := map[string]bool{"A": true, "B": true}
	got := plan.IdentifyTrivialIDBs(prog, base)
	if len(got) != 0 {
		t.Errorf("negation should disqualify, got %+v", got)
	}
}

// TestIdentifyTrivialIDBsRecursionDisqualifies: a self-recursive rule is not
// trivial. (And neither is anything indirectly recursive — the closure won't
// admit the SCC because each member depends on something not yet accepted.)
func TestIdentifyTrivialIDBsRecursionDisqualifies(t *testing.T) {
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			// P(x) :- A(x).
			rule(atom("P", "x"), posLit("A", "x")),
			// P(x) :- P(x).  (recursive)
			rule(atom("P", "x"), posLit("P", "x")),
		},
	}
	base := map[string]bool{"A": true}
	got := plan.IdentifyTrivialIDBs(prog, base)
	if len(got) != 0 {
		t.Errorf("self-recursion should disqualify, got %+v", got)
	}
}

// TestIdentifyTrivialIDBsMissingBaseDisqualifies: a rule whose body
// references an unknown (neither base nor admitted-trivial) predicate is not
// trivial.
func TestIdentifyTrivialIDBsMissingBaseDisqualifies(t *testing.T) {
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			rule(atom("Q", "x"), posLit("A", "x"), posLit("Mystery", "x")),
		},
	}
	base := map[string]bool{"A": true}
	got := plan.IdentifyTrivialIDBs(prog, base)
	if len(got) != 0 {
		t.Errorf("missing dep should disqualify, got %+v", got)
	}
}

// TestIdentifyTrivialIDBsBaseShadowExcluded: a head whose name matches a base
// predicate is excluded — base relations come from the DB and cannot be
// re-derived by the pre-pass.
func TestIdentifyTrivialIDBsBaseShadowExcluded(t *testing.T) {
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			rule(atom("A", "x"), posLit("B", "x")),
		},
	}
	base := map[string]bool{"A": true, "B": true}
	got := plan.IdentifyTrivialIDBs(prog, base)
	if len(got) != 0 {
		t.Errorf("base-shadow should be excluded, got %+v", got)
	}
}
