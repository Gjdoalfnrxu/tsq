// Tests for the per-file enrichment loop in enrichWithTsgo.
//
// These cover issue #115 — the pre-fix code:
//  1. Discarded ctx, so SIGINT or --timeout could not interrupt the loop.
//  2. Silently aggregated per-file errors into stderr warnings, so a
//     pipeline that failed 90% of files would still exit 0.
//
// Both regressions are exercised here with a fake enrichRunner so we don't
// need a real tsgo binary in CI.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Gjdoalfnrxu/tsq/extract"
	"github.com/Gjdoalfnrxu/tsq/extract/db"
	"github.com/Gjdoalfnrxu/tsq/extract/schema"
	"github.com/Gjdoalfnrxu/tsq/extract/typecheck"
)

// fakeEnricher implements enrichRunner. Tests configure its EnrichFileCtx to
// either succeed (returning empty stats), fail with an error, or block on
// ctx.Done so the test can deterministically interleave cancellation
// (including the mid-RPC blocking case).
type fakeEnricher struct {
	calls   atomic.Int64
	failAll bool
	// onCall, if non-nil, is invoked at the start of each EnrichFileCtx call.
	// Tests use it to block / cancel mid-loop. ctx is provided so the fake
	// can model an RPC that blocks until ctx is cancelled — which is what a
	// real tsgo RPC stuck in stdin/stdout looks like under cancellation.
	onCall func(ctx context.Context, filePath string)
}

func (f *fakeEnricher) RegisterFiles(paths []string) {}

func (f *fakeEnricher) EnrichFileCtx(ctx context.Context, filePath string, positions []typecheck.Position) ([]typecheck.TypeFact, typecheck.EnrichStats, error) {
	f.calls.Add(1)
	if f.onCall != nil {
		f.onCall(ctx, filePath)
	}
	if err := ctx.Err(); err != nil {
		return nil, typecheck.EnrichStats{}, err
	}
	if f.failAll {
		return nil, typecheck.EnrichStats{}, errors.New("fake enrich failure")
	}
	// Pretend we got something benign back — one fact, no stats noise.
	return []typecheck.TypeFact{{Line: 1, Col: 0, TypeDisplay: "string", TypeHandle: "t1"}},
		typecheck.EnrichStats{SymbolQueries: 1, TypeQueries: 1, FactsEmitted: 1}, nil
}

func (f *fakeEnricher) Close() error { return nil }

// buildDBForFiles seeds an in-memory DB with the relations runEnrichLoop
// touches (File for path enumeration, Node so collectEnrichmentPositions
// returns at least one position per file so EnrichFile actually gets called).
func buildDBForFiles(t *testing.T, files []string) *db.DB {
	t.Helper()
	database := db.NewDB()
	// Initialise registered relations so tuples can be added.
	for _, rd := range schema.Registry {
		_ = database.Relation(rd.Name)
	}
	fileRel := database.Relation("File")
	nodeRel := database.Relation("Node")
	for i, fp := range files {
		// Use the canonical FileID derivation so collectEnrichmentPositions's
		// FileID(filePath) lookup matches the stored Node.file column.
		fileID := int32(extract.FileID(fp))
		if err := fileRel.AddTuple(database, fileID, fp, fmt.Sprintf("sha:%d", i)); err != nil {
			t.Fatalf("seed File: %v", err)
		}
		// One VariableDeclarator node per file so collectEnrichmentPositions
		// returns >=1 position; that's what makes the loop call EnrichFile.
		// Node columns: id, file, kind, startLine, startCol, endLine, endCol.
		if err := nodeRel.AddTuple(database, int32(2000+i), fileID, "VariableDeclarator", int32(1), int32(0), int32(1), int32(10)); err != nil {
			t.Fatalf("seed Node: %v", err)
		}
	}
	return database
}

// --- Fix (1): ctx cancellation interrupts the loop promptly -----------------

