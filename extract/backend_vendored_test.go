package extract

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// vendoredTestdataDir returns the absolute path to testdata/ts/vendored/.
func vendoredTestdataDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "..", "testdata", "ts", "vendored")
}

func newOpenVendoredBackend(t *testing.T, rootDir string) *VendoredBackend {
	t.Helper()
	b := &VendoredBackend{}
	ctx := context.Background()
	if err := b.Open(ctx, ProjectConfig{RootDir: rootDir}); err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { b.Close() })
	return b
}

// TestVendoredBackend_Open_FindsFiles checks that Open resolves .ts files.
func TestVendoredBackend_Open_FindsFiles(t *testing.T) {
	dir := vendoredTestdataDir(t)
	b := newOpenVendoredBackend(t, dir)

	if b.treeSitter == nil {
		t.Fatal("tree-sitter backend not initialised")
	}
	if len(b.treeSitter.files) == 0 {
		t.Fatal("expected at least one source file, got none")
	}
}

// TestVendoredBackend_Open_SetsRootDir checks rootDir is set correctly.
func TestVendoredBackend_Open_SetsRootDir(t *testing.T) {
	dir := vendoredTestdataDir(t)
	b := newOpenVendoredBackend(t, dir)

	if b.rootDir != dir {
		t.Errorf("rootDir = %q, want %q", b.rootDir, dir)
	}
}

// TestVendoredBackend_WalkAST_VisitorCalled checks that the visitor receives calls.
func TestVendoredBackend_WalkAST_VisitorCalled(t *testing.T) {
	dir := vendoredTestdataDir(t)
	b := newOpenVendoredBackend(t, dir)
	v := newCollectingVisitor()

	if err := b.WalkAST(context.Background(), v); err != nil {
		t.Fatalf("WalkAST: %v", err)
	}

	if len(v.files) == 0 {
		t.Fatal("no files visited")
	}
	if len(v.allKinds) == 0 {
		t.Fatal("no nodes visited")
	}
}

// TestVendoredBackend_WalkAST_FunctionDeclaration checks that simple.ts yields
// FunctionDeclaration nodes.
func TestVendoredBackend_WalkAST_FunctionDeclaration(t *testing.T) {
	dir := vendoredTestdataDir(t)
	b := newOpenVendoredBackend(t, dir)
	v := newCollectingVisitor()

	if err := b.WalkAST(context.Background(), v); err != nil {
		t.Fatalf("WalkAST: %v", err)
	}

	if !v.hasKind("FunctionDeclaration") {
		t.Error("expected FunctionDeclaration nodes, got none")
	}
}

// TestVendoredBackend_WalkAST_ArrowFunction checks that arrow.ts yields
// ArrowFunction nodes.
func TestVendoredBackend_WalkAST_ArrowFunction(t *testing.T) {
	dir := vendoredTestdataDir(t)
	b := newOpenVendoredBackend(t, dir)
	v := newCollectingVisitor()

	if err := b.WalkAST(context.Background(), v); err != nil {
		t.Fatalf("WalkAST: %v", err)
	}

	if !v.hasKind("ArrowFunction") {
		t.Error("expected ArrowFunction nodes, got none")
	}
}

// TestVendoredBackend_WalkAST_Identifier checks that Identifier nodes are found.
func TestVendoredBackend_WalkAST_Identifier(t *testing.T) {
	dir := vendoredTestdataDir(t)
	b := newOpenVendoredBackend(t, dir)
	v := newCollectingVisitor()

	if err := b.WalkAST(context.Background(), v); err != nil {
		t.Fatalf("WalkAST: %v", err)
	}

	if !v.hasKind("Identifier") {
		t.Error("expected Identifier nodes, got none")
	}
}

// TestVendoredBackend_WalkAST_CallExpression checks that CallExpression nodes
// are found in arrow.ts (result = add(double(3), 4)).
func TestVendoredBackend_WalkAST_CallExpression(t *testing.T) {
	dir := vendoredTestdataDir(t)
	b := newOpenVendoredBackend(t, dir)
	v := newCollectingVisitor()

	if err := b.WalkAST(context.Background(), v); err != nil {
		t.Fatalf("WalkAST: %v", err)
	}

	if !v.hasKind("CallExpression") {
		t.Error("expected CallExpression nodes, got none")
	}
}

