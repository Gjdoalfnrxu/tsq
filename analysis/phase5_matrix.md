# Phase 5: Connascence Matrix

## Matrix

| FROM | TO | FORM | DEGREE | DIRECTION | VERDICT |
|------|-----|------|--------|-----------|---------|
| extract/ | extract/schema/ | CoN | 78 | uni | Necessary structure, accidental string-based name coupling on ~70 relation names. Generate typed emit helpers. |
| extract/ | extract/rules/ | CoP | 120 | uni | **Accidental.** Positional args in Datalog rules must match schema column order. Strongest accidental coupling in the codebase. Named-column rule builder needed. |
| extract/ | extract/typecheck/ | CoT | 8 | uni | Clean. Typed structs, minimal surface. No action needed. |
| extract/ | extract/db/ | CoM | 12 | uni | **Accidental.** `AddTuple(interface{}...)` trades type safety for convenience. Generate typed per-relation insert methods. |
| ql/parse/ | ql/ast/ | CoT | 30 | uni | **Necessary.** Parser-produces-AST is inherent. Clean boundary. |
| ql/ast/ | ql/resolve/ | CoI | 28 | uni | **Accidental** CoI via pointer-identity annotation maps. Fragile under AST transformations. Use node IDs as keys. |
| ql/resolve/ | ql/desugar/ | CoI | 36 | uni | **Accidental** CoI inherited from resolve's annotation maps. Same fix as 1.6. CoA on member lookup already resolved (heritage.go). |
| ql/desugar/ | ql/datalog/ | CoT | 12 | uni | **Necessary.** Clean producer-consumer of IR types. |
| ql/datalog/ | ql/plan/ | CoT | 15 | uni | **Necessary.** Plan embeds datalog types; clean design. |
| ql/plan/ | ql/eval/ | CoT+CoM | 28 | uni | Mostly necessary. Cross-subsystem dep (eval->extract/db) is **accidental**. Extract fact-loading adapter. |
| extract/schema/ | bridge/ | CoN | 110 | bi (implicit) | **Accidental** triple redundancy: schema names, manifest entries, .qll predicates. Generate manifest and stubs from schema. |
| bridge/ | ql/ (imports) | CoN+CoV | 60 | bi | **Accidental** CoV: path-to-file map duplicated between bridge/embed.go and cmd/tsq/main.go. Deduplicate. |
| cmd/tsq/ | everything | CoEx | 40+ | uni | Mostly **necessary** (orchestrator). Some accidental: domain knowledge (nonTaintablePrimitives) in wrong package. |
| output/ | ql/eval/ | CoT+CoM | 6 | uni | Mostly **necessary**. CoM on SARIF column-name heuristics is **accidental**. Define explicit location annotations. |

## Strength Ranking (strongest to weakest accidental coupling)

1. **extract/ <-> extract/rules/ (CoP, degree 120)** -- Positional argument coupling between Datalog rules and schema column definitions. A column reorder silently breaks all rules referencing that relation. This is the highest-risk boundary.

2. **extract/schema/ <-> bridge/ (CoN, degree 110)** -- Triple name redundancy (schema registry, capability manifest, .qll file predicates). Adding a relation requires three manual updates. High maintenance cost.

3. **ql/ast/ <-> ql/resolve/ <-> ql/desugar/ (CoI, degree 64 combined)** -- Pointer-identity-based annotation maps create fragile coupling. Any AST transformation that creates new node objects breaks the annotation lookup silently.

4. **bridge/ <-> ql/ via cmd/tsq/ (CoV, degree 60)** -- Duplicated path-to-file mapping between `bridge/embed.go` and `cmd/tsq/main.go`. A new bridge file requires updating both maps.

5. **extract/ <-> extract/db/ (CoM, degree 12)** -- The `interface{}` variadic tuple API means callers must know column type semantics without compiler help.

6. **output/ <-> ql/eval/ (CoM, degree 6)** -- Column-name heuristics in SARIF output assume semantic meaning of column names. Low degree, low risk.

## Cross-Subsystem Dependencies

The codebase has two major subsystems:
- **Extract** (extract/, extract/schema/, extract/db/, extract/typecheck/, extract/rules/)
- **Query** (ql/parse/, ql/ast/, ql/resolve/, ql/desugar/, ql/datalog/, ql/plan/, ql/eval/)

Cross-subsystem dependencies:
- `ql/eval/` -> `extract/db/`, `extract/schema/` (for `loadBaseRelations`)
- `extract/rules/` -> `ql/datalog/` (rules package produces datalog IR)
- `bridge/` -> `extract/schema/` (manifest coverage check)

The `ql/eval/ -> extract/db/` dependency is the most architecturally significant cross-subsystem coupling. It prevents the query engine from being tested independently of the extraction layer.

The `extract/rules/ -> ql/datalog/` dependency is a deliberate design choice: system rules are expressed as Datalog programs that get merged with user queries. This is architecturally sound.

## Refactoring Priority (by impact/effort ratio)

| Priority | Boundary | Action | Impact | Effort |
|----------|----------|--------|--------|--------|
| 1 | bridge/ <-> cmd/tsq/ | Deduplicate path-to-file map | Eliminate CoV, prevent desync | Low |
| 2 | extract/ <-> rules/ | Named-column rule builder | Convert CoP to CoN (120 points) | Medium |
| 3 | extract/schema/ <-> bridge/ | Generate manifest from schema | Eliminate 80+ name duplications | Medium |
| 4 | ql/eval/ <-> extract/db/ | Extract fact-loader adapter | Decouple subsystems | Low |
| 5 | extract/ <-> extract/db/ | Typed emit helpers per relation | Convert CoM to CoT | Medium |
| 6 | ql/ast/ annotations | Use node IDs not pointer identity | Eliminate CoI | Medium |
| 7 | cmd/tsq/ | Move nonTaintablePrimitives to extract/schema/ | Reduce orchestrator knowledge | Low |
| 8 | output/ SARIF | Define explicit location annotation | Eliminate column-name CoM | Low |

## Connascence Spectrum Summary

```
Weakest                                              Strongest
  CoN -------- CoT -------- CoP -------- CoM -------- CoI
   |            |            |            |            |
  schema/     parse/ast    rules/       db/AddTuple  resolve/
  bridge/     desugar/     (120 pts)    eval/load    desugar/
  (110 pts)   plan/eval                 output/SARIF annotations
              typecheck/
```

The codebase is generally well-structured with clean unidirectional boundaries in the QL pipeline (parse -> ast -> resolve -> desugar -> datalog -> plan -> eval). The strongest accidental couplings are concentrated in the extract-side infrastructure (rules positional coupling, schema-bridge name triplication) and the annotation identity coupling in the QL resolver/desugarer.
