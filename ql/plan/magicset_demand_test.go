package plan

import (
	"strings"
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
)

// progSynthDisjShape mirrors what the desugarer emits for a 2-arg
// synthesised disjunction predicate called from a class-extent-guarded
// caller:
//
//	Caller(c, y) :- ClassExt(c), _disj_2(c, y).
//	_disj_2(c, y) :- A(c, y).
//	_disj_2(c, y) :- B(c, m), C(m, n), D(n, y).
//	_disj_2(c, y) :- E(c, m), F(m, y).
//
// ClassExt is a small extent (well below SmallExtentThreshold).
// A, B, C, D, E, F are large-ish base relations.
//
// The query asks for `Caller(c, y)`. With magic-set augmentation
// applied to the rule-body demand, _disj_2 should be rewritten so
// `magic__disj_2(c)` filters its body, and a seed rule
// `magic__disj_2(c) :- ClassExt(c)` provides the magic extension.
func progSynthDisjShape() (*datalog.Program, map[string]int) {
	rules := []datalog.Rule{
		// Caller binds head col 0 of _disj_2 via the small ClassExt.
		{
			Head: datalog.Atom{Predicate: "Caller", Args: []datalog.Term{datalog.Var{Name: "c"}, datalog.Var{Name: "y"}}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "ClassExt", Args: []datalog.Term{datalog.Var{Name: "c"}}}},
				{Positive: true, Atom: datalog.Atom{Predicate: "_disj_2", Args: []datalog.Term{datalog.Var{Name: "c"}, datalog.Var{Name: "y"}}}},
			},
		},
		// _disj_2 disjunction branches: each is a base-relation join
		// with multiple atoms sharing a mid-var (the canonical synth
		// shape from desugar.go:625-651).
		{
			Head: datalog.Atom{Predicate: "_disj_2", Args: []datalog.Term{datalog.Var{Name: "c"}, datalog.Var{Name: "y"}}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "A", Args: []datalog.Term{datalog.Var{Name: "c"}, datalog.Var{Name: "y"}}}},
			},
		},
		{
			Head: datalog.Atom{Predicate: "_disj_2", Args: []datalog.Term{datalog.Var{Name: "c"}, datalog.Var{Name: "y"}}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "B", Args: []datalog.Term{datalog.Var{Name: "c"}, datalog.Var{Name: "m"}}}},
				{Positive: true, Atom: datalog.Atom{Predicate: "C", Args: []datalog.Term{datalog.Var{Name: "m"}, datalog.Var{Name: "n"}}}},
				{Positive: true, Atom: datalog.Atom{Predicate: "D", Args: []datalog.Term{datalog.Var{Name: "n"}, datalog.Var{Name: "y"}}}},
			},
		},
		{
			Head: datalog.Atom{Predicate: "_disj_2", Args: []datalog.Term{datalog.Var{Name: "c"}, datalog.Var{Name: "y"}}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "E", Args: []datalog.Term{datalog.Var{Name: "c"}, datalog.Var{Name: "m"}}}},
				{Positive: true, Atom: datalog.Atom{Predicate: "F", Args: []datalog.Term{datalog.Var{Name: "m"}, datalog.Var{Name: "y"}}}},
			},
		},
	}
	prog := &datalog.Program{
		Rules: rules,
		Query: &datalog.Query{
			Select: []datalog.Term{datalog.Var{Name: "c"}, datalog.Var{Name: "y"}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "Caller", Args: []datalog.Term{datalog.Var{Name: "c"}, datalog.Var{Name: "y"}}}},
			},
		},
	}
	hints := map[string]int{
		"ClassExt": 7,      // small extent — well under SmallExtentThreshold
		"A":        100000, // each branch large
		"B":        100000,
		"C":        100000,
		"D":        100000,
		"E":        100000,
		"F":        100000,
	}
	return prog, hints
}

