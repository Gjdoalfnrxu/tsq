package typecheck

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// writeFakeSource creates a fake file with `numLines` lines of `lineLen` 'x'
// characters terminated by '\n'. Returns the path. Useful when the line/col
// → byte-offset translation is the only thing the test actually needs.
func writeFakeSource(t *testing.T, name string, numLines int) string {
	t.Helper()
	dir := t.TempDir()
	const lineLen = 80
	body := make([]byte, 0, (lineLen+1)*numLines)
	for i := 0; i < numLines; i++ {
		body = append(body, bytes.Repeat([]byte{'x'}, lineLen)...)
		body = append(body, '\n')
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, body, 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

func TestEnricherEnrichFile(t *testing.T) {
	c := newMockClient(t, func(req jsonrpcRequest) interface{} {
		switch req.Method {
		case "initialize":
			return &InitializeResponse{UseCaseSensitiveFileNames: true, CurrentDirectory: "/project"}
		case "updateSnapshot":
			return map[string]interface{}{
				"snapshot": "n0000000000000001",
				"projects": []map[string]interface{}{{"id": "p.test", "configFileName": "/project/tsconfig.json"}},
			}
		case "getSymbolAtPosition":
			return &SymbolInfo{Handle: "s00001", Name: "x", Flags: 0}
		case "getTypeOfSymbol":
			return &TypeInfo{Handle: "t00001", DisplayName: "number", Flags: 0}
		case "typeToString":
			return "number"
		default:
			return &jsonrpcError{Code: -32601, Message: "Method not found"}
		}
	})

	enricher, err := NewEnricherWithConfig(c, "/project", "/project/tsconfig.json")
	if err != nil {
		t.Fatalf("NewEnricher: %v", err)
	}
	src := writeFakeSource(t, "index.ts", 10)

	positions := []Position{
		{Line: 1, Col: 4},
		{Line: 3, Col: 6},
	}

	facts, stats, err := enricher.EnrichFile(src, positions)
	if err != nil {
		t.Fatalf("EnrichFile: %v", err)
	}

	if len(facts) != 2 {
		t.Fatalf("len(facts) = %d, want 2; stats=%+v", len(facts), stats)
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
	if stats.SymbolQueries != 2 || stats.FactsEmitted != 2 {
		t.Errorf("stats = %+v, want 2 queries / 2 facts", stats)
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
		case "updateSnapshot":
			return map[string]interface{}{
				"snapshot": "s.0",
				"projects": []map[string]interface{}{{"id": "p.test", "configFileName": "/project/tsconfig.json"}},
			}
		case "getDefaultProjectForFile":
			return map[string]string{"id": "p.test", "configFileName": "/project/tsconfig.json"}
		case "getSymbolAtPosition":
			return &jsonrpcError{Code: -32000, Message: "No symbol at position"}
		default:
			return &jsonrpcError{Code: -32601, Message: "Method not found"}
		}
	})

	enricher, err := NewEnricherWithConfig(c, "/project", "/project/tsconfig.json")
	if err != nil {
		t.Fatalf("NewEnricher: %v", err)
	}
	src := writeFakeSource(t, "index.ts", 10)

	facts, stats, err := enricher.EnrichFile(src, []Position{{Line: 1, Col: 4}})
	if err != nil {
		t.Fatalf("EnrichFile: %v", err)
	}
	if len(facts) != 0 {
		t.Errorf("len(facts) = %d, want 0", len(facts))
	}
	if stats.SymbolErrors == 0 {
		t.Errorf("expected SymbolErrors > 0, got stats=%+v", stats)
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
		case "updateSnapshot":
			return map[string]interface{}{
				"snapshot": "s.0",
				"projects": []map[string]interface{}{{"id": "p.test", "configFileName": "/project/tsconfig.json"}},
			}
		case "getDefaultProjectForFile":
			return map[string]string{"id": "p.test", "configFileName": "/project/tsconfig.json"}
		case "getSymbolAtPosition":
			return &SymbolInfo{Handle: "s00001", Name: "x", Flags: 0}
		case "getTypeOfSymbol":
			return &jsonrpcError{Code: -32000, Message: "Type resolution failed"}
		default:
			return &jsonrpcError{Code: -32601, Message: "Method not found"}
		}
	})

	enricher, err := NewEnricherWithConfig(c, "/project", "/project/tsconfig.json")
	if err != nil {
		t.Fatalf("NewEnricher: %v", err)
	}
	src := writeFakeSource(t, "index.ts", 10)

	facts, stats, err := enricher.EnrichFile(src, []Position{{Line: 1, Col: 4}})
	if err != nil {
		t.Fatalf("EnrichFile: %v", err)
	}
	if len(facts) != 0 {
		t.Errorf("len(facts) = %d, want 0", len(facts))
	}
	if stats.TypeErrors == 0 {
		t.Errorf("expected TypeErrors > 0, got stats=%+v", stats)
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
		case "updateSnapshot":
			return map[string]interface{}{
				"snapshot": "s.0",
				"projects": []map[string]interface{}{{"id": "p.test", "configFileName": "/project/tsconfig.json"}},
			}
		case "getDefaultProjectForFile":
			return map[string]string{"id": "p.test", "configFileName": "/project/tsconfig.json"}
		case "getSymbolAtPosition":
			return &SymbolInfo{Handle: "", Name: "", Flags: 0}
		default:
			return &jsonrpcError{Code: -32601, Message: "Method not found"}
		}
	})

	enricher, err := NewEnricherWithConfig(c, "/project", "/project/tsconfig.json")
	if err != nil {
		t.Fatalf("NewEnricher: %v", err)
	}
	src := writeFakeSource(t, "index.ts", 10)

	facts, stats, err := enricher.EnrichFile(src, []Position{{Line: 1, Col: 4}})
	if err != nil {
		t.Fatalf("EnrichFile: %v", err)
	}
	if len(facts) != 0 {
		t.Errorf("len(facts) = %d, want 0 (empty handle should be skipped)", len(facts))
	}
	if stats.SymbolEmpty == 0 {
		t.Errorf("expected SymbolEmpty > 0, got stats=%+v", stats)
	}
}

