package typecheck

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// Client communicates with a tsgo --api --async subprocess via JSON-RPC 2.0
// using standard LSP Content-Length framing.
//
// The upstream typescript-go API (microsoft/typescript-go/internal/api) is a
// stateful session: the server holds an immutable Snapshot, and queries are
// scoped to (snapshot, project) handles returned from updateSnapshot. The
// client tracks the most recent snapshot so callers do not have to thread it
// through every method call.
type Client struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	nextID atomic.Int64
	mu     sync.Mutex

	// snapshot is the most recent snapshot handle returned by OpenProject /
	// updateSnapshot. Required as a parameter for nearly every subsequent
	// query method on the upstream API. Updated under mu.
	snapshot string
}

// Snapshot returns the most recent snapshot handle the client knows about.
// Empty until OpenProject has been called.
func (c *Client) Snapshot() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snapshot
}

// NewClient starts a tsgo subprocess. tsgoPath is the path to the tsgo binary
// (or "npx @typescript/native-preview" for the npx fallback). cwd is the
// working directory for the subprocess.
func NewClient(tsgoPath string, cwd string) (*Client, error) {
	var cmd *exec.Cmd
	if strings.HasPrefix(tsgoPath, "npx ") {
		parts := strings.Fields(tsgoPath)
		args := append(parts[1:], "--api", "--async")
		cmd = exec.Command(parts[0], args...)
	} else {
		cmd = exec.Command(tsgoPath, "--api", "--async")
	}
	cmd.Dir = cwd

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("typecheck: stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("typecheck: stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		stdin.Close()
		return nil, fmt.Errorf("typecheck: start tsgo: %w", err)
	}

	c := &Client{
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReader(stdout),
	}
	return c, nil
}

// Close shuts down the tsgo subprocess.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stdin.Close()
	return c.cmd.Wait()
}

// call sends a JSON-RPC request and reads the response.
func (c *Client) call(method string, params interface{}) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	id := c.nextID.Add(1)

	req := jsonrpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("typecheck: marshal request: %w", err)
	}

	// Write with Content-Length framing
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body))
	if _, err := io.WriteString(c.stdin, header); err != nil {
		return nil, fmt.Errorf("typecheck: write header: %w", err)
	}
	if _, err := c.stdin.Write(body); err != nil {
		return nil, fmt.Errorf("typecheck: write body: %w", err)
	}

	// Read response with Content-Length framing
	resp, err := c.readResponse()
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, resp.Error
	}

	return resp.Result, nil
}

// readResponse reads a Content-Length framed JSON-RPC response.
func (c *Client) readResponse() (*jsonrpcResponse, error) {
	// Read headers until blank line
	contentLength := -1
	for {
		line, err := c.stdout.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("typecheck: read header: %w", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if strings.HasPrefix(line, "Content-Length: ") {
			val := strings.TrimPrefix(line, "Content-Length: ")
			n, err := strconv.Atoi(val)
			if err != nil {
				return nil, fmt.Errorf("typecheck: bad Content-Length %q: %w", val, err)
			}
			contentLength = n
		}
	}

	if contentLength < 0 {
		return nil, fmt.Errorf("typecheck: missing Content-Length header")
	}

	// Read exactly contentLength bytes
	body := make([]byte, contentLength)
	if _, err := io.ReadFull(c.stdout, body); err != nil {
		return nil, fmt.Errorf("typecheck: read body: %w", err)
	}

	var resp jsonrpcResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("typecheck: unmarshal response: %w", err)
	}

	return &resp, nil
}

// Initialize sends the initialize request.
func (c *Client) Initialize() (*InitializeResponse, error) {
	raw, err := c.call("initialize", map[string]interface{}{})
	if err != nil {
		return nil, err
	}
	var resp InitializeResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("typecheck: unmarshal initialize: %w", err)
	}
	return &resp, nil
}

