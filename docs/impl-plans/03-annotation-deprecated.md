# 03 — Emit warnings for `deprecated` predicates and classes

## Scope

Compat-plan item 1l parses `deprecated` but does nothing with it. This
PR makes the resolver emit a non-fatal warning when a query references
a deprecated predicate, class, or member. Warnings are collected
alongside errors and surfaced by the CLI at the end of compilation.

This PR does NOT add a `--deprecation=error` flag (could be a
follow-up). It does NOT touch `private` (plan 02) or the pragma/
bindingset/language annotations.

## Dependencies

None strictly, but coordinates with plan 02 which adds annotation
awareness to the resolver. Implement after 02 if both are in flight, so
the plumbing is consistent.

## Files to change

- `/tmp/tsq/ql/resolve/resolve.go` — add a warning channel to the
  resolver (or reuse error list with a Severity field), emit a warning
  when resolving a deprecated target.
- `/tmp/tsq/ql/resolve/resolve_test.go` — add tests.
- `/tmp/tsq/cmd/tsq/main.go` — surface warnings in CLI output (after
  compile, before running). Keep exit code 0.
- `/tmp/tsq/ql/ast/ast.go` — no change.

## Implementation steps

1. Decide the warning representation: either a separate
   `Resolver.Warnings []Warning` field or reuse errors with a
   `Severity` enum. Prefer the former — errors and warnings are
   semantically different and mixing them complicates exit codes.
2. Add `isDeprecated(ann []Annotation) bool` helper.
3. At each call-site resolution (predicate, class reference, member
   call), after looking up the target, check the target's annotations
   and append a warning to the resolver if deprecated. Include the
   caller's span in the warning.
4. Thread warnings out through the resolver's result struct.
5. In `cmd/tsq/main.go`, print warnings to stderr after a successful
   compile, one per line, with file:line prefix. Do not increase exit
   code.
6. Tests as below.

## Test strategy

- `ql/resolve/resolve_test.go::TestDeprecatedPredicateWarning`: define
  a deprecated predicate and call it; assert resolve succeeds AND
  the returned warnings list contains one entry mentioning the
  predicate name.
- `ql/resolve/resolve_test.go::TestDeprecatedClassWarning`: same for
  a class reference.
- `ql/resolve/resolve_test.go::TestNoWarningForUndeprecated`: control
  case — same query without the annotation emits no warnings.
- `cmd/tsq/main_test.go` (if it exists) or a new `cmd/tsq/cli_test.go`
  asserting stderr contains the warning but exit code is 0.

## Acceptance criteria

- [ ] Deprecated call-sites emit warnings at resolve time.
- [ ] Warnings do not cause compile failure.
- [ ] CLI prints warnings to stderr, exit code unchanged.
- [ ] Existing tests still pass (the bridge may use `deprecated` — if
  so, suppress noise in the bridge loader or accept warnings in
  bridge-level tests; document the choice).

## Risks and open questions

- If any bridge file is already marked `deprecated`, running the test
  suite will start producing noise. Grep for it first. If present,
  either fix the bridge or add a flag to suppress warnings originating
  from `compat_*.qll`.
- Warning format: match CodeQL's or invent a tsq style? Pick tsq style
  and document.
- Deduplication: if a deprecated predicate is called 100 times, do we
  warn 100 times or once? Default to once per (target, caller-span)
  and note in risks that this may need tuning.

## Out of scope

- A `--deprecation=error` flag.
- Deprecation messages in annotations (CodeQL's `deprecated` takes no
  arg; plan does not add one).
- Suppressing specific deprecations via pragma.
