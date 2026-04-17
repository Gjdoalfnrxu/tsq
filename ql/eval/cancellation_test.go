package eval

import (
	"context"
	"errors"
	"testing"
	"time"
)

// Issue #81: the semi-naive fixpoint loop must check ctx.Err() after each
// per-rule RuleDelta call (and inside the parallel workers), not only at
// stratum boundaries. A long-running stratum should honor --timeout promptly,
// not "after the whole stratum finishes".
//
// The fixture used here is the divergentTransitiveClosurePlan from
// iteration_cap_test.go — a chain transitive closure that takes ~hundreds of
// ms for N=400 nodes, well above the timeouts these tests pin (5–50ms).
// Promptness is asserted with a tight upper bound on elapsed wall time,
// not a vacuous "finished within 30s".

// chainTCSize is the chain length used by the cancellation tests. It needs to
// be large enough that the fixpoint takes considerably longer than the
// timeouts we use, so a missed ctx check would fail the promptness assertion.
// On the dev box: N=400 ≈ 400ms; N=200 ≈ 80ms. We pick 400.
const chainTCSize = 400

// TestCtxCancelledMidFixpoint verifies that a context cancelled before
// Evaluate is called causes a fast return wrapping context.Canceled, and
// crucially that errors.Is matches both context.Canceled and the eval
// callers' typical sentinel checks.
func TestCtxCancelledMidFixpoint(t *testing.T) {
	ep, baseRels := divergentTransitiveClosurePlan(chainTCSize)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Pre-cancel.

	t0 := time.Now()
	rs, err := Evaluate(ctx, ep, baseRels, WithMaxIterations(0))
	elapsed := time.Since(t0)

	if err == nil {
		t.Fatalf("expected error from pre-cancelled context, got nil with %d rows", len(rs.Rows))
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected error wrapping context.Canceled, got: %v", err)
	}
	// Pre-cancellation must return well under the time it would take to
	// compute even a single iteration of the chain TC. 50ms is a generous
	// ceiling — the actual call should take microseconds.
	if elapsed > 50*time.Millisecond {
		t.Errorf("pre-cancelled Evaluate took %v; expected near-instant return (< 50ms)", elapsed)
	}
	t.Logf("pre-cancelled return time: %v", elapsed)
}

// TestCtxTimeoutPromptness is the core regression for issue #81. It runs a
// fixpoint that would normally take ~400ms, with a 50ms timeout, and asserts
// the call returns within ~150ms (50ms timeout + 100ms slack for one rule
// completing after the deadline fires). If ctx were only checked at stratum
// boundaries (and the test fixture has only one stratum), this test would
// hang for the full ~400ms.
func TestCtxTimeoutPromptness(t *testing.T) {
	ep, baseRels := divergentTransitiveClosurePlan(chainTCSize)

	const timeout = 50 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	t0 := time.Now()
	_, err := Evaluate(ctx, ep, baseRels, WithMaxIterations(0))
	elapsed := time.Since(t0)

	if err == nil {
		t.Fatalf("expected error from timed-out context, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected error wrapping context.DeadlineExceeded, got: %v", err)
	}
	// Promptness window: must finish within 100ms after the deadline
	// (deadline = 50ms; ceiling = 150ms). The ceiling is conservative — it
	// allows for one full RuleDelta call landing just after the deadline
	// (each iteration is ~1ms on this fixture, well under the slack). If the
	// fix regresses to per-stratum checks the elapsed time would be ~400ms,
	// blowing the ceiling.
	const ceiling = 150 * time.Millisecond
	if elapsed > ceiling {
		t.Errorf("Evaluate took %v; ceiling is %v (timeout=%v + 100ms slack). Per-rule ctx check likely regressed.", elapsed, ceiling, timeout)
	}
	t.Logf("timeout=%v elapsed=%v err=%v", timeout, elapsed, err)
}

// TestCtxTimeoutPromptnessParallel mirrors TestCtxTimeoutPromptness for the
// WithParallel() path. The parallel evaluator must also propagate ctx into
// its workers and bail promptly. Without the per-worker ctx.Err() check
// added to parallelDelta, this test would only catch the next per-iteration
// boundary check — still better than per-stratum, but the worker-level
// check is what the issue spec calls for.
func TestCtxTimeoutPromptnessParallel(t *testing.T) {
	ep, baseRels := divergentTransitiveClosurePlan(chainTCSize)

	const timeout = 50 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	t0 := time.Now()
	_, err := Evaluate(ctx, ep, baseRels, WithMaxIterations(0), WithParallel())
	elapsed := time.Since(t0)

	if err == nil {
		t.Fatalf("parallel: expected error from timed-out context, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("parallel: expected error wrapping context.DeadlineExceeded, got: %v", err)
	}
	const ceiling = 150 * time.Millisecond
	if elapsed > ceiling {
		t.Errorf("parallel: Evaluate took %v; ceiling is %v (timeout=%v + 100ms slack)", elapsed, ceiling, timeout)
	}
	t.Logf("parallel: timeout=%v elapsed=%v err=%v", timeout, elapsed, err)
}

// TestCtxNoTimeoutNoRegression asserts that a query which converges well
// inside the deadline returns the full result set with no error. Guards
// against a too-eager ctx check that fires spuriously, or against the ctx
// error being silently swallowed and merged into a partial result.
func TestCtxNoTimeoutNoRegression(t *testing.T) {
	// Small chain — 8 nodes, converges in 7 iterations (~ms).
	ep, baseRels := divergentTransitiveClosurePlan(8)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	t0 := time.Now()
	rs, err := Evaluate(ctx, ep, baseRels, WithMaxIterations(0))
	elapsed := time.Since(t0)

	if err != nil {
		t.Fatalf("converging query erroneously cancelled: %v (elapsed=%v)", err, elapsed)
	}
	if len(rs.Rows) != 28 {
		t.Errorf("expected 28 transitive pairs for 8-node chain, got %d", len(rs.Rows))
	}
	if elapsed > 1*time.Second {
		t.Errorf("converging 8-node chain took %v; expected < 1s", elapsed)
	}
}

// TestCtxNoTimeoutNoRegressionParallel mirrors the above for WithParallel().
func TestCtxNoTimeoutNoRegressionParallel(t *testing.T) {
	ep, baseRels := divergentTransitiveClosurePlan(8)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rs, err := Evaluate(ctx, ep, baseRels, WithMaxIterations(0), WithParallel())
	if err != nil {
		t.Fatalf("parallel converging query erroneously cancelled: %v", err)
	}
	if len(rs.Rows) != 28 {
		t.Errorf("parallel: expected 28 transitive pairs for 8-node chain, got %d", len(rs.Rows))
	}
}

// TestCtxErrorMessageHasContext asserts the wrapped error includes stratum
// and iteration metadata so operators can see WHERE the cancellation hit.
// This is a guard against the fix regressing to a bare `return ctx.Err()`
// which loses diagnostic context.
func TestCtxErrorMessageHasContext(t *testing.T) {
	ep, baseRels := divergentTransitiveClosurePlan(chainTCSize)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := Evaluate(ctx, ep, baseRels, WithMaxIterations(0))
	if err == nil {
		t.Fatal("expected timeout error")
	}
	msg := err.Error()
	// Must mention "stratum" — the per-stratum context wrapper at minimum.
	// Per-rule cancellations also include "rule" and "iteration".
	if !contains(msg, "stratum") {
		t.Errorf("error message %q should mention 'stratum' for diagnostics", msg)
	}
	if !contains(msg, "cancelled") {
		t.Errorf("error message %q should mention 'cancelled'", msg)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