// GetProjectForFile gets the project handle for a file path.
//
// Wire shape (microsoft/typescript-go/internal/api/proto.go,
// GetDefaultProjectForFileParams): { snapshot, file }. Note there is no
// "project" field on the request — the project is what we are resolving.
// Requires a prior OpenProject call so that c.snapshot is populated.
func (c *Client) GetProjectForFile(filePath string) (string, error) {
	c.mu.Lock()
	snap := c.snapshot
	c.mu.Unlock()
	if snap == "" {
		return "", fmt.Errorf("typecheck: getDefaultProjectForFile requires OpenProject first (no snapshot loaded)")
	}
	// IMPORTANT: send `file` as a plain string, not {"fileName": ...}.
	// Upstream DocumentIdentifier.UnmarshalJSONFrom (proto.go:191) handles
	// the string form by populating FileName, but its object branch only
	// reads `uri` — the `fileName` key is silently ignored, leaving an
	// empty DocumentIdentifier and producing a "source file not found"
	// error downstream. Verified against TypeScript 7.0.0-dev.20260416.
	raw, err := c.call("getDefaultProjectForFile", map[string]interface{}{
		"snapshot": snap,
		"file":     filePath,
	})
	if err != nil {
		return "", err
	}
	// The response is a ProjectResponse-shaped object: { id, configFileName, ... }.
	// Older code (and some mock test fixtures) encoded a bare {"project": "..."}
	// envelope; accept both shapes for safety.
	var result struct {
		ID      string `json:"id"`
		Project string `json:"project"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("typecheck: unmarshal project: %w", err)
	}
	if result.ID != "" {
		return result.ID, nil
	}
	return result.Project, nil
}

// OpenProject loads a tsconfig.json into the tsgo session and returns the
// resulting (snapshot, project) handles. This corresponds to typescript-go's
// `updateSnapshot` API method with `openProject` set to the absolute path of
// a tsconfig.json file.
//
// Wire shape (microsoft/typescript-go/internal/api/proto.go):
//
//	Request:  UpdateSnapshotParams{ OpenProject string `json:"openProject,omitempty"` }
//	Response: UpdateSnapshotResponse{
//	    Snapshot Handle[project.Snapshot] `json:"snapshot"`
//	    Projects []*ProjectResponse       `json:"projects"`
//	}
//	ProjectResponse{ Id Handle[project.Project] `json:"id"`, ConfigFileName string `json:"configFileName"` }
//
// Handles serialise as strings. Per upstream proto.go:31-37 + createHandle:
//   - Snapshot: 'n' + 16 hex digits  (e.g. "n0000000000000001")
//   - Symbol:   's' + 16 hex digits
//   - Type:     't' + 16 hex digits
//   - Signature:'g' + 16 hex digits
//
// Project handles are NOT createHandle-shaped — see ProjectHandle (proto.go:39):
// they serialise as "p." + tspath.Path (typically the absolute tsconfig path),
// e.g. "p./tmp/proj/tsconfig.json".
//
// On success the client caches the snapshot handle so subsequent query methods
// can supply it automatically. Returns the project handle whose configFileName
// matches the requested config (case-insensitive path compare), falling back
// to the first returned project if no exact match is found.
//
// configFileName must be an absolute path to a tsconfig.json file.
func (c *Client) OpenProject(configFileName string) (string, error) {
	return c.OpenProjectWithFiles(configFileName, nil)
}

// OpenProjectWithFiles is like OpenProject but additionally seeds the snapshot
// with a list of created source files via UpdateSnapshotParams.FileChanges.
// This is empirically required for the live tsgo binary to resolve subsequent
// position queries — even files reachable via the tsconfig `include` glob
// must be advertised as "created" before getSymbolAtPosition / getTypeAtPosition
// will return anything other than "source file not found".
//
// createdFiles must be a list of absolute file paths.
func (c *Client) OpenProjectWithFiles(configFileName string, createdFiles []string) (string, error) {
	params := map[string]interface{}{
		"openProject": configFileName,
	}
	if len(createdFiles) > 0 {
		// Send each entry as a plain string — DocumentIdentifier accepts
		// either a string or {uri} object (the {fileName} object form is
		// silently dropped by upstream's custom unmarshaller).
		created := make([]string, len(createdFiles))
		copy(created, createdFiles)
		params["fileChanges"] = map[string]interface{}{
			"created": created,
		}
	}
	raw, err := c.call("updateSnapshot", params)
	if err != nil {
		return "", err
	}
	var resp struct {
		Snapshot string `json:"snapshot"`
		Projects []struct {
			ID             string `json:"id"`
			ConfigFileName string `json:"configFileName"`
		} `json:"projects"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", fmt.Errorf("typecheck: unmarshal updateSnapshot: %w", err)
	}
	if resp.Snapshot == "" {
		return "", fmt.Errorf("typecheck: updateSnapshot returned no snapshot handle (raw=%s)", string(raw))
	}
	// Cache snapshot handle for subsequent calls.
	c.mu.Lock()
	c.snapshot = resp.Snapshot
	c.mu.Unlock()

	if len(resp.Projects) == 0 {
		return "", fmt.Errorf("typecheck: updateSnapshot returned no projects for %s", configFileName)
	}
	// Prefer an exact match on configFileName.
	for _, p := range resp.Projects {
		if strings.EqualFold(p.ConfigFileName, configFileName) {
			return p.ID, nil
		}
	}
	// Fall back to the first project (single-project openProject case).
	return resp.Projects[0].ID, nil
}

