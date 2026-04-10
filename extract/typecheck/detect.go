package typecheck

import (
	"os"
	"os/exec"
)

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