func TestInferRuleBodyDemandBindings_SynthDisjFires(t *testing.T) {
	prog, hints := progSynthDisjShape()
	idb := IDBPredicates(prog)

	bindings, seeds := InferRuleBodyDemandBindings(prog, idb, hints)
	if bindings == nil {
		t.Fatalf("expected non-nil bindings for _disj_2 under demand from ClassExt, got nil")
	}
	cols, ok := bindings["_disj_2"]
	if !ok {
		t.Fatalf("expected _disj_2 in bindings, got %v", bindings)
	}
	if len(cols) != 1 || cols[0] != 0 {
		t.Fatalf("expected _disj_2 bound at col 0, got %v", cols)
	}

	// Seed rule shape: magic__disj_2(c) :- ClassExt(c).
	if len(seeds) == 0 {
		t.Fatalf("expected at least one demand-seed rule for magic__disj_2, got 0")
	}
	foundClassExtSeed := false
	for _, sr := range seeds {
		if sr.Head.Predicate != "magic__disj_2" {
			continue
		}
		if len(sr.Head.Args) != 1 {
			t.Errorf("seed head arity: want 1, got %d", len(sr.Head.Args))
		}
		// Body should ground head var via ClassExt
		for _, lit := range sr.Body {
			if lit.Atom.Predicate == "ClassExt" {
				foundClassExtSeed = true
			}
		}
	}
	if !foundClassExtSeed {
		t.Fatalf("expected a demand-seed body grounding c via ClassExt; seeds=%+v", seeds)
	}
}

func TestWithMagicSetAuto_SynthDisjRewriteFires(t *testing.T) {
	prog, hints := progSynthDisjShape()
	ep, inf, errs := WithMagicSetAutoOpts(prog, hints, MagicSetOptions{Strict: true})
	if len(errs) > 0 {
		t.Fatalf("WithMagicSetAutoOpts strict failed: %v", errs)
	}
	if ep == nil {
		t.Fatalf("nil execution plan")
	}
	cols, ok := inf.Bindings["_disj_2"]
	if !ok || len(cols) == 0 || cols[0] != 0 {
		t.Fatalf("expected _disj_2 in inf.Bindings with col 0, got %v", inf.Bindings)
	}

	// The augmented program should include rewritten _disj_2 rules
	// whose body starts with magic__disj_2(c). Walk strata to find
	// _disj_2 plans and assert.
	rewrittenDisjSeen := 0
	for _, st := range ep.Strata {
		for _, r := range st.Rules {
			if r.Head.Predicate != "_disj_2" {
				continue
			}
			rewrittenDisjSeen++
			if len(r.Body) == 0 {
				t.Fatalf("_disj_2 rule has empty body")
			}
			first := r.Body[0]
			if first.Atom.Predicate != "magic__disj_2" {
				t.Errorf("expected first body literal of _disj_2 to be magic__disj_2, got %s", first.Atom.Predicate)
			}
		}
	}
	if rewrittenDisjSeen == 0 {
		t.Fatalf("no _disj_2 rules found in augmented plan; strata=%+v", ep.Strata)
	}

	// And: a magic__disj_2 seed must be present in the augmented
	// program (otherwise rewritten _disj_2 produces zero tuples).
	seedSeen := false
	for _, st := range ep.Strata {
		for _, r := range st.Rules {
			if r.Head.Predicate == "magic__disj_2" {
				seedSeen = true
				// At least one of the magic_disj_2 rules should ground
				// c via ClassExt (the small-extent demand source).
				for _, lit := range r.Body {
					if lit.Atom.Predicate == "ClassExt" {
						return // success
					}
				}
			}
		}
	}
	if !seedSeen {
		t.Fatalf("no magic__disj_2 seed rule found in augmented plan")
	}
	t.Fatalf("magic__disj_2 seeded but no ClassExt-grounded variant found")
}

// TestWithMagicSetAuto_SynthDisjJoinSeedSelectsMagic asserts that
// once the rewrite fires, each _disj_2 rule's planned join order
// places the magic literal first — that's the runtime mechanism by
// which the binding-cap blowup is bounded.
func TestWithMagicSetAuto_SynthDisjJoinSeedSelectsMagic(t *testing.T) {
	prog, hints := progSynthDisjShape()
	// Provide a small hint for magic__disj_2 too — the size is
	// bounded by ClassExt cardinality, so it's tiny by construction.
	hints["magic__disj_2"] = 7

	ep, _, errs := WithMagicSetAutoOpts(prog, hints, MagicSetOptions{Strict: true})
	if len(errs) > 0 {
		t.Fatalf("plan failed: %v", errs)
	}
	for _, st := range ep.Strata {
		for _, r := range st.Rules {
			if r.Head.Predicate != "_disj_2" {
				continue
			}
			if len(r.JoinOrder) == 0 {
				t.Fatalf("_disj_2 plan has no join order")
			}
			seed := r.JoinOrder[0].Literal
			if seed.Atom.Predicate != "magic__disj_2" {
				t.Errorf("expected magic__disj_2 to be the first join step (cardinality-bounded seed); got %s. Full join order: %s",
					seed.Atom.Predicate, dumpJoinOrder(r.JoinOrder))
			}
		}
	}
}

