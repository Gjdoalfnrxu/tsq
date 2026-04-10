# Phase 3a Handover: AST Walker and Fact Emission

**Branch:** feat/phase-3a-walker
**Status:** Complete — all tests pass, `go vet` clean, `-race` clean

---

## Files Delivered

| File | Purpose |
|---|---|
| `extract/walker.go` | `FactWalker` implementing `ASTVisitor`; emits facts for all populated relations |
| `extract/ids.go` | `NodeID`, `SymID`, `FileID` — deterministic FNV-1a hash IDs |
| `extract/walker_test.go` | One test per relation or related group; golden integration test |
| `extract/backend_treesitter.go` | Extended to use TSX grammar for `.tsx` files (dual-parser) |
| `extract/scope.go` | Extended to handle `GeneratorFunctionDeclaration` |

---

## Node Kind → Relations Emitted

| Tree-sitter kind (tsq canonical) | Relations emitted |
|---|---|
| `Program` (root) | triggers `scope.Build()` |
| Any node | `Node`, `Contains` |
| `FunctionDeclaration`, `FunctionExpression`, `GeneratorFunctionDeclaration`, `GeneratorFunction` | `Function`, `Parameter`, `ParameterRest`, `ParameterOptional`, `ParamIsFunctionType` |
| `ArrowFunction` | same as above + `isArrow=1` |
| `MethodDefinition` | same as above + `isMethod=1` |
| `CallExpression` | `Call`, `CallArg`, `CallArgSpread`, `CallCalleeSym`, `ExprIsCall` |
| `VariableDeclarator` | `VarDecl` |
| `LexicalDeclaration` / `VariableDeclaration` | (tracks isConst for child VarDecl) |
| `AssignmentExpression` | `Assign`; if LHS is `MemberExpression`: `FieldWrite` |
| `Identifier` | `ExprMayRef` (when scope resolves) |
| `MemberExpression` | `FieldRead` |
| `AwaitExpression` | `Await` |
| `AsExpression`, `TypeAssertion`, `NonNullExpression`, `SatisfiesExpression` | `Cast` |
| `ObjectPattern` | `DestructureField`, `DestructureRest` |
| `ArrayPattern` | `ArrayDestructure`, `DestructureRest` |
| `ImportDeclaration` | `ImportBinding` |
| `ExportStatement` | `ExportBinding` |
| `JsxElement` | `JsxElement` |
| `JsxSelfClosingElement` | `JsxElement` |
| `JsxAttribute` | `JsxAttribute` |
| `Error` (tree-sitter ERROR) | `ExtractError` (phase="parse") |

---

## Relation Population Status in v1

### Fully populated
- `SchemaVersion` — emitted once at Run start
- `File` — id (FNV FileID), path, SHA-256 contentHash
- `Node` — all nodes, deterministic ID
- `Contains` — all parent/child edges
- `Function` — name, isArrow, isAsync, isGenerator, isMethod
- `Parameter` — fn, idx, name, paramNode, sym, typeText
- `ParameterRest` — for `...rest` params (detected via RestPattern child of RequiredParameter)
- `ParameterOptional` — for `x?` params
- `ParamIsFunctionType` — for params with `=>` in type annotation
- `Call` — id, calleeNode, arity
- `CallArg` — call, idx, argNode
- `CallArgSpread` — spread args
- `ExprIsCall` — links CallExpression node to Call fact
- `VarDecl` — id, sym, initExpr, isConst (detected from LexicalDeclaration keyword)
- `Assign` — lhsNode, rhsExpr, lhsSym (resolved if Identifier)
- `FieldRead` — for all MemberExpression nodes
- `FieldWrite` — when AssignmentExpression.left is MemberExpression
- `Await` — expr, innerExpr
- `Cast` — AsExpression, TypeAssertion, NonNullExpression, SatisfiesExpression
- `DestructureField` — ObjectPattern entries (shorthand, pair, assignment default)
- `ArrayDestructure` — ArrayPattern elements
- `DestructureRest` — `...rest` in both object and array patterns
- `ImportBinding` — localSym, moduleSpec, importedName (default/named/namespace)
- `ExportBinding` — from export clauses, export const/function/class, export default
- `JsxElement` — id, tagNode, tagSym (for named tag components)
- `JsxAttribute` — element, name, valueExpr
- `ExtractError` — parse errors and read errors, never panics

