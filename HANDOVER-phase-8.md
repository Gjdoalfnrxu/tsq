# Phase 8: Integration Tests — Handover

## What was built

End-to-end integration tests verifying the complete tsq pipeline: extract → serialize → query → compare results.

### Test infrastructure

| Component | Description |
|-----------|-------------|
| `integration_test.go` | Golden test framework + negative tests + performance sanity tests |
| `testdata/projects/` | 5 TypeScript fixture projects |
| `testdata/queries/` | 12 QL query files (10 valid + 2 negative) |
| `testdata/expected/` | 15 golden CSV files |

### Pipeline exercised (no CLI shelling out)

```
TreeSitterBackend.Open() → FactWalker.Run()
  → db.DB.Encode() → db.ReadDB()  [serialization roundtrip]
  → parse.NewParser().Parse()
  → resolve.Resolve(mod, bridgeImportLoader)
  → desugar.Desugar(resolved)
  → plan.Plan(prog, nil)
  → eval.NewEvaluator(execPlan, factDB).Evaluate(ctx)
  → resultToCSV() → compareGolden()
```

### Test projects (5)

1. **`simple/`** — `main.ts` + `utils.ts`: function declarations, calls, imports, arrow functions
2. **`react-component/`** — `App.tsx` + `Button.tsx`: JSX elements, attributes, React patterns
3. **`destructuring/`** — `hooks.ts` + `consumer.ts`: object/array destructuring patterns
4. **`async-patterns/`** — `api.ts` + `handler.ts`: async functions, await expressions, Promise chains
5. **`imports/`** — `index.ts` + `lib.ts` + `consumer.ts`: named imports, default imports, namespace imports, re-exports

### QL queries (10 valid, 2 negative)

All valid queries use bridge classes from the `.qll` files:

| Query | Bridge import | What it tests |
|-------|--------------|---------------|
| `find_all_functions.ql` | `tsq::functions` | Function class + getName() |
| `find_all_calls.ql` | `tsq::calls` | Call class + getArity() |
| `find_calls_gt3_args.ql` | `tsq::calls` | Call + where clause with `>` comparison |
| `find_async_functions.ql` | `tsq::functions` | Function.isAsync() predicate |
| `find_arrow_functions.ql` | `tsq::functions` | Function.isArrow() predicate |
| `find_await_expressions.ql` | `tsq::expressions` | Await class |
| `find_jsx_elements.ql` | `tsq::jsx` | JsxElement class |
| `find_jsx_attributes.ql` | `tsq::jsx` | JsxAttribute class + getName() |
| `find_destructured_bindings.ql` | `tsq::expressions` | DestructureField + getSourceField()/getBindName() |
| `find_imports.ql` | `tsq::imports` | ImportBinding + getModuleSpec()/getImportedName() |

### Golden test cases (15)

Each (project, query) pair has a golden CSV file. Tests are deterministic (sorted rows).

| Test case | Project | Query |
|-----------|---------|-------|
| simple/find_all_functions | simple | find_all_functions.ql |
| simple/find_all_calls | simple | find_all_calls.ql |
| simple/find_calls_gt3_args | simple | find_calls_gt3_args.ql |
| simple/find_arrow_functions | simple | find_arrow_functions.ql |
| simple/find_async_functions | simple | find_async_functions.ql |
| react/find_jsx_elements | react-component | find_jsx_elements.ql |
| react/find_jsx_attributes | react-component | find_jsx_attributes.ql |
| destructuring/find_destructured_bindings | destructuring | find_destructured_bindings.ql |
| destructuring/find_arrow_functions | destructuring | find_arrow_functions.ql |
| async/find_async_functions | async-patterns | find_async_functions.ql |
| async/find_await_expressions | async-patterns | find_await_expressions.ql |
| async/find_all_functions | async-patterns | find_all_functions.ql |
| async/find_imports | async-patterns | find_imports.ql |
| imports/find_imports | imports | find_imports.ql |
| imports/find_all_functions | imports | find_all_functions.ql |

### Additional test functions (8)

| Test | Description |
|------|-------------|
| `TestExtractionDBRoundtrip` | Encode/decode roundtrip preserves query results (5 projects) |
| `TestNegativeSyntaxError` | Query with syntax error → parse error |
| `TestNegativeUnresolvedName` | Query with unknown type → resolve/desugar/plan error |
| `TestEmptyProject` | Empty dir → valid empty DB, queries return 0 rows |
| `TestPerformanceExtraction` | 5 projects extract in <10s each |
| `TestPerformanceQuery` | 3 queries evaluate in <5s each |
| `TestMultipleQueriesSameDB` | Multiple queries on same DB produce independent results |
| `TestExtractionProducesExpectedRelations` | 7 (project, query) pairs return ≥1 row |

### Golden file regeneration

```bash
go test -run TestGolden -count=1 -update
```

## Pipeline fixes discovered during integration testing

Integration testing was the first time the full pipeline (extract → bridge import → query) was exercised end-to-end. Three bugs were found and fixed:

### 1. Parser: `@type` syntax not supported (`ql/parse/parser.go`)

Bridge `.qll` files use `extends @node`, `extends @function`, etc. for database entity types. The parser's `parseTypeRef()` only accepted identifiers, not `@`-prefixed types. Fixed by checking for `TokAt` before `TokIdent` in `parseTypeRef()`.

### 2. Resolver: `@type` references flagged as undefined (`ql/resolve/resolve.go`)

`resolveTypeRef()` treated `@node` etc. as undefined types. Fixed by skipping validation for `@`-prefixed names (database entity types are always valid).

### 3. Desugarer: imported module classes not desugared (`ql/desugar/desugar.go`)

The desugarer only processed classes from `d.mod.AST.Classes` (the user's query file). Bridge classes like `Function`, `Call`, etc. from imported modules were never desugared into Datalog rules. Fixed by iterating `d.env.Imports` and desugaring imported classes/predicates before the main module.

### 4. Desugarer: `@type` supertypes generated invalid body literals (`ql/desugar/desugar.go`)

`superTypeConstraints()` generated `Literal{Predicate: "@node"}` for `extends @node`. No relation named `@node` exists. Fixed by skipping `@`-prefixed supertypes in constraint generation.

### 5. Planner: equality comparisons didn't propagate variable bindings (`ql/plan/validate.go`)

Bridge methods like `string getFunction() { result = this }` produce a rule where `result` appears only in an equality comparison, not a positive atom literal. The safety checker flagged this as unsafe. Fixed by propagating bindings transitively through `=` comparisons and treating synthetic `arith(...)` pseudo-variables as bound.

## Known issues captured by golden tests

- **Duplicate rows:** The semi-naive evaluator produces duplicate result rows (e.g., each function name appears twice). This is captured in the golden files as current behavior. Future deduplication improvements will be visible as golden file diffs.

## Test statistics

- **Total test cases:** 23 (15 golden + 8 other)
- **Total projects × queries exercised:** 15
- **Bridge imports tested:** `tsq::functions`, `tsq::calls`, `tsq::expressions`, `tsq::jsx`, `tsq::imports`
- **All tests pass:** `go test ./... -count=1` green
