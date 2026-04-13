# Plan 22 — E2E Phase 3 Fixtures

**Status:** not started
**Phase:** E2E test coverage expansion
**Dependencies:** Plan 21 (field-sensitive taint) for field-sensitive fixture;
Plans 18-20 for full sink coverage in deep-call-chain fixture

## Scope

Create three new compat fixture projects and corresponding queries/goldens:

### 22a. `testdata/compat/projects/deep-call-chain/`

Stress test inter-procedural taint flow through nested call chains.

```
src/
  entry.ts    — Express handler, reads req.query
  layer1.ts   — transform(data) → calls layer2
  layer2.ts   — process(data) → calls layer3
  layer3.ts   — format(data) → calls sink
```

Tests FlowStar transitive closure through 3+ function call levels.
Query: find TaintAlerts where source is http_input.

### 22b. `testdata/compat/projects/field-sensitive/`

Field-sensitive taint tracking patterns.

```
src/
  app.ts — Express routes using:
    - const id = req.query.id → db.query (field access)
    - const { name } = req.body → res.send (destructuring)
    - obj.field = tainted; use(obj.field) (field write/read)
```

**Blocked by Plan 21.** Create fixture now, skip test until Plan 21 lands.

### 22c. `testdata/compat/projects/koa-app/`

Non-Express framework to test Koa-specific framework rules.

```
src/
  app.ts — Koa app with ctx.request.query → ctx.body flow
```

Tests that the framework detection isn't Express-only.

## Acceptance criteria

- Each fixture has a QL query and committed golden
- deep-call-chain tests pass end-to-end
- field-sensitive test is skipped with documented reason
- koa-app tests pass if Koa framework rules exist, skipped otherwise
- Add all three to `compatTestCases()`