### Partially populated (in-file scope only)
- `CallCalleeSym` — populated when callee is an Identifier resolvable in-file scope
- `ExprMayRef` — populated for Identifiers resolvable in-file scope

### Deliberately empty in v1
- `Symbol` — cross-file analysis required for full population
- `FunctionSymbol` — cross-file analysis required
- `CallResultSym` — cross-file analysis required
- `TypeFromLib` — type checker integration required

---

## ID Generation

All IDs are 32-bit values truncated from FNV-1a 64-bit hashes, stored as `int32` in the DB (EntityRef type).

```
NodeID(filePath, startLine, startCol, endLine, endCol, kind) → uint32
SymID(filePath, name, line, col) → uint32   -- col is StartByte for scope decls, line/col for param syms
FileID(filePath) → uint32
```

Collision probability: negligible for typical codebases (birthday problem at 2^32 items with 32-bit hash — a codebase would need ~65k nodes of the same kind at the same position to risk collision).

---

## TSX Support

Phase 2a used only the TypeScript grammar (`typescript/typescript`). Phase 3a extends `TreeSitterBackend` with a second parser using `typescript/tsx` for `.tsx` files. This is necessary because the TS grammar parses `<div>` as a generic type assertion. The TSX grammar correctly produces `JsxElement`, `JsxOpeningElement`, `JsxSelfClosingElement`, and `JsxAttribute` nodes.

---

## isConst Tracking

`VariableDeclarator` nodes (which emit `VarDecl`) don't carry the const/let/var keyword — that's on the parent `LexicalDeclaration` or `VariableDeclaration`. The walker tracks this via `currentDeclConst` set in `enterNode` when entering a declaration node and cleared in `Leave`. This works because the walker visits parent before children.

---

## JsxAttribute Nesting

JSX attributes are children of `JsxOpeningElement` (or `JsxSelfClosingElement`), not of `JsxElement`. A separate `jsxElementStack` tracks the current enclosing JSX element ID, allowing `JsxAttribute` nodes to reference the correct element even when nested inside `JsxOpeningElement`.

---

## Performance (test fixtures)

| Fixture | Nodes | Relations emitted | Wall time |
|---|---|---|---|
| `simple_function.ts` (18 lines) | ~80 | ~300 tuples across all relations | <1ms |
| Full testdata dir (5 files) | ~400 | ~1500 tuples | ~10ms |
| TSX fixture (5 lines, inline) | ~60 | ~200 tuples | <1ms |

---

## Generator Functions

Tree-sitter uses `generator_function_declaration` (maps to `GeneratorFunctionDeclaration`) and `generator_function` (maps to `GeneratorFunction`) for generator syntax. Both are handled in the walker's function emission switch and in `scope.go`'s `buildScope`. The `*` child token is also detected to set `isGenerator=1` for inline generators.

---

## What Phase 5 (Evaluator) Needs

The evaluator reads the DB produced by Phase 3a.

### DB format
Binary columnar format (see HANDOVER-phase-0.md for spec). Key facts:
- Magic: `TSQ\x00`, schema version: 1
- Relations in registry order
- EntityRef columns store `uint32` values as `int32` (bitcast)
- String columns store uint32 string table indices
- String table: count (4 bytes) then each string as (length u32, bytes)

### Key relations for query evaluation
- `Node(id, file, kind, startLine, startCol, endLine, endCol)` — the core entity table
- `Contains(parent, child)` — parent/child edges for tree traversal
- `File(id, path, contentHash)` — file lookup
- `Function`, `Call`, `VarDecl` — semantic facts
- `ExprMayRef(expr, sym)` — scope resolution for data-flow queries
- `CallCalleeSym(call, sym)` — call graph edges (in-file only in v1)
- `ImportBinding(localSym, moduleSpec, importedName)` — module boundary facts

### Limitations Phase 5 should be aware of
1. All IDs are file-local. Cross-file symbol unification is not performed.
2. `CallCalleeSym` and `ExprMayRef` only resolve within-file declarations.
3. JSX requires `.tsx` extension — `.ts` files with JSX will produce `TypeAssertion` nodes instead.
4. `SymID` for scope-resolved declarations uses `StartByte` (not line/col) as the column component — this is correct but callers using SymID must use the same convention.