// snap returns the cached snapshot handle or an error if OpenProject has not
// been called yet. Holding the lock briefly is fine — callers do not need the
// snapshot to be atomic with the subsequent RPC.
func (c *Client) snap() (string, error) {
	c.mu.Lock()
	s := c.snapshot
	c.mu.Unlock()
	if s == "" {
		return "", fmt.Errorf("typecheck: no snapshot loaded; call OpenProject first")
	}
	return s, nil
}

// GetTypeAtOffset returns the type response at a byte offset in a file.
//
// Wire shape (GetTypeAtPositionParams, proto.go:726-731):
//
//	{ snapshot, project, file:DocumentIdentifier, position:uint32 }
//
// `position` is a uint32 BYTE offset, not a {line,character} object. The
// upstream session converts this to UTF-8 internally via UTF16ToUTF8, so the
// offset is interpreted as UTF-16 code-unit count from the start of the file.
// For pure ASCII / BMP source the UTF-16 offset equals the byte offset.
//
// Returns a populated TypeInfo whose Handle is set; DisplayName is left empty
// here — upstream TypeResponse does not include a display name. Callers that
// need a printable type string should chain a TypeToString call.
func (c *Client) GetTypeAtOffset(project, file string, offset uint32) (*TypeInfo, error) {
	snap, err := c.snap()
	if err != nil {
		return nil, err
	}
	raw, err := c.call("getTypeAtPosition", map[string]interface{}{
		"snapshot": snap,
		"project":  project,
		"file":     file, // plain string — see note on OpenProject
		"position": offset,
	})
	if err != nil {
		return nil, err
	}
	var info TypeInfo
	if err := json.Unmarshal(raw, &info); err != nil {
		return nil, fmt.Errorf("typecheck: unmarshal type: %w", err)
	}
	return &info, nil
}

// GetSymbolAtOffset returns the symbol response at a byte offset in a file.
// Same wire-shape notes as GetTypeAtOffset.
func (c *Client) GetSymbolAtOffset(project, file string, offset uint32) (*SymbolInfo, error) {
	snap, err := c.snap()
	if err != nil {
		return nil, err
	}
	raw, err := c.call("getSymbolAtPosition", map[string]interface{}{
		"snapshot": snap,
		"project":  project,
		"file":     file,
		"position": offset,
	})
	if err != nil {
		return nil, err
	}
	var info SymbolInfo
	if err := json.Unmarshal(raw, &info); err != nil {
		return nil, fmt.Errorf("typecheck: unmarshal symbol: %w", err)
	}
	return &info, nil
}

