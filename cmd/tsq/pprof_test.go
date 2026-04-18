package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Gjdoalfnrxu/tsq/extract/db"
)

// writeEmptyDB writes a serialized empty fact database to the given path.
// Used by the pprof flag tests to give `tsq query` something to load.
func writeEmptyDB(t *testing.T, path string) {
	t.Helper()
	database := db.NewDB()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create db: %v", err)
	}
	defer f.Close()
	if err := database.Encode(f); err != nil {
		t.Fatalf("encode db: %v", err)
	}
}

// writeTrivialQuery writes a query that has no body literals and so will
// finish almost instantly even on an empty DB. The literal `1=1` forces a
// constant-true filter; the result set is empty (no `from` bindings) but
// the evaluator still walks the full pipeline.
func writeTrivialQuery(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "trivial.ql")
	src := "from int x\nwhere x = 1\nselect x\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatalf("write query: %v", err)
	}
	return path
}

// TestQueryPprofFlags is the user-visible regression guard for the pprof
// scaffolding upstreamed from the cain-nas profile build (see #130). It
// asserts:
//  1. --cpu-profile and --mem-profile produce non-empty pprof files at the
//     declared paths after a successful query.
//  2. The binary does not crash when all three flags are passed together.
//  3. The flags are documented in the --help output (so future readers know
//     they exist without grepping source).
func TestQueryPprofFlags(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "tsq.db")
	writeEmptyDB(t, dbPath)
	queryPath := writeTrivialQuery(t, dir)

	cpuPath := filepath.Join(dir, "cpu.pprof")
	memPath := filepath.Join(dir, "mem.pprof")
	snapDir := filepath.Join(dir, "snapshots")

	var stdout, stderr bytes.Buffer
	code := run([]string{
		"query",
		"--db", dbPath,
		"--cpu-profile", cpuPath,
		"--mem-profile", memPath,
		"--mem-snapshot-dir", snapDir,
		queryPath,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, stderr.String())
	}

	// CPU and mem profile files must exist and be non-empty after the
	// query. (pprof files always have a gzip header + at least one sample
	// frame, so >0 bytes is a sufficient sanity check; the alternative —
	// parsing the profile via runtime/pprof — is much heavier and gives
	// no extra signal here.)
	for _, p := range []string{cpuPath, memPath} {
		st, err := os.Stat(p)
		if err != nil {
			t.Errorf("stat %s: %v", p, err)
			continue
		}
		if st.Size() == 0 {
			t.Errorf("%s is empty (want non-empty pprof file)", p)
		}
	}

	// snapshot dir must have been created. We don't assert that any
	// snapshot files exist — the trivial query completes in <1s, the
	// ticker fires every 10s, so under normal conditions no snapshot
	// is written. The dir existing proves the goroutine ran the
	// MkdirAll branch without crashing.
	if st, err := os.Stat(snapDir); err != nil {
		t.Errorf("stat snapshot dir: %v", err)
	} else if !st.IsDir() {
		t.Errorf("%s is not a dir", snapDir)
	}
}

// TestQueryPprofFlagsInHelp is a documentation regression — if someone
// renames or drops a flag, the help text changes and this test catches it.
// Cheap, fast, and prevents the "flag silently disappeared in a refactor"
// failure mode.
func TestQueryPprofFlagsInHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"query", "--help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, stderr.String())
	}
	help := stderr.String() + stdout.String()
	for _, want := range []string{"-cpu-profile", "-mem-profile", "-mem-snapshot-dir"} {
		if !strings.Contains(help, want) {
			t.Errorf("help missing %q\nfull help:\n%s", want, help)
		}
	}
}

// TestQueryCPUProfileBadPath asserts that an unwriteable --cpu-profile path
// causes a clean non-zero exit (rather than a panic or a silent skip). The
// pprof flags are diagnostic — surfacing setup failures loudly is the
// whole point.
func TestQueryCPUProfileBadPath(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "tsq.db")
	writeEmptyDB(t, dbPath)
	queryPath := writeTrivialQuery(t, dir)

	var stdout, stderr bytes.Buffer
	code := run([]string{
		"query",
		"--db", dbPath,
		"--cpu-profile", "/proc/cant-write-here/cpu.pprof",
		queryPath,
	}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("exit code = 0, want non-zero on unwriteable cpu profile path; stderr:\n%s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "cpu profile") {
		t.Errorf("stderr should mention cpu profile; got:\n%s", stderr.String())
	}
}
