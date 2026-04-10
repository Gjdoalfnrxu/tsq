# Phase 2a Handover: ExtractorBackend Interface and TreeSitter Backend

**Branch:** feat/phase-2a-treesitter-backend
**Status:** Complete — all tests pass, `go vet` clean

---

## Binding Version

- **go-tree-sitter:** `github.com/smacker/go-tree-sitter@v0.0.0-20240827094217-dd81d9e9be82`
- **TypeScript grammar:** included as `github.com/smacker/go-tree-sitter/typescript/typescript` (same module, subpackage)
- **cgo:** required. Build with `CGO_ENABLED=1`. The environment uses `zig cc` as the C compiler via `/home/cain/.local/bin/cc`. Set `CC=/home/cain/.local/bin/cc` if `gcc` is not in PATH.

---

## Files Delivered

| File | Purpose |
|---|---|
| `extract/backend.go` | `ExtractorBackend` interface, `ASTVisitor`, `ASTNode`, `ProjectConfig`, `SymbolRef`, `SymbolDecl`, `NodeRef`, `ErrUnsupported` |
| `extract/backend_treesitter.go` | `TreeSitterBackend` struct implementing `ExtractorBackend` |
| `extract/scope.go` | `ScopeAnalyzer`, `Scope`, `Declaration` — in-file scope analysis |
| `extract/backend_treesitter_test.go` | Backend tests |
| `extract/scope_test.go` | Scope analysis tests |
| `testdata/ts/*.ts` | Five test fixtures |

---

## Node Kind Normalisation Table

The `normalise()` function in `backend_treesitter.go` maps tree-sitter type strings to tsq PascalCase canonical names. Unknown types fall back to `snakeToPascal()`.

### Explicit map entries (tree-sitter type → tsq canonical)

| tree-sitter type | tsq canonical |
|---|---|
| `function_declaration` | `FunctionDeclaration` |
| `arrow_function` | `ArrowFunction` |
| `call_expression` | `CallExpression` |
| `identifier` | `Identifier` |
| `member_expression` | `MemberExpression` |
| `variable_declarator` | `VariableDeclarator` |
| `import_declaration` | `ImportDeclaration` |
| `import_statement` | `ImportDeclaration` ⚠️ alias — see note |
| `export_statement` | `ExportStatement` |
| `jsx_element` | `JsxElement` |
| `jsx_self_closing_element` | `JsxSelfClosingElement` |
| `as_expression` | `AsExpression` |
| `await_expression` | `AwaitExpression` |
| `assignment_expression` | `AssignmentExpression` |
| `binary_expression` | `BinaryExpression` |
| `object_pattern` | `ObjectPattern` |
| `array_pattern` | `ArrayPattern` |
| `rest_pattern` | `RestPattern` |
| `ERROR` | `Error` |
| + ~45 additional common nodes | (see `kindMap` in `backend_treesitter.go`) |

⚠️ **Import statement grammar note:** The TypeScript grammar in go-tree-sitter uses `import_statement` (not `import_declaration`) for `import ... from` statements. Both map to `ImportDeclaration` in tsq. Callers checking for `ImportDeclaration` will see it correctly. The schema's `ImportBinding` relation population (Phase 3a) must walk `ImportDeclaration` nodes.

### Fallback rule

Any type not in `kindMap` is converted by `snakeToPascal`: split on `_`, capitalise each segment, concatenate. Examples:
- `some_unknown_type` → `SomeUnknownType`
- `jsx_namespace_name` → `JsxNamespaceName`

---

## Scope Analysis Limitations

The `ScopeAnalyzer` in `scope.go` performs purely syntactic, in-file scope analysis. Limitations:

1. **No cross-file resolution.** Import bindings are declared in scope (so `import { x } from '...'` creates a binding for `x`) but there is no way to follow the import to its source file.

2. **`nodeScope` map is byte-keyed and approximate.** The `findScope(byte)` method finds the scope at the closest byte offset ≤ the query point. This works for well-formed code but may return a slightly outer scope for edge cases in deeply nested expressions. Phase 3a should refine this by tracking scope ranges (start byte, end byte) rather than just entry points.

3. **No Type information.** The analyzer understands variable existence, not types. TypeScript type annotations are parsed (they appear in the CST) but not interpreted.

4. **TDZ applies only within the same scope object.** A `let x` in a nested block creates a new `Scope` node. References in sibling scopes correctly go through the parent chain, and TDZ is checked against the declaration's `StartByte` on each `Scope.Resolve` call. Edge case: if the same name appears as `var` in a parent scope and `let` in a child scope, the child's TDZ takes precedence — correct.

5. **`var` hoisting is per-function, not per-file.** `var` declarations inside function bodies are hoisted to the enclosing function scope (the `fnScope` parameter in `buildScope`). File-level `var` is hoisted to the file scope (the initial `fnScope` passed to `Build`). This matches JS semantics.

