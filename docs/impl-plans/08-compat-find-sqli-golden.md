# 08 — SQL injection compat query golden

## Scope

End-to-end golden for `testdata/compat/find_sqli.ql`. Mirror of
plan 07 for SQL injection: fixture project, golden CSV, one more
harness row.

## Dependencies

- Plan 05 (fixtures)
- Plan 06 (harness)

## Files to change

- `/tmp/tsq/testdata/compat/projects/sqli/src/handler.js` (new) —
  Express-style route handler that concatenates `req.query.id` into
  a SQL string and passes it to `db.query(...)`.
- `/tmp/tsq/testdata/compat/projects/sqli/package.json` (new,
  minimal) — so the extractor treats it as a project root.
- `/tmp/tsq/testdata/compat/expected/find_sqli.csv` (new) — golden.
- `/tmp/tsq/compat_test.go` — append one row.

## Implementation steps

1. Confirm `find_sqli.ql` from plan 05 uses the compat SQL injection
   library (`import semmle.javascript.security.dataflow.SqlInjectionQuery`
   or the equivalent mapped path — plan 05 should have decided this).
2. Write `handler.js`:
   - Taint case: `app.get("/x", (req, res) => { db.query("SELECT * FROM t WHERE id=" + req.query.id); });`
   - Negative control: `db.query("SELECT * FROM t WHERE id = ?", [userId])` — parameterised, should NOT appear.
3. Append case to `compat_test.go` table.
4. Generate golden with `-update`, review, commit.

## Test strategy

- `compat_test.go::TestCompat/sqli` passes.
- Golden contains the concatenation taint, not the parameterised
  query.

## Acceptance criteria

- [ ] Golden non-empty, committed.
- [ ] Negative control absent.
- [ ] All tests green.

## Risks and open questions

- The compat bridge's SQL injection source list may not include
  `req.query.*` out of the box. Check
  `bridge/compat_security_sqli.qll` before writing the fixture. If
  missing, the fixture should use a source the bridge does know
  about, and note the limitation in the PR.
- `db` object shape: the bridge may look for specific sink names
  (`execute`, `query`, etc.) — match whatever the bridge already
  handles.

## Out of scope

- Extending the SQLi bridge.
- Multi-file taint paths.
- Prepared-statement detection beyond the single negative control.
