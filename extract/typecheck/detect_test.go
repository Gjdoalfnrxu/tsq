package typecheck

import (
	"os"
	"path/filepath"
	"strings"
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

func TestFindTSConfigNotFound(t *testing.T) {
	dir := t.TempDir()
	// Use a sub-directory of TempDir so we don't accidentally find a real
	// tsconfig.json by walking up the actual filesystem (the repo root may
	// not have one, but TempDir lives under /tmp so this is safe).
	sub := filepath.Join(dir, "no-config-here")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	// FindTSConfig walks all the way up to filesystem root; if it returns
	// non-empty, that's because some ancestor has a tsconfig.json. Skip
	// the test in that case rather than emit a false failure.
	if got := FindTSConfig(sub); got != "" {
		// Only fail if it returned a path inside our temp tree.
		if abs, _ := filepath.Abs(sub); strings.HasPrefix(got, abs+string(filepath.Separator)) || got == filepath.Join(abs, "tsconfig.json") {
			t.Errorf("FindTSConfig(%q) = %q, want empty", sub, got)
		}
	}
}
