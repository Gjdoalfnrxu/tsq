# 09 — User-defined DataFlow::Configuration golden

## Scope

End-to-end test that a user-written `.ql` file defining its own
`DataFlow::Configuration` subclass (overriding `isSource` and
`isSink`) runs correctly against tsq. Proves the dynamic dispatch
and abstract-class override machinery (compat items 1a + 1d)
actually wires up at query time, not just at parse time.

## Dependencies

- Plan 05 (fixtures)
- Plan 06 (harness)

## Files to change

- `/tmp/tsq/testdata/compat/projects/custom/src/sample.js` (new) —
  contains two functions: one calling `dangerous(userInput)`, one
  calling `safe(constInput)`.
- `/tmp/tsq/testdata/compat/expected/custom_config.csv` (new).
- `/tmp/tsq/compat_test.go` — append row.

## Implementation steps

1. Review `testdata/compat/custom_config.ql` from plan 05. It should
   define something like:
   ```
   class MyConfig extends DataFlow::Configuration {
     MyConfig() { this = "MyConfig" }
     override predicate isSource(DataFlow::Node n) { ... }
     override predicate isSink(DataFlow::Node n) { ... }
   }
   ```
2. Write `sample.js` so the config's `isSource`/`isSink` predicates
   are satisfied exactly once.
3. Append harness row. Generate golden with `-update`.
4. Review: golden should contain one row.
5. Also add a negative assertion — a second test case using a config
   whose `isSource` never matches, asserting zero results. This
   catches the "everything always matches" regression mode.

## Test strategy

- `compat_test.go::TestCompat/custom_config` — one expected match.
- `compat_test.go::TestCompat/custom_config_noop` (variant) — zero
  results with a restrictive config.

## Acceptance criteria

- [ ] Both positive and zero-result cases pass.
- [ ] Override resolution demonstrably works (positive case).
- [ ] All tests green.

## Risks and open questions

- The abstract class extent machinery already passes unit tests
  (compat item 1d), but may have gaps at the query level. If the
  override doesn't dispatch, file a bug; do not fix here.
- `MyConfig()` characteristic predicate syntax — confirm tsq parser
  accepts `this = "..."` form; if not, use `any()`.
- Multiple-config interaction: don't test that here, one config at
  a time.

## Out of scope

- Multiple configurations in the same query.
- Path queries (`DataFlow::PathGraph`).
- Performance of override dispatch.
