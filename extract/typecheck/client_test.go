package typecheck

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"testing"
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
	raw := `{"jsonrpc":"2.0","id":1,"result":{"handle":"t00001","displayName":"string","flags":0}}`
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
	if info.DisplayName != "string" {
		t.Errorf("displayName = %q, want %q", info.DisplayName, "string")
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
	t.Setenv("TSGO_PATH", "")
	// This test validates the function doesn't panic.
	_ = DetectTsgo()
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
		if req.Method == "getDefaultProjectForFile" {
			return map[string]string{"project": "p.abc123"}
		}
		return &jsonrpcError{Code: -32601, Message: "Method not found"}
	})

	proj, err := c.GetProjectForFile("/src/index.ts")
	if err != nil {
		t.Fatalf("GetProjectForFile: %v", err)
	}
	if proj != "p.abc123" {
		t.Errorf("project = %q, want %q", proj, "p.abc123")
	}
}

func TestMockGetTypeAtPosition(t *testing.T) {
	c := newMockClient(t, func(req jsonrpcRequest) interface{} {
		if req.Method == "getTypeAtPosition" {
			return &TypeInfo{Handle: "t00042", DisplayName: "string", Flags: 0}
		}
		return &jsonrpcError{Code: -32601, Message: "Method not found"}
	})

	info, err := c.GetTypeAtPosition("p.1", "/src/index.ts", 10, 5)
	if err != nil {
		t.Fatalf("GetTypeAtPosition: %v", err)
	}
	if info.Handle != "t00042" {
		t.Errorf("handle = %q, want %q", info.Handle, "t00042")
	}
	if info.DisplayName != "string" {
		t.Errorf("displayName = %q, want %q", info.DisplayName, "string")
	}
}

func TestMockGetSymbolAtPosition(t *testing.T) {
	c := newMockClient(t, func(req jsonrpcRequest) interface{} {
		if req.Method == "getSymbolAtPosition" {
			return &SymbolInfo{Handle: "s00001", Name: "foo", Flags: 4}
		}
		return &jsonrpcError{Code: -32601, Message: "Method not found"}
	})

	info, err := c.GetSymbolAtPosition("p.1", "/src/index.ts", 1, 0)
	if err != nil {
		t.Fatalf("GetSymbolAtPosition: %v", err)
	}
	if info.Name != "foo" {
		t.Errorf("name = %q, want %q", info.Name, "foo")
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
		if req.Method == "getMembersOfSymbol" {
			return []MemberInfo{
				{Handle: "s00010", Name: "x"},
				{Handle: "s00011", Name: "y"},
			}
		}
		return &jsonrpcError{Code: -32601, Message: "Method not found"}
	})

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
		if req.Method == "getBaseTypes" {
			return []TypeInfo{{Handle: "t00099", DisplayName: "Base", Flags: 0}}
		}
		return &jsonrpcError{Code: -32601, Message: "Method not found"}
	})

	types, err := c.GetBaseTypes("p.1", "t00001")
	if err != nil {
		t.Fatalf("GetBaseTypes: %v", err)
	}
	if len(types) != 1 || types[0].DisplayName != "Base" {
		t.Errorf("unexpected base types: %+v", types)
	}
}

func TestMockTypeToString(t *testing.T) {
	c := newMockClient(t, func(req jsonrpcRequest) interface{} {
		if req.Method == "typeToString" {
			return map[string]string{"displayName": "number | string"}
		}
		return &jsonrpcError{Code: -32601, Message: "Method not found"}
	})

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
		if req.Method == "getSemanticDiagnostics" {
			return []Diagnostic{{
				File:    "/src/index.ts",
				Line:    5,
				Col:     3,
				Message: "Cannot find name 'foo'.",
			}}
		}
		return &jsonrpcError{Code: -32601, Message: "Method not found"}
	})

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

func TestMockGetTypeOfSymbol(t *testing.T) {
	c := newMockClient(t, func(req jsonrpcRequest) interface{} {
		if req.Method == "getTypeOfSymbol" {
			return &TypeInfo{Handle: "t00055", DisplayName: "number", Flags: 8}
		}
		return &jsonrpcError{Code: -32601, Message: "Method not found"}
	})

	info, err := c.GetTypeOfSymbol("p.1", "s00001")
	if err != nil {
		t.Fatalf("GetTypeOfSymbol: %v", err)
	}
	if info.Handle != "t00055" {
		t.Errorf("handle = %q, want %q", info.Handle, "t00055")
	}
	if info.DisplayName != "number" {
		t.Errorf("displayName = %q, want %q", info.DisplayName, "number")
	}
}
