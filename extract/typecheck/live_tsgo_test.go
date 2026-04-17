package typecheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestOpenProjectLiveTSGo spins up a real tsgo binary and exercises the full
// downstream pipeline: open project → resolve symbol → resolve type → render
// type display name. This is the regression guard for the bug PR #84 fixes:
// previously the enricher would silently return zero facts because the wire
// format was wrong end-to-end.
//
// To run:
//
//	TSGO_PATH=/path/to/tsgo go test ./extract/typecheck/ \
//	    -run TestOpenProjectLiveTSGo -v
//
// What this proves over the mock tests:
//   - Content-Length framing matches the real tsgo subprocess.
//   - updateSnapshot returns the documented {snapshot, projects} shape.
//   - DocumentIdentifier-bearing requests (file as plain string) actually
//     resolve a source file — the {fileName: ...} object form silently drops
//     into an empty DocumentIdentifier and produces "source file not found".
//   - getSymbolAtPosition / getTypeOfSymbol / typeToString chain returns a
//     populated handle and display name.
func TestOpenProjectLiveTSGo(t *testing.T) {
	tsgoPath := os.Getenv("TSGO_PATH")
	if tsgoPath == "" {
		t.Skip("TSGO_PATH not set; skipping live tsgo integration test")
	}
	if _, err := os.Stat(tsgoPath); err != nil {
		t.Skipf("TSGO_PATH=%q not accessible: %v", tsgoPath, err)
	}

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

	if _, err := c.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	// OpenProject WITH the source file in fileChanges.created — empirically
	// required for the live tsgo binary to resolve the file in subsequent
	// position queries even though it's reachable via the include glob.
	project, err := c.OpenProjectWithFiles(configPath, []string{srcPath})
	if err != nil {
		t.Fatalf("OpenProjectWithFiles(%q): %v", configPath, err)
	}
	if project == "" {
		t.Fatal("OpenProjectWithFiles returned empty project handle")
	}
	snap := c.Snapshot()
	if snap == "" {
		t.Fatal("Snapshot() empty after OpenProject — handle was not cached")
	}

	// Real-binary handle shapes (verified against TypeScript 7.0.0-dev.20260416):
	//   - snapshot: 'n' + 16 hex digits (e.g. n0000000000000001)
	//   - project:  'p.' + tsconfig path (e.g. p./tmp/.../tsconfig.json)
	//
	// The createHandle("p", id) shape from upstream proto.go is NOT used
	// for projects — ProjectHandle (proto.go:39) builds "p.<path>". Either
	// shape is accepted here so the test survives an upstream change, but
	// at least one prefix must match.
	expectedPathPrefix := "p." + configPath
	if !(strings.HasPrefix(project, expectedPathPrefix) || strings.HasPrefix(project, "p.") || strings.HasPrefix(project, "p0")) {
		t.Errorf("project handle = %q; expected to start with %q (path-prefixed) or p<hex>", project, expectedPathPrefix)
	}
	if !strings.HasPrefix(snap, "n") {
		t.Errorf("snapshot handle = %q; expected to start with 'n' per upstream createHandle", snap)
	}

	// Real downstream pipeline: at the byte offset of "greeting" we expect
	// a non-empty symbol handle, then a non-empty type handle, then a
	// non-empty display name from typeToString.
	offset := uint32(strings.Index(srcBody, "greeting"))
	if offset == ^uint32(0) {
		t.Fatal("could not find 'greeting' in fixture source")
	}

	sym, err := c.GetSymbolAtOffset(project, srcPath, offset)
	if err != nil {
		t.Fatalf("GetSymbolAtOffset: %v (this is the bug PR #84 must fix; do not silence)", err)
	}
	if sym.Handle == "" {
		t.Fatalf("GetSymbolAtOffset returned empty handle: %+v", sym)
	}
	if sym.Name != "greeting" {
		t.Errorf("symbol name = %q, want %q", sym.Name, "greeting")
	}

	typeInfo, err := c.GetTypeOfSymbol(project, sym.Handle)
	if err != nil {
		t.Fatalf("GetTypeOfSymbol: %v", err)
	}
	if typeInfo.Handle == "" {
		t.Fatalf("GetTypeOfSymbol returned empty handle: %+v", typeInfo)
	}

	display, err := c.TypeToString(project, typeInfo.Handle)
	if err != nil {
		t.Fatalf("TypeToString: %v", err)
	}
	if display == "" {
		t.Fatal("TypeToString returned empty display name")
	}
	if display != "string" {
		t.Errorf("type display = %q, want %q for `const greeting: string`", display, "string")
	}
	t.Logf("end-to-end OK: symbol=%s type=%s display=%q", sym.Handle, typeInfo.Handle, display)
}

// TestEnricherLiveTSGo exercises NewEnricherWithConfig + EnrichFile end-to-end
// against a real tsgo binary. This is the regression guard for the v2 review
// finding: NewEnricherWithConfig → EnrichFile previously returned zero facts
// with no error against the live binary.
func TestEnricherLiveTSGo(t *testing.T) {
	tsgoPath := os.Getenv("TSGO_PATH")
	if tsgoPath == "" {
		t.Skip("TSGO_PATH not set; skipping live enricher test")
	}
	if _, err := os.Stat(tsgoPath); err != nil {
		t.Skipf("TSGO_PATH=%q not accessible: %v", tsgoPath, err)
	}

	dir := t.TempDir()
	configPath := filepath.Join(dir, "tsconfig.json")
	if err := os.WriteFile(configPath, []byte(`{
  "compilerOptions": {"target":"es2020","module":"commonjs","strict":true,"noEmit":true},
  "include":["src/**/*.ts"]
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	srcPath := filepath.Join(dir, "src", "index.ts")
	if err := os.WriteFile(srcPath, []byte("const greeting: string = \"hello\";\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	client, err := NewClient(tsgoPath, dir)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	enricher, err := NewEnricherWithConfig(client, dir, configPath)
	if err != nil {
		t.Fatalf("NewEnricherWithConfig: %v", err)
	}
	defer enricher.Close()
	enricher.RegisterFiles([]string{srcPath})

	// Position of `greeting` identifier on line 1, col 6 (0-based byte col).
	facts, stats, err := enricher.EnrichFile(srcPath, []Position{{Line: 1, Col: 6}})
	if err != nil {
		t.Fatalf("EnrichFile: %v (stats=%+v)", err, stats)
	}
	if len(facts) == 0 {
		t.Fatalf("EnrichFile returned 0 facts against live tsgo (this is the v2 BLOCKER); stats=%+v", stats)
	}
	got := facts[0]
	if got.TypeHandle == "" {
		t.Errorf("fact TypeHandle is empty: %+v", got)
	}
	if got.TypeDisplay != "string" {
		t.Errorf("fact TypeDisplay = %q, want %q", got.TypeDisplay, "string")
	}
	t.Logf("live enricher OK: %+v stats=%+v", got, stats)
}
