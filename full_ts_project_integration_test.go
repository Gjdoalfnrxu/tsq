// Package integration_test — full TypeScript project shape regression guard.
//
// Existing testdata/projects/* fixtures are bare TS file directories: no
// tsconfig.json, no package.json, no multi-directory structure. None of them
// exercises the project shape tsq actually encounters in the wild — and
// tsconfig handling is load-bearing for tsgo enrichment (PR #84, issue #91).
// This file adds:
//
//   - A fully-configured fixture (tsconfig.json with strict + paths, package.json
//     with realistic deps, src/{components,utils,types}/, tests/) under
//     testdata/projects/full-ts-project/.
//   - An end-to-end test that builds the tsq binary, runs `tsq extract` on the
//     fixture, and runs three representative queries against the resulting DB:
//     1. Function listing — smoke test.
//     2. Cross-module imports — verifies imports were extracted across the
//     multi-directory tree.
//     3. Tsgo type enrichment — verifies the project's tsconfig was honored
//     and tsgo populated ResolvedType. Skipped if tsgo is unavailable on
//     the host (so CI on bare clones is not broken).
//
// Historical note: the tsgo branch documented a CLI gotcha (issue #110) where
// relative --dir paths broke every getSymbolAtPosition query because file
// paths in the File relation were not absolutised before being handed to tsgo
// via RegisterFiles. The fix lives in cmdExtract (absolutise *dir at the CLI
// boundary), and the dedicated regression guard is
// TestCLI_ExtractRelativeDir_TsgoEnrichment in
// extract_relative_dir_integration_test.go. This test now uses the fixture
// path as-is without a workaround.
package integration_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fullTSProjectDir returns the path to the fixture, joined off the repo root.
// cliRepoRoot is already absolute (resolved from runtime.Caller), so this is
// absolute by construction — no filepath.Abs needed. Issue #110 (CLI now
// absolutises --dir) means this test would still pass if the path were
// relative, but keeping it absolute matches the rest of the integration suite.
func fullTSProjectDir(t *testing.T) string {
	t.Helper()
	root := cliRepoRoot(t)
	return filepath.Join(root, "testdata", "projects", "full-ts-project")
}

