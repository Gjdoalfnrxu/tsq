# Phase 6: Bridge Layer — Handover

## What was built

The bridge layer maps tsq's fact schema relations to QL-visible classes via `.qll` library files. This is the interface that query authors use when writing `.ql` queries against TypeScript codebases.

## Files created

### Bridge .qll files (8 files)

| File | Classes | Relations covered |
|------|---------|-------------------|
| `bridge/tsq_base.qll` | ASTNode, File, Contains, SchemaVersion | Node, File, Contains, SchemaVersion |
| `bridge/tsq_functions.qll` | Function, Parameter, ParameterRest, ParameterOptional, ParamIsFunctionType | Function, Parameter, ParameterRest, ParameterOptional, ParamIsFunctionType |
| `bridge/tsq_calls.qll` | Call, CallArg, CallArgSpread | Call, CallArg, CallArgSpread |
| `bridge/tsq_variables.qll` | VarDecl, Assign | VarDecl, Assign |
| `bridge/tsq_expressions.qll` | ExprMayRef, ExprIsCall, FieldRead, FieldWrite, Await, Cast, DestructureField, ArrayDestructure, DestructureRest | ExprMayRef, ExprIsCall, FieldRead, FieldWrite, Await, Cast, DestructureField, ArrayDestructure, DestructureRest |
| `bridge/tsq_jsx.qll` | JsxElement, JsxAttribute | JsxElement, JsxAttribute |
| `bridge/tsq_imports.qll` | ImportBinding, ExportBinding | ImportBinding, ExportBinding |
| `bridge/tsq_errors.qll` | ExtractError | ExtractError |

### Go files

| File | Purpose |
|------|---------|
| `bridge/embed.go` | `go:embed` directive bundling all .qll files; `LoadBridge()` returning `map[string][]byte`; `BridgeImportLoader()` for resolver integration |
| `bridge/bridge_test.go` | Tests: .qll structural parsing, relation reference validity, arity checking, no-DataFlow guard |
| `bridge/manifest_test.go` | Tests: manifest counts, coverage, warnings, uniqueness |
| `bridge/embed_test.go` | Tests: embed completeness, manifest-embed consistency, UTF-8 validity, import loader paths |

## Design decisions

1. **Relation names in .qll are snake_case** — matches the convention where the QL evaluator lowercases PascalCase relation names to snake_case for predicate lookup.

2. **Characteristic predicates use `this`** — each class has a char pred that binds `this` via the underlying relation predicate, ensuring the class only contains valid tuples.

3. **No DataFlow/TaintTracking** — fail-closed design. Tests enforce this.

4. **BridgeImportLoader** — provides a function that maps `tsq::base`, `tsq::functions`, etc. to embedded .qll content. This is the hook point for the resolver's `importLoader` parameter.

5. **28 available classes, 7 unavailable** — all 33 schema relations (+ 2 non-relation unavailable: DataFlow, TaintTracking) are accounted for in the manifest.

## Integration point for Phase 7+

The `BridgeImportLoader` function in `embed.go` returns a loader function. To wire it into the pipeline:

```go
bridgeFiles := bridge.LoadBridge()
// In the import resolution chain, check bridge first:
importLoader := func(path string) (*ast.Module, error) {
    if _, ok := bridge.BridgeImportLoader(bridgeFiles, nil)(path); ok {
        // Parse the .qll source and return the AST
        src := string(bridgeFiles[pathToFile[path]])
        return parse.NewParser(src, path).Parse()
    }
    return nil, fmt.Errorf("unknown import: %s", path)
}
```

The resolver (`ql/resolve/resolve.go`) already accepts an `importLoader func(path string) (*ast.Module, error)` — the bridge loader slots directly into this interface.

## What's NOT in this phase

- No changes to the resolver or parser — the bridge provides the .qll content and loader, but wiring it into the actual pipeline is a Phase 7 concern.
- No Symbol/FunctionSymbol/CallCalleeSym/CallResultSym/TypeFromLib bridge classes — these relations are empty in v1 (symbol resolution not yet implemented).
- No DataFlow or TaintTracking — requires inter-procedural analysis engine (v3).
