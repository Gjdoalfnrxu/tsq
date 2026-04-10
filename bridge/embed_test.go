package bridge

import (
	"testing"
)

// TestLoadBridgeReturnsAllFiles verifies LoadBridge returns all expected .qll files.
func TestLoadBridgeReturnsAllFiles(t *testing.T) {
	expected := []string{
		"tsq_base.qll",
		"tsq_functions.qll",
		"tsq_calls.qll",
		"tsq_variables.qll",
		"tsq_expressions.qll",
		"tsq_jsx.qll",
		"tsq_imports.qll",
		"tsq_errors.qll",
		"tsq_types.qll",
		"tsq_symbols.qll",
		"tsq_callgraph.qll",
		"tsq_dataflow.qll",
		"tsq_summaries.qll",
		"tsq_composition.qll",
		"tsq_taint.qll",
		"tsq_express.qll",
		"tsq_react.qll",
		"tsq_node.qll",
	}
	files := LoadBridge()
	if len(files) != len(expected) {
		t.Fatalf("expected %d files, got %d", len(expected), len(files))
	}
	for _, name := range expected {
		data, ok := files[name]
		if !ok {
			t.Errorf("missing bridge file: %q", name)
			continue
		}
		if len(data) == 0 {
			t.Errorf("bridge file %q is empty", name)
		}
	}
}

// TestLoadBridgeMatchesManifest verifies every .qll file referenced by the
// manifest is present in the embedded bridge.
func TestLoadBridgeMatchesManifest(t *testing.T) {
	m := V1Manifest()
	files := LoadBridge()

	// Collect unique file references from the manifest.
	needed := make(map[string]bool)
	for _, a := range m.Available {
		needed[a.File] = true
	}

	for filename := range needed {
		if _, ok := files[filename]; !ok {
			t.Errorf("manifest references %q but it is not in LoadBridge()", filename)
		}
	}
}

// TestLoadBridgeContentsAreUTF8 verifies bridge file contents are valid UTF-8.
func TestLoadBridgeContentsAreUTF8(t *testing.T) {
	files := LoadBridge()
	for name, data := range files {
		for i, b := range data {
			if b == 0 {
				t.Errorf("bridge file %q contains null byte at offset %d", name, i)
				break
			}
		}
	}
}

// TestBridgeImportLoaderKnownPaths verifies the import loader recognises bridge paths.
func TestBridgeImportLoaderKnownPaths(t *testing.T) {
	files := LoadBridge()
	stubParse := func(src, file string) interface{} {
		return src // return something non-nil so we can verify the loader calls parseFn
	}
	loader := BridgeImportLoader(files, stubParse)

	knownPaths := []string{
		"tsq::base",
		"tsq::functions",
		"tsq::calls",
		"tsq::variables",
		"tsq::expressions",
		"tsq::jsx",
		"tsq::imports",
		"tsq::errors",
		"tsq::types",
		"tsq::symbols",
	}
	for _, path := range knownPaths {
		result, ok := loader(path)
		if !ok {
			t.Errorf("BridgeImportLoader did not recognise path %q", path)
		}
		if result == nil {
			t.Errorf("BridgeImportLoader returned nil for known path %q", path)
		}
	}
}

// TestBridgeImportLoaderUnknownPaths verifies the import loader rejects unknown paths.
func TestBridgeImportLoaderUnknownPaths(t *testing.T) {
	files := LoadBridge()
	stubParse := func(src, file string) interface{} { return src }
	loader := BridgeImportLoader(files, stubParse)

	unknownPaths := []string{
		"javascript",
		"DataFlow::PathGraph",
		"",
	}
	for _, path := range unknownPaths {
		_, ok := loader(path)
		if ok {
			t.Errorf("BridgeImportLoader should not recognise path %q", path)
		}
	}
}
