# 07 — XSS compat query golden

## Scope

Add end-to-end coverage for `testdata/compat/find_xss.ql`: a new
fixture file with a known XSS sink, a golden CSV, and one more row in
the harness table. Validates that the `DomBasedXss`-style
reconstruction in `bridge/compat_security_xss.qll` actually produces
alerts when run end-to-end.

## Dependencies

- Plan 05 (fixtures exist)
- Plan 06 (harness exists)

## Files to change

- `/tmp/tsq/testdata/compat/projects/xss/src/app.js` (new) — minimal
  app with `document.write(location.hash)` or similar.
- `/tmp/tsq/testdata/compat/expected/find_xss.csv` (new) — golden.
- `/tmp/tsq/compat_test.go` — append one row to the table-driven test
  case list.

## Implementation steps

1. Re-read `testdata/compat/find_xss.ql` from plan 05. If the query
   uses `DataFlow::PathGraph`, the harness needs to support it — check
   plan 06 output.
2. Write `projects/xss/src/app.js` with:
   - One intentional taint: `var x = location.hash; document.write(x);`
   - One negative control: `document.write("static")` — must NOT
     appear in results.
3. Add the table case:
   `{Name: "xss", Project: "xss", Query: "find_xss.ql", Expected: "find_xss.csv"}`.
4. Generate the golden: `go test -update -run TestCompat ./...`.
5. Review: the golden must contain the taint row(s) and not the
   static string row.
6. Commit query, fixture, golden.

## Test strategy

- `compat_test.go::TestCompat/xss` — runs `find_xss.ql` against
  `projects/xss`, asserts matches the golden.
- Manual inspection of the golden: confirm the reported source is
  `location.hash` and the sink is `document.write`.

## Acceptance criteria

- [ ] Golden file committed, non-empty.
- [ ] Golden contains the taint case, not the control.
- [ ] `go test ./...` green.
- [ ] No change to the harness beyond appending one case.

## Risks and open questions

- The bridge's XSS detection may not yet cover `location.hash` →
  `document.write` without additional source models. If the golden
  comes out empty, either broaden the fixture or file a bug on the
  bridge. Do not widen the bridge in this PR.
- Alert span format may differ between runs (absolute paths). Use
  the harness's path-normalisation helper (add one in plan 06 if
  missing).

## Out of scope

- Improving the compat XSS bridge library.
- Adding more XSS sink types beyond what the fixture exercises.
- Path-query SARIF output.
