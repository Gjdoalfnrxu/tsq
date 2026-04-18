# Compat Golden Notes

Some golden CSVs in this directory enshrine known false negatives —
positives we *would* expect but cannot detect with the current pipeline.
Documented here because CSV does not support comments (the test does a
byte-exact compare in `compareGolden`).

## `find_sanitizer_bypass.csv` (2 rows; ideal: 4)

Fixture: `testdata/compat/projects/sanitizer-bypass/src/app.ts`. Expected
positives are the four "alert" routes (1, 3, 4, 6). The golden currently
has only routes 1 and 4.

Missing:

- Route 3 `xssWrongSanitizer` — uses `sqlEscape(q)` then sends the result
  to `res.send`. Should alert (xss).
- Route 6 `sqlWrongSanitizer` — uses `escapeHtml(q)` then sends the result
  to `db.query`. Should alert (sql).

Root cause: `FlowStar` does not propagate across a `CallResult`, so
`qSym → safeSym` is not a flow edge. Even with a working sanitizer-kind
match, the SanitizedEdge negation has nothing to block — and the alert
never fires either. See:

- issue #128 — FlowStar through CallResult.
- issue #127 — sanitizer kind mismatch (frameworks.go emits sink-side
  kinds while SanitizedEdge negates against source kinds, making
  sanitization a no-op pipeline-wide).

Pre-PR #126 the cross-product (cartesian over sink/sink-kind) caught
these by accident. Post-PR #126 they are genuine false negatives.

## `find_multi_vuln.csv` (2 rows; ideal: 3)

Fixture: `testdata/compat/projects/multi-vuln/src/server.ts`. Three sink
shapes in the file: SQL (route 1), XSS (route 2), command-injection
(route 3), and a path-traversal (route 4) that has no sink rule at all.

Missing:

- Route 3 cmd-injection — `const { exec } = require('child_process');
  exec('ls ' + q);`. Sink rule at `extract/rules/frameworks.go:30-40`
  requires a local `Function(execFn, "exec", ...)` decl that doesn't
  exist for the destructured CommonJS import shape. Pre-existing
  extractor limitation, not related to PR #126. See issue #129.
- Route 4 path-traversal — no `fs.readFile` sink rule exists at all.
  Wider coverage gap noted in PR #126 description.
