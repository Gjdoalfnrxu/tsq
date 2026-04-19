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
// NOTE: this constructor is pure — returns a freshly-allocated program
// and hints map on each call, so tests can mutate the result without
// cross-test contamination (Finding 6, PR #187 review).
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
	// (Finding 3, PR #187 review): tighten beyond name-only — the magic
	// literal's single arg must be the BOUND head var (`c`) at col 0,
	// not some other variable. If the magic-set transform projected the
	// wrong column from the head (e.g. `y` instead of `c`, or a wildcard
	// from a length-mismatch path), the magic seed and the consumer's
	// magic literal would key on different values and the demand prune
	// would silently miss tuples. This catches arity confusion that the
	// pure-name check cannot.
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
				continue
			}
			// Magic literal must be arity 1 (only `c` is bound).
			if len(first.Atom.Args) != 1 {
				t.Errorf("%s magic prefix should have arity 1 (only `c` bound); got %d args: %v",
					name, len(first.Atom.Args), first.Atom.Args)
				continue
			}
			// And that single arg must be the variable `c` — same name
			// as the bound col 0 of the rewritten branch head. Any other
			// var here means the transform projected the wrong column.
			arg, ok := first.Atom.Args[0].(datalog.Var)
			if !ok {
				t.Errorf("%s magic arg[0] should be a Var (the bound `c`); got %T %v",
					name, first.Atom.Args[0], first.Atom.Args[0])
				continue
			}
			if arg.Name != "c" {
				t.Errorf("%s magic arg[0] should be variable `c` (col 0 of branch head); got Var{%q} — magic-set projected the wrong column",
					name, arg.Name)
			}
			// Cross-check: the head's col 0 must also be `c` (so the
			// magic literal really does key on the bound col, not just
			// happen to be named `c` in some other position).
			if len(r.Head.Args) == 0 {
				t.Errorf("%s rule head has no args", name)
				continue
			}
			headCol0, ok := r.Head.Args[0].(datalog.Var)
			if !ok || headCol0.Name != "c" {
				t.Errorf("%s rule head col 0 should be Var{c} (matching magic prefix arg); got %v",
					name, r.Head.Args[0])
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
	// Run the assertion at TWO branch counts so a linear-with-bigger-
	// slope regression (e.g. coefficient creep from 2 → 4) surfaces as a
	// ratio change rather than passing both envelopes silently
	// (Finding 4, PR #187 review).
	for _, n := range []int{5, 10} {
		t.Run("N="+itoa(n), func(t *testing.T) {
			prog, hints := progManyBranchLiftedDisj(n)
			ep, _, errs := WithMagicSetAutoOpts(prog, hints, MagicSetOptions{Strict: true})
			if len(errs) > 0 {
				t.Fatalf("WithMagicSetAutoOpts failed for %d-branch lifted disjunction: %v", n, errs)
			}
			if ep == nil {
				t.Fatalf("nil execution plan for %d-branch lifted disjunction", n)
			}

			// Count magic rules in the augmented plan AND collect the
			// distinct magic head names (used for the per-branch lower-
			// bound check below — Finding 5).
			magicRules := 0
			magicHeadNames := map[string]bool{}
			for _, st := range ep.Strata {
				for _, r := range st.Rules {
					if strings.HasPrefix(r.Head.Predicate, "magic_") {
						magicRules++
						magicHeadNames[r.Head.Predicate] = true
					}
				}
			}

			// Tightened linear envelope (Finding 4): today the actual
			// count is 2*N + 1 (one rewritten union projection per
			// branch + one magic propagation per branch + one seed for
			// the union itself). Slack budget: +4 — absorbs minor
			// per-branch bookkeeping changes without admitting any
			// linear-with-2×-coefficient regression.
			//
			// Previous envelope was 4*N + 4 (loose enough that a
			// regression doubling the per-branch cost would still pass
			// silently). New envelope of 2*N + 5 forces such a
			// regression to surface immediately.
			const slackBudget = 4
			envelope := 2*n + 1 + slackBudget
			if magicRules > envelope {
				t.Fatalf("magic-set fan-out exceeds tightened linear envelope: %d magic rules for %d branches (envelope %d = 2N+1+%d). Either the per-branch coefficient regressed or branch interactions appeared — investigate.",
					magicRules, n, envelope, slackBudget)
			}

			// Per-branch ratio check (Finding 4): magicRules / N must
			// stay in [2, 3]. Linear-with-bigger-slope regressions
			// (e.g. coefficient 4) surface here even when the absolute
			// count stays under the envelope at small N.
			ratioMin, ratioMax := 2, 3
			ratio := magicRules / n
			if ratio < ratioMin || ratio > ratioMax {
				t.Errorf("magic-set per-branch ratio out of band: magicRules/N = %d/%d = %d, expected in [%d, %d]",
					magicRules, n, ratio, ratioMin, ratioMax)
			}

			// Lower bound: at least N magic-rewritten rules.
			if magicRules < n {
				t.Fatalf("expected at least %d magic rules (one per branch rewrite), got %d — magic-set rewrite is dropping branches",
					n, magicRules)
			}

			// Per-branch coverage (Finding 5): every per-branch IDB name
			// must produce at least one magic head — otherwise some
			// branches got no rewrite and the linear envelope above
			// passes only because the dropped branches reduced the
			// count. Walk the input fixture's branch IDB names and
			// confirm each has a corresponding `magic_<branchName>`
			// head somewhere in the augmented plan.
			for i := 0; i < n; i++ {
				branchName := "_disj_N_b" + string(rune('0'+i%10)) + string(rune('a'+i/10))
				wantMagic := "magic_" + branchName
				if !magicHeadNames[wantMagic] {
					t.Errorf("branch %d (%s) has no %s rule in augmented plan — magic-set rewrite dropped this branch; magicHeadNames=%v",
						i, branchName, wantMagic, magicHeadNames)
				}
			}
		})
	}
}

