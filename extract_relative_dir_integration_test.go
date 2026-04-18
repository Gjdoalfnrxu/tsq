// Package integration_test — regression guard for issue #110.
//
// `tsq extract --dir <relative>` used to break tsgo type enrichment because
// the walker stored relative file paths into the File relation and tsgo's
// DocumentIdentifier rejects relative paths ("source file not found" for
// every position query). Fixed by absolutising *dir at the CLI boundary in
// cmdExtract.
//
// This test runs `tsq extract` with cwd set to the repo root and --dir set
// to the *relative* fixture path. Without the fix, the tsgo enrichment
// summary line reports facts=0 and the Type query returns zero rows. With
// the fix, both pass.
package integration_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestCLI_ExtractRelativeDir_TsgoEnrichment is the issue #110 regression
// guard. It is structurally similar to the absolute-path full-project test
// but invokes the CLI with cmd.Dir = <repo root> and --dir = <relative>,
// which is the exact failure mode reported in the issue.
func TestCLI_ExtractRelativeDir_TsgoEnrichment(t *testing.T) {
	tsgoPath := detectTsgoForTest()
	if tsgoPath == "" {
		// The whole point of this test is to verify tsgo enrichment works
		// with a relative --dir. Without tsgo we cannot observe the bug,
		// so skip rather than provide false confidence. CI must set
		// TSGO_PATH (same contract as TestCLI_FullTSProject_EndToEnd).
		if os.Getenv("CI") != "" {
			t.Fatal("tsgo not available in CI: install tsgo or set TSGO_PATH so the issue #110 regression guard runs")
		}
		t.Skip("tsgo not available; cannot exercise the relative-dir tsgo path")
	}

	tsq := buildTSQBinary(t)
	repoRoot := cliRepoRoot(t)
	relDir := filepath.Join("testdata", "projects", "full-ts-project")

	// Sanity: the relative path resolves to the fixture from the repo root.
	if _, err := os.Stat(filepath.Join(repoRoot, relDir, "tsconfig.json")); err != nil {
		t.Fatalf("fixture missing at %s/%s: %v", repoRoot, relDir, err)
	}

	workDir := t.TempDir()
	dbFile := filepath.Join(workDir, "rel.db")

	cmd := exec.Command(tsq, "extract", "--dir", relDir, "--output", dbFile)
	cmd.Dir = repoRoot // <-- this is what makes --dir relative meaningful
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("tsq extract --dir %s (cwd=%s) failed: %v\nstderr: %s",
			relDir, repoRoot, err, stderr.String())
	}
	if _, err := os.Stat(dbFile); err != nil {
		t.Fatalf("extract produced no output file: %v", err)
	}

	stderrStr := stderr.String()

	// Pre-fix failure mode: the enrichment summary line reports facts=0
	// because every getSymbolAtPosition call errored with "source file not
	// found". Post-fix: facts > 0.
	if !strings.Contains(stderrStr, "tsgo type enrichment complete:") {
		t.Fatalf("expected enrichment summary in stderr; got:\n%s", stderrStr)
	}
	if strings.Contains(stderrStr, "facts=0 ") {
		t.Fatalf("issue #110: tsgo produced zero facts when --dir was relative (%s); stderr:\n%s",
			relDir, stderrStr)
	}

	// Cross-check via the Type query. ResolvedType is populated only by
	// tsgo enrichment, so a non-empty result confirms the relative --dir
	// path actually flowed through to tsgo correctly. We pin "User" exactly
	// (project-defined type from the fixture) so a stray primitive emission
	// like "string" can't carry the assertion.
	q := writeQueryFile(t, workDir, "q_types.ql",
		"import tsq::types\n\nfrom Type t\nselect t.getDisplayName() as \"name\"\n")
	out := runCLIQuery(t, tsq, dbFile, "csv", q)
	rows := dataRows(parseCSV(t, out))
	if len(rows) == 0 {
		t.Fatalf("Type query returned zero rows on relative-dir DB; issue #110 not fixed.\nstderr:\n%s\nquery output:\n%s",
			stderrStr, out)
	}
	if !hasRowWithExact(rows, "User") {
		t.Errorf("issue #110: expected exact-cell type %q in tsgo-derived types; rows: %v", "User", rows)
	}
}