func TestEnricherInitializeError(t *testing.T) {
	c := newMockClient(t, func(req jsonrpcRequest) interface{} {
		return &jsonrpcError{Code: -32000, Message: "Init failed"}
	})

	_, err := NewEnricherWithConfig(c, "/project", "/project/tsconfig.json")
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
		case "updateSnapshot":
			// Simulate tsgo failing to load the project (e.g. malformed tsconfig).
			return &jsonrpcError{Code: -32000, Message: "No project found"}
		default:
			return &jsonrpcError{Code: -32601, Message: "Method not found"}
		}
	})

	enricher, err := NewEnricherWithConfig(c, "/project", "/project/tsconfig.json")
	if err != nil {
		t.Fatalf("NewEnricher: %v", err)
	}

	positions := []Position{{Line: 1, Col: 4}}
	src := writeFakeSource(t, "index.ts", 10)

	_, _, err = enricher.EnrichFile(src, positions)
	if err == nil {
		t.Fatal("expected error when project resolution fails, got nil")
	}
}

// ───────────────────────────────────────────────────────────
// Feature-specific enricher tests using typed TS fixtures
// (plan 12). Each test simulates what tsgo would return for
// key positions in the corresponding testdata/ts/typed/ file.
// ───────────────────────────────────────────────────────────

// posKey encodes a position for mock dispatch.
func posKey(line, col int) string {
	return fmt.Sprintf("%d:%d", line, col)
}

