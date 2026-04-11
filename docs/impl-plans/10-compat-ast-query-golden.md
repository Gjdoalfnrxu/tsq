# 10 — AST query compat golden

## Scope

Promote the smoke golden committed in plan 06 into a full test: more
AST shapes covered by `find_ast_query.ql`, expanded JS fixture, and
confirmation that simple (non-dataflow) CodeQL-syntax queries work
against the `javascript.qll` compat layer.

## Dependencies

- Plan 05
- Plan 06 (uses the harness and the smoke case)

## Files to change

- `/tmp/tsq/testdata/compat/projects/basic/src/index.js` — expand
  to include variety: `eval()`, a `new Function(...)`, a class,
  an arrow function, an import, a string literal.
- `/tmp/tsq/testdata/compat/find_ast_query.ql` — expand to select
  several AST shapes (CallExpr where callee is `eval`,
  ClassDefinition, ArrowFunctionExpr) with comments per case.
- `/tmp/tsq/testdata/compat/expected/ast_query.csv` — regenerate.
- `/tmp/tsq/compat_test.go` — no structural change; the existing
  row is reused.

## Implementation steps

1. Expand `index.js` with the features above. Keep it under ~30
   lines.
2. Expand `find_ast_query.ql` to return a union of shapes. Each
   result row includes a kind tag so the golden is readable.
3. Regenerate golden with `-update`.
4. Review golden for correctness:
   - One `eval` call row
   - One class row
   - One arrow-function row
   - One import row
5. Confirm the test passes without `-update` after commit.

## Test strategy

- `compat_test.go::TestCompat/ast_query` passes against the expanded
  golden.
- Manually verify that removing one feature from `index.js` causes
  the corresponding row to disappear (quick sanity check, not
  committed).

## Acceptance criteria

- [ ] Golden has >=4 rows.
- [ ] Every row corresponds to a feature present in `index.js`.
- [ ] All tests green.
- [ ] No regression to plans 07–09 goldens.

## Risks and open questions

- CodeQL class name mapping: make sure each class used in the query
  (`CallExpr`, `ClassDefinition`, etc.) is defined in
  `bridge/compat_javascript.qll`. If any are missing, narrow the
  query rather than widening the bridge.
- The JS fixture is shared with plan 06's smoke golden — any change
  here regenerates that golden. Coordinate merge order.

## Out of scope

- Any type-based AST queries (those belong in plans 11–12).
- JSX-specific AST queries (could be a follow-up).
- Adding more fixture projects.
