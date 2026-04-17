package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Gjdoalfnrxu/tsq/bridge"
)

func TestVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"version"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "tsq version") {
		t.Errorf("stdout = %q, want 'tsq version ...'", stdout.String())
	}
}

func TestUnknownSubcommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"bogus"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "unknown command") {
		t.Errorf("stderr = %q, want 'unknown command'", stderr.String())
	}
}

func TestNoArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "usage:") {
		t.Errorf("stderr = %q, want usage info", stderr.String())
	}
}

func TestQueryMissingArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"query"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "QUERY_FILE") {
		t.Errorf("stderr = %q, want mention of QUERY_FILE", stderr.String())
	}
}

func TestCheckMissingArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"check"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "QUERY_FILE") {
		t.Errorf("stderr = %q, want mention of QUERY_FILE", stderr.String())
	}
}

func TestQueryBadFormat(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"query", "--format", "xml", "test.ql"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "unknown format") {
		t.Errorf("stderr = %q, want 'unknown format'", stderr.String())
	}
}

func TestCheckNonexistentFile(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"check", "/nonexistent/file.ql"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "error:") {
		t.Errorf("stderr = %q, want error message", stderr.String())
	}
}

// Regression: global flags only should print usage, not "unknown command --verbose".
func TestGlobalFlagsOnly(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--verbose"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if strings.Contains(stderr.String(), "unknown command") {
		t.Errorf("stderr should show usage, not 'unknown command'; got %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "usage:") {
		t.Errorf("stderr = %q, want usage info", stderr.String())
	}
}

func TestGlobalFlagsMultiple(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--verbose", "--quiet"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "usage:") {
		t.Errorf("stderr = %q, want usage info", stderr.String())
	}
}

// Regression: -h/--help should return exit code 0 for all subcommands.
func TestHelpExitCode(t *testing.T) {
	for _, args := range [][]string{
		{"extract", "-h"},
		{"extract", "--help"},
		{"query", "-h"},
		{"query", "--help"},
		{"check", "-h"},
		{"check", "--help"},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(args, &stdout, &stderr)
			if code != 0 {
				t.Errorf("exit code = %d, want 0 for %v", code, args)
			}
		})
	}
}

// Regression: usage errors return exit code 2, runtime errors return 1.
func TestUsageErrorExitCode(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want int
	}{
		{"no args", []string{}, 2},
		{"unknown command", []string{"bogus"}, 2},
		{"global flags only", []string{"--verbose"}, 2},
		{"query missing file", []string{"query"}, 2},
		{"check missing file", []string{"check"}, 2},
		// Runtime errors should be 1.
		{"check nonexistent file", []string{"check", "/nonexistent/file.ql"}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(tt.args, &stdout, &stderr)
			if code != tt.want {
				t.Errorf("exit code = %d, want %d; stderr: %s", code, tt.want, stderr.String())
			}
		})
	}
}

// Regression for issue #82: cmdCheck must run the same compilation pipeline
// as cmdQuery, including rules.MergeSystemRules. Before the fix, `check`
// stopped at user-program planning and would green-light a query that later
// failed (or hung / OOM'd) inside `query` because the system-rule-augmented
// rule graph differed.
//
// We assert two things:
//  1. buildProgram's resulting plan contains rules whose head names come
//     from the system-rule set (e.g. "CallTarget"). This proves
//     MergeSystemRules ran during the shared pipeline, which both `check`
//     and `query` now go through.
//  2. The plans built with sizeHints=nil (the `check` configuration) and
//     sizeHints=non-nil (the `query` configuration with real DB cardinality)
//     contain the same set of rule head predicates — i.e. the rule graph is
//     identical, only join ordering may differ. This is the exact
//     equivalence that issue #82 required.
func TestBuildProgramMergesSystemRulesForCheck(t *testing.T) {
	src := `import tsq::base
from int x
where x = 1
select x
`
	loader := makeBridgeImportLoader(bridgeLoadForTest())

	checkPlan, _, errs := buildProgram(src, "test.ql", loader, nil)
	if len(errs) > 0 {
		t.Fatalf("buildProgram (check config) returned errors: %v", errs)
	}
	if checkPlan == nil {
		t.Fatal("buildProgram (check config) returned nil plan")
	}

	// Collect head predicate names from every stratum.
	heads := make(map[string]bool)
	for _, stratum := range checkPlan.Strata {
		for _, r := range stratum.Rules {
			heads[r.Head.Predicate] = true
		}
	}

	// CallTarget is defined in extract/rules/callgraph.go (CallGraphRules).
	// Its presence proves MergeSystemRules ran inside buildProgram.
	if !heads["CallTarget"] {
		t.Errorf("expected CallTarget rule head in plan (proof of MergeSystemRules), got heads: %v", heads)
	}

	// Now build a second plan with size hints, simulating the `query` path.
	queryPlan, _, errs := buildProgram(src, "test.ql", loader, map[string]int{"Node": 100, "Call": 50})
	if len(errs) > 0 {
		t.Fatalf("buildProgram (query config) returned errors: %v", errs)
	}
	queryHeads := make(map[string]bool)
	for _, stratum := range queryPlan.Strata {
		for _, r := range stratum.Rules {
			queryHeads[r.Head.Predicate] = true
		}
	}

	// Plan rule-set must be identical between the two configurations: the
	// only difference allowed is join ordering driven by size hints.
	if len(heads) != len(queryHeads) {
		t.Errorf("plan rule-head count differs: check=%d query=%d", len(heads), len(queryHeads))
	}
	for h := range heads {
		if !queryHeads[h] {
			t.Errorf("head %q present in check plan but absent in query plan", h)
		}
	}
	for h := range queryHeads {
		if !heads[h] {
			t.Errorf("head %q present in query plan but absent in check plan", h)
		}
	}
}

// bridgeLoadForTest wraps bridge.LoadBridge so the test reads clearly.
func bridgeLoadForTest() map[string][]byte {
	return bridge.LoadBridge()
}

// Regression: global flags followed by a valid subcommand should work.
func TestGlobalFlagsBeforeSubcommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--verbose", "version"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "tsq version") {
		t.Errorf("stdout = %q, want 'tsq version ...'", stdout.String())
	}
}
