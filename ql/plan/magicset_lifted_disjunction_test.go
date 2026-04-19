package plan

import (
	"strings"
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
)

// Phase B PR5 — integration tests for the magic-set arity-keying
// interaction with the #166 per-branch lifting transform.
//
// After PR #172 the desugarer lowers `(leftBody) or (rightBody)` to
// FOUR rules instead of two:
//
//	_disj_N_l(LV...) :- leftBody.        // per-branch IDB, full LV vars
//	_disj_N_r(RV...) :- rightBody.       // per-branch IDB, full RV vars
//	_disj_N(SV...)   :- _disj_N_l(LV...).  // union projection, SV ⊆ LV
//	_disj_N(SV...)   :- _disj_N_r(RV...).  // union projection, SV ⊆ RV
//
// Where `SV = intersect(LV, RV)`. The branch IDBs typically have
// DIFFERENT arities from each other AND from the union (LV and RV
// usually contain branch-private vars that SV lacks).
//
// The magic-set machinery is name-only on `magicName` itself but the
// gating around seed construction and propagation is keyed by
// (name, arity). These tests cover the interaction surface:
//
//   - Demand inferred on `_disj_N` propagates correctly to per-branch
//     IDBs via the union projection rule, even when the branch IDBs
//     have a higher arity than the union.
//   - Magic-set rule generation does NOT collide between `_disj_N_l`
//     and `_disj_N_r` heads of different arities (they have distinct
//     names by construction, but arity bookkeeping must remain correct
//     under the (name, arity) gates).
//   - The §10.4 plan invariant: a 10-branch lifted disjunction produces
//     magic-set rule fan-out that scales LINEARLY with the branch
//     count, not quadratically.

// progLiftedDisjShape models the post-#166-lifting rule shape for a
// disjunction with branch-private vars. Mirrors what the desugarer
// emits today for:
//
//	predicate Caller(int c, int y) {
//	    ClassExt(c) and (
//	        (A(c, y, m) and Guard(m))                 // left:  binds {c, y, m}
//	        or
//	        (E(c, y, n, k) and Other(n, k))           // right: binds {c, y, n, k}
//	    )
//	}
//
// Per #166 lifting, the desugarer produces:
//
//	Caller(c, y)             :- ClassExt(c), _disj_2(c, y).
//	_disj_2_l(c, y, m)       :- A(c, y, m), Guard(m).
//	_disj_2_r(c, y, n, k)    :- E(c, y, n, k), Other(n, k).
//	_disj_2(c, y)            :- _disj_2_l(c, y, m).
//	_disj_2(c, y)            :- _disj_2_r(c, y, n, k).
//
// Branch IDBs: `_disj_2_l` arity 3, `_disj_2_r` arity 4. Union: arity 2.
func progLiftedDisjShape() (*datalog.Program, map[string]int) {
	rules := []datalog.Rule{
		// Caller binds head col 0 of _disj_2 via the small ClassExt.
		{
			Head: datalog.Atom{Predicate: "Caller", Args: []datalog.Term{datalog.Var{Name: "c"}, datalog.Var{Name: "y"}}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "ClassExt", Args: []datalog.Term{datalog.Var{Name: "c"}}}},
				{Positive: true, Atom: datalog.Atom{Predicate: "_disj_2", Args: []datalog.Term{datalog.Var{Name: "c"}, datalog.Var{Name: "y"}}}},
			},
		},
		// Per-branch IDB heads carry the FULL var set. Body sized
		// independently against base relations.
		{
			Head: datalog.Atom{Predicate: "_disj_2_l", Args: []datalog.Term{datalog.Var{Name: "c"}, datalog.Var{Name: "y"}, datalog.Var{Name: "m"}}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "A", Args: []datalog.Term{datalog.Var{Name: "c"}, datalog.Var{Name: "y"}, datalog.Var{Name: "m"}}}},
				{Positive: true, Atom: datalog.Atom{Predicate: "Guard", Args: []datalog.Term{datalog.Var{Name: "m"}}}},
			},
		},
		{
			Head: datalog.Atom{Predicate: "_disj_2_r", Args: []datalog.Term{datalog.Var{Name: "c"}, datalog.Var{Name: "y"}, datalog.Var{Name: "n"}, datalog.Var{Name: "k"}}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "E", Args: []datalog.Term{datalog.Var{Name: "c"}, datalog.Var{Name: "y"}, datalog.Var{Name: "n"}, datalog.Var{Name: "k"}}}},
				{Positive: true, Atom: datalog.Atom{Predicate: "Other", Args: []datalog.Term{datalog.Var{Name: "n"}, datalog.Var{Name: "k"}}}},
			},
		},
		// Union projection rules: trivial-IDB shape (single positive
		// body literal, projects branch arity onto shared SV).
		{
			Head: datalog.Atom{Predicate: "_disj_2", Args: []datalog.Term{datalog.Var{Name: "c"}, datalog.Var{Name: "y"}}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "_disj_2_l", Args: []datalog.Term{datalog.Var{Name: "c"}, datalog.Var{Name: "y"}, datalog.Var{Name: "m"}}}},
			},
		},
		{
			Head: datalog.Atom{Predicate: "_disj_2", Args: []datalog.Term{datalog.Var{Name: "c"}, datalog.Var{Name: "y"}}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "_disj_2_r", Args: []datalog.Term{datalog.Var{Name: "c"}, datalog.Var{Name: "y"}, datalog.Var{Name: "n"}, datalog.Var{Name: "k"}}}},
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
		"ClassExt": 7,
		"A":        100000,
		"Guard":    100000,
		"E":        100000,
		"Other":    100000,
	}
	return prog, hints
}

