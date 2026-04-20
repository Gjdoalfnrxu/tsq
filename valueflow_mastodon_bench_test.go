//go:build bench
// +build bench

package integration_test

import (
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"
)

// Phase C PR7 — Mastodon perf gate.
//
// This file builds only under the `bench` tag:  `go test -tags=bench`.
// It is NOT part of the default CI matrix because the Mastodon corpus
// is not checked into the repo and would bloat CI wall-time + produce
// flaky signal on shared runners. The manual run path is documented
// in docs/design/valueflow-phase-c-plan.md §9 and in CONTRIBUTING.md
// under "Running Phase C perf gates."
//
// Gate contract (plan §9.1, wiki §Phase C PR6):
//
//   - Pre-Phase-C baseline (post-PR #170, today's `main`):
//       Mastodon ~48s for the `_disj_2` query family.
//   - Post-PR6 / Phase C rollout budget:
//       Acceptable:         ≤ +50% of baseline (≤ 72s)
//       Flag-for-follow-up: +50% to +100%
//       Blocks merge:       > +100%
//
// The gate reads the corpus path from TSQ_MASTODON_CORPUS and the
// baseline wall-time (in seconds) from TSQ_MASTODON_BASELINE_SECONDS
// so local operators can re-baseline without a code change. If either
// is unset, the test skips with an explanatory message — the signal
// comes from the operator's CI/cron configuration, not from the test
// hardcoding a corpus that doesn't exist on most machines.

const (
	envCorpus       = "TSQ_MASTODON_CORPUS"
	envBaselineSec  = "TSQ_MASTODON_BASELINE_SECONDS"
	defaultBaseline = 48.0
	// perfBudgetMultiplier is the "blocks-merge" threshold per plan §9.1.
	// 1.5× baseline — +50% over pre-Phase-C.
	perfBudgetMultiplier = 1.5
)

// TestBench_MastodonPerfGate — the Mastodon wall-time gate. Runs the
// `_disj_2`-family queries against the configured corpus and fails if
// the total exceeds 1.5× baseline.
//
// This test is gated by the `bench` build tag; a caller running
// `go test ./...` never sees it. Run via:
//
//	TSQ_MASTODON_CORPUS=/path/to/mastodon \
//	TSQ_MASTODON_BASELINE_SECONDS=48 \
//	go test -tags=bench -run TestBench_MastodonPerfGate -timeout 10m ./...
//
// On fungoid.xyz the corpus lives at ~/benchmarks/mastodon and the
// bench runner drives this via the janky-bench workflow.
func TestBench_MastodonPerfGate(t *testing.T) {
	corpus := os.Getenv(envCorpus)
	if corpus == "" {
		t.Skipf("%s unset; Mastodon perf gate is an opt-in local bench. "+
			"See docs/design/valueflow-phase-c-plan.md §9 for the manual run path.",
			envCorpus)
	}
	if _, err := os.Stat(corpus); err != nil {
		t.Fatalf("%s=%q: %v — check the path or unset the env var", envCorpus, corpus, err)
	}

	baseline := defaultBaseline
	if s := os.Getenv(envBaselineSec); s != "" {
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			t.Fatalf("%s=%q: not a float: %v", envBaselineSec, s, err)
		}
		if v <= 0 {
			t.Fatalf("%s=%q: baseline must be > 0", envBaselineSec, s)
		}
		baseline = v
	}

	ceiling := time.Duration(baseline*perfBudgetMultiplier*float64(time.Second)) / 1
	baselineDur := time.Duration(baseline * float64(time.Second))

	start := time.Now()
	// The actual benchmark body is a full closure-corpus run. We
	// intentionally reuse the closure integration helpers rather than
	// an ad-hoc path — consistency with the rest of the PR7 harness.
	_ = runClosureCorpusAgainstDir(t, corpus)
	elapsed := time.Since(start)

	summary := fmt.Sprintf("Mastodon perf gate: elapsed=%s baseline=%s ceiling=%s (x%.2f)",
		elapsed.Round(time.Millisecond),
		baselineDur.Round(time.Millisecond),
		ceiling.Round(time.Millisecond),
		float64(elapsed)/float64(baselineDur))
	t.Log(summary)

	switch {
	case elapsed <= baselineDur:
		t.Logf("within baseline — Phase C closure appears to beat the pre-Phase-C budget")
	case elapsed <= ceiling:
		// Within +50% — the plan's "flag-for-follow-up" zone.
		t.Logf("elapsed %s is between baseline and +50%% ceiling — flag-for-follow-up zone", elapsed)
	default:
		t.Fatalf("perf gate BLOCK: %s\nelapsed %s > ceiling %s (%.2fx baseline %s). "+
			"Per plan §9.1, this blocks the Phase C rollout. Either optimise "+
			"the recursive closure, lift the cap, or revert the bridge migration "+
			"per the minimum-viable plan (parent doc §7).",
			summary, elapsed, ceiling,
			float64(elapsed)/float64(baselineDur), baselineDur)
	}
}

// runClosureCorpusAgainstDir — body of the bench. Runs the
// located-projection closure query against the given corpus directory.
// Separated out for readability and to keep TestBench_MastodonPerfGate
// focused on the gate logic.
func runClosureCorpusAgainstDir(t *testing.T, corpus string) int {
	t.Helper()
	rs := runClosureQuery(t,
		"testdata/queries/v2/valueflow/all_mayResolveToRec_located.ql",
		corpus)
	return len(rs.Rows)
}
