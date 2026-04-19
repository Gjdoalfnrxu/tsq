package plan_test

// Phase B PR5 — Finding 2 follow-up: drive an N-way `or` through the
// real desugarer and assert the §10.4 magic-set linearity invariant
// against the genuine post-lifting shape (cascading binary disjunctions
// — `_disj_N_l`/`_disj_N_r` tree), NOT the synthetic flat-N-sibling
// shape used in `progManyBranchLiftedDisj`. The desugarer never emits
// the flat shape, so an assertion against it cannot validate the
// production code path.

import (
	"strings"
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/desugar"
	"github.com/Gjdoalfnrxu/tsq/ql/parse"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
	"github.com/Gjdoalfnrxu/tsq/ql/resolve"
)

// desugar10WayOrSrc builds QL source with a 10-way `or` chain in the
// predicate body. The parser is left-associative on `or`, so this
// produces a tree of 9 nested binary `ast.Disjunction` nodes. After
// per-#166 lifting, the desugarer emits 9 `_disj_N`/`_disj_N_l`/
// `_disj_N_r` triples — the genuine cascading shape.
const desugar10WayOrSrc = `
predicate b0(int x) { B0(x) }
predicate b1(int x) { B1(x) }
predicate b2(int x) { B2(x) }
predicate b3(int x) { B3(x) }
predicate b4(int x) { B4(x) }
predicate b5(int x) { B5(x) }
predicate b6(int x) { B6(x) }
predicate b7(int x) { B7(x) }
predicate b8(int x) { B8(x) }
predicate b9(int x) { B9(x) }
predicate test(int x) {
    b0(x) or b1(x) or b2(x) or b3(x) or b4(x) or
    b5(x) or b6(x) or b7(x) or b8(x) or b9(x)
}
`

func desugarToProgram(t *testing.T, src string) *datalog.Program {
	t.Helper()
	p := parse.NewParser(src, "<test>")
	mod, err := p.Parse()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	rm, err := resolve.Resolve(mod, nil)
	if err != nil {
		t.Fatalf("resolve error: %v", err)
	}
	prog, errs := desugar.Desugar(rm)
	if len(errs) > 0 {
		t.Fatalf("desugar errors: %v", errs)
	}
	return prog
}

// TestLiftedDisj_RealDesugarer10WayOrLinearFanout drives a 10-way `or`
// through the real desugarer, runs WithMagicSetAutoOpts on the
// resulting program, and asserts the §10.4 linearity invariant on the
// genuine cascading-binary-disjunction shape.
//
// Replaces the synthetic flat-N-sibling shape in
// progManyBranchLiftedDisj — the desugarer never emits that shape
// (Finding 2, PR #187 review).
//
// The 10-way `or` desugars to 9 nested binary disjunctions — yielding
// 9 union names (`_disj_1` .. `_disj_9`) each with `_l`/`_r` branches.
// That's 9 unions + 18 branch IDBs + 18 union projection rules under
// the lifting transform. The magic-set rewrite must scale linearly in
// this rule count, NOT quadratically in branch interactions.
func TestLiftedDisj_RealDesugarer10WayOrLinearFanout(t *testing.T) {
	prog := desugarToProgram(t, desugar10WayOrSrc)

	// Confirm the desugarer produced cascading-binary shape (sanity for
	// the assertion below): count `_disj_*_l` heads — for a 10-way `or`
	// (9 binary nodes), expect 9.
	disjL := 0
	disjR := 0
	disjUnion := 0
	for _, r := range prog.Rules {
		name := r.Head.Predicate
		if !strings.HasPrefix(name, "_disj_") {
			continue
		}
		switch {
		case strings.HasSuffix(name, "_l"):
			disjL++
		case strings.HasSuffix(name, "_r"):
			disjR++
		default:
			disjUnion++
		}
	}
	if disjL == 0 || disjR == 0 || disjUnion == 0 {
		t.Fatalf("desugarer did not emit lifted-disj shape: _l=%d _r=%d union=%d (rule heads: %v)",
			disjL, disjR, disjUnion, ruleHeadNames(prog))
	}
	// 10-way `or` = 9 binary disjunctions = 9 of each.
	const expectedBinaryNodes = 9
	if disjL != expectedBinaryNodes || disjR != expectedBinaryNodes {
		t.Fatalf("expected %d _l and %d _r heads from a 10-way or (9 binary disj nodes), got _l=%d _r=%d",
			expectedBinaryNodes, expectedBinaryNodes, disjL, disjR)
	}

	// Provide minimal sizing hints so the magic-set demand inference
	// has a small-extent grounder somewhere. The base preds `b0`..`b9`
	// resolve to class-extent shapes via the `any()` body; we hint them
	// uniformly large so the planner doesn't elide them.
	hints := map[string]int{}
	for i := 0; i < 10; i++ {
		hints["b"+itoa(i)] = 100000
	}

	ep, _, errs := plan.WithMagicSetAutoOpts(prog, hints, plan.MagicSetOptions{Strict: false})
	if len(errs) > 0 {
		t.Fatalf("WithMagicSetAutoOpts failed: %v", errs)
	}
	if ep == nil {
		t.Fatal("nil execution plan")
	}

	// Count magic rules produced from the genuine cascading shape.
	magicRules := 0
	for _, st := range ep.Strata {
		for _, r := range st.Rules {
			if strings.HasPrefix(r.Head.Predicate, "magic_") {
				magicRules++
			}
		}
	}

	// Linearity invariant on the genuine shape: the binary-cascade
	// produces 9 union nodes + 18 branch IDBs + 18 projection rules =
	// O(N) total disj-related rules for a 10-way `or`. Magic-set
	// rewrite of those scales linearly. Envelope is generous (8×
	// binary-node count + constant) — the goal is to catch
	// quadratic blow-up (would be ~9² = 81 at this size), not to lock
	// the exact count. Today's actual count is well under this.
	envelope := 8*expectedBinaryNodes + 16
	if magicRules > envelope {
		t.Fatalf("magic-set fan-out non-linear on real-desugarer 10-way `or`: %d magic rules (envelope %d). Cascading-disj branch interactions appear to be scaling super-linearly — investigate.",
			magicRules, envelope)
	}
}

// ruleHeadNames is a debug helper for fatal messages.
func ruleHeadNames(prog *datalog.Program) []string {
	names := make([]string, 0, len(prog.Rules))
	for _, r := range prog.Rules {
		names = append(names, r.Head.Predicate)
	}
	return names
}

// itoa: tiny helper to avoid pulling in strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