// itoa is a tiny strconv-free helper so the test file's import block
// stays minimal (the test package already pulls in strings/testing).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
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

// progLiftedDisjShapeWithArityCollision models the lifted-disjunction
// shape PLUS the (name, arity) collision the magicset.go:270 gate is
// designed to catch: a name `VarDecl` lives BOTH as an arity-1 IDB
// helper head (`VarDecl(this) :- VarDecl(this,_,_,_).`) AND as an
// arity-4 base relation referenced inside a lifted-disj branch body.
//
// Without the gate, the branch body's arity-4 `VarDecl(c,_,_,_)` call
// would record `bindings["VarDecl"] = [0]` — wrong, because that key
// is consumed by downstream magic-set machinery as the IDB-arity-1
// shape and produces `magic_VarDecl(_)` propagation rules with
// wildcards at demanded positions (see magicset.go:208-214 comment).
//
// Shape:
//
//	Caller(c, y)             :- ClassExt(c), _disj_2(c, y).
//	_disj_2_l(c, y, m)       :- VarDecl(c,_,_,_), Guard(m).
//	_disj_2_r(c, y, n, k)    :- E(c, y, n, k), Other(n, k).
//	_disj_2(c, y)            :- _disj_2_l(c, y, m).
//	_disj_2(c, y)            :- _disj_2_r(c, y, n, k).
//	VarDecl(this)            :- VarDecl(this,_,_,_).   // arity-1 IDB shadow
//
// Pure constructor — fresh program + hints per call.
func progLiftedDisjShapeWithArityCollision() (*datalog.Program, map[string]int) {
	rules := []datalog.Rule{
		{
			Head: datalog.Atom{Predicate: "Caller", Args: []datalog.Term{datalog.Var{Name: "c"}, datalog.Var{Name: "y"}}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "ClassExt", Args: []datalog.Term{datalog.Var{Name: "c"}}}},
				{Positive: true, Atom: datalog.Atom{Predicate: "_disj_2", Args: []datalog.Term{datalog.Var{Name: "c"}, datalog.Var{Name: "y"}}}},
			},
		},
		// LEFT branch IDB: body uses arity-4 base `VarDecl/4` — the
		// colliding-name literal whose arity does NOT match any
		// `VarDecl` IDB head arity. Gate at magicset.go:270 must skip
		// the bindings record on this literal.
		{
			Head: datalog.Atom{Predicate: "_disj_2_l", Args: []datalog.Term{datalog.Var{Name: "c"}, datalog.Var{Name: "y"}, datalog.Var{Name: "m"}}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "VarDecl", Args: []datalog.Term{datalog.Var{Name: "c"}, datalog.Var{Name: "_"}, datalog.Var{Name: "y"}, datalog.Var{Name: "m"}}}},
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
		// Arity-1 IDB helper that collides on NAME with the arity-4
		// base relation referenced in the branch body above.
		{
			Head: datalog.Atom{Predicate: "VarDecl", Args: []datalog.Term{datalog.Var{Name: "this"}}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "VarDecl", Args: []datalog.Term{datalog.Var{Name: "this"}, datalog.Var{Name: "_"}, datalog.Var{Name: "_"}, datalog.Var{Name: "_"}}}},
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
		"VarDecl":  100000,
		"Guard":    100000,
		"E":        100000,
		"Other":    100000,
	}
	return prog, hints
}