// GetTypeOfSymbol returns the type of a symbol.
// Wire: GetTypeOfSymbolParams{ snapshot, project, symbol }.
func (c *Client) GetTypeOfSymbol(project string, symbolHandle string) (*TypeInfo, error) {
	snap, err := c.snap()
	if err != nil {
		return nil, err
	}
	raw, err := c.call("getTypeOfSymbol", map[string]interface{}{
		"snapshot": snap,
		"project":  project,
		"symbol":   symbolHandle,
	})
	if err != nil {
		return nil, err
	}
	var info TypeInfo
	if err := json.Unmarshal(raw, &info); err != nil {
		return nil, fmt.Errorf("typecheck: unmarshal type of symbol: %w", err)
	}
	return &info, nil
}

// GetMembersOfSymbol returns member symbols.
// Wire: GetMembersOfSymbolParams{ snapshot, symbol } — note no project field.
// The project arg is accepted for API consistency but not sent on the wire.
func (c *Client) GetMembersOfSymbol(project string, symbolHandle string) ([]MemberInfo, error) {
	_ = project // unused per upstream wire shape
	snap, err := c.snap()
	if err != nil {
		return nil, err
	}
	raw, err := c.call("getMembersOfSymbol", map[string]interface{}{
		"snapshot": snap,
		"symbol":   symbolHandle,
	})
	if err != nil {
		return nil, err
	}
	var members []MemberInfo
	if err := json.Unmarshal(raw, &members); err != nil {
		return nil, fmt.Errorf("typecheck: unmarshal members: %w", err)
	}
	return members, nil
}

// GetBaseTypes returns base type handles.
// Wire: CheckerTypeParams{ snapshot, project, type }.
func (c *Client) GetBaseTypes(project string, typeHandle string) ([]TypeInfo, error) {
	snap, err := c.snap()
	if err != nil {
		return nil, err
	}
	raw, err := c.call("getBaseTypes", map[string]interface{}{
		"snapshot": snap,
		"project":  project,
		"type":     typeHandle,
	})
	if err != nil {
		return nil, err
	}
	var types []TypeInfo
	if err := json.Unmarshal(raw, &types); err != nil {
		return nil, fmt.Errorf("typecheck: unmarshal base types: %w", err)
	}
	return types, nil
}

// TypeToString returns a human-readable type string for a type handle.
//
// Upstream (handleTypeToString, session.go:1352) returns a bare JSON string —
// e.g. `"string"` or `"Box<number>"` — NOT an object with a displayName field.
func (c *Client) TypeToString(project string, typeHandle string) (string, error) {
	snap, err := c.snap()
	if err != nil {
		return "", err
	}
	raw, err := c.call("typeToString", map[string]interface{}{
		"snapshot": snap,
		"project":  project,
		"type":     typeHandle,
	})
	if err != nil {
		return "", err
	}
	// First try the bare-string shape used by the real binary.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, nil
	}
	// Fall back to the legacy {displayName} object shape — preserved for
	// compatibility with older mocks / tsgo builds.
	var result struct {
		DisplayName string `json:"displayName"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("typecheck: unmarshal typeToString: %w", err)
	}
	return result.DisplayName, nil
}

// GetSemanticDiagnostics returns type errors for a file.
// Wire: GetDiagnosticsParams{ snapshot, project, file:DocumentIdentifier }.
func (c *Client) GetSemanticDiagnostics(project string, file string) ([]Diagnostic, error) {
	snap, err := c.snap()
	if err != nil {
		return nil, err
	}
	raw, err := c.call("getSemanticDiagnostics", map[string]interface{}{
		"snapshot": snap,
		"project":  project,
		"file":     file, // plain string per DocumentIdentifier note above
	})
	if err != nil {
		return nil, err
	}
	var diags []Diagnostic
	if err := json.Unmarshal(raw, &diags); err != nil {
		return nil, fmt.Errorf("typecheck: unmarshal diagnostics: %w", err)
	}
	return diags, nil
}
