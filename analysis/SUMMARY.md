# tsq Connascence Analysis — Summary

**Date:** 13 April 2026
**Scope:** Full codebase (~15.5k LOC Go, 119 source files, 6 packages)

---

## Connascence Matrix

| FROM | TO | FORM | DEGREE | DIR | VERDICT |
|------|-----|------|--------|-----|---------|
| extract/ | extract/schema/ | CoN | 78 | uni | Necessary structure; ~70 string relation names are accidental |
| extract/ | extract/rules/ | **CoP** | **120** | uni | **Highest risk.** Positional args must match schema column order |
| extract/ | extract/typecheck/ | CoT | 8 | uni | Clean. No action needed |
| extract/ | extract/db/ | CoM | 12 | uni | `interface{}` variadic API trades type safety for convenience |
| ql/parse/ | ql/ast/ | CoT | 30 | uni | Clean producer-consumer |
| ql/ast/ | ql/resolve/ | CoI | 28 | uni | Pointer-identity annotation maps — fragile |
| ql/resolve/ | ql/desugar/ | CoI | 36 | uni | Inherited CoI from resolve's annotation maps |
| ql/desugar/ | ql/datalog/ | CoT | 12 | uni | Clean producer-consumer |
| ql/datalog/ | ql/plan/ | CoT | 15 | uni | Clean |
| ql/plan/ | ql/eval/ | CoT+CoM | 28 | uni | Cross-subsystem dep on extract/db is accidental |
| extract/schema/ | bridge/ | CoN | 110 | bi | Triple redundancy: schema, manifest, .qll predicates |
| bridge/ | ql/ | CoN+CoV | 60 | bi | Path-to-file map duplicated in embed.go and main.go |
| cmd/tsq/ | everything | CoEx | 40+ | uni | Orchestrator — mostly necessary |
| output/ | ql/eval/ | CoT+CoM | 6 | uni | Likely SARIF column off-by-one bug (0-based → 1-based) |

---

## Top 5 Findings (by severity)

### 1. Positional Argument Coupling in Datalog Rules (CoP, 120 points)

**Location:** `extract/rules/*.go` ↔ `extract/schema/relations.go`

Datalog rules construct `datalog.Literal` with arguments at hardcoded positions that must match the column ordering in `schema.RelationDef`. Example: `pos("Assign", w(), v("rhsExpr"), v("lhsSym"))` assumes column 0=lhsNode, 1=rhsExpr, 2=lhsSym. Reordering columns in the schema silently breaks all rules referencing that relation with **no compile-time or runtime error** until query results are wrong.

**Fix:** Named-column rule builder that maps column names to positions. Converts CoP → CoN, caught at construction time.

### 2. Triple Name Redundancy in Schema–Bridge Chain (CoN, 110 points)

**Location:** `extract/schema/relations.go` → `bridge/manifest.go` → `bridge/*.qll`

Adding a new fact relation requires three manual updates in three different languages (Go, Go, QL). Bridge `.qll` files also access columns by pure position (`Node(this, _, _, result, _, _, _)` = column 3), compounding CoP on top of CoN.

**Fix:** Generate manifest and .qll stubs from the schema registry. Single source of truth.

### 3. Pointer-Identity Annotation Maps (CoI, 64 points combined)

**Location:** `ql/resolve/resolve.go` → `ql/desugar/desugar.go`

The resolver stores annotations in `map[*ast.ClassDecl]ResolvedInfo` keyed by pointer identity. The desugarer looks them up by the same pointers. If any AST transformation creates new node objects (copy, clone, rewrite), annotation lookup silently fails. This couples resolve and desugar through object identity rather than through data.

**Fix:** Assign stable IDs to AST nodes; key annotation maps by ID instead of pointer.

### 4. Magic String Proliferation ("this", "result", builtins)

**Location:** 4-way across `ql/parse/`, `ql/resolve/`, `ql/desugar/`, `ql/eval/`

The strings `"this"`, `"result"`, and `"super"` carry semantic meaning across four packages with no shared constant. The builtin naming convention (`"__builtin_string_" + methodName`) is synthesised by string concatenation in desugar and matched by registry keys in eval — an implicit CoA.