// fixtureEnricher builds a mock enricher backed by a fake source file (so that
// the line/col → byte-offset translation produces deterministic offsets) and a
// mock tsgo server that dispatches by (line, col) via a reverse lookup of the
// requested byte offset.
//
// symbolMap maps "line:col" to a SymbolInfo; typeMap maps symbol handle to a
// TypeInfo whose DisplayName is also returned by typeToString.
func fixtureEnricher(t *testing.T, fixturePath string, symbolMap map[string]*SymbolInfo, typeMap map[string]*TypeInfo) (*Enricher, string) {
	t.Helper()

	// Lay down a synthetic source file with a known shape so byte offsets
	// are deterministic. Each line is 80 bytes of 'x' + newline; that gives
	// us a generous range for any (line, col) pair the tests use.
	dir := t.TempDir()
	srcPath := filepath.Join(dir, filepath.Base(fixturePath))
	const lineLen = 80
	const numLines = 200
	body := make([]byte, 0, (lineLen+1)*numLines)
	for i := 0; i < numLines; i++ {
		body = append(body, bytes.Repeat([]byte{'x'}, lineLen)...)
		body = append(body, '\n')
	}
	if err := os.WriteFile(srcPath, body, 0o644); err != nil {
		t.Fatalf("write fixture source: %v", err)
	}

	offsetForKey := func(key string) (uint32, bool) {
		var line, col int
		if _, err := fmt.Sscanf(key, "%d:%d", &line, &col); err != nil {
			return 0, false
		}
		return uint32((line-1)*(lineLen+1) + col), true
	}
	keyForOffset := func(off uint32) string {
		// best-effort reverse map; tolerate offsets that don't land on
		// our row grid by returning the closest cell.
		row := int(off) / (lineLen + 1)
		col := int(off) % (lineLen + 1)
		return posKey(row+1, col)
	}
	// Validate every symbolMap key has a corresponding offset in our grid.
	for k := range symbolMap {
		if off, ok := offsetForKey(k); !ok {
			t.Fatalf("fixtureEnricher: bad posKey %q", k)
		} else if got := keyForOffset(off); got != k {
			t.Fatalf("fixtureEnricher: round-trip mismatch %q -> %d -> %q", k, off, got)
		}
	}

	c := newMockClient(t, func(req jsonrpcRequest) interface{} {
		switch req.Method {
		case "initialize":
			return &InitializeResponse{
				UseCaseSensitiveFileNames: true,
				CurrentDirectory:          "/project",
			}
		case "updateSnapshot":
			return map[string]interface{}{
				"snapshot": "n0000000000000001",
				"projects": []map[string]interface{}{{"id": "p." + srcPath, "configFileName": "/project/tsconfig.json"}},
			}
		case "getDefaultProjectForFile":
			return map[string]string{"id": "p.fixture", "configFileName": "/project/tsconfig.json"}
		case "getSymbolAtPosition":
			params, _ := json.Marshal(req.Params)
			var p struct {
				Position uint32 `json:"position"`
			}
			json.Unmarshal(params, &p)
			key := keyForOffset(p.Position)
			if sym, ok := symbolMap[key]; ok {
				return sym
			}
			return &jsonrpcError{Code: -32000, Message: "No symbol at position"}
		case "getTypeOfSymbol":
			params, _ := json.Marshal(req.Params)
			var p struct {
				Symbol string `json:"symbol"`
			}
			json.Unmarshal(params, &p)
			if ti, ok := typeMap[p.Symbol]; ok {
				return ti
			}
			return &jsonrpcError{Code: -32000, Message: "No type for symbol"}
		case "typeToString":
			params, _ := json.Marshal(req.Params)
			var p struct {
				Type string `json:"type"`
			}
			json.Unmarshal(params, &p)
			for _, ti := range typeMap {
				if ti.Handle == p.Type {
					return ti.DisplayName
				}
			}
			return ""
		default:
			return &jsonrpcError{Code: -32601, Message: "Method not found"}
		}
	})

	enricher, err := NewEnricherWithConfig(c, dir, "/project/tsconfig.json")
	if err != nil {
		t.Fatalf("fixtureEnricher: %v", err)
	}
	t.Cleanup(func() { enricher.Close() })
	return enricher, srcPath
}

