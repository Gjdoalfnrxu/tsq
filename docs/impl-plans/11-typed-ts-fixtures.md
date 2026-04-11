# 11 — Typed TypeScript fixture projects

## Scope

Create `testdata/ts/typed/` with TypeScript source files exercising
the type features tsgo is supposed to resolve: generics, conditional
types, mapped types, union/intersection, type aliases, and literal
types. Static fixtures only — consumed by plan 12's unit tests and
possibly by future typed compat queries.

## Dependencies

None. Fixtures are inert until plan 12 reads them.

## Files to change

- `/tmp/tsq/testdata/ts/typed/README.md` (new) — what each file
  exercises and why it matters.
- `/tmp/tsq/testdata/ts/typed/generics.ts` (new) — generic
  function, generic class, bounded generic.
- `/tmp/tsq/testdata/ts/typed/conditional.ts` (new) — `T extends U
  ? X : Y` types.
- `/tmp/tsq/testdata/ts/typed/mapped.ts` (new) — `{ [K in keyof T]: ... }`.
- `/tmp/tsq/testdata/ts/typed/union_intersection.ts` (new).
- `/tmp/tsq/testdata/ts/typed/literal_types.ts` (new) — string
  and numeric literal types.
- `/tmp/tsq/testdata/ts/typed/tsconfig.json` (new) — minimal
  strict config so tsgo has something to parse.

## Implementation steps

1. Confirm directory does not exist (it doesn't as of writing).
2. Write each `.ts` file as the smallest valid example that
   produces at least one non-trivial type fact. Each file gets
   a top-of-file comment listing which type features it
   exercises.
3. Write `tsconfig.json` with `"strict": true`,
   `"target": "ES2022"`, `"module": "commonjs"`.
4. Write the README as a table: file → features.
5. Manually run `tsc --noEmit` (or tsgo equivalent) against the
   fixtures to confirm they type-check cleanly.

## Test strategy

No Go tests in this PR. Verification:
- Each `.ts` file type-checks cleanly under `tsc`.
- Each feature in the README has at least one corresponding file.

## Acceptance criteria

- [ ] Five `.ts` files + tsconfig + README.
- [ ] All files type-check under `tsc --noEmit`.
- [ ] README table accurate.
- [ ] No Go changes.

## Risks and open questions

- Feature coverage vs. fixture size: easy to let these files
  balloon. Keep each under ~40 lines.
- Version: pick one TS version (matching tsgo's supported version)
  and document it.
- Some features (e.g., variadic tuple types) may not be needed by
  tsq's type facts yet — omit anything not on the compat-plan 3b
  relation list.

## Out of scope

- Go test code (plan 12).
- Golden output files.
- Real-world snippets — keep fixtures synthetic and small.
