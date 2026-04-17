package typecheck

import (
	"os"
	"os/exec"
	"path/filepath"
)

// FindTSConfig walks up from startDir looking for a tsconfig.json file.
// Returns the absolute path to the first tsconfig.json found, or empty
// string if none is found before reaching the filesystem root.
//
// Used to auto-discover the project config when --tsconfig is not given
// explicitly. Without a tsconfig the tsgo backend has no project loaded
// and type enrichment silently produces no facts.
func FindTSConfig(startDir string) string {
	abs, err := filepath.Abs(startDir)
	if err != nil {
		return ""
	}
	dir := abs
	for {
		candidate := filepath.Join(dir, "tsconfig.json")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// DetectTsgo attempts to find the tsgo binary. Checks:
//  1. TSGO_PATH environment variable
//  2. "tsgo" in PATH
//  3. npx @typescript/native-preview (fallback)
//
// Returns the path to the tsgo binary, or empty string if not found.
func DetectTsgo() string {
	// 1. Explicit env var
	if p := os.Getenv("TSGO_PATH"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	// 2. In PATH
	if p, err := exec.LookPath("tsgo"); err == nil {
		return p
	}

	// 3. npx fallback — check if npx can resolve the package
	if npx, err := exec.LookPath("npx"); err == nil {
		cmd := exec.Command(npx, "--yes", "@typescript/native-preview", "--version")
		if err := cmd.Run(); err == nil {
			return "npx @typescript/native-preview"
		}
	}

	return ""
}
