# v2 Phase A3: Type-Aware Fact Emission — Handover

## Summary

Phase A3 adds type-aware structural fact emission to the tsq extractor using
tree-sitter AST patterns. All 12 new v2 relations are registered, and 5
previously-empty v1 relations are now populated. The implementation degrades
gracefully when tsgo is unavailable (which is the current state).

## What was built

### 1. New v2 schema relations (`extract/schema/relations.go`)

12 new relations added (all Version: 2):

| Relation | Columns | Purpose |
|---|---|---|
| ClassDecl | id, name, file | Class declarations |
| InterfaceDecl | id, name, file | Interface declarations |
| Implements | classId, interfaceId | Class implements interface |
| Extends | childId, parentId | Class/interface inheritance |
| MethodDecl | classOrIfaceId, name, fnId | Method declarations |
| MethodCall | callId, receiverExpr, methodName | Member call expressions (obj.method()) |
| NewExpr | callId, classId | `new` expressions |
| ExprType | exprId, typeId | Expression types (empty without tsgo) |
| TypeDecl | typeId, name, kind, file | Type alias declarations |
| ReturnStmt | fnId, stmtNode, returnExpr | Return statements |
| FunctionContains | fnId, nodeId | Nodes inside function bodies |
| SymInFunction | sym, fnId | Symbol references inside functions |

Total relations: 33 (v1) + 12 (v2) = 45.

### 2. Previously-empty v1 relations now populated

- **Symbol**: populated from variable declarations, function declarations, class/interface names, type aliases
- **FunctionSymbol**: populated from named function declarations and variable-assigned functions
- **CallCalleeSym**: was already populated in v1 walker (via scope resolution)
- **CallResultSym**: registered but still requires cross-file analysis (remains empty)
- **TypeFromLib**: requires tsgo for population (remains empty)

### 3. TypeAwareWalker (`extract/walker_v2.go`)

Wraps the existing FactWalker and adds v2 emission. Key patterns:

- **ClassDecl/InterfaceDecl**: emitted on ClassDeclaration/InterfaceDeclaration nodes
- **Heritage clauses**: tree-sitter wraps class heritage in `ClassHeritage` -> `ExtendsClause`/`ImplementsClause`; interfaces use `ExtendsTypeClause` directly
- **MethodDecl**: emitted for MethodDefinition nodes inside class/interface bodies (tracked via stack)
- **FunctionContains**: uses a function stack; every node inside a function body gets a containment tuple
- **ReturnStmt**: associates return statements with their enclosing function
- **NewExpr**: structural match on NewExpression nodes
- **MethodCall**: detects CallExpression with MemberExpression callee
- **SymInFunction**: scope-resolved identifier references inside functions

### 4. Bridge updates

- **manifest.go**: Symbol, FunctionSymbol, CallCalleeSym, CallResultSym, TypeFromLib moved from Unavailable to Available. All 12 new v2 classes added. Only DataFlow and TaintTracking remain unavailable (v3).
- **tsq_types.qll**: new bridge file for ClassDecl, InterfaceDecl, Implements, Extends, MethodDecl, MethodCall, NewExpr, ExprType, TypeDecl
- **tsq_symbols.qll**: new bridge file for Symbol, FunctionSymbol, TypeFromLib, SymInFunction
- **tsq_functions.qll**: extended with ReturnStmt and FunctionContains classes
- **embed.go**: updated to embed and import-map the 2 new .qll files

### 5. Tests

- `extract/walker_v2_test.go`: 18 tests covering all v2 relations, backwards compatibility, fixture directory
- `extract/schema/relations_test.go`: updated counts and added v2 validation test
- `bridge/manifest_test.go`: updated counts (45 available, 2 unavailable)
- `bridge/embed_test.go`: updated to include new .qll files
- `testdata/ts/v2/classes.ts`: comprehensive fixture (classes, interfaces, inheritance, methods, new expressions, method calls, type aliases, returns)
- `testdata/ts/v2/generics.ts`: generic class/interface fixture

### 6. Kind map additions (`extract/backend_treesitter.go`)

Added mappings for: `extends_clause`, `implements_clause`, `class`, `abstract_class_declaration`, `public_field_definition`, `formal_parameters`, `required_parameter`, `optional_parameter`, `rest_parameter`, `pair_pattern`, `statement_block`, `type_assertion`, `non_null_expression`, `satisfies_expression`, `heritage_clause`, `extends_type_clause`, `class_heritage`.

## Graceful degradation

When tsgo is unavailable (current state):
- **ExprType**: registered but empty (requires type checker)
- **TypeFromLib**: registered but empty (requires type checker)
- **CallResultSym**: registered but empty (requires cross-file analysis)
- All other relations are fully populated via structural (tree-sitter) analysis

## What's next

- **Phase A4**: tsgo integration — when tsgo becomes available, enhance TypeAwareWalker to populate ExprType, TypeFromLib, and CallResultSym via JSON-RPC
- **v3**: DataFlow and TaintTracking (IPA-dependent)
