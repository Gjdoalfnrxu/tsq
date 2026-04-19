package eval

import (
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

// TestSaturatedSizeHintInSyncWithPlan asserts that eval.SaturatedSizeHint
// and plan.SaturatedSizeHint hold the same value. The recursive-IDB
// estimator (plan.EstimateRecursiveIDB) writes saturated hints from
// the planner side; the trivial-IDB pre-pass (EstimateNonRecursiveIDBSizes)
// writes them from the eval side. They must agree so the planner's
// scoring sees a single ceiling regardless of which estimator branch
// produced the hint.
func TestSaturatedSizeHintInSyncWithPlan(t *testing.T) {
	if SaturatedSizeHint != plan.SaturatedSizeHint {
		t.Fatalf("eval.SaturatedSizeHint=%d != plan.SaturatedSizeHint=%d; the two constants must stay in lock-step (see plan/estimate_recursive.go and eval/estimate.go)",
			SaturatedSizeHint, plan.SaturatedSizeHint)
	}
}