// TestEnricher_Generics exercises the enricher against generics.ts.
// Key positions: identity call result (line 23), longest call result (line 24),
// Box.map chain result (line 25).
func TestEnricher_Generics(t *testing.T) {
	symbolMap := map[string]*SymbolInfo{
		posKey(23, 6): {Handle: "s_num", Name: "num", Flags: 0},
		posKey(24, 6): {Handle: "s_str", Name: "str", Flags: 0},
		posKey(25, 6): {Handle: "s_box", Name: "box", Flags: 0},
		posKey(7, 9):  {Handle: "s_identity", Name: "identity", Flags: 0},
		posKey(11, 9): {Handle: "s_longest", Name: "longest", Flags: 0},
		posKey(15, 6): {Handle: "s_Box", Name: "Box", Flags: 0},
	}
	typeMap := map[string]*TypeInfo{
		"s_num":      {Handle: "t_number", DisplayName: "number", Flags: 0},
		"s_str":      {Handle: "t_string", DisplayName: "string", Flags: 0},
		"s_box":      {Handle: "t_box_string", DisplayName: "Box<string>", Flags: 0},
		"s_identity": {Handle: "t_identity", DisplayName: "<T>(value: T) => T", Flags: 0},
		"s_longest":  {Handle: "t_longest", DisplayName: "<T extends HasLength>(a: T, b: T) => T", Flags: 0},
		"s_Box":      {Handle: "t_Box", DisplayName: "typeof Box", Flags: 0},
	}

	enricher, src := fixtureEnricher(t, "generics.ts", symbolMap, typeMap)

	facts, stats, err := enricher.EnrichFile(src, []Position{
		{Line: 23, Col: 6},
		{Line: 24, Col: 6},
		{Line: 25, Col: 6},
	})
	if err != nil {
		t.Fatalf("EnrichFile: %v", err)
	}
	if len(facts) == 0 {
		t.Fatalf("enricher returned no facts despite full mock coverage; enricher regressed (stats=%+v)", stats)
	}

	want := map[int]string{
		23: "number",
		24: "string",
		25: "Box<string>",
	}
	for _, f := range facts {
		if expected, ok := want[f.Line]; ok {
			if f.TypeDisplay != expected {
				t.Errorf("line %d: TypeDisplay = %q, want %q", f.Line, f.TypeDisplay, expected)
			}
		}
	}

	sigFacts, _, err := enricher.EnrichFile(src, []Position{
		{Line: 7, Col: 9},
		{Line: 11, Col: 9},
	})
	if err != nil {
		t.Fatalf("EnrichFile (signatures): %v", err)
	}
	if len(sigFacts) == 0 {
		t.Fatalf("enricher returned no signature facts despite full mock coverage; enricher regressed")
	}
	for _, f := range sigFacts {
		if f.TypeDisplay == "" {
			t.Errorf("line %d: expected non-empty TypeDisplay for generic function", f.Line)
		}
	}
}

// TestEnricher_Conditional exercises the enricher against conditional.ts.
// Key positions: type alias resolutions and const variables.
func TestEnricher_Conditional(t *testing.T) {
	symbolMap := map[string]*SymbolInfo{
		posKey(17, 6): {Handle: "s_check", Name: "check", Flags: 0},
		posKey(18, 6): {Handle: "s_elem", Name: "elem", Flags: 0},
	}
	typeMap := map[string]*TypeInfo{
		"s_check": {Handle: "t_true", DisplayName: "true", Flags: 0},
		"s_elem":  {Handle: "t_number", DisplayName: "number", Flags: 0},
	}

	enricher, src := fixtureEnricher(t, "conditional.ts", symbolMap, typeMap)

	facts, stats, err := enricher.EnrichFile(src, []Position{
		{Line: 17, Col: 6},
		{Line: 18, Col: 6},
	})
	if err != nil {
		t.Fatalf("EnrichFile: %v", err)
	}
	if len(facts) == 0 {
		t.Fatalf("enricher returned no facts despite full mock coverage; enricher regressed (stats=%+v)", stats)
	}

	want := map[int]string{
		17: "true",
		18: "number",
	}
	for _, f := range facts {
		if expected, ok := want[f.Line]; ok {
			if f.TypeDisplay != expected {
				t.Errorf("line %d: TypeDisplay = %q, want %q", f.Line, f.TypeDisplay, expected)
			}
		}
	}

	// Type alias declarations (lines 3,5) are type-level only; the mock
	// has no symbol entries for them so EnrichFile correctly returns nothing
	// and that is the expected behaviour, not a regression.
	typeAliasFacts, aliasStats, err := enricher.EnrichFile(src, []Position{
		{Line: 3, Col: 5},
		{Line: 5, Col: 5},
	})
	if err != nil {
		t.Fatalf("EnrichFile (type aliases): %v", err)
	}
	if len(typeAliasFacts) != 0 {
		t.Errorf("expected zero facts for type-only positions, got %d (stats=%+v)", len(typeAliasFacts), aliasStats)
	}
	if aliasStats.SymbolErrors == 0 {
		t.Errorf("expected SymbolErrors > 0 for type-only positions, got %+v", aliasStats)
	}
}

