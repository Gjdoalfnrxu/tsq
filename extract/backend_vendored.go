package extract

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// DefaultRPCTimeout is the default timeout for individual RPC calls to the tsgo subprocess.
const DefaultRPCTimeout = 30 * time.Second

// ErrBackendPoisoned is returned by rpc() once a previous call timed out or was
// cancelled mid-flight. The vendored backend shares a single json.Decoder over
// the tsgo subprocess's stdout; when an RPC abandons its read goroutine on
// timeout, that orphan goroutine is still blocked on Decode and would race the
// next caller's decoder goroutine on the same byte stream — producing
// ID-mismatch errors at best and a torn JSON stream at worst (see issue #134).
//
// Rather than try to recover, we mark the backend poisoned, close stdin to
// unblock the orphan Decode, and refuse all subsequent RPCs. Callers that need
// a fresh tsgo session must construct a new VendoredBackend. Mirrors the
// poisoning discipline in extract/typecheck/client.go (issue #115 / PR #117).
var ErrBackendPoisoned = errors.New("vendored backend: poisoned by prior RPC timeout or cancellation")

// rpcKillAfterCancel is the grace period between closing tsgo's stdin on RPC
// cancellation and force-killing the subprocess. Matches the rationale in
// extract/typecheck/client.go: most healthy tsgo builds exit on stdin close
// within a few hundred ms; the kill fallback exists for hung processes that
// would otherwise leak the orphan Decode goroutine forever.
//
// Stored as an atomic.Pointer so tests can override the grace window without
// racing the cancellation goroutine.
var rpcKillAfterCancel atomic.Pointer[time.Duration]

func init() {
	d := 2 * time.Second
	rpcKillAfterCancel.Store(&d)
}

func getRPCKillAfterCancel() time.Duration {
	if p := rpcKillAfterCancel.Load(); p != nil {
		return *p
	}
	return 2 * time.Second
}

