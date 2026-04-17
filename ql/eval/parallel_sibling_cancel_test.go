package eval

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

// Issue #100: regression for sibling-cancellation timing in parallel rule
// evaluation. PR #93 added `childCtx, cancel := context.WithCancel(ctx)` and
// `cancel()` on first worker error in parallelBootstrap / parallelDelta. This
// gives millisecond sibling bail when one worker errors. Without the cancel(),
// a worker that hits BindingCapError immediately would still have to wait for
// the slowest sibling Rule() call (potentially hundreds of ms or more) before
// wg.Wait() returns and the error is reported.
//
// Mutation-kill: comment out the `cancel()` calls on parallel.go:75 and 147.
// This test must then fail (elapsed > ceiling), because the heavy sibling will
// run to completion before the trivial rule's BindingCapError surfaces.
//
// Fixture shape (two head groups, so they run in distinct parallel workers):
//
//	Trivial(a, b) :- TR(a), TR(b).      // ~1M intermediate bindings, trips cap
//	Bad(a, b)     :- S(a), S(b).        // ~360k bindings, slow but under cap
//	?- Trivial(a, b).
//
// With cap = 500_000:
//   - Trivial trips on join step 1 (1001*1001 = 1.002M > 500k) — microseconds.
//   - Bad's ~360k Cartesian fits under the cap; runs to completion (~hundreds
//     of ms on this fixture). Without sibling cancellation, the parallel
//     evaluator blocks on wg.Wait() until Bad finishes.

// Fixture sizing rationale:
//
// The cap is global (WithMaxBindingsPerRule applies to every rule). To use a
// trivial rule that trips on a small intermediate cardinality and a heavy
// rule that completes a much larger one, we set the cap somewhere between
// trivial's 2-step Cartesian (~1M) and heavy's 2-step Cartesian (~360k):
//
//	cap     = 500_000
//	trivial = TR(1001)  →  step1 = 1.002M outputs, trips at output #500k+1
//	heavy   = S(600)    →  step1 = 360k outputs, completes (under cap)
//
// Both Cartesian rules grow linearly in output count, so the trivial trip
// cost (~500k inserts) is comparable to or cheaper than the heavy completion
// cost (~360k inserts × wider join cost). On the dev box heavy-solo measures
// ~900ms-1.4s; trivial-trip-then-bail measures ~200-300ms (with cancel).
// Without the cancel(), the parallel run waits for heavy to finish, blowing
// any ceiling derived from heavy-solo.
const (
	siblingCancelTrivialN = 1001
	siblingCancelHeavyN   = 600
	siblingCancelCap      = 500_000
)

func twoRuleSiblingCancelPlan() (*plan.ExecutionPlan, map[string]*Relation) {
	trVals := make([]Value, 0, siblingCancelTrivialN)
	for i := 0; i < siblingCancelTrivialN; i++ {
		trVals = append(trVals, IntVal{V: int64(i)})
	}
	tr := makeRelation("TR", 1, trVals...)

	sVals := make([]Value, 0, siblingCancelHeavyN)
	for i := 0; i < siblingCancelHeavyN; i++ {
		sVals = append(sVals, IntVal{V: int64(i)})
	}
	s := makeRelation("S", 1, sVals...)

	baseRels := map[string]*Relation{"TR": tr, "S": s}

	ep := &plan.ExecutionPlan{
		Strata: []plan.Stratum{
			{
				Rules: []plan.PlannedRule{
					{
						// Trivial sibling: trips BindingCapError on step 1.
						Head: datalog.Atom{Predicate: "Trivial", Args: []datalog.Term{v("a"), v("b")}},
						JoinOrder: []plan.JoinStep{
							positiveStep("TR", v("a")),
							positiveStep("TR", v("b")),
						},
					},
					{
						// Heavy sibling: 2-way Cartesian then two redundant
						// re-binding steps. Each step is bounded by ~N² ≤ cap,
						// but the total CPU work is roughly 4x a single
						// Cartesian — wide enough to exceed trivial-trip cost
						// by a clear margin.
						Head: datalog.Atom{Predicate: "Bad", Args: []datalog.Term{v("a"), v("b")}},
						JoinOrder: []plan.JoinStep{
							positiveStep("S", v("a")),
							positiveStep("S", v("b")),
							positiveStep("S", v("a")),
							positiveStep("S", v("b")),
						},
					},
				},
			},
		},
		Query: &plan.PlannedQuery{
			Select: []datalog.Term{v("a"), v("b")},
			JoinOrder: []plan.JoinStep{
				positiveStep("Trivial", v("a"), v("b")),
			},
		},
	}
	return ep, baseRels
}