// TestRunEnrichLoop_CtxCancelInterrupts asserts that cancelling ctx mid-loop
// returns within a tight wall-clock bound and the returned error wraps
// context.Canceled. Without the fix, the loop runs all N files regardless of
// cancellation — this test would never return inside the 100ms bound when
// each fake call takes 50ms × 50 files = 2.5s.
func TestRunEnrichLoop_CtxCancelInterrupts(t *testing.T) {
	const nFiles = 50
	files := make([]string, nFiles)
	for i := range files {
		files[i] = fmt.Sprintf("/fake/file_%02d.ts", i)
	}
	database := buildDBForFiles(t, files)

	ctx, cancel := context.WithCancel(context.Background())

	fake := &fakeEnricher{}
	// Each call sleeps a beat so the loop spends real time between iterations,
	// giving the post-cancel iteration a chance to bail.
	fake.onCall = func(_ context.Context, _ string) { time.Sleep(20 * time.Millisecond) }

	// Cancel after the very first call lands.
	go func() {
		// Wait for loop to enter the first iteration.
		for fake.calls.Load() == 0 {
			time.Sleep(time.Millisecond)
		}
		cancel()
	}()

	var stderr bytes.Buffer
	start := time.Now()
	err := runEnrichLoop(ctx, fake, files, database, &stderr)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error from cancelled loop, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected error wrapping context.Canceled, got: %v", err)
	}
	// Generous bound: cancel + at-most-one-more-iteration's sleep + scheduling.
	// Without the ctx check it would take ~50 files × 20ms = 1s.
	if elapsed > 250*time.Millisecond {
		t.Errorf("loop took %v after cancel; expected <250ms (suggests ctx not honoured)", elapsed)
	}
	// And we should not have called EnrichFile for every file.
	if got := fake.calls.Load(); got >= int64(nFiles) {
		t.Errorf("EnrichFile called %d times for %d files; expected early exit", got, nFiles)
	}
}

// TestEnrichWithTsgo_PreCancelledCtx covers the cheap pre-flight: an
// already-cancelled ctx should bail before we even touch tsgo (no client
// process spawned). This is what makes SIGINT-during-startup safe.
func TestEnrichWithTsgo_PreCancelledCtx(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	database := db.NewDB()
	var stderr bytes.Buffer
	// tsgoPath="/no/such/path" — if the pre-flight check is removed, this
	// would attempt to start tsgo and fail with a different error. The
	// assertion that the error wraps context.Canceled pins the pre-flight.
	err := enrichWithTsgo(ctx, database, "/no/such/tsgo", "/tmp", "", &stderr)
	if err == nil {
		t.Fatal("expected error from pre-cancelled ctx, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected error wrapping context.Canceled, got: %v", err)
	}
}

// --- Fix (2): per-file failure ratio surfaces a hard error ------------------

// TestRunEnrichLoop_HighFailureRatioReturnsError seeds a loop where every
// file fails, asserts a hard error, and asserts the summary line carries
// the new failedFiles= / totalFiles= fields. Without the fix, the loop
// returns nil and the test would fail.
func TestRunEnrichLoop_HighFailureRatioReturnsError(t *testing.T) {
	const nFiles = 8 // > enrichFailureMinFiles
	files := make([]string, nFiles)
	for i := range files {
		files[i] = fmt.Sprintf("/fake/broken_%d.ts", i)
	}
	database := buildDBForFiles(t, files)

	fake := &fakeEnricher{failAll: true}
	var stderr bytes.Buffer
	err := runEnrichLoop(context.Background(), fake, files, database, &stderr)

	if err == nil {
		t.Fatal("expected hard error from 100% failure rate, got nil")
	}
	if !strings.Contains(err.Error(), "tsgo enrichment") {
		t.Errorf("expected error to mention tsgo enrichment, got: %v", err)
	}
	if !strings.Contains(stderr.String(), "failedFiles=") || !strings.Contains(stderr.String(), "totalFiles=") {
		t.Errorf("expected summary line to include failedFiles= and totalFiles=, got:\n%s", stderr.String())
	}
}

// TestRunEnrichLoop_AllFailedAlwaysErrors confirms 100%-failure runs error
// regardless of the enrichFailureMinFiles gate. Per PR #117 review feedback:
// a small project (e.g. 3 files) where every file fails is exactly the
// "broken pipeline in CI" signal we must not swallow. The min-files gate
// only applies to the partial-failure (>50%) case.
func TestRunEnrichLoop_AllFailedAlwaysErrors(t *testing.T) {
	// Use a count strictly below enrichFailureMinFiles so this test would
	// previously have passed (and incorrectly returned nil).
	files := []string{"/fake/a.ts", "/fake/b.ts", "/fake/c.ts"} // 3 < enrichFailureMinFiles=4
	database := buildDBForFiles(t, files)

	fake := &fakeEnricher{failAll: true}
	var stderr bytes.Buffer
	err := runEnrichLoop(context.Background(), fake, files, database, &stderr)
	if err == nil {
		t.Fatal("expected hard error from 100% failure on a small project, got nil")
	}
	if !strings.Contains(err.Error(), "all 3 processed files failed") {
		t.Errorf("expected error to mention all-failed case, got: %v", err)
	}
	if !strings.Contains(stderr.String(), "failedFiles=3") {
		t.Errorf("expected failedFiles=3 in summary, got:\n%s", stderr.String())
	}
}

