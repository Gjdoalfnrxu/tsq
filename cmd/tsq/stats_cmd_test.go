package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Gjdoalfnrxu/tsq/extract/db"
	"github.com/Gjdoalfnrxu/tsq/ql/stats"
)

// makeTinyDB writes a small EDB to dir/tsq.db for tests.
func makeTinyDB(t *testing.T, dir string) string {
	t.Helper()
	database := db.NewDB()
	files := database.Relation("File")
	for i := int32(1); i <= 5; i++ {
		if err := files.AddTuple(database, i, "/x/f.ts", "h"); err != nil {
			t.Fatal(err)
		}
	}
	dbPath := filepath.Join(dir, "tsq.db")
	f, err := os.Create(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Encode(f); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return dbPath
}

// `tsq stats compute` writes a sidecar that `tsq stats inspect` then reads.
func TestCLI_StatsComputeAndInspect(t *testing.T) {
	dir := t.TempDir()
	dbPath := makeTinyDB(t, dir)

	var stdout, stderr bytes.Buffer
	if code := run([]string{"stats", "compute", dbPath}, &stdout, &stderr); code != 0 {
		t.Fatalf("compute exit=%d, stderr=%s", code, stderr.String())
	}
	if _, err := os.Stat(stats.SidecarPath(dbPath)); err != nil {
		t.Fatalf("sidecar not written: %v", err)
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"stats", "inspect", dbPath}, &stdout, &stderr); code != 0 {
		t.Fatalf("inspect exit=%d, stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "rel File") {
		t.Errorf("inspect output missing rel File:\n%s", stdout.String())
	}
}

// `tsq stats inspect` on a missing sidecar exits non-zero with a warning.
func TestCLI_StatsInspectMissing(t *testing.T) {
	dir := t.TempDir()
	dbPath := makeTinyDB(t, dir)
	// Don't compute the sidecar.

	var stdout, stderr bytes.Buffer
	code := run([]string{"stats", "inspect", dbPath}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("expected non-zero exit when sidecar missing")
	}
	if !strings.Contains(stderr.String(), "default-stats mode") {
		t.Errorf("stderr should mention default-stats mode:\n%s", stderr.String())
	}
}

// Hash invalidation surfaces through the CLI as a warning + non-zero exit.
func TestCLI_StatsInspectStale(t *testing.T) {
	dir := t.TempDir()
	dbPath := makeTinyDB(t, dir)

	var b1, b2 bytes.Buffer
	if code := run([]string{"stats", "compute", dbPath}, &b1, &b2); code != 0 {
		t.Fatalf("compute failed: %s", b2.String())
	}
	// Mutate the EDB in place to invalidate the hash.
	if err := os.WriteFile(dbPath, []byte("not a real db anymore"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := run([]string{"stats", "inspect", dbPath}, &stdout, &stderr); code == 0 {
		t.Fatal("expected non-zero exit when sidecar is stale")
	}
	if !strings.Contains(stderr.String(), "stale") && !strings.Contains(stderr.String(), "mismatch") {
		t.Errorf("stderr should explain the staleness:\n%s", stderr.String())
	}
}
