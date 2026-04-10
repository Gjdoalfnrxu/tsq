package extract

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
)

// VendoredBackend implements ExtractorBackend using a combination of tree-sitter
// for AST walking and the tsgo CLI (typescript-go) for type checking and symbol
// resolution via its --api subprocess mode.
//
// typescript-go (github.com/microsoft/typescript-go) cannot be imported as a Go
// library because:
//   - All packages are under internal/ (not importable by external modules)
//   - Requires Go 1.26+ (current toolchain is 1.23.x)
//   - The public API is listed as "not ready" in the project README
//
// Instead, this backend spawns tsgo --api as a subprocess and communicates via
// JSON-RPC over stdin/stdout. AST walking is delegated to tree-sitter (reusing
// TreeSitterBackend) since tsgo's parser is internal-only.
//
// When tsgo is not found in PATH, the backend degrades gracefully: Open succeeds,
// AST walking works (via tree-sitter), and semantic methods (ResolveSymbol,
// ResolveType, CrossFileRefs) return ErrUnsupported.
type VendoredBackend struct {
	treeSitter *TreeSitterBackend // embedded for AST walking
	rootDir    string

	// tsgo subprocess state
	tsgoPath string    // path to tsgo binary, empty if not found
	tsgoCmd  *exec.Cmd // running tsgo --api process
	tsgoIn   io.WriteCloser
	tsgoOut  *json.Decoder
	tsgoMu   sync.Mutex // serialises tsgo RPC calls
	reqID    atomic.Int64

	// tsgoAvailable is true if tsgo was found and started successfully.
	tsgoAvailable bool
}

// jsonRPCRequest is a JSON-RPC 2.0 request envelope.
type jsonRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int64       `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// jsonRPCResponse is a JSON-RPC 2.0 response envelope.
type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

// jsonRPCError is a JSON-RPC 2.0 error object.
type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Open initialises the vendored backend. It resolves source files via tree-sitter
// and optionally starts a tsgo --api subprocess for semantic analysis.
func (b *VendoredBackend) Open(ctx context.Context, cfg ProjectConfig) error {
	b.rootDir = cfg.RootDir

	// Initialise tree-sitter backend for AST walking.
	b.treeSitter = &TreeSitterBackend{}
	if err := b.treeSitter.Open(ctx, cfg); err != nil {
		return fmt.Errorf("vendored backend: tree-sitter init: %w", err)
	}

	// Try to find and start tsgo.
	b.tsgoPath = findTsgo()
	if b.tsgoPath != "" {
		if err := b.startTsgo(ctx); err != nil {
			// tsgo failed to start -- degrade gracefully.
			b.tsgoAvailable = false
			b.tsgoPath = ""
		}
	}

	return nil
}

// WalkAST delegates to the tree-sitter backend for AST walking.
// tsgo's parser is internal-only and cannot be used directly.
func (b *VendoredBackend) WalkAST(ctx context.Context, v ASTVisitor) error {
	return b.treeSitter.WalkAST(ctx, v)
}

// ResolveSymbol attempts to resolve a symbol using tsgo's API.
// Falls back to ErrUnsupported when tsgo is not available.
func (b *VendoredBackend) ResolveSymbol(ctx context.Context, ref SymbolRef) (SymbolDecl, error) {
	if !b.tsgoAvailable {
		return SymbolDecl{}, ErrUnsupported
	}

	b.tsgoMu.Lock()
	defer b.tsgoMu.Unlock()

	resp, err := b.rpc(ctx, "getDefinition", map[string]interface{}{
		"file":   ref.FilePath,
		"offset": ref.StartByte,
	})
	if err != nil {
		return SymbolDecl{}, fmt.Errorf("vendored backend: resolve symbol: %w", err)
	}

	var result struct {
		File      string `json:"file"`
		Offset    int    `json:"offset"`
		Name      string `json:"name"`
		Available bool   `json:"available"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return SymbolDecl{}, fmt.Errorf("vendored backend: decode symbol: %w", err)
	}
	if !result.Available {
		return SymbolDecl{}, ErrUnsupported
	}

	return SymbolDecl{
		FilePath:  result.File,
		StartByte: result.Offset,
		Name:      result.Name,
	}, nil
}