// detectTsgoForTest mirrors typecheck.DetectTsgo's first two checks (TSGO_PATH
// env var and `tsgo` in PATH). We deliberately do NOT trigger the npx fallback
// here — npx --yes can take 10+s and fetch packages, which would slow every
// test run on hosts without tsgo installed. The type-enrichment assertion is
// allowed to skip on those hosts; the structural assertions still run.
func detectTsgoForTest() string {
	if p := os.Getenv("TSGO_PATH"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if p, err := exec.LookPath("tsgo"); err == nil {
		return p
	}
	return ""
}

// runExtractFull runs `tsq extract --dir <abs> --output <db>` and additionally
// allows the caller to opt out of tsgo (--tsgo off) for the structural pass.
// It captures stderr so we can assert on the tsgo enrichment summary line.
func runExtractFull(t *testing.T, tsq, dir, dbFile string, withTsgo bool) string {
	t.Helper()
	args := []string{"extract", "--dir", dir, "--output", dbFile}
	if !withTsgo {
		args = append(args, "--tsgo", "off")
	}
	cmd := exec.Command(tsq, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("tsq extract failed: %v\nstderr: %s", err, stderr.String())
	}
	if _, err := os.Stat(dbFile); err != nil {
		t.Fatalf("extract produced no output file %s: %v", dbFile, err)
	}
	return stderr.String()
}

// dataRows returns rows[1:] (everything past the header), filtering empty rows.
// CSV output from `tsq query` always has a header, even when there are zero
// data rows; callers want to assert on the data, not the header.
func dataRows(rows [][]string) [][]string {
	if len(rows) <= 1 {
		return nil
	}
	out := make([][]string, 0, len(rows)-1)
	for _, r := range rows[1:] {
		if len(r) == 0 {
			continue
		}
		out = append(out, r)
	}
	return out
}

// hasRowWith reports whether any row contains the given substring in any column.
// Lets the test pin specific facts (function names, module specifiers,
// type displays) without coupling to column ordering.
func hasRowWith(rows [][]string, needle string) bool {
	for _, row := range rows {
		for _, cell := range row {
			if strings.Contains(cell, needle) {
				return true
			}
		}
	}
	return false
}

// hasRowWithExact reports whether any row contains a cell that exactly equals
// needle. Used for type-pass assertions where substring matches would let
// "User" pass on rows like "UserList", "UserListProps", "UserId" — defeating
// the point of pinning the type to confirm tsgo resolved it.
func hasRowWithExact(rows [][]string, needle string) bool {
	for _, row := range rows {
		for _, cell := range row {
			if cell == needle {
				return true
			}
		}
	}
	return false
}

// TestCLI_FullTSProject_EndToEnd extracts the realistic-shape fixture and
// exercises three representative queries. Sequential (no -parallel) by design
// — the project guidance says 3GB host, no -race, sequential only.
func TestCLI_FullTSProject_EndToEnd(t *testing.T) {
	tsq := buildTSQBinary(t)
	dir := fullTSProjectDir(t)
	workDir := t.TempDir()

	// Sanity: the fixture exists. If a future cleanup deletes it, fail loud.
	if _, err := os.Stat(filepath.Join(dir, "tsconfig.json")); err != nil {
		t.Fatalf("fixture missing tsconfig.json at %s: %v", dir, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "package.json")); err != nil {
		t.Fatalf("fixture missing package.json at %s: %v", dir, err)
	}

	// --------------------------------------------------------------
	// Pass 1: structural extraction with tsgo OFF.
	// All structural queries (functions, imports) must work without tsgo.
	// --------------------------------------------------------------
	dbFile := filepath.Join(workDir, "full.db")
	_ = runExtractFull(t, tsq, dir, dbFile, false /*tsgo*/)

	// Query 1 — function listing. Smoke test.
	q1 := writeQueryFile(t, workDir, "q_functions.ql",
		"import tsq::functions\n\nfrom Function f\nselect f.getName() as \"name\"\n")
	out1 := runCLIQuery(t, tsq, dbFile, "csv", q1)
	rows1 := dataRows(parseCSV(t, out1))
	if len(rows1) == 0 {
		t.Fatalf("function-list query returned zero rows; output:\n%s", out1)
	}
	// Pin specific names from each src/ subdirectory so a regression in any
	// directory is caught. These are exported names from the fixture.
	for _, want := range []string{"UserList", "filterBy", "usersWithRole", "formatUserLabel", "greet"} {
		if !hasRowWith(rows1, want) {
			t.Errorf("function-list missing %q in results; got: %v", want, rows1)
		}
	}

	// Query 2 — cross-module imports. Verifies the multi-directory import
	// graph was extracted: components/ imports from utils/ and types/, and
	// the tests/ directory imports from src/. If the walker only handles a
	// single directory, this query collapses to one or two rows.
	q2 := writeQueryFile(t, workDir, "q_imports.ql",
		"import tsq::imports\n\nfrom ImportBinding ib\nselect ib.getModuleSpec() as \"module\", ib.getImportedName() as \"imported\"\n")
	out2 := runCLIQuery(t, tsq, dbFile, "csv", q2)
	rows2 := dataRows(parseCSV(t, out2))
	if len(rows2) == 0 {
		t.Fatalf("import query returned zero rows; output:\n%s", out2)
	}
	// Cross-directory edges (component → utils, component → types, tests → src):
	wantImportPairs := []struct {
		module, imported string
	}{
		{"../utils/format", "formatUserLabel"}, // components/UserList.tsx → utils/format.ts
		{"../utils/filter", "usersWithRole"},   // components/UserList.tsx → utils/filter.ts
		{"../types/user", "UserRole"},          // components/UserList.tsx → types/user.ts
		{"./format", "pluck"},                  // utils/filter.ts → utils/format.ts (sibling)
		{"../src/utils/format", "greet"},       // tests/format.test.ts → src/utils/format.ts
		{"react", "useState"},                  // external dep
	}
	for _, want := range wantImportPairs {
		found := false
		for _, row := range rows2 {
			if len(row) >= 2 && row[0] == want.module && row[1] == want.imported {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected import (%q, %q) not found; rows: %v", want.module, want.imported, rows2)
		}
	}

	// --------------------------------------------------------------
	// Pass 2: tsgo type enrichment. Skip if tsgo is unavailable.
	//
	// This is the regression guard for PR #84 / issue #91 against the
	// realistic project shape: a deep src/ tree, an explicit tsconfig with
	// strict + paths, multiple .ts and .tsx files.
	// --------------------------------------------------------------
	tsgoPath := detectTsgoForTest()
	if tsgoPath == "" {
		// On CI the type-enrichment regression guard MUST run. Silently
		// skipping (the local-dev convenience) would hide PR #84 regressions
		// from the gate that's supposed to catch them. Until CI installs
		// tsgo / sets TSGO_PATH, fail loud so the gap is visible rather than
		// papered over.
		if os.Getenv("CI") != "" {
			t.Fatal("tsgo not available in CI: install tsgo or set TSGO_PATH so the type-enrichment regression guard (PR #84 / issue #91) actually runs; refusing to silently skip in CI")
		}
		t.Log("tsgo not available (set TSGO_PATH or install `tsgo` in PATH); structural passes already validated, skipping type-info pass")
		return
	}
	_ = tsgoPath

	tsgoDB := filepath.Join(workDir, "full-tsgo.db")
	stderr := runExtractFull(t, tsq, dir, tsgoDB, true /*tsgo*/)

	// Tsgo must have used the project's tsconfig. The CLI logs a line of the
	// form "tsgo: using project <path>" when --tsconfig is auto-discovered.
	wantTSConfig := filepath.Join(dir, "tsconfig.json")
	if !strings.Contains(stderr, "tsgo: using project "+wantTSConfig) {
		t.Errorf("expected stderr to confirm tsgo used %s; got:\n%s", wantTSConfig, stderr)
	}
	// And it must have produced non-zero facts. Pre-PR #84, the wire format
	// bug meant facts=0 even with the tsconfig honored. The post-fix
	// expectation on this fixture is comfortably > 10.
	if !strings.Contains(stderr, "tsgo type enrichment complete:") {
		t.Errorf("expected enrichment summary line in stderr; got:\n%s", stderr)
	}
	if strings.Contains(stderr, "facts=0 ") {
		t.Errorf("tsgo produced zero facts — PR #84 regression or environment problem; stderr:\n%s", stderr)
	}

	// Query 3 — type-info-derived facts. The Type relation (ResolvedType
	// under the hood) is populated ONLY by tsgo enrichment. If the tsconfig
	// were ignored or the wire format regressed, this query returns zero
	// rows even though Pass 1 passed.
	q3 := writeQueryFile(t, workDir, "q_types.ql",
		"import tsq::types\n\nfrom Type t\nselect t.getDisplayName() as \"name\"\n")
	out3 := runCLIQuery(t, tsq, tsgoDB, "csv", q3)
	rows3 := dataRows(parseCSV(t, out3))
	if len(rows3) == 0 {
		t.Fatalf("type query returned zero rows on tsgo-enriched DB; PR #84 regression?\nstderr from extract:\n%s\nquery output:\n%s", stderr, out3)
	}
	// Pin specific type displays. User and UserRole are project-defined types
	// that can ONLY appear if tsgo actually resolved the fixture's tsconfig
	// and walked the type-only modules — so they're both REQUIRED, with exact
	// cell matches (substring would let "User" pass on "UserList",
	// "UserListProps", "UserId" — none of which prove tsgo enrichment).
	//
	// `string` is a primitive sanity check: tsgo emits it for nearly every
	// literal, so its presence alone proves nothing. We assert it separately
	// as a sanity hit, never as a substitute for the project-defined pins.
	requiredExact := []string{"User", "UserRole"}
	for _, want := range requiredExact {
		if !hasRowWithExact(rows3, want) {
			t.Errorf("expected exact-cell type %q in tsgo-derived types (substring matches like UserList do not count); rows: %v", want, rows3)
		}
	}
	if !hasRowWith(rows3, "string") {
		t.Errorf("expected primitive sanity hit %q in tsgo-derived types; rows: %v", "string", rows3)
	}
}

// TestCLI_FullTSProject_HasExpectedShape is a fast, tsgo-independent smoke
// guard that the fixture itself does not regress. If someone deletes a key
// file from the fixture, this test fails before the heavier extraction test
// even runs.
func TestCLI_FullTSProject_HasExpectedShape(t *testing.T) {
	dir := fullTSProjectDir(t)
	wantFiles := []string{
		"tsconfig.json",
		"package.json",
		"src/index.ts",
		"src/types/user.ts",
		"src/utils/format.ts",
		"src/utils/filter.ts",
		"src/components/UserList.tsx",
		"tests/format.test.ts",
	}
	for _, rel := range wantFiles {
		full := filepath.Join(dir, rel)
		info, err := os.Stat(full)
		if err != nil {
			t.Errorf("fixture missing %s: %v", rel, err)
			continue
		}
		if info.IsDir() {
			t.Errorf("fixture file %s is a directory", rel)
		}
		if info.Size() == 0 {
			t.Errorf("fixture file %s is empty", rel)
		}
	}
}
