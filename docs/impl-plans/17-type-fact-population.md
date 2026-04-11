# Plan 17: Populate type facts during extraction

## Scope

Register the missing type-fact relations and wire the extractor / enricher to actually write tuples into them at extraction time. Unblocks plans 11, 12 (typed-ts fixtures + checker tests) and retroactively gives plans 3c (`bridge/tsq_types.qll`) and 3d (type-aware dataflow) a non-empty extent to operate against.

Does NOT deliver: new bridge classes in `bridge/tsq_types.qll`, new type-aware dataflow rules, or changes to the tsgo enricher's wire protocol. This plan is strictly about (a) registering the missing relations in the schema and (b) writing tuples from the values the enricher already returns.

## Dependencies

None. The tsgo enricher (`extract/typecheck/enricher.go`) already returns `TypeFact` values — this plan wires them into the fact DB. Plan 04 (direct Go API) is independent and can land in either order.

## Context

Audit of current state (as of this plan):

**Registered relations** (`extract/schema/relations.go`): `ExprType`, `SymbolType`. These are the only two type relations in the schema.

**Missing from the schema** (called for in `CODEQL-COMPAT-PLAN.md` §3b): `TypeInfo`, `TypeMember`, `UnionMember`, `IntersectionMember`, `GenericInstantiation`, `TypeAlias`, `TypeParameter`.

**Population status of the two that do exist:** zero. `extract/walker_v2.go` contains a comment noting type relations are left empty; `extract/typecheck/enricher.go` returns `TypeFact` values but nothing writes those values into the fact DB as datalog tuples; `TestV2ExprTypeEmpty` literally pins `ExprType` at zero tuples. So 3c (`bridge/tsq_types.qll`) and 3d (`extract/rules/callgraph.go`, `extract/rules/taint.go`) both query empty relations — they compile, they "work," they produce nothing.

## Files to change

- `extract/schema/relations.go` — register the seven missing relations (`TypeInfo`, `TypeMember`, `UnionMember`, `IntersectionMember`, `GenericInstantiation`, `TypeAlias`, `TypeParameter`). Column definitions should match CODEQL-COMPAT-PLAN §3b.
- `extract/walker_v2.go` — remove the "type relations left empty" comment and wire the walker to call into the enricher writeback path.
- `extract/typecheck/enricher.go` — change from "return values" to "write tuples into the fact DB." Add a writeback helper that takes the extractor's fact-emission callback and a `TypeFact` and writes one row per relation that applies.
- `extract/walker_v2_test.go` (or sibling) — replace `TestV2ExprTypeEmpty`'s zero-assertion with a test that extracts a small typed TS file and asserts the expected tuple counts per relation.

## Implementation steps

1. Read `extract/schema/relations.go` around the existing `ExprType` / `SymbolType` registrations. Copy the registration shape for the seven missing relations, matching the columns listed in `CODEQL-COMPAT-PLAN.md` §3b.
2. Read `extract/typecheck/enricher.go` to understand the shape of `TypeFact` and how the tsgo JSON-RPC response is parsed.
3. Add a `writeTypeFact(emit func(relName string, cols ...any), fact TypeFact)` helper that dispatches on `fact.Kind` and writes one or more tuples.
4. In `extract/walker_v2.go`, after the tsgo pass, iterate the returned `TypeFact` values and call `writeTypeFact` for each.
5. Delete or rewrite `TestV2ExprTypeEmpty` — it pins the bug in place. Replace with `TestV2TypeFactsPopulated` asserting non-zero counts per relation against a small typed fixture.
6. Verify `bridge/tsq_types.qll` still compiles against the updated schema. If the new relations' column order doesn't match what `tsq_types.qll` expects, update `tsq_types.qll` to match the schema (it's the junior partner — the schema is the source of truth from the compat plan).
7. Run the existing type-related golden tests. Some may start producing additional rows now that the extent is non-empty; regenerate goldens with scrutiny — check each diff is a true positive before accepting.

## Test strategy

- **Fixture:** `testdata/projects/typed-basic/` containing a small `.ts` file exercising each of the seven relations (a union type, an intersection type, a type alias, a generic instantiation, a type parameter constraint, a class with member types).
- **`TestV2TypeFactsPopulated`**: extracts the fixture, asserts `TypeInfo`, `TypeMember`, `UnionMember`, `IntersectionMember`, `GenericInstantiation`, `TypeAlias`, `TypeParameter`, `ExprType`, `SymbolType` all have non-zero row counts.
- **`TestExprTypeBindsSymbolToType`**: asserts one specific symbol in the fixture is associated with the type the fixture declares for it (not vacuous — the test must break if the enricher starts writing nonsense).
- **Golden regeneration check:** any existing tests that asserted empty type extents must be rewritten, not just re-saved. If a golden changes because the new relations produce rows, verify those rows are semantically correct before accepting the new golden. Do not use golden regeneration as a shortcut to hide a bug.

## Acceptance criteria

- [ ] All seven missing relations registered in the schema.
- [ ] Extraction against the fixture populates every relation with the expected tuples.
- [ ] `TestV2ExprTypeEmpty` is removed (it encoded the bug); replacement test asserts populated state.
- [ ] `go test ./...` passes.
- [ ] `bridge/tsq_types.qll` queries return rows against the fixture (not a golden assertion — just a smoke test that the class extent is non-empty).
- [ ] No existing non-type-related golden silently changes. Any changes must be explained in the PR description.

## Risks and open questions

- The tsgo enricher's `TypeFact` value shape may not carry all the information needed for seven relations. If it only returns `ExprType`/`SymbolType`-equivalent info, this plan needs a paired upstream-enricher change to emit richer facts — escalate at the point the gap is found, don't silently write zero to the missing relations.
- `TestV2ExprTypeEmpty` is acting as a canary for the bug. Removing it is correct, but double-check no other test depends on the empty state (grep for `ExprType` in `*_test.go`).
- Schema version bump: adding relations changes the DB schema version. Check `extract/db/db.go` for how version bumps are handled; readers of older DBs must still work.
- Some of the seven relations may be hard to emit from the enricher without more tsgo API surface. If `TypeParameter` or `GenericInstantiation` turn out to be blocked on the JSON-RPC protocol, land the easy ones in this plan and carve the blocked ones into a follow-up.

## Out of scope

- New classes in `bridge/tsq_types.qll` beyond what already exists (3c is a separate plan).
- New type-aware callgraph / taint rules (3d).
- Changing the tsgo wire protocol or adopting a direct Go API (plan 04).
- Performance optimisation of type-heavy extraction — profile after correctness.