// TestParallelSiblingCancellationOnError is the issue #100 regression. It
// asserts that when one parallel-bootstrap worker returns BindingCapError, the
// shared child context is cancelled and the heavy sibling worker bails out
// promptly — not at the natural end of its Rule() call.
//
// Methodology:
//   - Calibrate trivial-solo cost (T_trivial): how fast the trivial rule trips
//     the cap when run by itself. With-cancel parallel run cost is bounded by
//     T_trivial + bail_latency.
//   - Calibrate heavy-solo cost (T_heavy): how long the heavy rule takes by
//     itself. Without-cancel parallel run cost is bounded below by ~T_heavy.
//   - Require T_heavy is meaningfully larger than T_trivial; otherwise the
//     fixture is too small on this hardware and we skip.
//   - Set ceiling = T_trivial + small slack for bail latency. The mutated code
//     must blow past this; the live code must stay comfortably under it.
//
// Mutation kill: comment out `cancel()` on parallel.go:75 (and :147 for the
// delta path). The heavy worker then runs to completion before parallel.go
// reports the error, pushing elapsed up toward T_heavy and failing the
// assertion.
func TestParallelSiblingCancellationOnError(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping sibling-cancellation timing test in -short mode")
	}
	// Timing-based assertion: only run when the environment opts in. Default-
	// skip avoids silent green passes on slow/contended CI runners that can't
	// produce reliable calibration. Adversarial-review point: a calibration-
	// failure t.Skip on a CI runner would mask the very regression this test
	// exists to catch (missing cancel() in parallelBootstrap).
	if os.Getenv("TSQ_TIMING_TESTS") != "1" {
		t.Skip("skipping sibling-cancellation timing test; set TSQ_TIMING_TESTS=1 to enable")
	}

	// Calibration. Run each sibling in isolation to set the assertion
	// thresholds based on observed hardware speed, not magic numbers.
	heavySolo := timeHeavyAlone(t)
	trivialSolo := timeTrivialAlone(t)
	t.Logf("calibration: trivial-solo=%v heavy-solo=%v", trivialSolo, heavySolo)

	// Sanity: heavy must be substantially slower than trivial for the test to
	// discriminate. Require at least a 3x ratio AND a heavy-solo floor so
	// scheduler jitter cannot dominate. With TSQ_TIMING_TESTS=1 the caller
	// has explicitly asked for the timing assertion to run, so calibration
	// failure is a hard error (test fixture or hardware mismatch), not a skip.
	if heavySolo < 200*time.Millisecond {
		t.Fatalf("test fixture sizing too small for this CI runner — heavy-solo %v under 200ms floor; increase work in heavy rule (siblingCancelHeavyN) or run on faster hardware", heavySolo)
	}
	if heavySolo < 3*trivialSolo {
		t.Fatalf("test fixture sizing too small for this CI runner — heavy-solo (%v) not at least 3x trivial-solo (%v); increase work in heavy rule or run on faster hardware", heavySolo, trivialSolo)
	}

	// Ceiling derivation. With cancel(): wall time ≈ contended-trivial-trip
	// + heavy-bail-latency. With two CPU-bound goroutines sharing N cores,
	// each goroutine can run at as little as ~half speed, so contended
	// trivial trip can take up to 2x trivialSolo. We then add slack for
	// the heavy worker's throttled ctx check (every 8192 outputs ≈ tens of
	// ms on this fixture) and goroutine wakeup.
	//
	// Without cancel(): wall time ≈ contended-heavy-completion ≈ heavySolo
	// (heavy gets full CPU once trivial dies, but had been at half speed
	// for the trivial-trip portion). We've required heavySolo > 3*trivialSolo,
	// so 2*trivialSolo + 200ms < heavySolo holds with margin in normal
	// operation, giving a clear pass/fail boundary.
	ceiling := 2*trivialSolo + 200*time.Millisecond
	// Defensive upper bound: never let the ceiling creep close to heavySolo.
	// Use 3/4 (not 2/3) so the cap doesn't collapse below 2*trivialSolo when
	// heavySolo is just barely over the 3x trivialSolo gate.
	if maxCeiling := heavySolo * 3 / 4; ceiling > maxCeiling {
		ceiling = maxCeiling
	}

	ep, baseRels := twoRuleSiblingCancelPlan()

	t0 := time.Now()
	_, err := Evaluate(context.Background(), ep, baseRels,
		WithMaxIterations(0),
		WithMaxBindingsPerRule(siblingCancelCap),
		WithParallel(),
	)
	elapsed := time.Since(t0)

	if err == nil {
		t.Fatalf("expected BindingCapError from trivial sibling, got nil")
	}
	// The originating error (BindingCapError) must surface, not the
	// ctx-wrapped variant from the cancelled heavy sibling. firstError now
	// prefers non-ctx errors over ctx errors precisely so this diagnostic
	// is preserved regardless of head-group iteration order (issue #100).
	if !errors.Is(err, ErrBindingCapExceeded) {
		t.Fatalf("expected BindingCapError to surface (firstError should prefer non-ctx errors), got: %v", err)
	}

	if elapsed > ceiling {
		t.Errorf("parallel evaluation took %v; ceiling is %v (trivial-solo=%v + 150ms slack; heavy-solo=%v). cancel() likely missing in parallelBootstrap.", elapsed, ceiling, trivialSolo, heavySolo)
	}
	t.Logf("sibling-cancel: elapsed=%v ceiling=%v trivial-solo=%v heavy-solo=%v err=%v", elapsed, ceiling, trivialSolo, heavySolo, err)
}