// TestRunEnrichLoop_PartialFailureBelowMinNoError confirms the
// enrichFailureMinFiles gate still applies to the partial-failure case.
// A 3-file project with 1 failure (33%) should not error — too little signal
// to distinguish "broken" from "one weird file in a tiny project".
func TestRunEnrichLoop_PartialFailureBelowMinNoError(t *testing.T) {
	files := []string{"/fake/a.ts", "/fake/b.ts", "/fake/c.ts"}
	database := buildDBForFiles(t, files)

	// Only the first file fails; the rest succeed. 1/3 failure ratio,
	// below the partial-failure threshold check. The dedicated
	// partialFailEnricher fake (below) handles this — fakeEnricher only
	// supports all-success or all-fail.
	pf := &partialFailEnricher{}
	var stderr bytes.Buffer
	err := runEnrichLoop(context.Background(), pf, files, database, &stderr)
	if err != nil {
		t.Fatalf("expected no error on partial failure below min-files gate, got: %v", err)
	}
	if !strings.Contains(stderr.String(), "failedFiles=1") {
		t.Errorf("expected failedFiles=1 in summary, got:\n%s", stderr.String())
	}
}

// partialFailEnricher fails the first call and succeeds the rest. Used to
// exercise the partial-failure-below-min-files path independently of the
// all-failed path (which now always errors per PR #117 fix).
type partialFailEnricher struct {
	calls atomic.Int64
}

func (p *partialFailEnricher) RegisterFiles(paths []string) {}
func (p *partialFailEnricher) Close() error                 { return nil }
func (p *partialFailEnricher) EnrichFileCtx(ctx context.Context, filePath string, positions []typecheck.Position) ([]typecheck.TypeFact, typecheck.EnrichStats, error) {
	n := p.calls.Add(1)
	if n == 1 {
		return nil, typecheck.EnrichStats{}, errors.New("first-call failure")
	}
	return []typecheck.TypeFact{{Line: 1, Col: 0, TypeDisplay: "string", TypeHandle: "t1"}},
		typecheck.EnrichStats{SymbolQueries: 1, TypeQueries: 1, FactsEmitted: 1}, nil
}

// TestRunEnrichLoop_CtxThreadedIntoEnrichFileCtx verifies that the loop
// threads its ctx into the per-file enrichRunner.EnrichFileCtx call. With
// ctx threaded, a fake enricher blocking on ctx.Done() unblocks promptly on
// cancel; without it, the loop would only check ctx between files and a
// hung RPC would hold the pipeline past SIGINT / --timeout indefinitely.
//
// SCOPE NOTE (PR #117 review B2): this test only proves the *loop* passes
// ctx into EnrichFileCtx — it does NOT exercise the typecheck.Client.callCtx
// stdin-close mechanism, because the fake bypasses callCtx entirely. The
// real-RPC unblock path is covered separately by
// TestClient_CallCtxCancellationUnblocksStdinReader in extract/typecheck.
func TestRunEnrichLoop_CtxThreadedIntoEnrichFileCtx(t *testing.T) {
	files := []string{"/fake/blocking.ts"}
	database := buildDBForFiles(t, files)

	ctx, cancel := context.WithCancel(context.Background())
	fake := &fakeEnricher{}
	entered := make(chan struct{})
	fake.onCall = func(ctx context.Context, _ string) {
		// Signal the test that we're inside the "RPC", then await the
		// loop-threaded ctx. If ctx threading is broken, this would hang
		// forever (the outer-test ctx that triggers cancel is the same
		// ctx we get here only because the loop forwarded it).
		close(entered)
		<-ctx.Done()
	}

	// Cancel shortly after the fake RPC starts.
	go func() {
		<-entered
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	var stderr bytes.Buffer
	start := time.Now()
	err := runEnrichLoop(ctx, fake, files, database, &stderr)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error from cancelled blocking RPC, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected error wrapping context.Canceled, got: %v", err)
	}
	if elapsed > 250*time.Millisecond {
		t.Errorf("loop took %v after cancel; expected <250ms (ctx not threaded into per-file RPC?)", elapsed)
	}
}

// TestRunEnrichLoop_LowFailureRatioOK confirms a successful run with no
// failures returns nil and reports failedFiles=0.
func TestRunEnrichLoop_LowFailureRatioOK(t *testing.T) {
	const nFiles = 8
	files := make([]string, nFiles)
	for i := range files {
		files[i] = fmt.Sprintf("/fake/ok_%d.ts", i)
	}
	database := buildDBForFiles(t, files)

	fake := &fakeEnricher{} // failAll=false
	var stderr bytes.Buffer
	err := runEnrichLoop(context.Background(), fake, files, database, &stderr)
	if err != nil {
		t.Fatalf("expected no error on all-success run, got: %v", err)
	}
	if !strings.Contains(stderr.String(), "failedFiles=0") {
		t.Errorf("expected failedFiles=0 in summary, got:\n%s", stderr.String())
	}
	if !strings.Contains(stderr.String(), fmt.Sprintf("totalFiles=%d", nFiles)) {
		t.Errorf("expected totalFiles=%d in summary, got:\n%s", nFiles, stderr.String())
	}
}