// TestVendoredBackend_WalkAST_NodePositions verifies position values are sensible.
func TestVendoredBackend_WalkAST_NodePositions(t *testing.T) {
	dir := vendoredTestdataDir(t)
	b := newOpenVendoredBackend(t, dir)

	pv := &funcVisitor{
		enterFn: func(node ASTNode) (bool, error) {
			if node.StartLine() < 1 {
				t.Errorf("node %s: StartLine %d < 1", node.Kind(), node.StartLine())
			}
			if node.EndLine() < node.StartLine() {
				t.Errorf("node %s: EndLine %d < StartLine %d", node.Kind(), node.EndLine(), node.StartLine())
			}
			return true, nil
		},
	}

	if err := b.WalkAST(context.Background(), pv); err != nil {
		t.Fatalf("WalkAST: %v", err)
	}
}

// TestVendoredBackend_WalkAST_DescendFalse checks that descend=false skips children.
func TestVendoredBackend_WalkAST_DescendFalse(t *testing.T) {
	dir := vendoredTestdataDir(t)
	b := newOpenVendoredBackend(t, dir)

	count := 0
	pv := &funcVisitor{
		enterFn: func(node ASTNode) (bool, error) {
			count++
			return false, nil
		},
	}

	if err := b.WalkAST(context.Background(), pv); err != nil {
		t.Fatalf("WalkAST: %v", err)
	}

	fileCount := len(b.treeSitter.files)
	if count != fileCount {
		t.Errorf("expected %d nodes (one root per file), got %d", fileCount, count)
	}
}

// TestVendoredBackend_WalkAST_VisitorError checks that an error from Enter aborts.
func TestVendoredBackend_WalkAST_VisitorError(t *testing.T) {
	dir := vendoredTestdataDir(t)
	b := newOpenVendoredBackend(t, dir)

	sentinel := errors.New("stop walking")
	count := 0
	pv := &funcVisitor{
		enterFn: func(node ASTNode) (bool, error) {
			count++
			if count >= 3 {
				return false, sentinel
			}
			return true, nil
		},
	}

	err := b.WalkAST(context.Background(), pv)
	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel error, got: %v", err)
	}
}

// TestVendoredBackend_WalkAST_ContextCancel checks context cancellation.
func TestVendoredBackend_WalkAST_ContextCancel(t *testing.T) {
	dir := vendoredTestdataDir(t)
	b := newOpenVendoredBackend(t, dir)

	ctx, cancel := context.WithCancel(context.Background())

	count := 0
	pv := &funcVisitor{
		enterFileFn: func(path string) error {
			count++
			if count >= 1 {
				cancel()
			}
			return nil
		},
		enterFn: func(node ASTNode) (bool, error) {
			return true, nil
		},
	}

	err := b.WalkAST(ctx, pv)
	if err == nil && len(b.treeSitter.files) > 1 {
		t.Error("expected walk to be cancelled but it completed")
	}
}

// TestVendoredBackend_SemanticMethods_NoTsgo checks that semantic methods return
// ErrUnsupported when tsgo is not available (expected in CI/test environment).
func TestVendoredBackend_SemanticMethods_NoTsgo(t *testing.T) {
	dir := vendoredTestdataDir(t)
	b := newOpenVendoredBackend(t, dir)
	ctx := context.Background()

	// tsgo is almost certainly not installed in the test environment.
	if b.TsgoAvailable() {
		t.Skip("tsgo is available -- skipping degraded-mode tests")
	}

	_, err := b.ResolveSymbol(ctx, SymbolRef{FilePath: "test.ts", Name: "x"})
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("ResolveSymbol: expected ErrUnsupported, got %v", err)
	}

	_, err = b.ResolveType(ctx, NodeRef{FilePath: "test.ts"})
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("ResolveType: expected ErrUnsupported, got %v", err)
	}

	_, err = b.CrossFileRefs(ctx, SymbolRef{FilePath: "test.ts", Name: "x"})
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("CrossFileRefs: expected ErrUnsupported, got %v", err)
	}
}