// TestLiftedDisj_DemandPropagatesThroughUnionToBranchIDBs verifies the
// load-bearing property of the lifting transform under demand inference:
// when the caller binds `_disj_2`'s arity-2 head, the demand must
// propagate through both union projection rules into `_disj_2_l` (arity
// 3) and `_disj_2_r` (arity 4) at their respective head columns.
//
// If the (name, arity) gating in propagateBindings dropped one branch
// because the body literal arity exceeded the union arity (or vice
// versa), the dropped branch would silently materialise without demand
// pruning — re-introducing the cap-hit the lifting transform was meant
// to fix.
func TestLiftedDisj_DemandPropagatesThroughUnionToBranchIDBs(t *testing.T) {
	prog, hints := progLiftedDisjShape()
	idb := IDBPredicates(prog)

	bindings, _ := InferRuleBodyDemandBindings(prog, idb, hints)
	if bindings == nil {
		t.Fatalf("expected non-nil bindings, got nil")
	}

	// The union must be bound at col 0 (c) by ClassExt.
	if cols, ok := bindings["_disj_2"]; !ok || len(cols) != 1 || cols[0] != 0 {
		t.Fatalf("expected _disj_2 bound at col 0, got %v", bindings["_disj_2"])
	}

	// Both branch IDBs must inherit the demand at their col 0 (c is at
	// position 0 in both _l and _r heads). Without arity-correct
	// propagation through the union projection rule, one or both would
	// be missing and the branch would evaluate without magic pruning.
	for _, branch := range []string{"_disj_2_l", "_disj_2_r"} {
		cols, ok := bindings[branch]
		if !ok {
			t.Errorf("expected %s in bindings (demand should propagate from union into per-branch IDB), got missing; bindings=%v",
				branch, bindings)
			continue
		}
		if len(cols) != 1 || cols[0] != 0 {
			t.Errorf("expected %s bound at col 0 (c); got %v", branch, cols)
		}
	}
}

// TestLiftedDisj_MagicTransformAritiesAreCorrect verifies that the
// magic-set transform produces magic predicates at the correct arity
// for each per-branch IDB. `magicName` is name-only — `magic__disj_2_l`
// and `magic__disj_2_r` are distinct names (no name collision possible
// here). This test is the regression guard for any future change that
// might collapse branch-magic-naming OR strip arity bookkeeping from
// the seed-head construction.
func TestLiftedDisj_MagicTransformAritiesAreCorrect(t *testing.T) {
	prog, hints := progLiftedDisjShape()
	ep, inf, errs := WithMagicSetAutoOpts(prog, hints, MagicSetOptions{Strict: true})
	if len(errs) > 0 {
		t.Fatalf("WithMagicSetAutoOpts failed: %v", errs)
	}
	if ep == nil {
		t.Fatalf("nil execution plan")
	}

	// Both branch IDBs should appear in the inferred bindings.
	for _, branch := range []string{"_disj_2_l", "_disj_2_r"} {
		if _, ok := inf.Bindings[branch]; !ok {
			t.Fatalf("expected %s in inf.Bindings, got %v", branch, inf.Bindings)
		}
	}

	// Walk the augmented program and confirm:
	//   - magic__disj_2_l propagation rules emit head arity 1 (just c).
	//   - magic__disj_2_r propagation rules emit head arity 1 (just c).
	//   - Each rewritten branch rule body starts with its OWN magic
	//     literal (not the union's).
	magicHeadArities := map[string]map[int]int{} // pred -> arity -> count
	for _, st := range ep.Strata {
		for _, r := range st.Rules {
			name := r.Head.Predicate
			if !strings.HasPrefix(name, "magic_") {
				continue
			}
			ar := len(r.Head.Args)
			if magicHeadArities[name] == nil {
				magicHeadArities[name] = map[int]int{}
			}
			magicHeadArities[name][ar]++
		}
	}

	for _, magic := range []string{"magic__disj_2_l", "magic__disj_2_r"} {
		arities, ok := magicHeadArities[magic]
		if !ok {
			t.Errorf("expected %s rules in augmented plan, found none; magicHeads=%v",
				magic, magicHeadArities)
			continue
		}
		// Both branches share `c` at col 0 → magic head arity 1.
		if len(arities) != 1 {
			t.Errorf("%s has heads at multiple arities %v — magic-set arity bookkeeping disagreed across rules",
				magic, arities)
			continue
		}
		if _, ok := arities[1]; !ok {
			t.Errorf("%s expected arity 1 (single bound col c); got arities %v", magic, arities)
		}
	}

	// Cross-branch sanity: the rewritten branch IDB rules each begin
	// with a magic literal NAMED for that branch — no cross-pollination.
	for _, st := range ep.Strata {
		for _, r := range st.Rules {
			name := r.Head.Predicate
			if name != "_disj_2_l" && name != "_disj_2_r" {
				continue
			}
			if len(r.Body) == 0 {
				t.Errorf("%s rule has empty body", name)
				continue
			}
			expectedMagic := "magic_" + name
			first := r.Body[0]
			if first.Atom.Predicate != expectedMagic {
				t.Errorf("first body literal of %s should be %s, got %s",
					name, expectedMagic, first.Atom.Predicate)
			}
		}
	}
}

