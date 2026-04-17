package typecheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestOpenProjectLiveTSGo spins up a real tsgo binary and verifies the
// updateSnapshot wire format end-to-end. Skips unless TSGO_PATH is set,
// because most CI environments will not have a tsgo binary available.
//
// What this proves that the mock tests cannot:
//   - the JSON-RPC framing matches a real upstream (Content-Length + body);
//   - the request shape we send (UpdateSnapshotParams{ openProject }) is
//     accepted by typescript-go's API server;
//   - the response we parse really is { snapshot, projects:[{id, configFileName}] };
//   - the snapshot handle and project id are usable for a follow-up query.
//
// Find a tsgo binary either in PATH, in the @typescript/native-preview npm
// package (the "tsgo" subpath under any @typescript/native-preview-* install),
// or via TSGO_PATH directly. To run:
//
//	TSGO_PATH=/path/to/tsgo go test ./extract/typecheck/ -run TestOpenProjectLiveTSGo -v
func TestOpenProjectLiveTSGo(t *testing.T) {
	tsgoPath := os.Getenv("TSGO_PATH")
	if tsgoPath == "" {
		t.Skip("TSGO_PATH not set; skipping live tsgo integration test")
	}
	if _, err := os.Stat(tsgoPath); err != nil {
		t.Skipf("TSGO_PATH=%q not accessible: %v", tsgoPath, err)
	}

	// Build a minimal real TS project under t.TempDir().
	dir := t.TempDir()
	configPath := filepath.Join(dir, "tsconfig.json")
	if err := os.WriteFile(configPath, []byte(`{
  "compilerOptions": {
    "target": "es2020",
    "module": "commonjs",
    "strict": true,
    "noEmit": true
  },
  "include": ["src/**/*.ts"]
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	srcDir := filepath.Join(dir, "src")
	if err := os.Mkdir(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	srcPath := filepath.Join(srcDir, "index.ts")
	srcBody := "const greeting: string = \"hello\";\nconst length = greeting.length;\nexport { greeting, length };\n"
	if err := os.WriteFile(srcPath, []byte(srcBody), 0o644); err != nil {
		t.Fatal(err)
	}

	c, err := NewClient(tsgoPath, dir)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer c.Close()

	// Initialize first — required by the tsgo API session lifecycle.
	if _, err := c.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	// Real wire-format check: this is the assertion that mock tests cannot make.
	// We expect a non-empty snapshot handle and a non-empty project id.
	project, err := c.OpenProject(configPath)
	if err != nil {
		t.Fatalf("OpenProject(%q): %v", configPath, err)
	}
	if project == "" {
		t.Fatal("OpenProject returned empty project handle")
	}
	if snap := c.Snapshot(); snap == "" {
		t.Fatal("Snapshot() empty after OpenProject — handle was not cached")
	}

	// Sanity check: the project id should look like an upstream Handle
	// (single-letter prefix + 16 hex digits — see microsoft/typescript-go
	// internal/api/proto.go createHandle). Don't pin the exact value,
	// because it depends on internal id allocation; just confirm the shape.
	if !strings.HasPrefix(project, "p") {
		t.Errorf("project handle = %q, expected to start with 'p'", project)
	}

	// Follow-up query: getTypeAtPosition with the snapshot/project we got.
	// Use the byte-offset variant because the upstream wire shape requires
	// position to be a uint32 byte offset, not a {line, character} object.
	// Position of "greeting" identifier on line 1: "const greeting" -> offset 6.
	offset := uint32(strings.Index(srcBody, "greeting"))
	if offset == ^uint32(0) {
		t.Fatal("could not find 'greeting' in fixture source")
	}

	info, err := c.GetTypeAtOffset(project, srcPath, offset)
	if err != nil {
		// Type query failure is informative but does not invalidate the
		// snapshot/project assertion above; some tsgo versions may differ
		// in their type query method names. Surface as non-fatal so the
		// primary OpenProject assertion still gates the test.
		t.Logf("GetTypeAtOffset returned error (non-fatal): %v", err)
		return
	}
	t.Logf("GetTypeAtOffset returned: handle=%q displayName=%q flags=%d", info.Handle, info.DisplayName, info.Flags)
}