// TestEnricher_Mapped exercises the enricher against mapped.ts.
// Key positions: frozen (line 19), partial (line 20), nullable (line 21).
func TestEnricher_Mapped(t *testing.T) {
	symbolMap := map[string]*SymbolInfo{
		posKey(19, 6): {Handle: "s_frozen", Name: "frozen", Flags: 0},
		posKey(20, 6): {Handle: "s_partial", Name: "partial", Flags: 0},
		posKey(21, 6): {Handle: "s_nullable", Name: "nullable", Flags: 0},
	}
	typeMap := map[string]*TypeInfo{
		"s_frozen":   {Handle: "t_readonly_user", DisplayName: "ReadonlyAll<User>", Flags: 0},
		"s_partial":  {Handle: "t_optional_user", DisplayName: "Optional<User>", Flags: 0},
		"s_nullable": {Handle: "t_nullable_user", DisplayName: "Nullable<User>", Flags: 0},
	}

	enricher, src := fixtureEnricher(t, "mapped.ts", symbolMap, typeMap)

	facts, stats, err := enricher.EnrichFile(src, []Position{
		{Line: 19, Col: 6},
		{Line: 20, Col: 6},
		{Line: 21, Col: 6},
	})
	if err != nil {
		t.Fatalf("EnrichFile: %v", err)
	}
	if len(facts) == 0 {
		t.Fatalf("enricher returned no facts despite full mock coverage; enricher regressed (stats=%+v)", stats)
	}
	if len(facts) != 3 {
		t.Errorf("len(facts) = %d, want 3", len(facts))
	}

	want := map[int]string{
		19: "ReadonlyAll<User>",
		20: "Optional<User>",
		21: "Nullable<User>",
	}
	for _, f := range facts {
		if expected, ok := want[f.Line]; ok {
			if f.TypeDisplay != expected {
				t.Errorf("line %d: TypeDisplay = %q, want %q", f.Line, f.TypeDisplay, expected)
			}
		}
	}
}

// TestEnricher_UnionIntersection exercises the enricher against union_intersection.ts.
// Key positions: area function (line 16), entity (line 29), a (line 30).
func TestEnricher_UnionIntersection(t *testing.T) {
	symbolMap := map[string]*SymbolInfo{
		posKey(16, 9): {Handle: "s_area", Name: "area", Flags: 0},
		posKey(29, 6): {Handle: "s_entity", Name: "entity", Flags: 0},
		posKey(30, 6): {Handle: "s_a", Name: "a", Flags: 0},
	}
	typeMap := map[string]*TypeInfo{
		"s_area":   {Handle: "t_area_fn", DisplayName: "(shape: Shape) => number", Flags: 0},
		"s_entity": {Handle: "t_entity", DisplayName: "Entity", Flags: 0},
		"s_a":      {Handle: "t_number", DisplayName: "number", Flags: 0},
	}

	enricher, src := fixtureEnricher(t, "union_intersection.ts", symbolMap, typeMap)

	facts, stats, err := enricher.EnrichFile(src, []Position{
		{Line: 16, Col: 9},
		{Line: 29, Col: 6},
		{Line: 30, Col: 6},
	})
	if err != nil {
		t.Fatalf("EnrichFile: %v", err)
	}
	if len(facts) == 0 {
		t.Fatalf("enricher returned no facts despite full mock coverage; enricher regressed (stats=%+v)", stats)
	}

	// Verify the function taking a union type parameter is resolved
	found := false
	for _, f := range facts {
		if f.Line == 16 && f.TypeDisplay == "(shape: Shape) => number" {
			found = true
		}
	}
	if !found {
		t.Error("expected TypeFact for area function with union parameter type")
	}

	// Verify intersection type variable
	for _, f := range facts {
		if f.Line == 29 {
			if f.TypeDisplay != "Entity" {
				t.Errorf("line 29: TypeDisplay = %q, want %q", f.TypeDisplay, "Entity")
			}
		}
	}
}