// TestLiftedDisj_ArityCollisionGateSuppressesArityNBinding is the
// *non-vacuous* regression test for the (name, arity) gate at
// magicset.go:270. The fixture above has a real name+arity collision:
// `VarDecl` has IDB heads only at arity 1, but a lifted-disj branch
// body calls `VarDecl(c,_,_,_)` (arity 4, base usage).
//
// With the gate intact: the arity-4 body call does NOT contribute to
// bindings["VarDecl"], so any later bindings entry for VarDecl reflects
// only flow from arity-1-matched paths (which there are none here, so
// the entry stays absent or empty).
//
// With the gate REMOVED: the arity-4 call records bindings["VarDecl"]
// = [0] from the `_disj_2_l` rule when col 0 is the bound `c`. That's
// the bug shape the gate exists to suppress.
//
// We assert: bindings["VarDecl"] is absent OR has zero entries.
//
// SPOT-CHECK PROCEDURE (for future maintainers): comment out the gate
// at magicset.go:270 (the `if arities, hasIDB := ...` block) and
// re-run this test. It MUST fail. Restore and re-run; it MUST pass.
// This was performed when the test was authored (PR #187, Finding 1).
func TestLiftedDisj_ArityCollisionGateSuppressesArityNBinding(t *testing.T) {
	prog, hints := progLiftedDisjShapeWithArityCollision()
	ep, inf, errs := WithMagicSetAutoOpts(prog, hints, MagicSetOptions{Strict: true})
	if len(errs) > 0 {
		t.Fatalf("WithMagicSetAutoOpts failed: %v", errs)
	}
	if ep == nil {
		t.Fatal("nil execution plan")
	}

	// Sanity: union and branch IDBs still get bindings. The gate must
	// not over-reject legitimate IDB-call propagation.
	if cols, ok := inf.Bindings["_disj_2"]; !ok || len(cols) != 1 || cols[0] != 0 {
		t.Fatalf("expected _disj_2 bound at col 0, got %v", inf.Bindings["_disj_2"])
	}

	// Load-bearing differential assertion: walk the augmented plan and
	// find every rule whose head is `VarDecl`. Each such rule's body
	// must NOT begin with a `magic_VarDecl(...)` literal.
	//
	// With the magicset.go:270 gate intact, propagateBindings does NOT
	// record bindings["VarDecl"] (the arity-4 body call is gate-skipped),
	// so the outer emission loop never iterates pred=VarDecl, and the
	// arity-1 IDB rule `VarDecl(this) :- VarDecl(this,_,_,_)` survives
	// unrewritten.
	//
	// With the gate REMOVED, bindings["VarDecl"] = [0] leaks in from the
	// `_disj_2_l` branch's arity-4 base usage, and the IDB rule becomes
	// `VarDecl(this) :- magic_VarDecl(this), VarDecl(this,_,_,_)` — a
	// rewrite that prefixes a magic literal onto an arity-1 IDB whose
	// magic seed has no consumer chain. This is the bug shape the gate
	// exists to suppress.
	//
	// Spot-check verified at authorship time: commenting out the gate
	// at magicset.go:270 makes this assertion fire (PR #187, Finding 1).
	for _, st := range ep.Strata {
		for _, r := range st.Rules {
			if r.Head.Predicate != "VarDecl" {
				continue
			}
			if len(r.Body) == 0 {
				continue
			}
			first := r.Body[0]
			if first.Atom.Predicate == "magic_VarDecl" {
				t.Fatalf("(name, arity) gate failed: VarDecl IDB rule was magic-rewritten with %s prefix, indicating arity-1/arity-4 binding collision leaked through propagateBindings; rule head=%v body[0]=%v",
					first.Atom.Predicate, r.Head, first.Atom)
			}
		}
	}
}
