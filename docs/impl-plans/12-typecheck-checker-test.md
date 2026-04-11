# 12 — Unit tests for the typecheck client and enricher

## Scope

Add per-feature unit tests under `extract/typecheck/` that run the
current typecheck client against plan 11's fixtures and assert that
specific type facts (generics instantiations, union members,
conditional type branches, etc.) are emitted into the extracted DB.

The existing `extract/typecheck/client_test.go` and
`extract/typecheck/enricher_test.go` cover happy-path basics; this
PR adds feature-specific assertions that catch regressions when
tsgo upstream changes.

## Dependencies

- Plan 11 (fixtures must exist).
- Plan 04 is helpful but not required — these tests work against
  whichever transport is live.

## Files to change

- `/tmp/tsq/extract/typecheck/checker_test.go` (new) — feature-
  specific tests. If the name collides with an existing file, use
  `typed_features_test.go`.
- `/tmp/tsq/extract/typecheck/enricher_test.go` — extend with new
  sub-tests that consume plan 11 fixtures.
- `/tmp/tsq/extract/typecheck/testdata/` (new, if not already) —
  no new files here; reuse `testdata/ts/typed/` via a relative
  path.

## Implementation steps

1. Read the existing `enricher_test.go` to learn the test shape
   (how it creates a client, runs enrichment, and inspects facts).
2. For each fixture file in `testdata/ts/typed/`, add a sub-test:
   - `TestEnricher_Generics` — assert at least one
     `GenericInstantiation` tuple is emitted.
   - `TestEnricher_Conditional` — assert `TypeInfo` contains
     conditional-type kind.
   - `TestEnricher_Mapped` — assert mapped-type facts.
   - `TestEnricher_UnionIntersection` — assert `UnionMember` and
     `IntersectionMember` populated.
   - `TestEnricher_LiteralTypes` — assert literal-type kind.
3. Each sub-test loads the fixture, runs extraction with tsgo
   enrichment on, and queries the resulting DB directly (no QL
   layer — keep these low-level).
4. If any expected relation is not populated, the test fails with
   a message pointing to the specific fact that's missing.

## Test strategy

- Five new sub-tests under `extract/typecheck/`.
- Each passes against the current enricher implementation OR fails
  with a precise "expected X, got Y" message that tells the next
  implementer what to fix in the enricher.
- Run `go test ./extract/typecheck/...` — must be green OR failing
  for a documented reason (e.g., enricher gap).

## Acceptance criteria

- [ ] Five feature-specific tests added.
- [ ] Each test uses a committed fixture from plan 11.
- [ ] Test failures (if any) are documented in the PR as known
  enricher gaps, not test bugs.
- [ ] `go test ./...` passes OR failures are explicitly skipped
  with `t.Skip("enricher gap: ...")` and tracked.

## Risks and open questions

- Enricher may not emit all the relations listed in compat-plan
  3b — some may still be no-ops. If so, skip the test and file an
  issue; do not retrofit the enricher here.
- Transport flakiness: if the JSON-RPC client has timing bugs,
  these tests will be flaky. Plan 04 fixes that; until then, mark
  flaky tests with a TODO comment referencing plan 04.
- tsgo version drift: pin the version in go.mod.

## Out of scope

- Enricher improvements.
- New schema relations.
- QL-layer tests for type queries (those belong in a future compat
  typed-query plan, not this testing PR).
