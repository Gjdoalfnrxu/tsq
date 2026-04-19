package stats

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPersist_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	edb := filepath.Join(dir, "fake.db")
	if err := os.WriteFile(edb, []byte("hello edb"), 0o600); err != nil {
		t.Fatal(err)
	}
	hash, err := HashFile(edb)
	if err != nil {
		t.Fatal(err)
	}
	s := sampleSchema()
	s.EDBHash = hash

	if err := Save(edb, s); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := os.Stat(SidecarPath(edb)); err != nil {
		t.Fatalf("sidecar not at %s: %v", SidecarPath(edb), err)
	}

	var warnBuf bytes.Buffer
	got, err := Load(edb, &warnBuf)
	if err != nil {
		t.Fatalf("load: %v (warnings: %s)", err, warnBuf.String())
	}
	if got == nil {
		t.Fatal("nil schema")
	}
	if got.EDBHash != hash {
		t.Fatal("hash mismatch on round trip")
	}
}

// Hash invalidation: mutate the EDB after sidecar write — Load must
// reject and return ErrHashMismatch with a stderr warning.
func TestPersist_HashInvalidation(t *testing.T) {
	dir := t.TempDir()
	edb := filepath.Join(dir, "fake.db")
	os.WriteFile(edb, []byte("original"), 0o600)
	hash, _ := HashFile(edb)
	s := sampleSchema()
	s.EDBHash = hash
	if err := Save(edb, s); err != nil {
		t.Fatal(err)
	}

	// Simulate stale sidecar: mutate the EDB, leave the sidecar.
	os.WriteFile(edb, []byte("mutated"), 0o600)

	var warnBuf bytes.Buffer
	_, err := Load(edb, &warnBuf)
	if !errors.Is(err, ErrHashMismatch) {
		t.Fatalf("expected ErrHashMismatch, got %v", err)
	}
	if warnBuf.Len() == 0 {
		t.Fatal("expected stderr warning on hash mismatch (default-stats degradation must be loud)")
	}
}

// Missing sidecar: Load returns the os.ErrNotExist error and warns.
func TestPersist_MissingSidecar(t *testing.T) {
	dir := t.TempDir()
	edb := filepath.Join(dir, "fake.db")
	os.WriteFile(edb, []byte("x"), 0o600)
	var warnBuf bytes.Buffer
	_, err := Load(edb, &warnBuf)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected ErrNotExist, got %v", err)
	}
	if warnBuf.Len() == 0 {
		t.Fatal("expected warning on missing sidecar")
	}
}
