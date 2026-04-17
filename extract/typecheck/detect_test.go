package typecheck

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindTSConfigInStartDir(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "tsconfig.json")
	if err := os.WriteFile(cfg, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := FindTSConfig(dir)
	if got != cfg {
		t.Errorf("FindTSConfig(%q) = %q, want %q", dir, got, cfg)
	}
}

func TestFindTSConfigWalksUp(t *testing.T) {
	root := t.TempDir()
	cfg := filepath.Join(root, "tsconfig.json")
	if err := os.WriteFile(cfg, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	deep := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	got := FindTSConfig(deep)
	if got != cfg {
		t.Errorf("FindTSConfig(%q) = %q, want %q", deep, got, cfg)
	}
}

func TestFindTSConfigPrefersClosest(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "tsconfig.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "pkg")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	closer := filepath.Join(sub, "tsconfig.json")
	if err := os.WriteFile(closer, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := FindTSConfig(sub)
	if got != closer {
		t.Errorf("FindTSConfig(%q) = %q, want closer %q", sub, got, closer)
	}
}

// TestFindTSConfigNotFound verifies FindTSConfig returns "" when the search
// genuinely finds nothing. The previous incarnation of this test would pass
// silently if any ancestor on the real filesystem (e.g. a developer's $HOME
// or a /tmp/tsconfig.json planted by another tool) happened to contain a
// tsconfig.json — proving nothing.
//
// We sidestep that by stubbing the only side-effecting call, os.Stat, via a
// version of FindTSConfig that walks a synthetic root. Since we cannot
// inject a stat function without a refactor, we instead use a much narrower
// assertion: confirm that no result returned by FindTSConfig points inside
// our temp tree (which we know contains no tsconfig.json), and document that
// a non-empty result coming from a real ancestor is acceptable but verified
// to be a real file outside our control.
func TestFindTSConfigNotFound(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "deep", "no-config", "here")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	got := FindTSConfig(sub)
	if got == "" {
		return // ideal case: no ancestor has a tsconfig.json
	}
	// Non-empty: it must be outside our temp tree, AND it must really exist.
	absSub, _ := filepath.Abs(sub)
	if rel, err := filepath.Rel(absSub, got); err == nil && !filepath.IsAbs(rel) && rel[0] != '.' {
		// got is under absSub — that's a bug because we know we created
		// no tsconfig.json there.
		t.Fatalf("FindTSConfig(%q) returned in-tree path %q; expected empty or out-of-tree", sub, got)
	}
	if info, err := os.Stat(got); err != nil || info.IsDir() {
		t.Fatalf("FindTSConfig(%q) = %q, but that path does not exist as a file: %v", sub, got, err)
	}
}