// TestVendoredBackend_Close_Idempotent checks double-close doesn't panic.
func TestVendoredBackend_Close_Idempotent(t *testing.T) {
	dir := vendoredTestdataDir(t)
	b := &VendoredBackend{}
	if err := b.Open(context.Background(), ProjectConfig{RootDir: dir}); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := b.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if err := b.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

// TestVendoredBackend_TsgoAvailable_False checks TsgoAvailable when tsgo is absent.
func TestVendoredBackend_TsgoAvailable_False(t *testing.T) {
	dir := vendoredTestdataDir(t)
	b := newOpenVendoredBackend(t, dir)

	// In the test environment tsgo is almost certainly not installed.
	// This test documents the degraded state.
	if b.TsgoAvailable() {
		t.Skip("tsgo is available; cannot test unavailable state")
	}
	// Confirm the backend is still usable for AST walking.
	v := newCollectingVisitor()
	if err := b.WalkAST(context.Background(), v); err != nil {
		t.Fatalf("WalkAST should work without tsgo: %v", err)
	}
	if len(v.allKinds) == 0 {
		t.Error("expected nodes from WalkAST even without tsgo")
	}
}

// TestVendoredBackend_WalkAST_ChildCount checks child consistency.
func TestVendoredBackend_WalkAST_ChildCount(t *testing.T) {
	dir := vendoredTestdataDir(t)
	b := newOpenVendoredBackend(t, dir)

	pv := &funcVisitor{
		enterFn: func(node ASTNode) (bool, error) {
			count := node.ChildCount()
			for i := 0; i < count; i++ {
				child := node.Child(i)
				if child == nil {
					t.Errorf("node %s: Child(%d) returned nil but ChildCount=%d",
						node.Kind(), i, count)
				}
			}
			return true, nil
		},
	}

	if err := b.WalkAST(context.Background(), pv); err != nil {
		t.Fatalf("WalkAST: %v", err)
	}
}

// TestVendoredBackend_WalkAST_FieldNames checks that some nodes have field names.
func TestVendoredBackend_WalkAST_FieldNames(t *testing.T) {
	dir := vendoredTestdataDir(t)
	b := newOpenVendoredBackend(t, dir)

	var namedFields []string
	pv := &funcVisitor{
		enterFn: func(node ASTNode) (bool, error) {
			if fn := node.FieldName(); fn != "" {
				namedFields = append(namedFields, fn)
			}
			return true, nil
		},
	}

	if err := b.WalkAST(context.Background(), pv); err != nil {
		t.Fatalf("WalkAST: %v", err)
	}

	if len(namedFields) == 0 {
		t.Error("expected some nodes with field names, got none")
	}
}

// TestVendoredBackend_RPC_CancelledContext tests that an RPC call with a
// cancelled context returns promptly rather than blocking forever (bug #1).
func TestVendoredBackend_RPC_CancelledContext(t *testing.T) {
	// Set up a fake tsgo subprocess using pipes so we can control responses.
	b := &VendoredBackend{
		tsgoAvailable: true,
	}

	// Create pipes to simulate stdin/stdout of tsgo process.
	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	defer stdinR.Close()
	defer stdoutW.Close()

	b.tsgoIn = stdinW
	b.tsgoOut = json.NewDecoder(stdoutR)

	// Drain stdin so writes don't block.
	go func() {
		io.Copy(io.Discard, stdinR)
	}()

	// Cancel the context before calling rpc.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, err := b.rpc(ctx, "test", nil)
		if err == nil {
			t.Error("expected error from cancelled context, got nil")
		}
	}()

	select {
	case <-done:
		// Good -- returned promptly.
	case <-time.After(2 * time.Second):
		t.Fatal("rpc with cancelled context blocked for >2s -- timeout bug not fixed")
	}
}

// TestVendoredBackend_SubprocessSurvivesOpenContext tests that the tsgo
// subprocess is not killed when the Open() caller's context expires (bug #2).
func TestVendoredBackend_SubprocessSurvivesOpenContext(t *testing.T) {
	dir := vendoredTestdataDir(t)

	// Use a context with a short timeout for Open().
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	b := &VendoredBackend{}
	if err := b.Open(ctx, ProjectConfig{RootDir: dir}); err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer b.Close()

	// Let the Open context expire.
	<-ctx.Done()
	time.Sleep(100 * time.Millisecond)

	// The backend's tree-sitter should still be usable.
	v := newCollectingVisitor()
	if err := b.WalkAST(context.Background(), v); err != nil {
		t.Fatalf("WalkAST failed after Open context expired: %v", err)
	}
	if len(v.allKinds) == 0 {
		t.Error("expected nodes from WalkAST after Open context expired")
	}
}

