package typecheck

import (
	"testing"
)

func TestEnricherEnrichFile(t *testing.T) {
	c := newMockClient(t, func(req jsonrpcRequest) interface{} {
		switch req.Method {
		case "initialize":
			return &InitializeResponse{
				UseCaseSensitiveFileNames: true,
				CurrentDirectory:          "/project",
			}
		case "getDefaultProjectForFile":
			return map[string]string{"project": "p.test"}
		case "getSymbolAtPosition":
			return &SymbolInfo{Handle: "s00001", Name: "x", Flags: 0}
		case "getTypeOfSymbol":
			return &TypeInfo{Handle: "t00001", DisplayName: "number", Flags: 0}
		default:
			return &jsonrpcError{Code: -32601, Message: "Method not found"}
		}
	})

	enricher, err := NewEnricher(c, "/project")
	if err != nil {
		t.Fatalf("NewEnricher: %v", err)
	}

	positions := []Position{
		{Line: 1, Col: 4},
		{Line: 3, Col: 6},
	}

	facts, err := enricher.EnrichFile("/project/src/index.ts", positions)
	if err != nil {
		t.Fatalf("EnrichFile: %v", err)
	}

	if len(facts) != 2 {
		t.Fatalf("len(facts) = %d, want 2", len(facts))
	}
	if facts[0].TypeDisplay != "number" {
		t.Errorf("facts[0].TypeDisplay = %q, want %q", facts[0].TypeDisplay, "number")
	}
	if facts[0].TypeHandle != "t00001" {
		t.Errorf("facts[0].TypeHandle = %q, want %q", facts[0].TypeHandle, "t00001")
	}
	if facts[0].Line != 1 {
		t.Errorf("facts[0].Line = %d, want 1", facts[0].Line)
	}
	if facts[1].Line != 3 {
		t.Errorf("facts[1].Line = %d, want 3", facts[1].Line)
	}
}

func TestEnricherHandlesSymbolError(t *testing.T) {
	c := newMockClient(t, func(req jsonrpcRequest) interface{} {
		switch req.Method {
		case "initialize":
			return &InitializeResponse{
				UseCaseSensitiveFileNames: true,
				CurrentDirectory:          "/project",
			}
		case "getDefaultProjectForFile":
			return map[string]string{"project": "p.test"}
		case "getSymbolAtPosition":
			return &jsonrpcError{Code: -32000, Message: "No symbol at position"}
		default:
			return &jsonrpcError{Code: -32601, Message: "Method not found"}
		}
	})

	enricher, err := NewEnricher(c, "/project")
	if err != nil {
		t.Fatalf("NewEnricher: %v", err)
	}

	positions := []Position{
		{Line: 1, Col: 4},
	}

	facts, err := enricher.EnrichFile("/project/src/index.ts", positions)
	if err != nil {
		t.Fatalf("EnrichFile: %v", err)
	}

	// Should gracefully return empty when tsgo returns errors
	if len(facts) != 0 {
		t.Errorf("len(facts) = %d, want 0 (graceful degradation)", len(facts))
	}
}

func TestEnricherHandlesTypeError(t *testing.T) {
	c := newMockClient(t, func(req jsonrpcRequest) interface{} {
		switch req.Method {
		case "initialize":
			return &InitializeResponse{
				UseCaseSensitiveFileNames: true,
				CurrentDirectory:          "/project",
			}
		case "getDefaultProjectForFile":
			return map[string]string{"project": "p.test"}
		case "getSymbolAtPosition":
			return &SymbolInfo{Handle: "s00001", Name: "x", Flags: 0}
		case "getTypeOfSymbol":
			return &jsonrpcError{Code: -32000, Message: "Type resolution failed"}
		default:
			return &jsonrpcError{Code: -32601, Message: "Method not found"}
		}
	})

	enricher, err := NewEnricher(c, "/project")
	if err != nil {
		t.Fatalf("NewEnricher: %v", err)
	}

	positions := []Position{
		{Line: 1, Col: 4},
	}

	facts, err := enricher.EnrichFile("/project/src/index.ts", positions)
	if err != nil {
		t.Fatalf("EnrichFile: %v", err)
	}

	if len(facts) != 0 {
		t.Errorf("len(facts) = %d, want 0 (graceful degradation)", len(facts))
	}
}

func TestEnricherEmptySymbolHandle(t *testing.T) {
	c := newMockClient(t, func(req jsonrpcRequest) interface{} {
		switch req.Method {
		case "initialize":
			return &InitializeResponse{
				UseCaseSensitiveFileNames: true,
				CurrentDirectory:          "/project",
			}
		case "getDefaultProjectForFile":
			return map[string]string{"project": "p.test"}
		case "getSymbolAtPosition":
			return &SymbolInfo{Handle: "", Name: "", Flags: 0}
		default:
			return &jsonrpcError{Code: -32601, Message: "Method not found"}
		}
	})

	enricher, err := NewEnricher(c, "/project")
	if err != nil {
		t.Fatalf("NewEnricher: %v", err)
	}

	positions := []Position{
		{Line: 1, Col: 4},
	}

	facts, err := enricher.EnrichFile("/project/src/index.ts", positions)
	if err != nil {
		t.Fatalf("EnrichFile: %v", err)
	}

	if len(facts) != 0 {
		t.Errorf("len(facts) = %d, want 0 (empty handle should be skipped)", len(facts))
	}
}

func TestEnricherInitializeError(t *testing.T) {
	c := newMockClient(t, func(req jsonrpcRequest) interface{} {
		return &jsonrpcError{Code: -32000, Message: "Init failed"}
	})

	_, err := NewEnricher(c, "/project")
	if err == nil {
		t.Fatal("expected error from NewEnricher when initialize fails, got nil")
	}
}

func TestEnricherProjectError(t *testing.T) {
	c := newMockClient(t, func(req jsonrpcRequest) interface{} {
		switch req.Method {
		case "initialize":
			return &InitializeResponse{
				UseCaseSensitiveFileNames: true,
				CurrentDirectory:          "/project",
			}
		case "getDefaultProjectForFile":
			return &jsonrpcError{Code: -32000, Message: "No project found"}
		default:
			return &jsonrpcError{Code: -32601, Message: "Method not found"}
		}
	})

	enricher, err := NewEnricher(c, "/project")
	if err != nil {
		t.Fatalf("NewEnricher: %v", err)
	}

	positions := []Position{{Line: 1, Col: 4}}

	_, err = enricher.EnrichFile("/project/src/index.ts", positions)
	if err == nil {
		t.Fatal("expected error when project resolution fails, got nil")
	}
}

func TestEnricherNoPositions(t *testing.T) {
	c := newMockClient(t, func(req jsonrpcRequest) interface{} {
		switch req.Method {
		case "initialize":
			return &InitializeResponse{
				UseCaseSensitiveFileNames: true,
				CurrentDirectory:          "/project",
			}
		case "getDefaultProjectForFile":
			return map[string]string{"project": "p.test"}
		default:
			return &jsonrpcError{Code: -32601, Message: "Method not found"}
		}
	})

	enricher, err := NewEnricher(c, "/project")
	if err != nil {
		t.Fatalf("NewEnricher: %v", err)
	}

	facts, err := enricher.EnrichFile("/project/src/index.ts", nil)
	if err != nil {
		t.Fatalf("EnrichFile: %v", err)
	}
	if len(facts) != 0 {
		t.Errorf("len(facts) = %d, want 0", len(facts))
	}
}
