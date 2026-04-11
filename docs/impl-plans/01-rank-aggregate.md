# 01 — True ordinal `rank` aggregate

## Scope

Replace the v1 `rank` aggregate implementation (which returns the size of
the group, making `rank` behave identically to `count`) with a true
ordinal ranking: for each tuple in the group, emit its 1-indexed position
after ordering by the ranking expression. This finishes compat-plan item
1h, which shipped the keyword and parser support but deferred full
semantics.

This PR does NOT introduce new aggregate functions, does NOT change the
syntactic surface of `rank`, and does NOT touch `concat`/`strictcount`/
`strictsum` (already correct).

## Dependencies

None. `rank` is already parsed and plumbed through; only the evaluator
behaviour changes.

## Files to change

- `/tmp/tsq/ql/eval/aggregate.go` — replace the `case "rank"` branch
  with real ordinal logic; may require changing the caller signature so
  `rank` can return a relation (multiple tuples) rather than a scalar.
- `/tmp/tsq/ql/eval/aggregate_test.go` — add ordering-sensitive tests
  covering duplicate values, empty groups, and stable ordering.
- `/tmp/tsq/ql/desugar/desugar.go` — verify `rank` desugaring passes
  the ordering expression through; adjust if it was assumed scalar.
- `/tmp/tsq/ql/eval/seminaive.go` — confirm aggregate dispatch handles
  relation-valued results (read-only check; may need a one-line patch).

## Implementation steps

1. Read `ql/eval/aggregate.go` lines 1–195 to understand the current
   `computeAggregate` signature and call sites.
2. In `aggregate.go`, change `rank`'s branch: sort `vals` by the
   ordering key provided by the aggregate expression, then return a
   slice of `(rank, value)` pairs. If the current signature cannot
   return multiple tuples, introduce a sibling function
   `computeRankAggregate` that returns `[]binding` and dispatch to it
   from the caller.
3. In the caller (`seminaive.go` aggregate dispatch), branch on
   aggregate kind and feed rank results into the outer relation as
   multiple tuples.
4. Add test `TestRankOrdinal` in `aggregate_test.go` asserting that
   `rank(int i | i in [10, 20, 30] | i)` yields `{1, 2, 3}`, not `{3}`.
5. Add test `TestRankWithTies` asserting documented tie-breaking
   (choose dense rank or standard competition rank — pick one, document
   it in a comment above the branch, and stick to it).
6. Add test `TestRankEmptyGroup` asserting empty input yields no rows.
7. Update the wiki note in `~/Documents/ObsidianVault/Wiki/Tech/tsq-codeql-compat.md`
   to remove the "v1 approximation" caveat after merge.

## Test strategy

- `ql/eval/aggregate_test.go::TestRankOrdinal` — three-value input,
  assert emitted tuples match expected ranks.
- `ql/eval/aggregate_test.go::TestRankWithTies` — duplicate values.
- `ql/eval/aggregate_test.go::TestRankEmptyGroup` — empty input.
- Full suite (`go test ./...`) must remain green; in particular
  `ql/eval/aggregate_test.go` existing `rank` tests must be updated, not
  deleted.

## Acceptance criteria

- [ ] `rank` returns ordinal positions, not group size.
- [ ] Tie-breaking semantics documented in a comment in `aggregate.go`.
- [ ] New tests above pass; existing tests updated where they asserted
  the old approximation behaviour.
- [ ] `go test ./...` green.
- [ ] No changes to `concat`/`strictcount`/`strictsum`.

## Risks and open questions

- The current `computeAggregate` signature returns a single `Value`.
  Rank is inherently multi-tuple. If we keep the scalar contract, the
  desugarer must expand `rank` into a multi-row helper predicate
  instead — heavier change. Decide which at implementation time.
- CodeQL's documented `rank` semantics must be re-read before coding;
  if tsq already has dependent tests pinning the approximation, they
  need updating in the same PR.
- Ordering stability: tsq relations are unordered sets, so the sort key
  must be an explicit expression in the aggregate body, not tuple
  insertion order.

## Out of scope

- `rank` with partitioning (CodeQL supports `rank[grp]` style). Keep
  global-group rank only.
- Performance optimisation of repeated rank evaluations.
- Updating the CodeQL compat spec documentation beyond the one-line
  caveat removal.