**Fix:** Define shared constants in `ql/ast/` or a `ql/convention` package.

### 5. Duplicate Path-to-File Maps (CoV)

**Location:** `bridge/embed.go` ↔ `cmd/tsq/main.go`

Both files maintain identical 30-entry maps from import paths to .qll filenames. Adding a new bridge file requires updating both. Low-effort fix, high confidence.

**Fix:** Single map exported from `bridge/`, consumed by `cmd/tsq/`.

---

## Likely Bug Found

**SARIF off-by-one column:** `output/sarif.go` copies the 0-based `startCol` from the fact schema directly into `sarifRegion.StartColumn`, but SARIF 2.1.0 specifies 1-based columns. All SARIF column numbers are off by one.

---

## Architecture Assessment

**Well-designed boundaries:**
- The QL pipeline (parse → ast → resolve → desugar → datalog → plan → eval) has clean unidirectional CoT boundaries. This is textbook compiler pipeline design.
- The `ExtractorBackend` interface cleanly separates tree-sitter and vendored backends. `ErrUnsupported` sentinel enables graceful degradation.
- Error propagation is well-contained — each layer defines its own error shape with no cross-layer error type coupling.

**Architectural concern:**
- `ql/eval/` imports `extract/db/` and `extract/schema/` directly for base fact loading. This couples the query engine to the extraction layer, preventing independent testing. A fact-loader adapter interface would decouple the subsystems.

**Structural observations:**
- `walker_v2.go` is a decorator around `walker.go` (not a fork), but reaches into `FactWalker`'s internal fields directly (CoI) — fragile.
- `scope.go` hardcodes `FunctionKinds` list instead of using the shared `IsFunctionKind()` from `kinds.go` — silent divergence risk.
- `aggregate.go`'s `evalLiterals` duplicates the join loop from `evalJoinSteps` — operates on `datalog.Literal` instead of `plan.JoinStep`.
- `childByField()` is duplicated identically in `scope.go` and `walker.go`.

---

## Refactoring Priority (impact/effort ratio)

| # | Action | Impact | Effort |
|---|--------|--------|--------|
| 1 | Deduplicate path-to-file map (bridge ↔ main) | Eliminate CoV desync risk | Low |
| 2 | Fix SARIF column off-by-one | Correctness bug | Low |
| 3 | Named-column rule builder | Convert 120 CoP points to CoN | Medium |
| 4 | Generate manifest + stubs from schema | Eliminate 110 name duplications | Medium |
| 5 | Extract fact-loader adapter for eval | Decouple subsystems | Low |
| 6 | Shared constants for "this"/"result"/builtins | Eliminate 4-way magic strings | Low |
| 7 | Node IDs for annotation maps (resolve/desugar) | Eliminate CoI fragility | Medium |
| 8 | Typed emit helpers per relation | Convert CoM to CoT | Medium |

---

## Connascence Spectrum

```
Weakest                                              Strongest
  CoN -------- CoT -------- CoP -------- CoM -------- CoI
   |            |            |            |            |
  schema/     parse/ast    rules/       db/AddTuple  resolve/
  bridge/     desugar/     (120 pts)    eval/load    desugar/
  (110 pts)   plan/eval                 output/SARIF annotations
              typecheck/
```

The strongest accidental couplings are concentrated in two areas:
1. **Extract-side infrastructure** — rules positional coupling, schema-bridge name triplication
2. **QL resolver/desugarer** — pointer-identity annotation maps

The QL pipeline itself is well-layered. The output layer is clean. The CLI has expected orchestration coupling with some domain logic that should be pushed down.

---

## Detailed Analysis Files

- `phase1_boundaries.md` — All 14 inter-package boundaries
- `phase2_intrapackage.md` — Intra-package coupling (extract/, ql/eval/, ql/parse/, extract/rules/)
- `phase3_hotspots.md` — Deep dives on 8 files >500 LOC
- `phase4_crosscutting.md` — Entity IDs, source positions, relation names, QL semantics, error propagation
- `phase5_matrix.md` — Connascence matrix and refactoring priorities