// TestFirstErrorPrefersNonCtxErrors guards the issue #100 firstError change:
// when one parallel worker errors with a non-ctx error and a sibling bails
// with a ctx-wrapped error in response to sibling-cancellation, the surfaced
// error must be the originating non-ctx error — not the ctx-wrap that masks
// the diagnostic. Slice-position must NOT determine which error wins when
// one is ctx-derived and another is not.
func TestFirstErrorPrefersNonCtxErrors(t *testing.T) {
	originating := &BindingCapError{Rule: "X", Cap: 100, Cardinality: 101}
	// Only Unwrap matters for errors.Is(ctx-target); the message is irrelevant.
	var ctxWrap error = &wrappedCtx{err: errors.New("rule Y cancelled"), inner: context.Canceled}

	cases := []struct {
		name string
		errs []error
		want error
	}{
		{"non-ctx first", []error{originating, ctxWrap}, originating},
		{"ctx-wrap first", []error{ctxWrap, originating}, originating},
		{"only ctx-wraps", []error{ctxWrap, ctxWrap}, ctxWrap},
		{"only non-ctx", []error{originating}, originating},
		{"all nil", []error{nil, nil}, nil},
		{"nil + ctx + non-ctx", []error{nil, ctxWrap, originating}, originating},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := firstError(tc.errs)
			if got != tc.want {
				t.Errorf("firstError = %v, want %v", got, tc.want)
			}
		})
	}
}

// wrappedCtx is a minimal error wrapper that satisfies errors.Is(ctx-target).
type wrappedCtx struct {
	err   error
	inner error
}

func (w *wrappedCtx) Error() string { return w.err.Error() }
func (w *wrappedCtx) Unwrap() error { return w.inner }

// timeTrivialAlone runs the trivial cap-tripping rule on its own and returns
// the wall-clock time to detect the cap and return BindingCapError. This is
// the absolute floor on a parallel run that cancels its sibling at first
// error — the trivial worker still has to do its own work before it can
// signal cancellation.
func timeTrivialAlone(t *testing.T) time.Duration {
	t.Helper()

	trVals := make([]Value, 0, siblingCancelTrivialN)
	for i := 0; i < siblingCancelTrivialN; i++ {
		trVals = append(trVals, IntVal{V: int64(i)})
	}
	tr := makeRelation("TR", 1, trVals...)
	baseRels := map[string]*Relation{"TR": tr}

	ep := &plan.ExecutionPlan{
		Strata: []plan.Stratum{
			{
				Rules: []plan.PlannedRule{
					{
						Head: datalog.Atom{Predicate: "Trivial", Args: []datalog.Term{v("a"), v("b")}},
						JoinOrder: []plan.JoinStep{
							positiveStep("TR", v("a")),
							positiveStep("TR", v("b")),
						},
					},
				},
			},
		},
		Query: nil,
	}

	t0 := time.Now()
	_, err := Evaluate(context.Background(), ep, baseRels,
		WithMaxIterations(0),
		WithMaxBindingsPerRule(siblingCancelCap),
		WithParallel(),
	)
	elapsed := time.Since(t0)
	if err == nil {
		t.Fatalf("trivial-solo calibration: expected BindingCapError, got nil")
	}
	if !errors.Is(err, ErrBindingCapExceeded) {
		t.Fatalf("trivial-solo calibration: expected ErrBindingCapExceeded, got: %v", err)
	}
	return elapsed
}

// timeHeavyAlone runs the heavy Cartesian rule on its own (no trivial
// sibling, no cap) and returns wall-clock elapsed. Used to establish the
// floor that sibling cancellation must beat.
func timeHeavyAlone(t *testing.T) time.Duration {
	t.Helper()

	sVals := make([]Value, 0, siblingCancelHeavyN)
	for i := 0; i < siblingCancelHeavyN; i++ {
		sVals = append(sVals, IntVal{V: int64(i)})
	}
	s := makeRelation("S", 1, sVals...)
	baseRels := map[string]*Relation{"S": s}

	ep := &plan.ExecutionPlan{
		Strata: []plan.Stratum{
			{
				Rules: []plan.PlannedRule{
					{
						Head: datalog.Atom{Predicate: "Bad", Args: []datalog.Term{v("a"), v("b")}},
						JoinOrder: []plan.JoinStep{
							positiveStep("S", v("a")),
							positiveStep("S", v("b")),
							positiveStep("S", v("a")),
							positiveStep("S", v("b")),
						},
					},
				},
			},
		},
		// No query: we want to time the bootstrap (Rule call) cost, not the
		// post-fixpoint result materialisation. A nil-Query Evaluate still
		// runs all strata; we discard the result.
		Query: nil,
	}

	t0 := time.Now()
	_, err := Evaluate(context.Background(), ep, baseRels,
		WithMaxIterations(0),
		WithMaxBindingsPerRule(siblingCancelCap),
		WithParallel(),
	)
	elapsed := time.Since(t0)
	if err != nil {
		t.Fatalf("heavy-solo calibration evaluation failed: %v", err)
	}
	return elapsed
}
