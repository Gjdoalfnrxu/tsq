package typecheck

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestJSONRPCRequestSerialization(t *testing.T) {
	req := jsonrpcRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
		Params:  map[string]interface{}{},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded["jsonrpc"] != "2.0" {
		t.Errorf("jsonrpc = %q, want %q", decoded["jsonrpc"], "2.0")
	}
	if decoded["method"] != "initialize" {
		t.Errorf("method = %q, want %q", decoded["method"], "initialize")
	}
	if id, ok := decoded["id"].(float64); !ok || id != 1 {
		t.Errorf("id = %v, want 1", decoded["id"])
	}
}

func TestJSONRPCRequestWithParams(t *testing.T) {
	req := jsonrpcRequest{
		JSONRPC: "2.0",
		ID:      42,
		Method:  "getTypeAtPosition",
		Params: map[string]interface{}{
			"project":  "p.123",
			"file":     "/src/index.ts",
			"position": map[string]int{"line": 10, "character": 5},
		},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded jsonrpcRequest
	decoded.Params = map[string]interface{}{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Method != "getTypeAtPosition" {
		t.Errorf("method = %q, want %q", decoded.Method, "getTypeAtPosition")
	}
	if decoded.ID != 42 {
		t.Errorf("id = %d, want 42", decoded.ID)
	}
}

func TestResponseParsingSuccess(t *testing.T) {
	// Upstream TypeResponse uses `id`, not `handle`. The displayName field is
	// not part of TypeResponse and must come from a separate typeToString
	// call, so it should NOT appear in the result.
	raw := `{"jsonrpc":"2.0","id":1,"result":{"id":"t00001","flags":0}}`
	var resp jsonrpcResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.ID != 1 {
		t.Errorf("id = %d, want 1", resp.ID)
	}
	if resp.Error != nil {
		t.Errorf("unexpected error: %v", resp.Error)
	}

	var info TypeInfo
	if err := json.Unmarshal(resp.Result, &info); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if info.Handle != "t00001" {
		t.Errorf("handle = %q, want %q", info.Handle, "t00001")
	}
	if info.DisplayName != "" {
		t.Errorf("displayName = %q, want empty (TypeResponse has no displayName field)", info.DisplayName)
	}
}

func TestResponseParsingError(t *testing.T) {
	raw := `{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"Method not found"}}`
	var resp jsonrpcResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.Error == nil {
		t.Fatal("expected error, got nil")
	}
	if resp.Error.Code != -32601 {
		t.Errorf("error code = %d, want -32601", resp.Error.Code)
	}
	if resp.Error.Message != "Method not found" {
		t.Errorf("error message = %q, want %q", resp.Error.Message, "Method not found")
	}
	if resp.Error.Error() != "Method not found" {
		t.Errorf("Error() = %q, want %q", resp.Error.Error(), "Method not found")
	}
}

func TestResponseParsingDiagnostics(t *testing.T) {
	raw := `[{"file":"/src/index.ts","line":10,"col":5,"message":"Type 'number' is not assignable to type 'string'."}]`
	var diags []Diagnostic
	if err := json.Unmarshal([]byte(raw), &diags); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(diags) != 1 {
		t.Fatalf("len(diags) = %d, want 1", len(diags))
	}
	if diags[0].Line != 10 {
		t.Errorf("line = %d, want 10", diags[0].Line)
	}
	if diags[0].Message != "Type 'number' is not assignable to type 'string'." {
		t.Errorf("message = %q", diags[0].Message)
	}
}

func TestNewClientBinaryNotFound(t *testing.T) {
	_, err := NewClient("/nonexistent/path/tsgo-definitely-not-here", "/tmp")
	if err == nil {
		t.Fatal("expected error for nonexistent binary, got nil")
	}
}

func TestDetectTsgoNoEnvNoPath(t *testing.T) {
	// With no TSGO_PATH and an empty PATH, DetectTsgo must return "".
	// (t.Setenv automatically restores the previous value on test exit.)
	t.Setenv("TSGO_PATH", "")
	t.Setenv("PATH", "")
	if got := DetectTsgo(); got != "" {
		t.Errorf("DetectTsgo() = %q, want \"\" when no env and no PATH", got)
	}
}

func TestDetectTsgoWithEnvVar(t *testing.T) {
	t.Setenv("TSGO_PATH", "/nonexistent/tsgo-binary")
	result := DetectTsgo()
	if result == "/nonexistent/tsgo-binary" {
		t.Error("should not return nonexistent path")
	}
}

func TestDetectTsgoWithValidEnvVar(t *testing.T) {
	goPath, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go binary not in PATH")
	}
	t.Setenv("TSGO_PATH", goPath)
	result := DetectTsgo()
	if result != goPath {
		t.Errorf("DetectTsgo() = %q, want %q", result, goPath)
	}
}

// parseFrame extracts a Content-Length framed message from a byte buffer.
func parseFrame(data []byte) (msg []byte, rest []byte, ok bool) {
	s := string(data)
	idx := indexOf(s, "\r\n\r\n")
	if idx < 0 {
		return nil, data, false
	}

	headers := s[:idx]
	contentLength := -1
	for _, line := range splitLines(headers) {
		if len(line) > 16 && line[:16] == "Content-Length: " {
			n := 0
			for _, ch := range line[16:] {
				if ch >= '0' && ch <= '9' {
					n = n*10 + int(ch-'0')
				}
			}
			contentLength = n
		}
	}

	if contentLength < 0 {
		return nil, data, false
	}

	bodyStart := idx + 4 // past \r\n\r\n
	if len(data) < bodyStart+contentLength {
		return nil, data, false
	}

	return data[bodyStart : bodyStart+contentLength], data[bodyStart+contentLength:], true
}

func indexOf(s, sep string) int {
	for i := 0; i <= len(s)-len(sep); i++ {
		if s[i:i+len(sep)] == sep {
			return i
		}
	}
	return -1
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			line := s[start:i]
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			lines = append(lines, line)
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

// newMockClient creates a Client connected to a mock server via pipes.
// The handler function receives each JSON-RPC request and returns either
// a result value (marshalled to JSON) or a *jsonrpcError for error responses.
func newMockClient(t *testing.T, handler func(req jsonrpcRequest) interface{}) *Client {
	t.Helper()

	clientReader, serverWriter := io.Pipe()
	serverReader, clientWriter := io.Pipe()

	go func() {
		defer serverWriter.Close()
		buf := make([]byte, 4096)
		var accum []byte
		for {
			n, err := serverReader.Read(buf)
			if err != nil {
				return
			}
			accum = append(accum, buf[:n]...)

			for {
				msg, rest, ok := parseFrame(accum)
				if !ok {
					break
				}
				accum = rest

				var req jsonrpcRequest
				if err := json.Unmarshal(msg, &req); err != nil {
					continue
				}

				result := handler(req)

				var resp jsonrpcResponse
				resp.JSONRPC = "2.0"
				resp.ID = req.ID

				if errResp, ok := result.(*jsonrpcError); ok {
					resp.Error = errResp
				} else {
					body, _ := json.Marshal(result)
					resp.Result = json.RawMessage(body)
				}

				respBody, _ := json.Marshal(resp)
				header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(respBody))
				serverWriter.Write([]byte(header))
				serverWriter.Write(respBody)
			}
		}
	}()

	c := &Client{
		cmd:    exec.Command("true"),
		stdin:  clientWriter,
		stdout: bufio.NewReader(clientReader),
	}

	t.Cleanup(func() {
		clientWriter.Close()
		serverReader.Close()
	})

	return c
}

func TestMockInitialize(t *testing.T) {
	c := newMockClient(t, func(req jsonrpcRequest) interface{} {
		if req.Method == "initialize" {
			return &InitializeResponse{
				UseCaseSensitiveFileNames: true,
				CurrentDirectory:          "/project",
			}
		}
		return &jsonrpcError{Code: -32601, Message: "Method not found"}
	})

	resp, err := c.Initialize()
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if !resp.UseCaseSensitiveFileNames {
		t.Error("expected UseCaseSensitiveFileNames=true")
	}
	if resp.CurrentDirectory != "/project" {
		t.Errorf("CurrentDirectory = %q, want %q", resp.CurrentDirectory, "/project")
	}
}

func TestMockGetProjectForFile(t *testing.T) {
	c := newMockClient(t, func(req jsonrpcRequest) interface{} {
		switch req.Method {
		case "updateSnapshot":
			return map[string]interface{}{
				"snapshot": "n0000000000000001",
				"projects": []map[string]interface{}{
					{"id": "p0000000000000001", "configFileName": "/abs/tsconfig.json"},
				},
			}
		case "getDefaultProjectForFile":
			// Verify the request carries snapshot but NOT project, and that
			// `file` is sent as a plain string (the only DocumentIdentifier
			// shape upstream actually populates).
			params, _ := req.Params.(map[string]interface{})
			if params["snapshot"] != "n0000000000000001" {
				return &jsonrpcError{Code: -32602, Message: fmt.Sprintf("missing snapshot, got %v", params["snapshot"])}
			}
			if _, hasProject := params["project"]; hasProject {
				return &jsonrpcError{Code: -32602, Message: "request must not include project field"}
			}
			if _, isString := params["file"].(string); !isString {
				return &jsonrpcError{Code: -32602, Message: fmt.Sprintf("file must be a string, got %T", params["file"])}
			}
			return map[string]string{"id": "p0000000000000099", "configFileName": "/abs/tsconfig.json"}
		}
		return &jsonrpcError{Code: -32601, Message: "Method not found"}
	})

	// Must open project first so the client has a snapshot to send.
	if _, err := c.OpenProject("/abs/tsconfig.json"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	proj, err := c.GetProjectForFile("/src/index.ts")
	if err != nil {
		t.Fatalf("GetProjectForFile: %v", err)
	}
	if proj != "p0000000000000099" {
		t.Errorf("project = %q, want %q", proj, "p0000000000000099")
	}
}

func TestMockGetProjectForFileRequiresSnapshot(t *testing.T) {
	// Without a prior OpenProject the client must refuse to send the request
	// rather than silently send a malformed one.
	c := newMockClient(t, func(req jsonrpcRequest) interface{} {
		t.Errorf("server should not see any request, got %s", req.Method)
		return &jsonrpcError{Code: -32601, Message: "Method not found"}
	})
	if _, err := c.GetProjectForFile("/src/index.ts"); err == nil {
		t.Fatal("expected error when no snapshot is loaded, got nil")
	}
}

// TestMockGetTypeAtOffset verifies the live wire shape: position is a uint32
// byte offset (NOT a {line, character} object) and `file` is a plain string.
// The previous incarnation of this test exercised the line/col API which
// silently produced a malformed request against real tsgo; that variant has
// been removed entirely.
func TestMockGetTypeAtOffset(t *testing.T) {
	c := newMockClient(t, func(req jsonrpcRequest) interface{} {
		switch req.Method {
		case "updateSnapshot":
			return map[string]interface{}{
				"snapshot": "n0000000000000001",
				"projects": []map[string]interface{}{
					{"id": "p.1", "configFileName": "/abs/tsconfig.json"},
				},
			}
		case "getTypeAtPosition":
			params, _ := req.Params.(map[string]interface{})
			if params["snapshot"] != "n0000000000000001" {
				return &jsonrpcError{Code: -32602, Message: fmt.Sprintf("missing snapshot, got %v", params["snapshot"])}
			}
			if params["project"] != "p.1" {
				return &jsonrpcError{Code: -32602, Message: fmt.Sprintf("wrong project, got %v", params["project"])}
			}
			if _, isString := params["file"].(string); !isString {
				return &jsonrpcError{Code: -32602, Message: fmt.Sprintf("file must be a string, got %T", params["file"])}
			}
			pos, ok := params["position"].(float64) // JSON numbers decode as float64
			if !ok {
				return &jsonrpcError{Code: -32602, Message: fmt.Sprintf("position must be a number, got %T", params["position"])}
			}
			if pos != 42 {
				return &jsonrpcError{Code: -32602, Message: fmt.Sprintf("wrong position, got %v", pos)}
			}
			return &TypeInfo{Handle: "t00042", DisplayName: "string", Flags: 0}
		}
		return &jsonrpcError{Code: -32601, Message: "Method not found"}
	})

	if _, err := c.OpenProject("/abs/tsconfig.json"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	info, err := c.GetTypeAtOffset("p.1", "/src/index.ts", 42)
	if err != nil {
		t.Fatalf("GetTypeAtOffset: %v", err)
	}
	if info.Handle != "t00042" {
		t.Errorf("handle = %q, want %q", info.Handle, "t00042")
	}
}

func TestMockGetSymbolAtOffset(t *testing.T) {
	c := newMockClient(t, func(req jsonrpcRequest) interface{} {
		switch req.Method {
		case "updateSnapshot":
			return map[string]interface{}{
				"snapshot": "n0000000000000001",
				"projects": []map[string]interface{}{
					{"id": "p.1", "configFileName": "/abs/tsconfig.json"},
				},
			}
		case "getSymbolAtPosition":
			params, _ := req.Params.(map[string]interface{})
			if params["snapshot"] != "n0000000000000001" {
				return &jsonrpcError{Code: -32602, Message: "missing snapshot"}
			}
			if _, isString := params["file"].(string); !isString {
				return &jsonrpcError{Code: -32602, Message: "file must be a string"}
			}
			return &SymbolInfo{Handle: "s00001", Name: "foo", Flags: 4}
		}
		return &jsonrpcError{Code: -32601, Message: "Method not found"}
	})

	if _, err := c.OpenProject("/abs/tsconfig.json"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	info, err := c.GetSymbolAtOffset("p.1", "/src/index.ts", 0)
	if err != nil {
		t.Fatalf("GetSymbolAtOffset: %v", err)
	}
	if info.Name != "foo" {
		t.Errorf("name = %q, want %q", info.Name, "foo")
	}
	if info.Handle != "s00001" {
		t.Errorf("handle = %q, want %q (id field must populate Handle)", info.Handle, "s00001")
	}
}

func TestMockErrorResponse(t *testing.T) {
	c := newMockClient(t, func(req jsonrpcRequest) interface{} {
		return &jsonrpcError{Code: -32600, Message: "Invalid Request"}
	})

	_, err := c.Initialize()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() != "Invalid Request" {
		t.Errorf("error = %q, want %q", err.Error(), "Invalid Request")
	}
}

func TestMockGetMembersOfSymbol(t *testing.T) {
	c := newMockClient(t, func(req jsonrpcRequest) interface{} {
		switch req.Method {
		case "updateSnapshot":
			return map[string]interface{}{
				"snapshot": "n0000000000000001",
				"projects": []map[string]interface{}{{"id": "p.1", "configFileName": "/abs/tsconfig.json"}},
			}
		case "getMembersOfSymbol":
			params, _ := req.Params.(map[string]interface{})
			if params["snapshot"] != "n0000000000000001" {
				return &jsonrpcError{Code: -32602, Message: "missing snapshot"}
			}
			if _, hasProject := params["project"]; hasProject {
				return &jsonrpcError{Code: -32602, Message: "getMembersOfSymbol must not include project"}
			}
			return []MemberInfo{
				{Handle: "s00010", Name: "x"},
				{Handle: "s00011", Name: "y"},
			}
		}
		return &jsonrpcError{Code: -32601, Message: "Method not found"}
	})

	if _, err := c.OpenProject("/abs/tsconfig.json"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	members, err := c.GetMembersOfSymbol("p.1", "s00001")
	if err != nil {
		t.Fatalf("GetMembersOfSymbol: %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("len(members) = %d, want 2", len(members))
	}
	if members[0].Name != "x" {
		t.Errorf("members[0].Name = %q, want %q", members[0].Name, "x")
	}
}

func TestMockGetBaseTypes(t *testing.T) {
	c := newMockClient(t, func(req jsonrpcRequest) interface{} {
		switch req.Method {
		case "updateSnapshot":
			return map[string]interface{}{
				"snapshot": "n0000000000000001",
				"projects": []map[string]interface{}{{"id": "p.1", "configFileName": "/abs/tsconfig.json"}},
			}
		case "getBaseTypes":
			params, _ := req.Params.(map[string]interface{})
			if params["snapshot"] != "n0000000000000001" {
				return &jsonrpcError{Code: -32602, Message: "missing snapshot"}
			}
			return []TypeInfo{{Handle: "t00099", DisplayName: "Base", Flags: 0}}
		}
		return &jsonrpcError{Code: -32601, Message: "Method not found"}
	})

	if _, err := c.OpenProject("/abs/tsconfig.json"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	types, err := c.GetBaseTypes("p.1", "t00001")
	if err != nil {
		t.Fatalf("GetBaseTypes: %v", err)
	}
	// TypeResponse has no displayName field — only Handle (=id) is populated.
	if len(types) != 1 || types[0].Handle != "t00099" {
		t.Errorf("unexpected base types: %+v", types)
	}
}

// TestMockTypeToStringBareString verifies the wire shape used by the real
// tsgo binary: typeToString returns a bare JSON string, not an object.
func TestMockTypeToStringBareString(t *testing.T) {
	c := newMockClient(t, func(req jsonrpcRequest) interface{} {
		switch req.Method {
		case "updateSnapshot":
			return map[string]interface{}{
				"snapshot": "n0000000000000001",
				"projects": []map[string]interface{}{{"id": "p.1", "configFileName": "/abs/tsconfig.json"}},
			}
		case "typeToString":
			return "Box<string>"
		}
		return &jsonrpcError{Code: -32601, Message: "Method not found"}
	})
	if _, err := c.OpenProject("/abs/tsconfig.json"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	s, err := c.TypeToString("p.1", "t00001")
	if err != nil {
		t.Fatalf("TypeToString: %v", err)
	}
	if s != "Box<string>" {
		t.Errorf("TypeToString = %q, want %q", s, "Box<string>")
	}
}

func TestMockTypeToString(t *testing.T) {
	c := newMockClient(t, func(req jsonrpcRequest) interface{} {
		switch req.Method {
		case "updateSnapshot":
			return map[string]interface{}{
				"snapshot": "n0000000000000001",
				"projects": []map[string]interface{}{{"id": "p.1", "configFileName": "/abs/tsconfig.json"}},
			}
		case "typeToString":
			params, _ := req.Params.(map[string]interface{})
			if params["snapshot"] != "n0000000000000001" {
				return &jsonrpcError{Code: -32602, Message: "missing snapshot"}
			}
			return map[string]string{"displayName": "number | string"}
		}
		return &jsonrpcError{Code: -32601, Message: "Method not found"}
	})

	if _, err := c.OpenProject("/abs/tsconfig.json"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	s, err := c.TypeToString("p.1", "t00001")
	if err != nil {
		t.Fatalf("TypeToString: %v", err)
	}
	if s != "number | string" {
		t.Errorf("TypeToString = %q, want %q", s, "number | string")
	}
}

func TestMockGetSemanticDiagnostics(t *testing.T) {
	c := newMockClient(t, func(req jsonrpcRequest) interface{} {
		switch req.Method {
		case "updateSnapshot":
			return map[string]interface{}{
				"snapshot": "n0000000000000001",
				"projects": []map[string]interface{}{{"id": "p.1", "configFileName": "/abs/tsconfig.json"}},
			}
		case "getSemanticDiagnostics":
			params, _ := req.Params.(map[string]interface{})
			if params["snapshot"] != "n0000000000000001" {
				return &jsonrpcError{Code: -32602, Message: "missing snapshot"}
			}
			return []Diagnostic{{
				File:    "/src/index.ts",
				Line:    5,
				Col:     3,
				Message: "Cannot find name 'foo'.",
			}}
		}
		return &jsonrpcError{Code: -32601, Message: "Method not found"}
	})

	if _, err := c.OpenProject("/abs/tsconfig.json"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	diags, err := c.GetSemanticDiagnostics("p.1", "/src/index.ts")
	if err != nil {
		t.Fatalf("GetSemanticDiagnostics: %v", err)
	}
	if len(diags) != 1 {
		t.Fatalf("len(diags) = %d, want 1", len(diags))
	}
	if diags[0].Message != "Cannot find name 'foo'." {
		t.Errorf("message = %q", diags[0].Message)
	}
}

// TestMockOpenProjectRealWireFormat verifies OpenProject parses the actual
// upstream UpdateSnapshotResponse shape:
//
//	{ "snapshot": "<handle>",
//	  "projects": [ { "id": "<handle>", "configFileName": "/abs/tsconfig.json", ... } ] }
//
// (Confirmed against microsoft/typescript-go/internal/api/proto.go.)
func TestMockOpenProjectRealWireFormat(t *testing.T) {
	var sawParams map[string]interface{}
	c := newMockClient(t, func(req jsonrpcRequest) interface{} {
		if req.Method == "updateSnapshot" {
			sawParams, _ = req.Params.(map[string]interface{})
			return map[string]interface{}{
				"snapshot": "n0000000000000001",
				"projects": []map[string]interface{}{
					{
						"id":              "p0000000000000007",
						"configFileName":  "/abs/path/tsconfig.json",
						"rootFiles":       []string{"/abs/path/src/index.ts"},
						"compilerOptions": map[string]interface{}{},
					},
				},
			}
		}
		return &jsonrpcError{Code: -32601, Message: "Method not found"}
	})

	proj, err := c.OpenProject("/abs/path/tsconfig.json")
	if err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	if proj != "p0000000000000007" {
		t.Errorf("project = %q, want %q", proj, "p0000000000000007")
	}
	if got := c.Snapshot(); got != "n0000000000000001" {
		t.Errorf("snapshot = %q, want %q", got, "n0000000000000001")
	}
	if got := sawParams["openProject"]; got != "/abs/path/tsconfig.json" {
		t.Errorf("openProject param = %v, want /abs/path/tsconfig.json", got)
	}
}

// TestMockOpenProjectMatchesByConfigPath verifies that when multiple projects
// are returned (uncommon for openProject but possible for fileChanges), the
// project whose configFileName matches the requested path is preferred.
func TestMockOpenProjectMatchesByConfigPath(t *testing.T) {
	c := newMockClient(t, func(req jsonrpcRequest) interface{} {
		if req.Method == "updateSnapshot" {
			return map[string]interface{}{
				"snapshot": "s0000000000000002",
				"projects": []map[string]interface{}{
					{"id": "p.other", "configFileName": "/some/other/tsconfig.json"},
					{"id": "p.target", "configFileName": "/abs/path/tsconfig.json"},
				},
			}
		}
		return &jsonrpcError{Code: -32601, Message: "Method not found"}
	})
	proj, err := c.OpenProject("/abs/path/tsconfig.json")
	if err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	if proj != "p.target" {
		t.Errorf("project = %q, want %q (match by configFileName)", proj, "p.target")
	}
}

// TestMockOpenProjectMissingSnapshotErrors guards against a regression where
// the parser previously fell back to using configFileName as the project
// handle when the response was malformed — silently masking server errors.
func TestMockOpenProjectMissingSnapshotErrors(t *testing.T) {
	c := newMockClient(t, func(req jsonrpcRequest) interface{} {
		if req.Method == "updateSnapshot" {
			// Response with no snapshot field — must error, not silently succeed.
			return map[string]interface{}{"projects": []map[string]interface{}{{"id": "p.x", "configFileName": "/x"}}}
		}
		return &jsonrpcError{Code: -32601, Message: "Method not found"}
	})
	if _, err := c.OpenProject("/abs/path/tsconfig.json"); err == nil {
		t.Fatal("expected error when snapshot field is missing, got nil")
	}
}

func TestMockOpenProjectNoProjectsErrors(t *testing.T) {
	c := newMockClient(t, func(req jsonrpcRequest) interface{} {
		if req.Method == "updateSnapshot" {
			return map[string]interface{}{"snapshot": "s.0", "projects": []map[string]interface{}{}}
		}
		return &jsonrpcError{Code: -32601, Message: "Method not found"}
	})
	if _, err := c.OpenProject("/abs/path/tsconfig.json"); err == nil {
		t.Fatal("expected error when no projects returned, got nil")
	}
}

func TestMockOpenProjectError(t *testing.T) {
	c := newMockClient(t, func(req jsonrpcRequest) interface{} {
		return &jsonrpcError{Code: -32000, Message: "bad config"}
	})

	if _, err := c.OpenProject("/abs/path/tsconfig.json"); err == nil {
		t.Fatal("expected error from OpenProject, got nil")
	}
}

func TestMockGetTypeOfSymbol(t *testing.T) {
	c := newMockClient(t, func(req jsonrpcRequest) interface{} {
		switch req.Method {
		case "updateSnapshot":
			return map[string]interface{}{
				"snapshot": "n0000000000000001",
				"projects": []map[string]interface{}{{"id": "p.1", "configFileName": "/abs/tsconfig.json"}},
			}
		case "getTypeOfSymbol":
			params, _ := req.Params.(map[string]interface{})
			if params["snapshot"] != "n0000000000000001" {
				return &jsonrpcError{Code: -32602, Message: "missing snapshot"}
			}
			return &TypeInfo{Handle: "t00055", DisplayName: "number", Flags: 8}
		}
		return &jsonrpcError{Code: -32601, Message: "Method not found"}
	})

	if _, err := c.OpenProject("/abs/tsconfig.json"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	info, err := c.GetTypeOfSymbol("p.1", "s00001")
	if err != nil {
		t.Fatalf("GetTypeOfSymbol: %v", err)
	}
	if info.Handle != "t00055" {
		t.Errorf("handle = %q, want %q", info.Handle, "t00055")
	}
	// Upstream TypeResponse has no displayName — that comes from a
	// separate typeToString round-trip — so the field stays empty.
	if info.DisplayName != "" {
		t.Errorf("displayName = %q, want empty (TypeResponse has no displayName field)", info.DisplayName)
	}
}

// TestClient_CallCtxCancellationUnblocksStdinReader is the client-level test
// for the callCtx stdin-close mechanism (PR #117 review B2). It covers:
//   - Cancelling ctx mid-RPC unblocks within ~250ms.
//   - The Client is poisoned afterward (atomic.Bool set).
//   - A second callCtx returns ErrClientPoisoned IMMEDIATELY rather than
//     queueing on c.mu behind the still-blocked first reader.
//
// Mutation kill: removing the `c.stdin.Close()` line in callCtx makes this
// test fail (or flake on the kill-fallback timer); removing the poison
// mechanism makes the second-call assertion fail.
//
// Note: this test deliberately uses a server goroutine that NEVER writes a
// response, so the read in doCall is genuinely stuck. This is the gap the
// loop-level fake test (which bypasses callCtx entirely) cannot cover.
func TestClient_CallCtxCancellationUnblocksStdinReader(t *testing.T) {
	clientReader, serverWriter := io.Pipe()
	serverReader, clientWriter := io.Pipe()

	// "Server": read framed requests forever, never reply. We DO need to
	// drain bytes off the pipe so the client's write doesn't block — pipes
	// are synchronous. serverEOF closes when the server observes EOF on its
	// read end — that EOF only happens if the client actually closes c.stdin
	// (which is the production unblock mechanism we're testing).
	serverDone := make(chan struct{})
	serverEOF := make(chan struct{})
	go func() {
		defer close(serverDone)
		buf := make([]byte, 4096)
		for {
			if _, err := serverReader.Read(buf); err != nil {
				close(serverEOF)
				return
			}
		}
	}()

	// Use a long-running real subprocess so the kill-fallback path has a
	// real os.Process to operate on. `sleep 300` is portable enough for
	// Linux CI; the subprocess does not interact with the pipes we wired
	// above — those are fake stdio for the JSON-RPC layer.
	cmd := exec.Command("sleep", "300")
	if err := cmd.Start(); err != nil {
		t.Skipf("sleep binary unavailable: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	// Shrink the kill-fallback grace window so the test can verify the
	// process was killed without waiting the production 2s.
	origKillAfter := killAfterCancel
	killAfterCancel = 200 * time.Millisecond
	t.Cleanup(func() { killAfterCancel = origKillAfter })

	c := &Client{
		cmd:    cmd,
		stdin:  clientWriter,
		stdout: bufio.NewReader(clientReader),
	}

	t.Cleanup(func() {
		// Best-effort cleanup of pipes; the server goroutine exits when
		// serverReader observes EOF.
		_ = clientWriter.Close()
		_ = serverReader.Close()
		_ = clientReader.Close()
		_ = serverWriter.Close()
		<-serverDone
	})

	ctx, cancel := context.WithCancel(context.Background())

	// Fire the first call in a goroutine — it will block in readResponse
	// because the "server" never writes anything back.
	type callResult struct {
		err error
		dur time.Duration
	}
	first := make(chan callResult, 1)
	go func() {
		start := time.Now()
		_, err := c.callCtx(ctx, "initialize", map[string]interface{}{})
		first <- callResult{err: err, dur: time.Since(start)}
	}()

	// Give the goroutine time to enter doCall and block on readResponse.
	time.Sleep(50 * time.Millisecond)

	cancel()

	select {
	case r := <-first:
		if !errors.Is(r.err, context.Canceled) {
			t.Fatalf("first call err = %v, want context.Canceled", r.err)
		}
		if r.dur > 250*time.Millisecond {
			t.Errorf("first call took %v after cancel; want <250ms (stdin-close unblock not working?)", r.dur)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first call did not return within 2s of cancel — stdin-close mechanism broken")
	}

	if !c.poisoned.Load() {
		t.Error("expected c.poisoned to be true after cancellation")
	}

	// Direct assertion that callCtx actually closed c.stdin (the B2 smoking
	// gun). Without this, callCtx could still return ctx.Canceled via the
	// select while leaking the doCall goroutine forever on its blocked read.
	// The server side observes EOF only when the production code closes the
	// pipe writer. Allow generous slack vs the 250ms first-call window — we
	// only care that close happened, not exact timing.
	select {
	case <-serverEOF:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("server never observed EOF on its read pipe — callCtx did not close c.stdin (B2 not fixed)")
	}

	// Second call must fast-fail with ErrClientPoisoned. Critically: the
	// in-flight doCall MAY still be holding c.mu (its read is unblocked by
	// stdin close, but in adversarial timing it could still be unwinding).
	// The poison check must happen BEFORE c.mu.Lock() in doCall — otherwise
	// this call would queue indefinitely. We give it a generous 500ms cap
	// to detect that hang.
	second := make(chan callResult, 1)
	go func() {
		start := time.Now()
		_, err := c.callCtx(context.Background(), "initialize", map[string]interface{}{})
		second <- callResult{err: err, dur: time.Since(start)}
	}()

	select {
	case r := <-second:
		if !errors.Is(r.err, ErrClientPoisoned) {
			t.Errorf("second call err = %v, want ErrClientPoisoned", r.err)
		}
		if r.dur > 500*time.Millisecond {
			t.Errorf("second call took %v; want immediate (poison check must precede c.mu.Lock)", r.dur)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second call hung — poison check is downstream of c.mu.Lock (B1 not fixed)")
	}
}

// TestClient_CallCtxAlreadyPoisoned verifies that a Client which is already
// marked poisoned refuses new calls instantly without touching the pipes.
func TestClient_CallCtxAlreadyPoisoned(t *testing.T) {
	c := &Client{
		cmd:    exec.Command("true"),
		stdin:  nopWriteCloser{io.Discard},
		stdout: bufio.NewReader(strings.NewReader("")),
	}
	c.poisoned.Store(true)

	_, err := c.callCtx(context.Background(), "anything", nil)
	if !errors.Is(err, ErrClientPoisoned) {
		t.Fatalf("err = %v, want ErrClientPoisoned", err)
	}
}

// nopWriteCloser turns an io.Writer into an io.WriteCloser whose Close is a
// no-op. Used in tests where a stdin pipe substitute is needed but no real
// process consumes from it.
type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }
