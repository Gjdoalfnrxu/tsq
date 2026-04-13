# Plan 23 — E2E Phase 4: Performance Benchmarks

**Status:** not started
**Phase:** Performance regression detection
**Dependencies:** none (can be done independently)

## Scope

### 23a. Fixture generator

Create `testdata/perf/gen_fixture.go` that generates synthetic JS/TS projects:

```
go run testdata/perf/gen_fixture.go -files 50 -functions-per-file 10 -call-depth 5 -taint-chains 20
```

Each generated file:
- Imports from other generated files (cross-file flow)
- Defines N functions with local variable chains (local flow)
- Calls functions from other files (inter-procedural flow)
- Includes taint sources and sinks (taint analysis)

### 23b. Medium-tier benchmarks

Add to `benchmark_test.go`:

- `BenchmarkExtractionMedium` — extraction on ~1000 LOC generated project
- `BenchmarkFlowStarConvergence` — FlowStar fixpoint on deep call chains
- `BenchmarkTaintAnalysisMedium` — full taint analysis on medium project
- `BenchmarkMultipleConfigurations` — 4 configs on the same DB
- `BenchmarkDBSerializationMedium` — encode/decode roundtrip on medium DB

### 23c. CI baseline

- Commit `testdata/perf/baseline.txt` with benchmark output
- Add `bench-check` Makefile target:
  ```makefile
  bench-check:
      go test -bench=. -benchtime=3s -count=3 > /tmp/bench-new.txt
      benchstat testdata/perf/baseline.txt /tmp/bench-new.txt
  ```

### 23d. Timeout thresholds

Add per-query timing assertions to compat tests:
```go
if elapsed > 5*time.Second {
    t.Errorf("query took %v (>5s threshold)", elapsed)
}
```

## Acceptance criteria

- Fixture generator produces valid JS that extracts without errors
- Medium benchmarks run in CI
- Baseline committed and benchstat comparison works
- No existing benchmark regresses >20%