// TestWithMagicSetAuto_SynthDisjQueryOnlyFallsThrough is the negative
// control: when there's no caller-side small-extent grounding, the
// rule-body magic-set augmentation should NOT fire and the planner
// should produce the plain Plan output. This confirms the gating is
// driven by the demand map rather than blanket-rewriting every
// `_disj_*` predicate by name.
func TestWithMagicSetAuto_SynthDisjNoDemandFallsThrough(t *testing.T) {
	rules := []datalog.Rule{
		// Caller has NO small-extent atom — only large IDBs and base
		// relations of unknown size. _disj_2 has no demand source.
		{
			Head: datalog.Atom{Predicate: "Caller", Args: []datalog.Term{datalog.Var{Name: "c"}, datalog.Var{Name: "y"}}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "_disj_2", Args: []datalog.Term{datalog.Var{Name: "c"}, datalog.Var{Name: "y"}}}},
			},
		},
		{
			Head: datalog.Atom{Predicate: "_disj_2", Args: []datalog.Term{datalog.Var{Name: "c"}, datalog.Var{Name: "y"}}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "A", Args: []datalog.Term{datalog.Var{Name: "c"}, datalog.Var{Name: "y"}}}},
			},
		},
	}
	prog := &datalog.Program{
		Rules: rules,
		Query: &datalog.Query{
			Select: []datalog.Term{datalog.Var{Name: "c"}, datalog.Var{Name: "y"}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "Caller", Args: []datalog.Term{datalog.Var{Name: "c"}, datalog.Var{Name: "y"}}}},
			},
		},
	}
	hints := map[string]int{"A": 100000}
	idb := IDBPredicates(prog)
	bindings, seeds := InferRuleBodyDemandBindings(prog, idb, hints)
	if len(bindings) != 0 {
		t.Fatalf("expected no rule-body bindings without small-extent demand; got %v", bindings)
	}
	if len(seeds) != 0 {
		t.Fatalf("expected no rule-body seeds; got %d", len(seeds))
	}
}