// ResolveType attempts to get type information using tsgo's API.
// Falls back to ErrUnsupported when tsgo is not available.
func (b *VendoredBackend) ResolveType(ctx context.Context, node NodeRef) (string, error) {
	if !b.tsgoAvailable {
		return "", ErrUnsupported
	}

	b.tsgoMu.Lock()
	defer b.tsgoMu.Unlock()

	resp, err := b.rpc(ctx, "getQuickInfo", map[string]interface{}{
		"file":   node.FilePath,
		"offset": node.StartByte,
	})
	if err != nil {
		return "", fmt.Errorf("vendored backend: resolve type: %w", err)
	}

	var result struct {
		DisplayString string `json:"displayString"`
		Available     bool   `json:"available"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return "", fmt.Errorf("vendored backend: decode type: %w", err)
	}
	if !result.Available {
		return "", ErrUnsupported
	}

	return result.DisplayString, nil
}

// CrossFileRefs attempts to find cross-file references using tsgo's API.
// Falls back to ErrUnsupported when tsgo is not available.
func (b *VendoredBackend) CrossFileRefs(ctx context.Context, sym SymbolRef) ([]NodeRef, error) {
	if !b.tsgoAvailable {
		return nil, ErrUnsupported
	}

	b.tsgoMu.Lock()
	defer b.tsgoMu.Unlock()

	resp, err := b.rpc(ctx, "getReferences", map[string]interface{}{
		"file":   sym.FilePath,
		"offset": sym.StartByte,
	})
	if err != nil {
		return nil, fmt.Errorf("vendored backend: cross-file refs: %w", err)
	}

	var result struct {
		Refs []struct {
			File      string `json:"file"`
			Start     int    `json:"start"`
			End       int    `json:"end"`
			Kind      string `json:"kind"`
			Available bool   `json:"available"`
		} `json:"refs"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("vendored backend: decode refs: %w", err)
	}

	refs := make([]NodeRef, 0, len(result.Refs))
	for _, r := range result.Refs {
		refs = append(refs, NodeRef{
			FilePath:  r.File,
			StartByte: r.Start,
			EndByte:   r.End,
			Kind:      r.Kind,
		})
	}
	return refs, nil
}

// Close shuts down the tsgo subprocess and releases tree-sitter resources.
func (b *VendoredBackend) Close() error {
	var errs []error

	if b.tsgoCmd != nil && b.tsgoCmd.Process != nil {
		if b.tsgoIn != nil {
			b.tsgoIn.Close()
		}
		if err := b.tsgoCmd.Process.Kill(); err != nil {
			errs = append(errs, fmt.Errorf("kill tsgo: %w", err))
		}
		_ = b.tsgoCmd.Wait()
		b.tsgoCmd = nil
		b.tsgoAvailable = false
	}

	if b.treeSitter != nil {
		if err := b.treeSitter.Close(); err != nil {
			errs = append(errs, err)
		}
		b.treeSitter = nil
	}

	if len(errs) > 0 {
		return fmt.Errorf("vendored backend close: %v", errs)
	}
	return nil
}

// TsgoAvailable reports whether the tsgo subprocess is running.
func (b *VendoredBackend) TsgoAvailable() bool {
	return b.tsgoAvailable
}

// startTsgo launches the tsgo --api subprocess.
func (b *VendoredBackend) startTsgo(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, b.tsgoPath, "--api", "--async", "--cwd", b.rootDir)
	cmd.Stderr = os.Stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("tsgo stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return fmt.Errorf("tsgo stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		stdin.Close()
		return fmt.Errorf("tsgo start: %w", err)
	}

	b.tsgoCmd = cmd
	b.tsgoIn = stdin
	b.tsgoOut = json.NewDecoder(stdout)
	b.tsgoAvailable = true

	return nil
}

// rpc sends a JSON-RPC request and waits for the response.
// Caller must hold tsgoMu.
func (b *VendoredBackend) rpc(ctx context.Context, method string, params interface{}) (*jsonRPCResponse, error) {
	if !b.tsgoAvailable {
		return nil, ErrUnsupported
	}

	id := b.reqID.Add(1)
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	// Write request with Content-Length header (LSP-style framing).
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
	if _, err := io.WriteString(b.tsgoIn, header); err != nil {
		return nil, fmt.Errorf("write header: %w", err)
	}
	if _, err := b.tsgoIn.Write(data); err != nil {
		return nil, fmt.Errorf("write body: %w", err)
	}

	// Read response. Context cancellation is handled by the command context.
	var resp jsonRPCResponse
	if err := b.tsgoOut.Decode(&resp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("tsgo error %d: %s", resp.Error.Code, resp.Error.Message)
	}

	return &resp, nil
}

// findTsgo searches for the tsgo binary in PATH and common locations.
func findTsgo() string {
	// Check PATH first.
	if path, err := exec.LookPath("tsgo"); err == nil {
		return path
	}

	// Check common npm global install locations.
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, "node_modules", ".bin", "tsgo"),
		filepath.Join(home, ".npm-global", "bin", "tsgo"),
		"/usr/local/bin/tsgo",
	}

	// Also check npx-style paths.
	if npmRoot, err := exec.Command("npm", "root", "-g").Output(); err == nil {
		root := strings.TrimSpace(string(npmRoot))
		candidates = append(candidates, filepath.Join(root, ".bin", "tsgo"))
	}

	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}

	return ""
}
