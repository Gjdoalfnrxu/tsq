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
type Client struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	nextID atomic.Int64
	mu     sync.Mutex
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
func (c *Client) GetProjectForFile(filePath string) (string, error) {
	raw, err := c.call("getDefaultProjectForFile", map[string]interface{}{
		"file": filePath,
	})
	if err != nil {
		return "", err
	}
	var result struct {
		Project string `json:"project"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("typecheck: unmarshal project: %w", err)
	}
	return result.Project, nil
}

// GetTypeAtPosition returns the type handle and type string at a position.
func (c *Client) GetTypeAtPosition(project string, file string, line, col int) (*TypeInfo, error) {
	raw, err := c.call("getTypeAtPosition", map[string]interface{}{
		"project":  project,
		"file":     file,
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

// GetSymbolAtPosition returns the symbol handle and info at a position.
func (c *Client) GetSymbolAtPosition(project string, file string, line, col int) (*SymbolInfo, error) {
	raw, err := c.call("getSymbolAtPosition", map[string]interface{}{
		"project":  project,
		"file":     file,
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
func (c *Client) GetTypeOfSymbol(project string, symbolHandle string) (*TypeInfo, error) {
	raw, err := c.call("getTypeOfSymbol", map[string]interface{}{
		"project": project,
		"symbol":  symbolHandle,
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
func (c *Client) GetMembersOfSymbol(project string, symbolHandle string) ([]MemberInfo, error) {
	raw, err := c.call("getMembersOfSymbol", map[string]interface{}{
		"project": project,
		"symbol":  symbolHandle,
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
func (c *Client) GetBaseTypes(project string, typeHandle string) ([]TypeInfo, error) {
	raw, err := c.call("getBaseTypes", map[string]interface{}{
		"project": project,
		"type":    typeHandle,
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
	raw, err := c.call("typeToString", map[string]interface{}{
		"project": project,
		"type":    typeHandle,
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
func (c *Client) GetSemanticDiagnostics(project string, file string) ([]Diagnostic, error) {
	raw, err := c.call("getSemanticDiagnostics", map[string]interface{}{
		"project": project,
		"file":    file,
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