// setRPCKillAfterCancel replaces the grace window and returns the previous
// value, allowing tests to restore it via t.Cleanup.
func setRPCKillAfterCancel(d time.Duration) time.Duration {
	prev := getRPCKillAfterCancel()
	rpcKillAfterCancel.Store(&d)
	return prev
}

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
	tsgoPath   string    // path to tsgo binary, empty if not found
	tsgoCmd    *exec.Cmd // running tsgo --api process
	tsgoIn     io.WriteCloser
	tsgoOut    *json.Decoder
	tsgoMu     sync.Mutex         // serialises tsgo RPC calls
	tsgoStderr bytes.Buffer       // captured stderr from tsgo subprocess
	tsgoCancel context.CancelFunc // cancels the subprocess context
	reqID      atomic.Int64

	// tsgoAvailable is true if tsgo was found and started successfully.
	tsgoAvailable bool

	// poisoned is set (atomically, no lock required) when an RPC abandons its
	// Decode goroutine via timeout or ctx cancellation. Once set it stays set
	// for the lifetime of the backend; every subsequent rpc() returns
	// ErrBackendPoisoned immediately rather than racing a fresh Decode
	// goroutine against the stuck orphan on the shared json.Decoder. See
	// ErrBackendPoisoned and issue #134.
	poisoned atomic.Bool

	// killOnce guards the cmd.Process.Kill() fallback path so concurrent
	// timeouts cannot race into double-kill or double-timer-start. (RPCs are
	// already serialised by tsgoMu, but the kill goroutine outlives the
	// caller's mutex hold, so we still need this guard.)
	killOnce sync.Once
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
		if b.tsgoCancel != nil {
			b.tsgoCancel()
		}
		_ = b.tsgoCmd.Wait()
		b.tsgoCmd = nil
		b.tsgoCancel = nil
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
// The subprocess uses its own background context so it is not bound to the
// caller's context lifetime. Use Close() to shut down the subprocess.
func (b *VendoredBackend) startTsgo(_ context.Context) error {
	subCtx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(subCtx, b.tsgoPath, "--api", "--async", "--cwd", b.rootDir)
	b.tsgoCancel = cancel
	cmd.Stderr = &b.tsgoStderr

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
//
// Caller must hold tsgoMu (all current callers do). The call is bounded by
// DefaultRPCTimeout and respects the caller's context cancellation.
//
// Poisoning discipline (issue #134): the vendored backend shares a single
// json.Decoder across all RPCs. If we abandoned the Decode goroutine on
// timeout, the next RPC would spawn a second Decode goroutine on the same
// stream — two goroutines racing on one decoder, with predictable corruption.
// Instead, on timeout/cancel we mark the backend poisoned, close stdin to
// unblock the orphan Decode (so the goroutine exits cleanly rather than
// leaking forever), and force-kill the subprocess after a grace window if
// stdin-close didn't unstick it. After poisoning, every subsequent rpc()
// returns ErrBackendPoisoned. Mirrors extract/typecheck/client.go's
// poisoned/killOnce pattern (issue #115 / PR #117).
func (b *VendoredBackend) rpc(ctx context.Context, method string, params interface{}) (*jsonRPCResponse, error) {
	if !b.tsgoAvailable {
		return nil, ErrUnsupported
	}
	if b.poisoned.Load() {
		return nil, ErrBackendPoisoned
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

	// Write request with Content-Length header (LSP-style framing). A write
	// failure here means stdin is broken (subprocess exited, pipe closed) —
	// poison so subsequent callers don't retry against a dead pipe and so we
	// don't leave a half-written framed message that would desync the next
	// reader.
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
	if _, err := io.WriteString(b.tsgoIn, header); err != nil {
		b.poisoned.Store(true)
		return nil, fmt.Errorf("write header: %w", err)
	}
	if _, err := b.tsgoIn.Write(data); err != nil {
		b.poisoned.Store(true)
		return nil, fmt.Errorf("write body: %w", err)
	}

	// Apply a timeout to bound the read so we don't block forever if tsgo hangs.
	rpcCtx, rpcCancel := context.WithTimeout(ctx, DefaultRPCTimeout)
	defer rpcCancel()

	// Read response in a goroutine so we can select on context cancellation.
	type decodeResult struct {
		resp jsonRPCResponse
		err  error
	}
	ch := make(chan decodeResult, 1)
	go func() {
		var resp jsonRPCResponse
		err := b.tsgoOut.Decode(&resp)
		ch <- decodeResult{resp: resp, err: err}
	}()

	select {
	case <-rpcCtx.Done():
		// Poison the backend BEFORE closing stdin. Order matters: any caller
		// that races us to the next rpc() must observe poisoned=true and bail
		// out at the top rather than spawn a competing Decode goroutine on
		// the shared b.tsgoOut.
		b.poisoned.Store(true)
		// Close stdin so tsgo sees EOF, exits, closes its stdout, and the
		// orphan Decode goroutine in `go func()` above unblocks (returning an
		// error into ch, which is buffered so the send is non-blocking and
		// the goroutine exits cleanly without leaking).
		if b.tsgoIn != nil {
			_ = b.tsgoIn.Close()
		}
		// Best-effort kill if stdin-close didn't unstick the subprocess. Use
		// sync.Once so this only fires for the first poisoning event.
		b.killOnce.Do(func() {
			cmd := b.tsgoCmd
			go func() {
				select {
				case <-ch:
					// Decode unblocked within grace window — no kill needed.
					return
				case <-time.After(getRPCKillAfterCancel()):
					if cmd != nil && cmd.Process != nil {
						_ = cmd.Process.Kill()
					}
				}
			}()
		})
		return nil, fmt.Errorf("rpc %s (id=%d): %w", method, id, rpcCtx.Err())
	case result := <-ch:
		if result.err != nil {
			// Decode failures indicate the stream is no longer in a known
			// state — poison so the next caller doesn't try to reuse the
			// torn decoder.
			b.poisoned.Store(true)
			return nil, fmt.Errorf("decode response: %w", result.err)
		}
		resp := &result.resp

		// Validate response ID matches the request. A mismatch means the
		// stream framing has drifted (an earlier orphan Decode lost a
		// response, or tsgo emitted out-of-order). Either way the decoder is
		// no longer trustworthy — poison and refuse subsequent calls.
		if resp.ID != id {
			b.poisoned.Store(true)
			return nil, fmt.Errorf("rpc response id mismatch: got %d, want %d", resp.ID, id)
		}

		if resp.Error != nil {
			return nil, fmt.Errorf("tsgo error %d: %s", resp.Error.Code, resp.Error.Message)
		}

		return resp, nil
	}
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
	npmCtx, npmCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer npmCancel()
	if npmRoot, err := exec.CommandContext(npmCtx, "npm", "root", "-g").Output(); err == nil {
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