// TestInferRuleBodyDemandBindings_RenameTrampolinePropagation
// mirrors the Mastodon `_disj_2` shape that PR #149 missed and this
// follow-up fixes:
//
//	top(b) :- SmallExt(a), Bridge(a, x), mid(x, b).
//	mid(x, b) :- _disj_N(x, b).
//	_disj_N(x, b) :- BigBase1(x, m), BigBase2(m, b).
//
// `_disj_N`'s only call site is the pure-rename trampoline `mid(x,b)
// :- _disj_N(x,b)`. The trampoline has zero preceding literals to
// ground `x`, so `buildDemandSeedsForPred` cannot synthesise a safe
// `magic__disj_N(x)` seed at that site. The grounding actually
// exists at the grandparent `top` rule via `SmallExt(a)` (small
// extent) → `Bridge(a, x)` (constant-bearing base shares `a`).
//
// The fix lifts the demand into `mid` (the parent of the rename) so
// `magic_mid(x)` is seeded from `top`'s body, and the magic-set
// transform's `propagateBindings` then chains
// `magic_mid` → `magic__disj_N` automatically.
func TestInferRuleBodyDemandBindings_RenameTrampolinePropagation(t *testing.T) {
	rules := []datalog.Rule{
		{
			Head: datalog.Atom{Predicate: "top", Args: []datalog.Term{datalog.Var{Name: "b"}}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "SmallExt", Args: []datalog.Term{datalog.Var{Name: "a"}}}},
				{Positive: true, Atom: datalog.Atom{Predicate: "Bridge", Args: []datalog.Term{datalog.Var{Name: "a"}, datalog.Var{Name: "x"}, datalog.IntConst{Value: 0}}}},
				{Positive: true, Atom: datalog.Atom{Predicate: "mid", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "b"}}}},
			},
		},
		// Pure rename: mid(x,b) :- _disj_N(x,b). No preceding
		// literals to ground x.
		{
			Head: datalog.Atom{Predicate: "mid", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "b"}}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "_disj_N", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "b"}}}},
			},
		},
		// Big base join — the cardinality-dangerous body.
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
		"SmallExt": 7,
		"Bridge":   100000,
		"BigBase1": 100000,
		"BigBase2": 100000,
	}
	idb := IDBPredicates(prog)

	bindings, seeds := InferRuleBodyDemandBindings(prog, idb, hints)
	if len(bindings) == 0 {
		t.Fatalf("expected non-empty bindings (parent-lifted), got nil")
	}
	if cols, ok := bindings["_disj_N"]; !ok || len(cols) == 0 || cols[0] != 0 {
		t.Fatalf("expected _disj_N bound at col 0, got %v", bindings["_disj_N"])
	}
	if cols, ok := bindings["mid"]; !ok || len(cols) == 0 || cols[0] != 0 {
		t.Fatalf("expected parent `mid` lifted into bindings at col 0, got %v", bindings["mid"])
	}
	// Seeds must include a magic_mid(x) :- SmallExt(a), Bridge(a,x,0)
	// (or similar shape) — the grandparent grounding context.
	foundParentSeed := false
	for _, sr := range seeds {
		if sr.Head.Predicate != "magic_mid" {
			continue
		}
		for _, lit := range sr.Body {
			if lit.Atom.Predicate == "SmallExt" {
				foundParentSeed = true
			}
		}
	}
	if !foundParentSeed {
		t.Fatalf("expected magic_mid seed grounding x via SmallExt; seeds=%+v", seeds)
	}
}

// TestWithMagicSetAuto_RenameTrampolineEndToEnd asserts that the
// rename-trampoline shape produces a fully wired augmented program:
// magic_mid is seeded from the grandparent context, magic__disj_N is
// derived from magic_mid by `propagateBindings`, and `_disj_N`'s
// body is rewritten with `magic__disj_N(x)` as its first literal.
func TestWithMagicSetAuto_RenameTrampolineEndToEnd(t *testing.T) {
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
		"SmallExt": 7,
		"Bridge":   100000,
		"BigBase1": 100000,
		"BigBase2": 100000,
	}
	ep, inf, errs := WithMagicSetAutoOpts(prog, hints, MagicSetOptions{Strict: true})
	if len(errs) > 0 {
		t.Fatalf("WithMagicSetAutoOpts strict failed: %v", errs)
	}
	if ep == nil {
		t.Fatalf("nil execution plan")
	}
	if cols, ok := inf.Bindings["_disj_N"]; !ok || len(cols) == 0 || cols[0] != 0 {
		t.Fatalf("expected _disj_N bound at col 0 in inf.Bindings, got %v", inf.Bindings)
	}
	if cols, ok := inf.Bindings["mid"]; !ok || len(cols) == 0 || cols[0] != 0 {
		t.Fatalf("expected mid lifted into inf.Bindings at col 0, got %v", inf.Bindings)
	}
	// _disj_N rules must be rewritten to lead with magic__disj_N.
	disjRewritten := 0
	for _, st := range ep.Strata {
		for _, r := range st.Rules {
			if r.Head.Predicate != "_disj_N" {
				continue
			}
			disjRewritten++
			if len(r.Body) == 0 || r.Body[0].Atom.Predicate != "magic__disj_N" {
				t.Errorf("expected first body literal of _disj_N to be magic__disj_N; got %s", r.Body[0].Atom.Predicate)
			}
		}
	}
	if disjRewritten == 0 {
		t.Fatalf("no _disj_N rules in augmented plan")
	}
}

func dumpJoinOrder(steps []JoinStep) string {
	parts := make([]string, 0, len(steps))
	for _, s := range steps {
		parts = append(parts, s.Literal.Atom.Predicate)
	}
	return strings.Join(parts, " -> ")
}
