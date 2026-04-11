# 06 — End-to-end compat test harness

## Scope

Add a top-level Go test file `compat_test.go` that runs the CodeQL-
syntax fixture queries from plan 05 against the basic fixture project
and compares the emitted tuples against golden CSVs. The harness
mirrors the existing `integration_test.go` shape: extract a project,
load the bridge, run a query, diff against a golden.

This PR delivers the harness plus a single smoke golden (for
`ast_query.ql`, the simplest fixture). Plans 07–10 then add goldens
for the other three queries.

## Dependencies

- Plan 05 (`testdata/compat/` fixtures must exist).

## Files to change

- `/tmp/tsq/compat_test.go` (new) — top-level test file in package
  `tsq_test` (or `compat_test`); mirrors integration_test.go style.
- `/tmp/tsq/testdata/compat/expected/ast_query.csv` (new) — golden for
  the smoke test.
- `/tmp/tsq/Makefile` — add a `test-compat` target that runs
  `go test -run TestCompat ./...` (optional but consistent with
  existing targets — check first).

## Implementation steps

1. Read `integration_test.go` lines 1–200 to learn the fixture→DB→
   query→result pattern. Copy the `extractProject`, `runQuery`, and
   golden-diffing helpers, adapted to use the compat import loader.
2. Use `bridge.LoadBridge()` + an import loader that maps
   `"javascript"`, `"DataFlow"`, `"TaintTracking"`, and
   `"semmle.javascript.security.*"` to the bridge's compat files
   (same map as `bridge/compat_test.go::makeCompatImportLoader`).
3. Add `TestCompatASTQuery` that:
   - Extracts `testdata/compat/projects/basic/`
   - Runs `testdata/compat/find_ast_query.ql` (name per plan 05)
   - Serializes results to CSV (column order: sort by first column)
   - Diffs against `testdata/compat/expected/ast_query.csv`
   - Supports the `-update` flag like integration_test.go
4. Add a table-driven scaffold (empty cases) so plans 07–10 only need
   to append rows.
5. Generate the initial golden with `go test -update`. Review it.
   Commit.

## Test strategy

- `compat_test.go::TestCompatASTQuery` passes against a committed
  golden. Running with `-update` regenerates the golden.
- The harness rejects empty result sets (guard against "golden is
  empty because query silently failed").
- Import loader must also map `"tsq::*"` paths so queries can mix
  compat and native imports if they want.

## Acceptance criteria

- [ ] `compat_test.go` compiles and runs.
- [ ] `TestCompatASTQuery` passes.
- [ ] Running `go test -update ./...` regenerates
  `testdata/compat/expected/ast_query.csv`.
- [ ] Table-driven scaffold is present so plans 07–10 add one line.
- [ ] `go test ./...` green.
- [ ] No regression in `integration_test.go`.

## Risks and open questions

- Import loader duplication: three loaders now exist
  (`bridge/compat_test.go`, `integration_test.go`,
  `compat_test.go`). Decide whether to extract a shared helper into
  `bridge/` or live with the duplication. Default: duplicate — the
  shared helper can be a follow-up.
- Result ordering: QL eval yields unordered tuples; CSV must be
  sorted deterministically before diffing.
- Golden brittleness: span offsets in results will change if the
  fixture JS file changes. Consider stripping spans to
  `file:line` granularity.

## Out of scope

- Goldens for XSS/SQLi/custom config (plans 07–09).
- Performance benchmarks for compat queries.
- Running compat tests in CI parallel to integration tests — they
  will run under `go test ./...` by default, that's enough.