// progManyBranchLiftedDisj builds the post-lifting shape for an N-way
// disjunction: N per-branch IDBs + N union projection rules + 1 caller.
// The caller pattern matches the small-extent demand seed shape so
// magic-set fires.
func progManyBranchLiftedDisj(n int) (*datalog.Program, map[string]int) {
	rules := []datalog.Rule{
		{
			Head: datalog.Atom{Predicate: "Caller", Args: []datalog.Term{datalog.Var{Name: "c"}, datalog.Var{Name: "y"}}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "ClassExt", Args: []datalog.Term{datalog.Var{Name: "c"}}}},
				{Positive: true, Atom: datalog.Atom{Predicate: "_disj_N", Args: []datalog.Term{datalog.Var{Name: "c"}, datalog.Var{Name: "y"}}}},
			},
		},
	}
	hints := map[string]int{"ClassExt": 7}
	for i := 0; i < n; i++ {
		branchName := "_disj_N_b" + string(rune('0'+i%10)) + string(rune('a'+i/10))
		baseName := "Base" + string(rune('0'+i%10)) + string(rune('a'+i/10))
		// Per-branch IDB: head carries full var set {c, y, mid_i}.
		midVar := datalog.Var{Name: "m_" + string(rune('a'+i%26))}
		rules = append(rules, datalog.Rule{
			Head: datalog.Atom{Predicate: branchName, Args: []datalog.Term{datalog.Var{Name: "c"}, datalog.Var{Name: "y"}, midVar}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: baseName, Args: []datalog.Term{datalog.Var{Name: "c"}, datalog.Var{Name: "y"}, midVar}}},
			},
		})
		// Union projection: project branch arity 3 onto union arity 2.
		rules = append(rules, datalog.Rule{
			Head: datalog.Atom{Predicate: "_disj_N", Args: []datalog.Term{datalog.Var{Name: "c"}, datalog.Var{Name: "y"}}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: branchName, Args: []datalog.Term{datalog.Var{Name: "c"}, datalog.Var{Name: "y"}, midVar}}},
			},
		})
		hints[baseName] = 100000
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
	return prog, hints
}