6. **No `with` statement support.** `with` is not handled (it's banned in strict mode TypeScript anyway).

7. **Class bodies.** Class declarations add the class name to scope but do not analyse method bodies specially — they're treated as function-scoped blocks. Property declarations are not added to any scope (they're accessed via `this`, not by name).

8. **`for` loop variable scoping.** `for (let i = ...)` creates `i` in the block scope of the `for` body via the `LexicalDeclaration` handler. This is correct for block-scoped `let`/`const`. `for (var i = ...)` hoists to function scope — also handled.

9. **Tree lifetime.** `ScopeAnalyzer.Build(root ASTNode)` must be called while the tree-sitter parse tree is still open. In `WalkAST`, this means inside the `Enter` or `Leave` callbacks. After `WalkAST` returns, all `sitter.Node` pointers are invalid. The `*Scope` and `*Declaration` objects returned by `Build` are safe to use after the tree closes — they contain only Go strings and integers.

---

## What Phase 3a (Walker + Fact Emission) Needs

Phase 3a implements `extract/walker.go` as a concrete visitor that emits fact tuples during `WalkAST`. It needs from this phase:

### From `ExtractorBackend` / `ASTNode`

- `WalkAST(ctx, visitor)` — the walker is a visitor. No changes to the interface needed.
- `ASTNode.Kind()` — maps to the `kind` column of the `Node` relation. Use the normalised PascalCase names.
- `ASTNode.StartLine()`, `StartCol()`, `EndLine()`, `EndCol()` — populate the `Node` relation columns.
- `ASTNode.Text()` — used to extract names (function names, variable names, import module specs, etc.)
- `ASTNode.FieldName()` — used to identify named fields (e.g., `"name"` field on `function_declaration`, `"body"` field on arrow functions).
- `ASTNode.ChildCount()` / `ASTNode.Child(i)` — used to walk subtrees for specific patterns (e.g., finding the callee of a call_expression).

### From `ScopeAnalyzer`

- `NewScopeAnalyzer(filePath)` + `Build(root ASTNode)` — build scope per file inside `EnterFile`/first `Enter`.
- `Scope.Resolve(name, atByte int)` — resolve references for the `ExprMayRef` relation (in-file only; cross-file is ErrUnsupported from the backend).
- `Declaration.StartByte` — used as the entity ID seed for symbol entities.

### Important node type observations for fact emission

These are tree-sitter grammar realities discovered in Phase 2a that Phase 3a must account for:

| Fact relation | Key node type(s) | Notes |
|---|---|---|
| `Function` | `FunctionDeclaration`, `FunctionExpression`, `ArrowFunction`, `MethodDefinition` | Name via `name` field (absent on anonymous fns/arrows) |
| `Call` | `CallExpression` | Callee via `function` field; args via `arguments` field |
| `VarDecl` | `VariableDeclarator` | Name in `name` field (may be a pattern); parent `LexicalDeclaration`/`VariableDeclaration` has `const`/`let`/`var` keyword |
| `ImportBinding` | `ImportDeclaration` | Walk `import_clause` → `named_imports` → `import_specifier`; default import is direct `identifier` child of `import_clause` |
| `Await` | `AwaitExpression` | Inner expr is first named child |
| `Cast` | `AsExpression` | Inner expr is `expression` field |
| `JsxElement` | `JsxElement`, `JsxSelfClosingElement` | Tag is `name` field of opening element |
| `Parameter` | `required_parameter`, `optional_parameter`, `rest_parameter` | Children of `formal_parameters` |
| `AssignmentExpression` | `AssignmentExpression` | LHS via `left` field, RHS via `right` field |
| `MemberExpression` | `MemberExpression` | Base via `object` field, property via `property` field |
| `Assign` (FieldWrite) | `AssignmentExpression` where LHS is `MemberExpression` | Detect this pattern in the walker |
| `FieldRead` | `MemberExpression` not in LHS of assignment | Context-dependent detection |
| `Error` / parse errors | `Error` (normalised from `ERROR`) | Emit to `ExtractError` relation with phase="parse" |

### Entity ID strategy

The walker needs a consistent entity ID scheme. Recommended: hash(filePath + ":" + startByte) truncated to uint32. All nodes with the same file+startByte get the same entity ID — this is safe because two distinct nodes cannot start at the same byte. The `ScopeAnalyzer` already uses `StartByte` as a key, making it easy to cross-reference.

### TSX files

The TypeScript grammar handles `.tsx` files — JSX nodes appear in `.tsx` and sometimes in `.ts` with JSX mode enabled. The walker should emit `JsxElement` and `JsxAttribute` facts for both `JsxElement` and `JsxSelfClosingElement` nodes.
