# Plan 15: Add `.splitAt` string built-in

## Scope

Implement the missing `.splitAt(int)` string built-in in `ql/eval/builtins.go`. CodeQL semantics: `s.splitAt(i)` returns the substring starting at index `i` (equivalent to `s.substring(i, s.length())`). Registers alongside the twelve existing methods.

Does NOT deliver: any other missing built-ins (audit shows only `splitAt` missing), any changes to the built-in dispatch loop.

## Dependencies

None. Leaf plan.

## Files to change

- `ql/eval/builtins.go` — add the `splitAt` case to the string method dispatch.
- `ql/eval/builtins_test.go` (or equivalent existing test file) — add a unit test asserting `"hello".splitAt(2) = "llo"` and edge cases (`splitAt(0)`, `splitAt(len)`, `splitAt(len+1)` for out-of-range behaviour matching CodeQL).

## Implementation steps

1. Read `ql/eval/builtins.go` to find how `.substring` is implemented — splitAt is a strict substring-from-index.
2. Add a new case in the string method dispatch that takes one int arg and returns `s[i:]` (bounds-clamped).
3. Match CodeQL's out-of-range behaviour: if `i < 0` or `i > len(s)`, the predicate fails (no result). Verify against CodeQL documentation before committing.
4. Add a unit test exercising normal, zero, full-length, and out-of-range inputs.

## Test strategy

- `TestBuiltinStringSplitAt` in `ql/eval/builtins_test.go` (or wherever existing string built-in tests live).
- Assertions: `"hello".splitAt(2) = "llo"`, `"hello".splitAt(0) = "hello"`, `"hello".splitAt(5) = ""`, `"hello".splitAt(6)` fails (no row), `"hello".splitAt(-1)` fails.

## Acceptance criteria

- [ ] `go test ./ql/eval/...` passes including the new test
- [ ] `go test ./...` passes (no regressions elsewhere)
- [ ] A query file `testdata/queries/v2/splitat_smoke.ql` with a trivial assertion using `.splitAt()` compiles and runs

## Risks and open questions

- CodeQL's exact out-of-range semantics should be confirmed from public docs before coding the test. If the behaviour is "return empty string" rather than "fail," match that — this plan's assertions are based on "predicate fails" which is the more common convention.

## Out of scope

- Audit of any other missing string built-ins (there are none per current grep, but this plan does not re-audit).
- Changes to how built-in dispatch finds methods.
