package plan

import (
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
)

// White-box regression for issue #109. Drives pickTinySeed directly with a
// small defaultUnhintedSize so the unhinted constant-arg candidate would
// win on size alone — the only thing that prevents regression is the
// explicit hinted-preference rule. On main (before the fix) this test
// fails because the size-based tiebreak alone picks Big.
//
// Construction:
//   - Big("k", x):  unhinted, qualifies as tiny via constant-arg branch.
//   - Tiny("k", x): hint=10 (≤ tinySeedThreshold), qualifies via hint.
//
// With defaultUnhintedSize=1, Big's effective size is 1 and Tiny's is
// 10. Without the hinted-preference rule, Big wins (1 < 10). With the
// fix, Tiny wins because hinted strictly beats unhinted.
func TestPickTinySeedHintedBeatsUnhintedWhenUnhintedWouldWinOnSize(t *testing.T) {
	body := []datalog.Literal{
		bigConstLit("Big"),
		bigConstLit("Tiny"),
	}
	hints := map[string]int{"Tiny": 10}
	placed := []bool{false, false}
	bound := map[string]bool{}

	idx := pickTinySeed(body, placed, bound, hints, 1)
	if idx == -1 {
		t.Fatal("expected a tiny-seed pick, got -1")
	}
	got := body[idx].Atom.Predicate
	if got != "Tiny" {
		t.Errorf("expected Tiny (hinted) to beat Big (unhinted) even when "+
			"defaultUnhintedSize (1) makes Big's effective size smaller than "+
			"Tiny's hint (10); got %s", got)
	}
}

// Mirror test: when neither candidate is hinted, the size-based tiebreak
// still applies. Guards against an over-eager fix that would regress the
// unhinted-vs-unhinted ordering.
func TestPickTinySeedUnhintedTieBreakBySize(t *testing.T) {
	body := []datalog.Literal{
		bigConstLit("A"),
		bigConstLit("B"),
	}
	placed := []bool{false, false}
	bound := map[string]bool{}

	// Neither hinted. Both qualify via constant-arg. Effective size =
	// defaultUnhintedSize for both, so first eligible (stable index) wins.
	idx := pickTinySeed(body, placed, bound, nil, 1000)
	if idx != 0 {
		t.Errorf("expected index 0 (A) on stable tie among unhinted, got %d", idx)
	}
}

// TestPickTinySeedAmongHintedSmallerWins: when both candidates are
// hinted, smaller hint wins.
func TestPickTinySeedAmongHintedSmallerWins(t *testing.T) {
	body := []datalog.Literal{
		bigConstLit("Bigger"),
		bigConstLit("Smaller"),
	}
	hints := map[string]int{"Bigger": 20, "Smaller": 5}
	placed := []bool{false, false}
	bound := map[string]bool{}

	idx := pickTinySeed(body, placed, bound, hints, 1000)
	if idx == -1 {
		t.Fatal("expected a tiny-seed pick, got -1")
	}
	if body[idx].Atom.Predicate != "Smaller" {
		t.Errorf("expected Smaller (hint=5) over Bigger (hint=20), got %s",
			body[idx].Atom.Predicate)
	}
}

// bigConstLit builds a positive literal pred("k", x) — has a constant
// arg so it qualifies for the tiny-seed constant-arg branch.
func bigConstLit(pred string) datalog.Literal {
	return datalog.Literal{
		Positive: true,
		Atom: datalog.Atom{
			Predicate: pred,
			Args: []datalog.Term{
				datalog.StringConst{Value: "k"},
				datalog.Var{Name: "x"},
			},
		},
	}
}