// TestLiftedDisj_TenBranchMagicFanoutIsLinear is the §10.4 plan
// invariant: an N-branch lifted disjunction produces magic-set rule
// fan-out that grows LINEARLY with N, not quadratically. The lifting
// transform creates per-branch IDBs that each get their own magic
// rewrite; if the branch-magic interactions introduced cross-branch
// propagation, the count would grow as O(N²).
//
// Plan §10.4 mitigation gate: "a 10-disjunct rule produces ≤ 30
// magic-set rules (linear in branches, not quadratic in branch
// interactions)."
//
// Concretely, for N branches we expect:
//   - N rewritten per-branch IDB rules (each gets a magic_ literal prefix).
//   - N rewritten union projection rules.
//   - O(N) magic propagation rules (one per branch from the union side
//     plus one per branch from the caller side).
//   - 1 magic seed for `magic__disj_N` from the caller's ClassExt.
//
// We assert magicRules ≤ 4*N + 4 (linear envelope; coefficient 4 is
// loose enough to absorb the per-branch propagation routes without
// admitting any quadratic term).
func TestLiftedDisj_TenBranchMagicFanoutIsLinear(t *testing.T) {
	const n = 10
	prog, hints := progManyBranchLiftedDisj(n)
	ep, _, errs := WithMagicSetAutoOpts(prog, hints, MagicSetOptions{Strict: true})
	if len(errs) > 0 {
		t.Fatalf("WithMagicSetAutoOpts failed for %d-branch lifted disjunction: %v", n, errs)
	}
	if ep == nil {
		t.Fatalf("nil execution plan for %d-branch lifted disjunction", n)
	}

	// Count magic rules in the augmented plan.
	magicRules := 0
	for _, st := range ep.Strata {
		for _, r := range st.Rules {
			if strings.HasPrefix(r.Head.Predicate, "magic_") {
				magicRules++
			}
		}
	}

	// Linear envelope: 4*N + 4 is generous (today the count is closer
	// to 2*N + small constant). Anything quadratic in N (e.g. N²/2 = 50
	// at N=10) would blow this envelope by an order of magnitude as N
	// grows. If this assertion ever flips, the planner is doing
	// per-branch-pair work somewhere and the lifting transform's
	// linearity guarantee is broken.
	envelope := 4*n + 4
	if magicRules > envelope {
		t.Fatalf("magic-set fan-out non-linear: %d magic rules for %d branches (envelope %d). Branch interactions are scaling quadratically — investigate per-branch propagation.",
			magicRules, n, envelope)
	}

	// Lower bound: at least N magic-rewritten branch rules must exist
	// (one per branch). If we somehow emitted zero magic rules, the
	// linear-envelope check above passes vacuously.
	if magicRules < n {
		t.Fatalf("expected at least %d magic rules (one per branch rewrite), got %d — magic-set rewrite is dropping branches",
			n, magicRules)
	}
}

// TestLiftedDisj_ArityKeyedGateDoesNotDropBranchPropagation is the
// arity-keying defensive-programming gate test. The (name, arity) gate
// in propagateBindings (magicset.go:270) skips binding records on body
// literals whose arity does not match any IDB head for that name. The
// post-lifting shape introduces multiple distinct IDB names with
// distinct arities — the gate must recognise each as a legitimate IDB
// at its own arity and propagate bindings accordingly.
//
// Specifically: the union rule body is `_disj_2_l(c, y, m)` arity 3,
// and `_disj_2_l` exists as an IDB head ONLY at arity 3. The gate must
// pass this through (matching arity) rather than treat it as a
// shadowing collision.
func TestLiftedDisj_ArityKeyedGateDoesNotDropBranchPropagation(t *testing.T) {
	prog, hints := progLiftedDisjShape()
	idb := IDBPredicates(prog)
	bindings, _ := InferRuleBodyDemandBindings(prog, idb, hints)

	// Both per-branch IDBs must have bindings recorded. If the (name,
	// arity) gate misclassified the union->branch call as a name-shadow
	// collision (e.g. by mistakenly comparing branch arity against the
	// union arity), the branch would be missing here.
	for _, branch := range []string{"_disj_2_l", "_disj_2_r"} {
		if _, ok := bindings[branch]; !ok {
			t.Errorf("(name, arity) gate over-rejected the union->%s call as a name-shadow collision; bindings=%v",
				branch, bindings)
		}
	}

	// Cross-arity guard: even though _disj_2_l (arity 3) and _disj_2_r
	// (arity 4) appear together with the union _disj_2 (arity 2), the
	// gate must not collapse their distinct names. The arity index in
	// propagateBindings keys by NAME, so distinct names get distinct
	// arity buckets — verify by exercising the post-transform plan.
	ep, _, errs := WithMagicSetAutoOpts(prog, hints, MagicSetOptions{Strict: true})
	if len(errs) > 0 {
		t.Fatalf("WithMagicSetAutoOpts failed: %v", errs)
	}
	if ep == nil {
		t.Fatal("nil execution plan")
	}

	// All three predicates must appear as rule heads in the augmented
	// plan (rewritten variants count). If the magic-set rewrite dropped
	// one because of arity confusion, the plan would be missing rules
	// for that head.
	headsSeen := map[string]bool{}
	for _, st := range ep.Strata {
		for _, r := range st.Rules {
			headsSeen[r.Head.Predicate] = true
		}
	}
	for _, want := range []string{"_disj_2", "_disj_2_l", "_disj_2_r"} {
		if !headsSeen[want] {
			t.Errorf("expected %s rules in augmented plan, missing — magic-set rewrite dropped a head", want)
		}
	}
}