// TestVendoredBackend_RPC_MismatchedID tests that a mismatched response ID
// returns an error (bug #3).
func TestVendoredBackend_RPC_MismatchedID(t *testing.T) {
	b := &VendoredBackend{
		tsgoAvailable: true,
	}

	// Create pipes to simulate stdin/stdout of tsgo process.
	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	defer stdinR.Close()
	defer stdoutW.Close()

	b.tsgoIn = stdinW
	b.tsgoOut = json.NewDecoder(stdoutR)

	// Drain stdin so writes don't block.
	go func() {
		io.Copy(io.Discard, stdinR)
	}()

	// Write a response with a wrong ID from a goroutine.
	go func() {
		resp := jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      99999, // Wrong ID -- should not match any request.
			Result:  json.RawMessage(`{}`),
		}
		json.NewEncoder(stdoutW).Encode(resp)
	}()

	_, err := b.rpc(context.Background(), "test", nil)
	if err == nil {
		t.Fatal("expected error for mismatched response ID, got nil")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("mismatch")) {
		t.Errorf("expected mismatch error, got: %v", err)
	}
}

// TestVendoredBackend_StderrCaptured tests that tsgo stderr is captured to a
// buffer rather than going to os.Stderr (bug #5).
func TestVendoredBackend_StderrCaptured(t *testing.T) {
	b := &VendoredBackend{}
	// Verify the stderr buffer exists on the struct (compile-time check
	// that the field is bytes.Buffer, not *os.File).
	var _ bytes.Buffer = b.tsgoStderr
}

// TestVendoredBackend_RPC_TimeoutPoisonsBackend is the regression test for
// issue #134. Before the fix, a timed-out RPC abandoned its Decode goroutine,
// leaving the orphan blocked on the shared json.Decoder. The next rpc() call
// would spawn a second Decode goroutine on the same byte stream — two
// goroutines racing one decoder, producing ID-mismatch errors at best and
// stream corruption at worst.
//
// The fix is to mark the backend poisoned on timeout and refuse subsequent
// calls. This test asserts that exact behaviour:
//  1. First rpc() with a ~immediately-cancelled context returns a context error.
//  2. The backend is now poisoned.
//  3. A second rpc() — even with a fresh, healthy context and a tsgo stdout
//     pipe that is now serving valid responses — fails fast with
//     ErrBackendPoisoned, NOT with an ID mismatch error caused by consuming
//     request 1's late response.
//
// This test would FAIL on the pre-fix code: the second rpc() would consume
// request 1's late response (id=1, wrong) and surface "id mismatch" — not
// ErrBackendPoisoned — proving the orphan goroutine racing the new one.
func TestVendoredBackend_RPC_TimeoutPoisonsBackend(t *testing.T) {
	// Tighten the kill grace window so the test doesn't have to wait 2s for
	// the (unused, in this test) kill goroutine to settle.
	prev := setRPCKillAfterCancel(50 * time.Millisecond)
	t.Cleanup(func() { setRPCKillAfterCancel(prev) })

	b := &VendoredBackend{tsgoAvailable: true}

	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	t.Cleanup(func() {
		stdinR.Close()
		stdoutW.Close()
	})

	b.tsgoIn = stdinW
	b.tsgoOut = json.NewDecoder(stdoutR)

	// Drain stdin so writes don't block the test.
	go func() { _, _ = io.Copy(io.Discard, stdinR) }()

	// First call: cancel ctx immediately. The Decode goroutine inside rpc()
	// will block on stdoutR (we never write to stdoutW here) until poisoning
	// closes stdin — which doesn't help unblock the Decode either. So the
	// orphan goroutine is genuinely stuck on Decode at the moment we return
	// from the first rpc(). That is the precondition for the bug.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := b.rpc(ctx, "first", nil)
	if err == nil {
		t.Fatal("first rpc with cancelled ctx: expected error, got nil")
	}
	if !b.poisoned.Load() {
		t.Fatal("expected backend to be poisoned after timed-out rpc")
	}

	// Second call. On the pre-fix code, this would proceed past the check,
	// spawn a fresh Decode goroutine racing the orphan, and we'd see an
	// id-mismatch or decode error if anything ever arrived on the wire.
	// Post-fix, it must fail fast with ErrBackendPoisoned and NOT spawn a
	// new Decode goroutine at all.
	_, err = b.rpc(context.Background(), "second", nil)
	if !errors.Is(err, ErrBackendPoisoned) {
		t.Fatalf("second rpc after timeout: want ErrBackendPoisoned, got %v", err)
	}

	// Even if we now feed a "valid" response onto the wire (simulating the
	// late tsgo reply that caused the original bug), the backend must still
	// refuse — because there is no way to know which request that response
	// belongs to.
	go func() {
		resp := jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      1, // The id of the (now abandoned) first request.
			Result:  json.RawMessage(`{}`),
		}
		_ = json.NewEncoder(stdoutW).Encode(resp)
	}()
	// Brief delay so the encode goroutine actually writes — without this
	// the test passes for the wrong reason (nothing on the wire yet).
	time.Sleep(20 * time.Millisecond)

	_, err = b.rpc(context.Background(), "third", nil)
	if !errors.Is(err, ErrBackendPoisoned) {
		t.Fatalf("third rpc with late response on wire: want ErrBackendPoisoned, got %v", err)
	}
}