// TestEnricher_LiteralTypes exercises the enricher against literal_types.ts.
// Key positions: move function (line 9), handleStatus (line 11), config (line 20), status (line 23).
func TestEnricher_LiteralTypes(t *testing.T) {
	symbolMap := map[string]*SymbolInfo{
		posKey(9, 9):  {Handle: "s_move", Name: "move", Flags: 0},
		posKey(11, 9): {Handle: "s_handleStatus", Name: "handleStatus", Flags: 0},
		posKey(20, 6): {Handle: "s_config", Name: "config", Flags: 0},
		posKey(23, 6): {Handle: "s_status", Name: "status", Flags: 0},
	}
	typeMap := map[string]*TypeInfo{
		"s_move":         {Handle: "t_move_fn", DisplayName: "(dir: Direction) => void", Flags: 0},
		"s_handleStatus": {Handle: "t_handle_fn", DisplayName: "(code: HttpStatus) => string", Flags: 0},
		"s_config":       {Handle: "t_config", DisplayName: "{ readonly endpoint: \"/api\"; readonly retries: 3; }", Flags: 0},
		"s_status":       {Handle: "t_string", DisplayName: "string", Flags: 0},
	}

	enricher, src := fixtureEnricher(t, "literal_types.ts", symbolMap, typeMap)

	facts, stats, err := enricher.EnrichFile(src, []Position{
		{Line: 9, Col: 9},
		{Line: 11, Col: 9},
		{Line: 20, Col: 6},
		{Line: 23, Col: 6},
	})
	if err != nil {
		t.Fatalf("EnrichFile: %v", err)
	}
	if len(facts) == 0 {
		t.Fatalf("enricher returned no facts despite full mock coverage; enricher regressed (stats=%+v)", stats)
	}

	// Verify const assertion preserves literal types
	for _, f := range facts {
		if f.Line == 20 {
			if f.TypeDisplay == "" {
				t.Error("line 20 (config): expected non-empty type for const assertion")
			}
			// The const assertion should produce a readonly literal object type
			if f.TypeDisplay != "{ readonly endpoint: \"/api\"; readonly retries: 3; }" {
				t.Errorf("line 20: TypeDisplay = %q, want const assertion type", f.TypeDisplay)
			}
		}
	}

	// Verify function with literal union parameter
	for _, f := range facts {
		if f.Line == 9 && f.TypeDisplay != "(dir: Direction) => void" {
			t.Errorf("line 9: TypeDisplay = %q, want %q", f.TypeDisplay, "(dir: Direction) => void")
		}
	}

	if len(facts) != 4 {
		t.Errorf("len(facts) = %d, want 4", len(facts))
	}
}

