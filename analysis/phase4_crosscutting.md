# Phase 4: Cross-Cutting Connascence Analysis

## 4.1 Entity ID Generation and Referencing

### The Full Chain

Entity IDs flow through five layers:

1. **Generation** (`extract/ids.go`): Five `uint32` ID generators using FNV-1a hashing truncated from 64 to 32 bits:
   - `NodeID(filePath, startLine, startCol, endLine, endCol, kind)` — AST nodes
   - `SymID(filePath, name, startLine, startCol)` — symbols
   - `FileID(filePath)` — files (prefixed with `"file:"`)
   - `TypeEntityID(typeHandle)` — type entities (prefixed with `"type:"`)
   - `PositionNodeID(filePath, line, col)` — position-only nodes (prefixed with `"posnode:"`)
   - `ReturnSymID(filePath, fnStartLine, fnStartCol)` — delegates to `SymID` with name `"$return"`

2. **Schema** (`extract/schema/registry.go`): `TypeEntityRef` is declared as a column type distinct from `TypeInt32`, but both map to `int32` in storage. The distinction is purely semantic — it exists in the schema metadata but is never enforced differently at the storage layer.

3. **Serialization** (`extract/db/writer.go`): `AddTuple` accepts `interface{}` values. For `TypeEntityRef` and `TypeInt32` columns, `toInt32()` converts via type-switch supporting `int32`, `int`, `uint32`, `int64`. The `uint32` from ID generators is cast to `int32` (line 151: `return int32(x), true`). **This is a lossy conversion** — IDs with bit 31 set will become negative `int32` values. Both write and read treat the column as 4 bytes of little-endian data, so the round-trip preserves bits, but the Go-level sign interpretation flips.

4. **Deserialization** (`extract/db/reader.go`): Reads 4 bytes as `uint32`, then casts to `int32` (line 122: `int32(le.Uint32(...))`). Bit-preserving round-trip confirmed.

5. **Evaluation** (`ql/eval/eval.go`): `loadBaseRelations` widens `int32` to `int64` for the eval `IntVal` type (line 58: `IntVal{V: int64(v)}`). Entity refs and plain integers become indistinguishable `IntVal` values at the eval layer.

### Connascence Analysis