// TestVendoredBackend_RPC_DecodeErrorPoisonsBackend asserts that a decode
// failure (torn JSON, IO error) also poisons the backend rather than leaving
// it half-broken with a corrupt decoder state.
func TestVendoredBackend_RPC_DecodeErrorPoisonsBackend(t *testing.T) {
	b := &VendoredBackend{tsgoAvailable: true}

	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	t.Cleanup(func() {
		stdinR.Close()
		stdoutR.Close()
	})

	b.tsgoIn = stdinW
	b.tsgoOut = json.NewDecoder(stdoutR)

	go func() { _, _ = io.Copy(io.Discard, stdinR) }()

	// Write malformed JSON onto the stream.
	go func() {
		_, _ = stdoutW.Write([]byte("not valid json at all\n"))
		stdoutW.Close()
	}()

	_, err := b.rpc(context.Background(), "test", nil)
	if err == nil {
		t.Fatal("expected decode error, got nil")
	}
	if !b.poisoned.Load() {
		t.Fatal("expected backend to be poisoned after decode error")
	}

	_, err = b.rpc(context.Background(), "next", nil)
	if !errors.Is(err, ErrBackendPoisoned) {
		t.Fatalf("subsequent rpc after decode error: want ErrBackendPoisoned, got %v", err)
	}
}

// TestVendoredBackend_RPC_IDMismatchPoisonsBackend asserts that an id-mismatch
// response (which means the stream framing drifted) also poisons the backend.
// The pre-existing TestVendoredBackend_RPC_MismatchedID test only checks that
// the first call returns an error — this test additionally checks that the
// backend refuses subsequent RPCs.
func TestVendoredBackend_RPC_IDMismatchPoisonsBackend(t *testing.T) {
	b := &VendoredBackend{tsgoAvailable: true}

	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	t.Cleanup(func() {
		stdinR.Close()
		stdoutW.Close()
	})

	b.tsgoIn = stdinW
	b.tsgoOut = json.NewDecoder(stdoutR)

	go func() { _, _ = io.Copy(io.Discard, stdinR) }()
	go func() {
		resp := jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      99999,
			Result:  json.RawMessage(`{}`),
		}
		_ = json.NewEncoder(stdoutW).Encode(resp)
	}()

	_, err := b.rpc(context.Background(), "first", nil)
	if err == nil {
		t.Fatal("expected mismatch error, got nil")
	}
	if !b.poisoned.Load() {
		t.Fatal("expected backend to be poisoned after id mismatch")
	}

	_, err = b.rpc(context.Background(), "second", nil)
	if !errors.Is(err, ErrBackendPoisoned) {
		t.Fatalf("subsequent rpc after id mismatch: want ErrBackendPoisoned, got %v", err)
	}
}