// TestEnricherWithConfigUsesOpenProject verifies that when a tsconfig path is
// provided, the enricher resolves the project via updateSnapshot/openProject
// rather than getDefaultProjectForFile. This is the bug the --tsconfig
// plumbing was added to fix: without OpenProject the tsgo session has no
// loaded project and getDefaultProjectForFile silently returns nothing,
// killing every downstream type query.
func TestEnricherWithConfigUsesOpenProject(t *testing.T) {
	var sawOpenProject bool
	var sawDefaultProject bool
	var openProjectArg string
	c := newMockClient(t, func(req jsonrpcRequest) interface{} {
		switch req.Method {
		case "initialize":
			return &InitializeResponse{UseCaseSensitiveFileNames: true, CurrentDirectory: "/project"}
		case "updateSnapshot":
			sawOpenProject = true
			if params, ok := req.Params.(map[string]interface{}); ok {
				openProjectArg, _ = params["openProject"].(string)
			}
			return map[string]interface{}{
				"snapshot": "s.0",
				"projects": []map[string]interface{}{{"id": "p.fromconfig", "configFileName": "/project/tsconfig.json"}},
			}
		case "getDefaultProjectForFile":
			sawDefaultProject = true
			return map[string]string{"id": "p.fallback", "configFileName": "/project/tsconfig.json"}
		case "getSymbolAtPosition":
			return &SymbolInfo{Handle: "s1", Name: "x"}
		case "getTypeOfSymbol":
			return &TypeInfo{Handle: "t1", DisplayName: "number"}
		case "typeToString":
			return "number"
		default:
			return &jsonrpcError{Code: -32601, Message: "Method not found"}
		}
	})

	enricher, err := NewEnricherWithConfig(c, "/project", "/project/tsconfig.json")
	if err != nil {
		t.Fatalf("NewEnricherWithConfig: %v", err)
	}
	src := writeFakeSource(t, "index.ts", 10)

	facts, _, err := enricher.EnrichFile(src, []Position{{Line: 1, Col: 4}})
	if err != nil {
		t.Fatalf("EnrichFile: %v", err)
	}
	if !sawOpenProject {
		t.Error("expected updateSnapshot/openProject call, did not receive one")
	}
	if openProjectArg != "/project/tsconfig.json" {
		t.Errorf("openProject arg = %q, want /project/tsconfig.json", openProjectArg)
	}
	if sawDefaultProject {
		t.Error("did not expect getDefaultProjectForFile when tsconfig is provided")
	}
	if len(facts) != 1 || facts[0].TypeDisplay != "number" {
		t.Errorf("facts = %+v, want one number fact", facts)
	}
}

// TestEnricherWithoutConfigSurfacesDefaultProjectFailure documents the
// behaviour of the legacy NewEnricher (no tsconfig) path. With the corrected
// upstream wire format, getDefaultProjectForFile requires a snapshot to have
// been loaded first — without a tsconfig the client cannot produce one. The
// previous incarnation of this test pretended this path worked; in reality
// the bare NewEnricher constructor is broken against a real tsgo backend.
func TestEnricherWithoutConfigSurfacesDefaultProjectFailure(t *testing.T) {
	c := newMockClient(t, func(req jsonrpcRequest) interface{} {
		switch req.Method {
		case "initialize":
			return &InitializeResponse{UseCaseSensitiveFileNames: true, CurrentDirectory: "/project"}
		case "updateSnapshot":
			t.Error("legacy path should not call updateSnapshot")
			return &jsonrpcError{Code: -32601, Message: "unexpected"}
		case "getDefaultProjectForFile":
			t.Error("client must refuse to call getDefaultProjectForFile without a snapshot")
			return &jsonrpcError{Code: -32601, Message: "unexpected"}
		}
		return &jsonrpcError{Code: -32601, Message: "Method not found"}
	})

	enricher, err := NewEnricher(c, "/project") // no tsconfig
	if err != nil {
		t.Fatalf("NewEnricher: %v", err)
	}
	src := writeFakeSource(t, "index.ts", 10)
	_, _, err = enricher.EnrichFile(src, []Position{{Line: 1, Col: 4}})
	if err == nil {
		t.Fatal("expected error from legacy no-tsconfig path, got nil")
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
		case "updateSnapshot":
			return map[string]interface{}{
				"snapshot": "s.0",
				"projects": []map[string]interface{}{{"id": "p.test", "configFileName": "/project/tsconfig.json"}},
			}
		case "getDefaultProjectForFile":
			return map[string]string{"id": "p.test", "configFileName": "/project/tsconfig.json"}
		default:
			return &jsonrpcError{Code: -32601, Message: "Method not found"}
		}
	})

	enricher, err := NewEnricherWithConfig(c, "/project", "/project/tsconfig.json")
	if err != nil {
		t.Fatalf("NewEnricher: %v", err)
	}
	src := writeFakeSource(t, "index.ts", 10)

	facts, _, err := enricher.EnrichFile(src, nil)
	if err != nil {
		t.Fatalf("EnrichFile: %v", err)
	}
	if len(facts) != 0 {
		t.Errorf("len(facts) = %d, want 0", len(facts))
	}
}
