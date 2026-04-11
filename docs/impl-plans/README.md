# CodeQL Compatibility — Implementation Plans

This directory breaks `CODEQL-COMPAT-PLAN.md` into PR-sized units. Each plan
is independently reviewable and lists its own dependencies, steps, and
acceptance criteria.

## Status legend

- **not started** — no work done
- **in progress** — branch exists, PR not merged
- **done** — merged to `main`
- **already done** — landed before impl plans were written; no plan doc needed

## Phase 1–3 — already landed

Before these plans were written, a deep audit of `origin/main` showed that
the bulk of Phases 1, 2, and 3 are already merged. The table below records
this so contributors don't accidentally re-scope finished work. See the
commit SHAs on `main` for provenance.

| Compat-plan item | Status       | Landed in |
|------------------|--------------|-----------|
| 1a modules       | already done | PR #26 (`phase1-abcd`) |
| 1b disjunction   | already done | PR #26 |
| 1c negation      | already done | PR #26 |
| 1d abstract      | already done | PR #26 |
| 1e string builtins | **partial**  | PR #27 (`phase1-efg`); `.splitAt` missing — see plan 15 |
| 1f if-then-else  | already done | PR #27 |
| 1g closure syntax | already done | PR #27 |
| 1h aggregates    | **partial**  | PR #28; `rank` is a count approximation — see plan 01 |
| 1i forex         | already done | PR #28 |
| 1j super         | already done | PR #28 |
| 1k multi-inherit | already done | PR #28 |
| 1l annotations   | **partial**  | PR #28; parse-only, no semantics — see plans 02, 03 |
| 2a javascript.qll | already done | PR #29 |
| 2b dataflow.qll  | **partial**  | PR #30; API shape only, `hasFlow` ignores user `Configuration` overrides — see plan 16 |
| 2c tainttracking | **partial**  | PR #31; same Configuration-dispatch gap as 2b — see plan 16 |
| 2d security libs | already done | PR #32 |
| 2e import mapping | already done | PR #29 |
| 3a tsgo integration | **partial** | PR #33; implemented via JSON-RPC, not direct Go dep — see plan 04 |
| 3b type facts    | **partial**  | PR #34; only `ExprType`/`SymbolType` registered (7 of 9 relations missing), and both are never populated at extraction time — see plan 17 |
| 3c typed bridge  | **partial**  | PR #35; operates on empty extent because 3b is unpopulated — unblocked by plan 17 |
| 3d typed dataflow | **partial**  | PR #36; operates on empty extent because 3b is unpopulated — unblocked by plan 17 |

## Remaining plans

Merge order is top to bottom. See `DEPENDENCY-GRAPH.md` for the DAG.

| #  | Plan                                           | Phase item(s) | Status      |
|----|------------------------------------------------|---------------|-------------|
| 01 | [rank-aggregate](01-rank-aggregate.md)         | 1h            | not started |
| 02 | [annotation-private](02-annotation-private.md) | 1l            | not started |
| 03 | [annotation-deprecated](03-annotation-deprecated.md) | 1l      | not started |
| 04 | [tsgo-direct-go-api](04-tsgo-direct-go-api.md) | 3a            | not started |
| 05 | [compat-query-fixtures](05-compat-query-fixtures.md) | 4a      | not started |
| 06 | [compat-e2e-harness](06-compat-e2e-harness.md) | 4a            | not started |
| 07 | [compat-find-xss-golden](07-compat-find-xss-golden.md) | 4a    | not started |
| 08 | [compat-find-sqli-golden](08-compat-find-sqli-golden.md) | 4a  | not started |
| 09 | [compat-custom-config-golden](09-compat-custom-config-golden.md) | 4a | not started |
| 10 | [compat-ast-query-golden](10-compat-ast-query-golden.md) | 4a  | not started |
| 11 | [typed-ts-fixtures](11-typed-ts-fixtures.md)   | 4b            | not started |
| 12 | [typecheck-checker-test](12-typecheck-checker-test.md) | 4b    | not started |
| 13 | [stdlib-class-coverage](13-stdlib-class-coverage.md) | 4c      | not started |
| 14 | [adversarial-review-checklist](14-adversarial-review-checklist.md) | 4c | not started |
| 15 | [builtin-splitat](15-builtin-splitat.md)       | 1e residual   | not started |
| 16 | [dataflow-config-dispatch](16-dataflow-config-dispatch.md) | 2b, 2c | not started |
| 17 | [type-fact-population](17-type-fact-population.md) | 3b           | not started |

## How to execute a plan

1. Pick the lowest-numbered plan whose dependencies are all **done**.
2. Read the plan end to end. Open the referenced files. Confirm the plan
   still matches reality — `main` moves.
3. Implement on a branch named after the plan file (e.g. `plan-01-rank`).
4. Run `go test ./...` before opening the PR.
5. Request adversarial review (see plan 14).
6. After merge, flip status to **done** here and record the PR number.