| Form | Instances | Severity |
|------|-----------|----------|
| **CoA (Algorithm)** | All five ID generators must use FNV-1a with the same field ordering, separator bytes, and truncation. Anyone producing an ID that refers to the same entity must replicate the exact algorithm. `walker.go` calls `NodeID(fw.filePath, node.StartLine(), node.StartCol(), node.EndLine(), node.EndCol(), node.Kind())` — the argument order must exactly match `ids.go`. | **High** |
| **CoA (Algorithm)** | `PositionNodeID` and `NodeID` generate different IDs for the same node (different prefixes, different input fields). The `enrichWithTsgo` function in `main.go` uses `PositionNodeID` to link ExprType tuples, while the walker uses `NodeID`. These only match if a QL query never needs to join them — which is exactly the design, but it's an implicit contract. | **Medium** |
| **CoT (Type)** | The `uint32 → int32 → int64` widening chain creates a type connascence. IDs generated as `uint32` are stored as `int32` (reinterpreted bits, not value-preserving for large IDs), then widened to `int64` in eval. The round-trip works because all conversions preserve the bit pattern, but this is fragile — any intermediate code that does arithmetic on the `int32` value (e.g., comparison, sorting) will get wrong results for "negative" IDs. | **Medium** |
| **CoM (Meaning)** | The semantic distinction between `TypeEntityRef` and `TypeInt32` exists only in schema metadata. At eval time, both are `IntVal`. A query cannot distinguish an entity reference from a plain integer. This is deliberate (Datalog doesn't have types), but means ID semantics are entirely implicit. | **Low** |
| **CoV (Value)** | Hash collisions. FNV-1a-64 truncated to 32 bits has ~50% collision probability at ~77k entities (birthday bound). For large TypeScript projects this is a real concern. No collision detection exists. | **Medium** |

### ID Semantics: Implicit, Not Documented

- **Not monotonic**: Hash-based, order depends on input strings.
- **Not globally unique**: Only unique per-entity-class by construction (different prefixes). But `NodeID` and `SymID` share the `TypeEntityRef` column type with no namespace separation at the storage level.
- **Per-file?** No — `FileID` includes the path, `NodeID` includes the path, so they're project-scoped. But `TypeEntityID` is path-independent (keyed by type handle string).
- **No documentation** of these properties exists. The contracts are entirely implicit in the hash function implementations.

---

## 4.2 Source Position Threading

### Position Representation at Each Layer

| Layer | Line Basis | Column Basis | Representation |
|-------|-----------|-------------|----------------|
| tree-sitter (backend) | 0-based (rows) | 0-based bytes | `TSPoint{Row, Column}` |
| `ASTNode` interface | **1-based** | **0-based byte** | `StartLine() int`, `StartCol() int` (documented in `backend.go:70-73`) |
| `tsgoNode` adapter | **1-based** | **0-based byte** | Same contract (documented in `tsgonode.go:17-20`) |
| `extract/scope.go` Declaration | **1-based** | **0-based byte** | `StartLine int`, `StartCol int` (documented in `scope.go:19-20`) |
| Schema (Node relation) | **1-based** | **0-based byte** | `startLine TypeInt32`, `startCol TypeInt32` (stored as-is from ASTNode) |
| Schema (ExtractError) | **1-based** | N/A | `nodeStartLine TypeInt32` |
| `ql/ast.Span` | 1-based | 1-based character | `StartLine`, `StartCol`, `EndLine`, `EndCol` (no documentation of basis) |
| QL lexer `Token` | **1-based** | **1-based character** | `Line int`, `Col int` (lexer.go initializes `line: 1, col: 1`) |
| SARIF output | **1-based** (SARIF spec) | **1-based** (SARIF spec) | `StartLine`, `StartColumn` |
| `typecheck.Position` | 1-based | 0-based | Used for tsgo enrichment queries |

### Connascence Analysis

| Form | Issue | Severity |
|------|-------|----------|
| **CoM (Meaning)** | **Column basis mismatch between extraction and QL AST.** The extraction layer uses 0-based byte columns (from tree-sitter), stored directly into the Node relation. The QL parser/lexer uses 1-based character columns. These are different coordinate systems. A QL query that reads `startCol` from the Node relation gets a 0-based byte offset, while `Span.StartCol` in the QL AST is 1-based character. They never interact directly (one is TypeScript source positions, the other is QL source positions), but the naming overlap (`startCol` vs `StartCol`) creates confusion risk. | **Low** (currently non-interacting) |
| **CoM (Meaning)** | **SARIF output does not adjust column basis.** `sarif.go:178` copies `IntVal.V` directly into `sarifRegion.StartColumn`. SARIF spec requires 1-based columns. The stored `startCol` is 0-based. **This is a bug**: SARIF consumers will see columns off by one. | **High** |
| **CoA (Algorithm)** | The `NodeID` hash includes `startLine` and `startCol` as decimal strings. The `PositionNodeID` also includes line and col. Both depend on the ASTNode interface returning consistent values. If tree-sitter and tsgo report different positions for the same node (which they can — tsgo uses byte offsets internally, adapter converts), the IDs won't match. The `enrichWithTsgo` path in `main.go` (line 285) uses `PositionNodeID` with tsgo-reported positions, creating a join dependency on position agreement between backends. | **Medium** |
| **CoV (Value)** | `StartLine` and `EndLine` must satisfy `EndLine >= StartLine`, and when equal, `EndCol >= StartCol`. This constraint is implicit — no validation exists. | **Low** |

### Position Survival Path

```
tree-sitter (0-based row, 0-based byte col)
  → TreeSitterBackend.nodeFromSitter() adds 1 to row → (1-based line, 0-based byte col)
    → walker.go Enter() → NodeID() → stored in Node relation as int32
      → db.Encode() → binary format
        → db.ReadDB() → int32
          → eval.loadBaseRelations() → IntVal{V: int64(v)}
            → query select → ResultSet row
              → output/sarif.go → sarifRegion.StartLine, .StartColumn
```

The key observation: positions are opaque integers after extraction. No layer between extraction and SARIF output interprets or transforms them. The SARIF column off-by-one bug survives because no layer knows the semantic convention.

---

## 4.3 Relation Name Contracts

### The Chain of Agreement

Four artifacts must agree on every relation name:

1. **`extract/schema/relations.go`** — Canonical source of truth. `init()` calls `RegisterRelation(RelationDef{Name: "Node", ...})` for every relation. This is the single authoritative registry.

2. **`extract/walker*.go`** — Emits tuples by calling `fw.emit("Node", ...)` with string literal relation names. The `emit` method calls `db.Relation(name)` which calls `schema.Lookup(name)` and **panics** if the name isn't registered. This provides a runtime check but no compile-time safety.

3. **`bridge/*.qll`** — References relation names as bare predicates: `Node(this, _, _, _, _, _, _)`. The QL parser treats these as predicate calls. At eval time, the evaluator looks up relations by name in the `baseRels` map loaded from the DB.

4. **`bridge/manifest.go`** — Maps QL class names to relation names: `{Name: "ASTNode", Relation: "Node", File: "tsq_base.qll"}`. This is used by `AllRelationsCovered()` to verify completeness.

### Tests That Guard the Chain

| Test | What It Checks | Gap |
|------|---------------|-----|
| `TestAllRelationsCovered` (manifest_test.go) | Every relation in `schema.Registry` has a corresponding entry in the manifest's `Available` or `Unavailable` list | Does NOT check that the manifest's `Relation` field matches actual .qll predicate usage |
| `TestAvailableClassesHaveFiles` | Every available class has a non-empty `Relation` and `File` field | Does NOT check that the .qll file actually contains the named predicate |
| `TestManifestAvailableNamesUnique` | No duplicate class names in the manifest | Does NOT check relation name uniqueness |
| `db.Relation()` panic guard | Runtime: panics if you emit to an unregistered relation | Catches walker typos at test time, not at compile time |
| Walker tests (`walker_test.go`) | End-to-end: extract TypeScript, check tuple counts by relation name | Only covers relations exercised by test fixtures |

### Blast Radius of a Relation Rename

**Severity: High. A relation rename requires coordinated changes across 5+ files with no compiler enforcement.**

If `"Node"` were renamed to `"AstNode"`:

1. `extract/schema/relations.go` — Change `RegisterRelation(RelationDef{Name: "Node", ...})` → `"AstNode"`
2. `extract/walker.go` — Change every `fw.emit("Node", ...)` call (and `TypeAwareWalker`)
3. `bridge/tsq_base.qll` — Change `Node(this, _, ...)` to `AstNode(this, _, ...)`
4. `bridge/manifest.go` — Change `Relation: "Node"` to `Relation: "AstNode"` in the manifest entry
5. `extract/db/writer.go` — No change needed (names are data, not code)
6. `cmd/tsq/main.go` — Change `collectEnrichmentPositions` which hardcodes column indices by calling `database.Relation("Node")`
7. Every `.ql` query file that references the `Node` predicate via the bridge

The chain is held together by string equality with no type-safe wiring. The manifest's `AllRelationsCovered` test would catch a mismatch between schema and manifest, but nothing catches a mismatch between manifest and `.qll` file content.

### Connascence

| Form | Description | Severity |
|------|-------------|----------|
| **CoN (Name)** | All four artifacts (schema, walker, bridge .qll, manifest) must use identical string names for each relation | **High** — 80+ relation names, all string-based |
| **CoP (Position)** | Bridge .qll predicates reference columns by position: `Node(this, _, _, result, _, _, _)` means column 3 = startLine. This must match the column order in `relations.go`. No names, pure position. | **High** |
| **CoV (Value)** | Arity must match: `Node(this, _, _, _, _, _, _)` has 7 args, which must equal the 7 columns in the Node relation definition | **High** |

---

## 4.4 QL Language Semantics Shared Knowledge

### Magic Strings Across Boundaries

| String | Where Used | Semantics |
|--------|-----------|-----------|
| `"this"` | **parse** (lexer keyword `TokKwThis` → parser emits `Variable{Name: "this"}`), **resolve** (pre-bound in class scope, special-cased in `resolveVariable`), **desugar** (injected as `datalog.Var{Name: "this"}` in class rule heads), **eval** (builtins use `atom.Args[0]` as "this" by convention — `__builtin_string_length(this, result)`) | Implicit receiver identity |
| `"result"` | **parse** (keyword `TokKwResult` → `Variable{Name: "result"}`), **resolve** (bound when method/predicate has ReturnType), **desugar** (added to head args for return-type predicates), **eval** (builtins bind result via `atom.Args[1]` or similar) | Implicit return value identity |
| `"super"` | **parse** (keyword `TokKwSuper` → `Variable{Name: "super"}`), **resolve** (special-cased: resolves to first supertype), **desugar** (line 984/1058: special handling for super method calls, delegates to parent class predicate) | Parent class reference |
| `"none"` | **parse** (keyword `TokKwNone` → `None{}` AST node), **resolve** (no special handling), **desugar** (emitted as empty body = always false), **eval** (no special handling needed — empty relation) | Always-false formula |
| `"_"` | **datalog** (Wildcard type), **eval** (builtins check `v.Name != "_"` before binding in `bindResult`), **desugar** (emitted for don't-care positions) | Wildcard/don't-care |
| `"__builtin_string_*"` | **desugar** (synthesizes `"__builtin_string_" + methodName` for string method calls, line 716/960), **eval** (`builtinRegistry` maps these to Go functions) | Method dispatch by name convention |
| `"@"` prefix | **resolve** (line 398: `@`-prefixed types are always valid, no class declaration needed), **bridge .qll** (classes use `extends @node`, `extends @file` etc.) | Database entity types |

### Semantic Knowledge Distribution

```
parse/  → Knows: syntax, keywords, operator precedence, AST structure
resolve/ → Knows: scoping rules, type references, "this"/"result"/"super" binding, @-type convention
desugar/ → Knows: class-to-Datalog lowering, "this"/"result" as head args, builtin name synthesis, super dispatch
eval/   → Knows: semi-naive fixpoint, join evaluation, builtin dispatch, value types (IntVal/StrVal)
```

### Duplicated/Shared Semantic Knowledge

| Knowledge | Shared Between | Form |
|-----------|---------------|------|
| "this" is always first arg of class predicates | resolve (binds it), desugar (puts it in head position 0) | **CoP** — position 0 is hardcoded in both |
| "result" is last arg of return-type predicates | resolve (binds it), desugar (appends to head args) | **CoP** |
| String methods become `__builtin_string_<name>` | desugar (name construction), eval (registry keys) | **CoA** — both must agree on the naming algorithm |
| `@`-prefixed types are entity types | resolve (skips validation), bridge .qll (uses in `extends`) | **CoM** — the `@` prefix carries semantic meaning |
| Aggregate function names ("count", "min", etc.) | parse (keywords), datalog IR (Aggregate.Func string), eval (aggregate evaluation) | **CoN** — string names must match across three packages |
| Comparison operators ("=", "!=", "<", etc.) | parse (tokens), ast (Comparison.Op string), datalog (Comparison.Op string), eval (Compare function switch) | **CoN** across four packages |

### No Contradictions Found

The semantic knowledge is well-partitioned: each layer adds its own interpretation without contradicting others. The main risk is that the `__builtin_string_*` naming convention is an implicit CoA between desugar and eval with no shared constant or type.

---

## 4.5 Error Propagation Patterns

### Error Types by Layer

| Layer | Error Type | How Created |
|-------|-----------|-------------|
| `extract/` | `ErrUnsupported` (sentinel `errors.New`) | Returned by semantic methods when backend doesn't support operation |
| `extract/` | `fmt.Errorf` wrapping | Returned by `walker.Run`, backend operations |
| `extract/schema/` | `fmt.Errorf` | `RelationDef.Validate()` |
| `extract/db/` | `fmt.Errorf` with `%w` | Reader/writer errors wrap underlying IO errors |
| `ql/parse/` | Single `error` return from `Parse()` | `fmt.Errorf` with position info |
| `ql/resolve/` | `[]resolve.Error` (custom type with Span) | Accumulated non-fatally, returned in `ResolvedModule.Errors` |
| `ql/resolve/` | `[]resolve.Warning` (custom type with Span) | Accumulated alongside errors |
| `ql/desugar/` | `[]error` | Accumulated via `errorf`, returned alongside program |
| `ql/plan/` | `[]error` | From `ValidateRule` and stratification |
| `ql/eval/` | Single `error` return | `fmt.Errorf` with `%w` wrapping |
| `output/` | Single `error` return | From JSON/CSV encoding |

### Error Propagation to CLI

In `cmd/tsq/main.go`, the `compileAndEval` function is the primary pipeline:

```
Parse error → fmt.Errorf("parse: %w", err) → single error return
Resolve errors → collected into string list → fmt.Errorf("resolve errors:\n  ...") 
Desugar errors → collected into string list → fmt.Errorf("desugar errors:\n  ...")
Plan errors → collected into string list → fmt.Errorf("plan errors:\n  ...")
Eval error → fmt.Errorf("evaluate: %w", err) → single error return
```

The `cmdCheck` function handles each phase separately, printing errors to stderr and continuing to the next phase. This means check reports errors from ALL phases, while query stops at the first failing phase.

### `ErrUnsupported` Sentinel

**Definition:** `extract/backend.go:11` — `var ErrUnsupported = errors.New("operation not supported by backend")`

**Producers:**
- `TreeSitterBackend.ResolveSymbol/ResolveType/CrossFileRefs` — always returns `ErrUnsupported`
- `VendoredBackend.ResolveSymbol/ResolveType/CrossFileRefs` — returns `ErrUnsupported` when `!b.tsgoAvailable`
- `VendoredBackend.rpc()` — returns `ErrUnsupported` when tsgo unavailable

**Consumers:**
- `extract/vendored_scope.go:70` — `// Any error (including ErrUnsupported) means we fall back.` Treats it as "not available, degrade gracefully."
- Test files: Check with `errors.Is(err, ErrUnsupported)` to verify graceful degradation.

**Notably absent:** No consumer in the CLI (`main.go`) checks for `ErrUnsupported` — it's fully handled within the extract layer. The walker never surfaces it to the CLI.

### Connascence Analysis

| Form | Description | Severity |
|------|-------------|----------|
| **CoM (Meaning)** | `ErrUnsupported` carries implicit meaning: "this is expected, degrade gracefully, don't treat as failure." Callers must know this convention. Well-documented but not enforced by type system. | **Low** — small, well-contained |
| **CoT (Type)** | Each layer defines its own error type: `resolve.Error` (with Span), `desugar` uses plain `error`, `plan` uses plain `error`. The CLI must handle both `[]error` (from resolve/desugar/plan) and single `error` (from parse/eval). No shared error interface for positioned errors. | **Medium** |
| **CoM (Meaning)** | The resolve layer's distinction between `Errors` (fatal) and `Warnings` (non-fatal) is a semantic convention. Warnings include deprecation notices. The CLI (`cmdCheck`) prints both to stderr but only counts errors for exit code. The `cmdQuery` path (`compileAndEval`) checks `len(resolved.Errors) > 0` but silently drops warnings. | **Low** |
| **CoEx (Execution)** | Error checking order matters: in `compileAndEval`, resolve errors are checked before desugar runs, but `Desugar(resolved)` is called even when `resolved.Errors` is non-empty (desugar gets a valid program because resolve continues past errors). This is safe because desugar operates on the AST, not on resolution results. But in `cmdCheck`, all phases run regardless of earlier errors, meaning plan validation runs on a potentially malformed program. | **Low** — intentional for check mode |

### Missing: Structured Error Codes

There are no error codes or typed error categories beyond `ErrUnsupported`. All errors are string-formatted. This means:
- Programmatic error handling is limited to `errors.Is(err, ErrUnsupported)` and `len(errors) > 0`
- Error messages are the only "API" for distinguishing error causes
- No CoN on error names/codes exists because there are no error names/codes

---

## Summary: Cross-Cutting Risk Matrix

| Concern | Highest Connascence | Risk Level | Key Finding |
|---------|-------------------|------------|-------------|
| **4.1 Entity IDs** | CoA (algorithm replication across callers) | **Medium** | Hash collision probability for large projects; uint32→int32 sign-flip is correct but brittle |
| **4.2 Positions** | CoM (column basis meaning) | **High** | SARIF output likely has off-by-one column bug (0-based stored, 1-based required by SARIF spec) |
| **4.3 Relation names** | CoN + CoP (names and column positions across 4 artifacts) | **High** | 80+ relation names held together by string equality; .qll files use positional column access with no compile-time check |
| **4.4 QL semantics** | CoA (builtin name synthesis) | **Medium** | `__builtin_string_*` convention is implicit CoA between desugar and eval; "this"/"result" position conventions span 3 packages |
| **4.5 Error propagation** | CoT (heterogeneous error types) | **Low** | Each layer defines its own error shape; ErrUnsupported is well-contained; no structured error codes |
