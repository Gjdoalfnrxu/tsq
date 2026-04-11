# 05 — Add CodeQL-syntax query fixtures

## Scope

Create the `testdata/compat/` directory with CodeQL-syntax `.ql` query
files that exercise the compat bridge (`import javascript`,
`DataFlow::Configuration`, etc.). This PR adds ONLY the static fixture
files and a short README — no Go test code. Plan 06 wires them into a
test harness; plans 07–10 add the per-query goldens.

Splitting fixture creation from harness creation lets the static files
be reviewed for clean-room correctness (do any of these look like
CodeQL source?) without being tangled up with Go plumbing.

## Dependencies

None.

## Files to change

- `/tmp/tsq/testdata/compat/README.md` (new) — explains the clean-room
  rule: these queries are written from scratch against the public
  CodeQL API documentation, not copied from CodeQL's repository.
- `/tmp/tsq/testdata/compat/find_xss.ql` (new) — stub XSS query.
- `/tmp/tsq/testdata/compat/find_sqli.ql` (new) — stub SQL injection.
- `/tmp/tsq/testdata/compat/custom_config.ql` (new) — user-defined
  `DataFlow::Configuration` subclass.
- `/tmp/tsq/testdata/compat/ast_query.ql` (new) — simple AST query:
  "find all `eval()` calls" in CodeQL syntax.
- `/tmp/tsq/testdata/compat/projects/basic/src/index.js` (new) — a
  tiny JS source file the queries can run against.

## Implementation steps

1. Confirm `testdata/compat/` does not exist on `main` (it doesn't as
   of writing). If it's been created by another PR, rebase and merge
   contents.
2. Write `README.md` with the clean-room disclaimer and a pointer to
   CODEQL-COMPAT-PLAN.md.
3. Write each `.ql` file. Every query must:
   - Start with `import javascript` (or `DataFlow::PathGraph` etc.)
   - Be syntactically valid per tsq's current parser (compile-only,
     running is plan 06's problem)
   - Contain comments pointing to the CodeQL documentation URL the
     query's structure was derived from
4. Write `projects/basic/src/index.js` containing enough code for each
   query to produce at least one result: a `document.write(userInput)`
   (XSS), a `db.query(userInput)` (SQLi), an `eval(x)` (AST), and a
   helper that imports `express`.
5. Run `go run ./cmd/tsq compile testdata/compat/find_xss.ql` (or
   equivalent) manually to verify each file at least parses and
   resolves against the bridge. Record any that don't in the PR
   description — plan 07+ will assert actual results.

## Test strategy

No Go tests in this PR. Verification is:
- `grep -r "CodeQL source" testdata/compat/` returns nothing
  (no copied code markers).
- Each `.ql` file parses cleanly with the existing tsq parser.
- `testdata/compat/projects/basic/src/index.js` is valid JS (visually
  reviewed or run through a parser).

## Acceptance criteria

- [ ] Four `.ql` files under `testdata/compat/`.
- [ ] One JS fixture project under `testdata/compat/projects/basic/`.
- [ ] README with clean-room rule.
- [ ] Each `.ql` file parses without error (manual check or ad-hoc
  script in the PR description).
- [ ] No Go changes.

## Risks and open questions

- Clean-room discipline: reviewer must confirm the files don't look
  line-for-line like CodeQL's published examples. Paraphrase structure
  where needed.
- The JS fixture project must be small enough to review but realistic
  enough to hit all the queries.
- File naming: `find_xss.ql` vs `xss.ql` — match the compat plan's
  naming (`find_xss.ql`) for consistency.

## Out of scope

- Go test code (plan 06).
- Golden output files (plans 07–10).
- Additional queries beyond the four named in the compat plan.
