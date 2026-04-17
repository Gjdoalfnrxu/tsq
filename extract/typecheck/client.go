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
	raw, err := c.call("getDefaultProjectForFile", map[string]interface{}{
		"snapshot": snap,
		"file":     map[string]string{"fileName": filePath},
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
// Handles serialise as strings (e.g. "p" + 16 hex digits, "s" + 16 hex digits).
//
// On success the client caches the snapshot handle so subsequent query methods
// can supply it automatically. Returns the project handle whose configFileName
// matches the requested config (case-insensitive path compare), falling back
// to the first returned project if no exact match is found.
//
// configFileName must be an absolute path to a tsconfig.json file.
func (c *Client) OpenProject(configFileName string) (string, error) {
	raw, err := c.call("updateSnapshot", map[string]interface{}{
		"openProject": configFileName,
	})
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

// GetTypeAtPosition returns the type handle and type string at a position.
//
// Wire shape (GetTypeAtPositionParams): { snapshot, project, file:DocumentIdentifier, position:uint32 }.
//
// NOTE: The upstream `position` field is a uint32 byte offset, not a
// {line,character} object. The line/col API is preserved for backwards
// compatibility with the existing enricher pipeline; for new code that talks
// to a real tsgo backend, prefer GetTypeAtOffset.
func (c *Client) GetTypeAtPosition(project string, file string, line, col int) (*TypeInfo, error) {
	snap, err := c.snap()
	if err != nil {
		return nil, err
	}
	raw, err := c.call("getTypeAtPosition", map[string]interface{}{
		"snapshot": snap,
		"project":  project,
		"file":     map[string]string{"fileName": file},
		"position": map[string]int{"line": line, "character": col},
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

// GetTypeAtOffset matches the real upstream wire format: position is a byte
// offset into the file. Use this for live-tsgo callers.
func (c *Client) GetTypeAtOffset(project, file string, offset uint32) (*TypeInfo, error) {
	snap, err := c.snap()
	if err != nil {
		return nil, err
	}
	raw, err := c.call("getTypeAtPosition", map[string]interface{}{
		"snapshot": snap,
		"project":  project,
		"file":     map[string]string{"fileName": file},
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

// GetSymbolAtPosition returns the symbol handle and info at a position.
// See GetTypeAtPosition note re: position encoding.
func (c *Client) GetSymbolAtPosition(project string, file string, line, col int) (*SymbolInfo, error) {
	snap, err := c.snap()
	if err != nil {
		return nil, err
	}
	raw, err := c.call("getSymbolAtPosition", map[string]interface{}{
		"snapshot": snap,
		"project":  project,
		"file":     map[string]string{"fileName": file},
		"position": map[string]int{"line": line, "character": col},
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

// TypeToString returns a human-readable type string.
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
		"file":     map[string]string{"fileName": file},
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
