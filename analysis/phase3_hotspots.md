# Phase 3: Hot-Spot Deep Dives

Connascence analysis of the 8 largest/most critical files in the tsq codebase.

---

## 3.1 ql/parse/parser.go (1522 LOC)

### Connascence Forms

**CoP (Position) in production rule ordering:**
The parser is recursive-descent, so operator precedence is encoded in call depth:
`parseFormula -> parseOr -> parseAnd -> parseNot -> parseComparisonOrAtom` and
`parseExpr -> parseAddSub -> parseMulDiv -> parseUnary -> parsePostfix -> parsePrimary`.
Any reordering of this call chain silently changes language semantics. This is standard for RD parsers but the positional contract is entirely implicit -- there are no comments or constants documenting the intended precedence levels.

**CoN (Name) with lexer token types:**
The parser references ~40 `Tok*` constants (`TokKwImport`, `TokKwClass`, `TokIdent`, etc.) that must exactly match the lexer's definitions. Standard CoN but the surface area is large.

**CoV (Value) between lexer position and error span:**
Error reporting uses `p.current.Line` and `p.current.Col` directly from the lexer's token to construct `ast.Span` values. The parser trusts these are 1-based line / 0-based column -- any drift in the lexer's convention silently corrupts all error messages and span annotations downstream.

**CoM (Meaning) -- embedded QL semantic knowledge:**
- The parser hardcodes the distinction between characteristic predicates and methods by comparing `qualParts[0] == className` (line 451). This is QL class semantics leaking into parse rules.
- `parseClassMember` contains logic for `override` modifier, return types, characteristic predicates -- all semantic distinctions that could live in a separate pass.
- `this`, `result`, `super` are parsed as `ast.Variable` nodes with hardcoded string names (lines 1232-1254). The meaning of these names is a contract shared with resolve.go and desugar.go.

**CoT (Type) -- token-to-AST mappings:**
`exprToFormula` (line 876) converts `*ast.MethodCall` to `*ast.PredicateCall` and `*ast.Variable` to a zero-arg predicate call. This type-level decision about what constitutes a formula vs expression is a semantic rule embedded in the parser.

### Duplicated Logic

- `parseAnnotations()` handles `private`, `deprecated`, `pragma`, `bindingset`, `language` -- these are hardcoded keyword checks. If a new annotation type is added, both the lexer (for the keyword token) and this function must be updated.
- Backtracking state save/restore (`saveState`/`restoreState`, lines 51-67) exposes lexer internals (pos, line, col, err pointer) -- tight structural coupling.

### Summary

The parser is clean for a recursive-descent design but embeds QL semantic knowledge (characteristic predicate detection, `this`/`result`/`super` semantics) that could be deferred to a later pass. The precedence chain is a standard CoP hot spot.

---

## 3.2 ql/desugar/desugar.go (1363 LOC)

### Connascence Forms

**CoM (Meaning) -- "this" and "result" variable injection:**
The desugarer injects `datalog.Var{Name: "this"}` as the first head argument for class predicates (line 239) and `datalog.Var{Name: "result"}` as the last argument for methods with return types (line 421). These magic strings must match:
- resolve.go's `s.vars["this"]` and `s.vars["result"]` bindings (lines 339, 353)
- The parser's `TokKwThis`/`TokKwResult` -> `ast.Variable{Name: "this"/"result"}` mapping
- The eval engine's variable binding semantics

This is CoM: the string "this" means "the receiver instance" and "result" means "the return value." If any component uses a different convention, the pipeline silently produces wrong results.

**CoA (Algorithm) -- override dispatch semantics:**
`desugarMethod` (line 365) implements override dispatch by:
1. Collecting all transitive overriding subclasses
2. Emitting one rule per overrider under the base class predicate name
3. Each rule excludes its own direct sub-overriders via `not SubClass(this)` literals

This algorithm must match the evaluator's predicate union semantics (multiple rules for the same predicate = union of results). If the eval engine changed to, say, first-match semantics, override dispatch would break silently.

**CoA (Algorithm) -- super resolution:**
`resolveSuperMethod` (line 1089) walks supertypes left-to-right. This ordering contract is shared with resolve.go's `lookupMemberRec` (which also walks supertypes in order). Both must agree on the resolution strategy for multiple inheritance.

**CoN (Name) -- predicate name mangling:**
`mangle(className, methodName)` returns `className + "_" + methodName` (line 506). This naming convention is a contract between desugar and eval -- the evaluator must look up predicates by these exact mangled names. If a class or method name contains `_`, there's potential for name collisions (no escaping).

**CoM (Meaning) -- entityTypeRelation map:**
The `entityTypeRelation` map (lines 284-297) hardcodes the mapping from `@`-prefixed entity types to schema relation names and arities. This is a critical CoM hot spot: if the schema adds a column to `Node` (currently arity 7) or renames a relation, this map silently produces wrong arities.

**CoM (Meaning) -- stringBuiltins map:**
The `stringBuiltins` map (line 1252) must agree with builtins.go's `builtinRegistry` on which string methods exist. The naming convention (`"__builtin_string_" + methodName`) is computed by string concatenation, not a shared constant.

**CoM (Meaning) -- isPrimitive:**
`isPrimitive()` (line 1344) duplicates the same set as resolve.go's `primitiveTypes` map but with a different format (switch vs map). They must stay in sync.

### Heritage/Inheritance Handling

The subclass map construction (`buildSubclassMap`, `buildSubclassMapForModule`) walks both imported and local classes, building a `map[string][]string` of parent->children. The `allConcreteSubclasses` function then traverses this to implement abstract class union semantics. The cycle guard in `superTypeConstraintsInner` (line 307) prevents infinite recursion but relies on the resolver having already detected cycles.

### Duplicated Logic

- Type constraint emission (`if !isPrimitive(typeName) { emit Literal }`) appears identically in `desugarExists`, `desugarSelect`, `desugarAggregateExpr`, and `desugarForall` -- 4 copies of the same pattern.
- `resolveMethodCallPred` and `resolvePredicateCallRecvPred` are parallel implementations for expression-position vs formula-position method calls, with overlapping but non-identical logic.

---

## 3.3 extract/walker_v2.go (1043 LOC) + extract/walker.go (964 LOC)

### Why Two Walkers?

`walker.go` defines `FactWalker` -- the v1 structural fact emitter handling basic AST nodes (functions, calls, variables, imports, exports, JSX, destructuring, template literals).

`walker_v2.go` defines `TypeAwareWalker` -- a **decorator/wrapper** around `FactWalker`. It delegates all v1 fact emission to the inner `FactWalker` and adds v2 type-aware facts: class/interface declarations, heritage clauses, method declarations, new expressions, return statements, function containment, type information, decorators, namespaces, type guards.

This is NOT a feature flag or backend-specific split. It is a layered architecture: `TypeAwareWalker` composes `FactWalker` via delegation (`tw.fw.Enter(node)`, `tw.fw.emit(...)`, `tw.fw.nid(...)`, `tw.fw.scope.Resolve(...)`).

### Connascence Forms

**CoI (Identity) -- shared FactWalker state:**
`TypeAwareWalker` accesses `tw.fw.filePath`, `tw.fw.fileID`, `tw.fw.scope`, `tw.fw.nid()`, and `tw.fw.emit()` directly. This is CoI: both walkers reference the same `FactWalker` instance and depend on its internal state being correctly maintained. If `FactWalker.Enter` changes the order of its state updates (e.g., `scope.Build` happens later), `TypeAwareWalker.emitV2Facts` would see stale scope data.

**CoEx (Execution) -- Enter/Leave ordering:**
`TypeAwareWalker.Enter` calls `tw.fw.Enter(node)` first, then `tw.emitV2Facts(node)`. This ordering matters because `fw.Enter` builds scope data that `emitV2Facts` relies on (e.g., `emitSymInFunction` calls `tw.fw.scope.Resolve`). The `Leave` method similarly delegates to `tw.fw.Leave(node)` after popping v2 stacks. The execution order is a fragile implicit contract.

**CoN (Name) -- hardcoded relation names:**
Both walkers emit facts via `fw.emit("RelationName", ...)` with string literals. There are 50+ unique relation name strings scattered across these two files. They must match:
- `extract/schema/relations.go` (schema definitions)
- `bridge/manifest.go` (AvailableClass.Relation mappings)
- Bridge `.qll` files (QL predicate definitions)

No constants, no registry lookup at emit time. Pure string matching.

**CoN (Name) -- hardcoded AST node kind strings:**
Both files use string literals for tree-sitter node kinds: `"ClassDeclaration"`, `"FunctionDeclaration"`, `"VariableDeclarator"`, `"MemberExpression"`, etc. These must match tree-sitter's TypeScript grammar. Changes to the grammar (e.g., renaming `"JsxElement"` to `"JSXElement"`) would silently break extraction.

### Shared vs Duplicated Logic

The walkers share utility functions defined in `walker.go`: `childByField()`, `childByKind()`, `boolInt()`. `walker_v2.go` reuses these freely.

However, fact emission patterns are duplicated: both walkers iterate over children, skip punctuation (`","`, `"("`, `")"`, `"{"`, `"}"`), and extract names via `childByField(node, "name")`. Each new relation type reimplements this boilerplate.

### Schema Relation Name Usage

All hardcoded. No registry or constant pool. The `emit()` method calls `fw.db.Relation(rel)` which does a string lookup in the DB's relation map. If a relation name is misspelled, `AddTuple` silently fails (the error is dropped: `_ = r.AddTuple(fw.db, vals...)`).

---

## 3.4 ql/resolve/resolve.go (731 LOC)

### Connascence Forms

**CoM (Meaning) -- "this", "result", "super" semantics:**
The resolver hardcodes implicit variable bindings:
- `s.vars["this"] = varInfo{typeName: cd.Name}` in class bodies (line 339)
- `s.vars["result"] = varInfo{typeName: md.ReturnType.String()}` in methods with return types (line 353)
- `resolveVariable` special-cases "this", "result", "super" with hardcoded string comparisons (lines 586-611)

These must agree with the parser's token-to-variable mapping and the desugarer's variable injection.

**CoM (Meaning) -- primitiveTypes:**
`primitiveTypes` map (lines 76-82) defines `int`, `float`, `string`, `boolean`, `date` as built-in scalars. This must match desugar.go's `isPrimitive()` function. Currently they agree, but they are maintained independently.

**CoM (Meaning) -- @-prefixed entity types:**
`resolveTypeRef` (line 398) treats any type starting with `@` as valid without checking against a registry. This is an implicit contract with the bridge layer -- `@node`, `@symbol`, etc. must correspond to actual entity types in the schema, but the resolver doesn't validate this.

**CoA (Algorithm) -- scope copying:**
`scope.child()` (line 310) copies all parent variables into the child scope via a map copy. This implements flat scoping (every child sees all parent bindings). The algorithm must match desugar.go's expectation that all variables are visible within their scope.

**CoA (Algorithm) -- member lookup chain:**
`lookupMemberRec` (line 706) walks the supertype chain depth-first, left-to-right, with a visited set for cycle prevention. This resolution order is a contract shared with desugar.go's `memberDefiningClass` (which delegates to `ast.MemberDefiningClass`).

### Import Resolution Coupling to Bridge

`processImports` (line 160) calls the provided `importLoader` function for each import path. In `cmd/tsq/main.go`, this loader is `makeBridgeImportLoader` which maps import paths like `"tsq::base"` to bridge `.qll` filenames. The resolver recursively resolves imported modules with `nil` as the import loader (line 179), meaning imports are flat (no transitive import loading). This is an explicit design choice but creates a coupling: bridge `.qll` files cannot import other bridges.

---

## 3.5 ql/eval/builtins.go (691 LOC)

### Connascence Forms

**CoN (Name) -- builtin registry:**
`builtinRegistry` (lines 19-47) maps `__builtin_string_*` and `__builtin_int_*` names to Go functions. These names must match exactly what desugar.go generates via `"__builtin_string_" + mc.Method` (desugar.go line 718). There is no shared constant or type-safe registry.

**CoP (Position) -- argument positions:**
Every builtin function hardcodes its argument positions. For example, `builtinStringSubstring` expects `atom.Args[0]` = this, `[1]` = start, `[2]` = end, `[3]` = result. These positions must match what the desugarer emits. The arity check (`if len(atom.Args) != N`) is the only guard against mismatches.

**CoA (Algorithm) -- type coercion rules:**
The builtins use `resolveStringArg` and `resolveIntArg` which do strict type checking via Go type assertions (`v.(StrVal)`, `v.(IntVal)`). There is no implicit coercion -- a string "42" won't match as an int. This strictness is an implicit contract with the QL type system.

**CoM (Meaning) -- string matching semantics:**
`builtinStringMatches` (line 295) implements CodeQL-style glob matching by converting `%` to `*` and `_` to `?`, then using `filepath.Match`. This is a specific semantic interpretation that QL query authors must know about. The comment says "CodeQL matches uses glob-like patterns" but this conversion is one-way and lossy (a literal `*` in the pattern would be ambiguous).

### Duplicated Logic

Every builtin function follows the identical pattern:
1. Check arity
2. Loop over bindings
3. Resolve args with `resolveStringArg`/`resolveIntArg`
4. Compute result
5. Call `bindResult`
6. Append to output

This boilerplate is repeated 19 times with only the core computation differing. A higher-order helper could reduce this to ~50 LOC.

### Magic Values

- `math.MinInt64` check in `builtinIntAbs` (line 560) -- silently drops the result for MinInt64 overflow.
- String length is byte-based (`len(s)`), not rune-based. This is an implicit choice that would produce wrong results for multi-byte Unicode strings.

---

## 3.6 cmd/tsq/main.go (622 LOC)

### CoEx (Execution) -- Pipeline Orchestration

The query pipeline follows a strict sequence:
1. Parse (`parse.NewParser` -> `Parse()`)
2. Resolve (`resolve.Resolve` with bridge import loader)
3. Desugar (`desugar.Desugar`)
4. Plan (`plan.Plan`)
5. Load fact DB (`db.ReadDB`)
6. Evaluate (`eval.NewEvaluator` -> `Evaluate`)

This sequence is duplicated between `compileAndEval` (lines 496-567, full pipeline) and `cmdCheck` (lines 402-493, pipeline without eval). The duplication means a change to the pipeline (e.g., adding a new pass) must be applied in both places.

**CoEx within extract:**
The extract pipeline is: Open backend -> emit SchemaVersion -> WalkAST -> (optional) tsgo enrichment -> encode DB. The tsgo enrichment phase depends on the extract phase having populated `Node`, `VarDecl`, and other relations first.

### Configuration Threading

**CoN (Name) -- import path to bridge file mapping:**
`makeBridgeImportLoader` (lines 571-617) contains a hardcoded map of 30+ import paths to `.qll` filenames. This is a critical CoN hot spot -- adding a new bridge module requires updating this map, the bridge `LoadBridge()` function, and `bridge/manifest.go`.

**CoM (Meaning) -- nonTaintablePrimitives:**
The `nonTaintablePrimitives` map (lines 36-44) defines TypeScript primitives that break taint chains. This is QL evaluation semantics embedded in the CLI entry point -- it arguably belongs in the taint analysis layer.

**CoN (Name) -- backend selection:**
The `--backend` flag accepts `"treesitter"` or `"vendored"` as string literals, matched to concrete types `extract.TreeSitterBackend` and `extract.VendoredBackend`. These strings are not exported constants.

### Duplicated Logic

- `compileAndEval` and `cmdCheck` duplicate the parse-resolve-desugar-plan pipeline with slightly different error handling.
- Bridge loading (`bridge.LoadBridge()`) is called separately in both `compileAndEval` and `cmdCheck`.

---

## 3.7 extract/scope.go (498 LOC)

### CoA (Algorithm) -- TypeScript Scoping Rules

The `ScopeAnalyzer` implements JavaScript/TypeScript scoping:
- `var` declarations are hoisted to the enclosing function scope
- `let`/`const` declarations are block-scoped with temporal dead zone (TDZ)
- Function declarations are hoisted to the enclosing function scope
- Import bindings go to the module (file) scope
- `catch` clause creates a new block scope for the error parameter

This algorithm must faithfully replicate the ECMAScript specification's scoping rules. Any deviation produces incorrect scope resolution, which cascades to wrong `ExprMayRef`, `CallCalleeSym`, `FieldRead`, and `FieldWrite` facts.

**CoM (Meaning) -- TDZ semantics:**
TDZ is implemented via `atByte < d.StartByte` comparison in `Scope.Resolve` (line 50). This means TDZ is approximated by byte position rather than control flow. A reference in a function body defined before a `let` declaration but called after it would incorrectly fail TDZ (JavaScript allows this). This is a deliberate simplification but an implicit semantic contract.

### Consumer Coupling

**CoI (Identity) -- shared between walker.go and walker_v2.go:**
`ScopeAnalyzer` is created in `FactWalker.EnterFile` (line 63) and stored as `fw.scope`. It is used by:
- `FactWalker.emitExprMayRef` (via `fw.scope.Resolve`)
- `FactWalker.emitCall` (via `fw.scope.Resolve` for `CallCalleeSym`)
- `FactWalker.emitAssign` (via `fw.scope.Resolve` for LHS symbol)
- `FactWalker.emitFieldRead/Write` (via `fw.scope.Resolve` for base symbol)
- `TypeAwareWalker.emitSymInFunction` (via `tw.fw.scope.Resolve`)
- `TypeAwareWalker.emitClassDecl` (for JSX tag symbol resolution, inherited via FactWalker)

The scope must be built (via `scope.Build`) before any `Resolve` call. This is ensured by the `rootSeen` flag in `FactWalker.enterNode` (line 117-119), which calls `scope.Build` on the first `Program` node.

### Duplicated Logic with walker.go

Both `scope.go` and `walker.go` define `childByField()` methods -- `scope.go` has `sa.childByField` (line 469) and `walker.go` has the package-level `childByField` (line 218). They have identical implementations. Similarly, `sa.firstChildByKind` duplicates `childByKind`.

### Performance Concern -- findScope

`findScope` (line 429) does a linear scan of all recorded scope entries to find the closest one <= startByte. For files with many scopes this is O(n) per resolution. A sorted slice with binary search would be O(log n).

---

## 3.8 bridge/manifest.go (236 LOC)

### Schema <-> Bridge Predicate Name Mapping

**CoN (Name) -- the central mapping:**
The `v2Manifest()` function (lines 41-196) is a massive static list of `AvailableClass` structs, each mapping:
- `Name`: the QL class name visible to queries (e.g., `"ASTNode"`, `"Function"`)
- `Relation`: the underlying schema relation name (e.g., `"Node"`, `"Function"`)
- `File`: which `.qll` file defines the bridge class

This is the **single most important CoN hot spot** in the codebase. There are 100+ entries. Every entry creates a 3-way name contract:
1. The schema relation name must match `extract/schema/relations.go`
2. The QL class name must match what bridge `.qll` files define
3. The file name must match what `bridge/embed.go` or `LoadBridge()` provides

### Where Mismatches Hide

**Name != Relation divergences:**
Most entries have `Name == Relation` (e.g., `Function`/`Function`), but several diverge:
- `"ASTNode"` -> `"Node"` -- QL users write `ASTNode`, schema stores `Node`
- `"Type"` -> `"ResolvedType"` -- alias for the same backing relation
- `"SymbolTypeBinding"` -> `"SymbolType"` -- different QL name
- `"DataFlow::Node"` -> `"Symbol"` -- CodeQL compatibility shim maps to a different relation
- `"DataFlow::PathNode"` -> `"Symbol"` -- same backing relation as DataFlow::Node
- `"DOM::Element"` -> `"JsxElement"` -- compatibility mapping
- `"DOM::InnerHtmlWrite"` -> `"FieldWrite"` -- reuses existing relation
- `"HTTP::ResponseBody"` -> `"TaintSink"` -- compatibility alias
- `"DatabaseAccess"` -> `"TaintSink"` -- another alias

These are the places where a schema rename would silently break the bridge without any compile-time error.

**Relation reuse risk:**
Multiple QL classes map to the same backing relation: `"DataFlow::Node"`, `"DataFlow::PathNode"`, and `"TaintTracking::Configuration"` all map to `"Symbol"`. This means a schema change to `Symbol` would break 3+ bridge classes simultaneously.

**CoV (Value) -- AllRelationsCovered check:**
The `AllRelationsCovered` method (line 221) validates that every schema relation has a corresponding bridge entry. This is a runtime coverage check, not a compile-time guarantee. It iterates `schema.Registry` and checks against the manifest. If a new relation is added to the schema without a manifest entry, this check catches it -- but only if the check is actually called (it appears to be used only in tests).

### Magic Values

- The `Unavailable` slice is currently empty (line 194: `Unavailable: []UnavailableClass{}`), meaning the `CheckQuery` method (line 200) will never produce warnings. This is dead code until features are marked unavailable.

---

## Cross-Cutting Findings

### Highest-Risk Connascence

1. **Relation name strings** (CoN, high fan-out): ~50+ relation names used as bare strings in walker.go, walker_v2.go, manifest.go, desugar.go, and main.go. A single typo silently drops facts.

2. **"this"/"result" variable convention** (CoM, 4-way): parser, resolver, desugarer, and evaluator must all agree that `"this"` means receiver and `"result"` means return value. No shared constant.

3. **entityTypeRelation arities** (CoV): hardcoded arities in desugar.go must match schema definitions. Adding a column to Node (arity 7) without updating this map produces wrong grounding constraints.

4. **Builtin naming convention** (CoA, 2-way): desugar.go concatenates `"__builtin_string_" + methodName`, builtins.go uses the same strings as map keys. No shared definition.

5. **Import path -> filename mapping** (CoN, 2-way): main.go's `pathToFile` map and bridge's file loading must agree on filenames.

### Recommended Mitigations (Priority Order)

1. **Extract relation names into a `schema/names` constants package** used by walkers, manifest, and desugar. Compile-time enforcement of CoN.

2. **Create a shared `ql/conventions` package** defining `ThisVarName = "this"`, `ResultVarName = "result"`, `BuiltinPrefix = "__builtin_"`. Eliminates the 4-way CoM.

3. **Generate entityTypeRelation from schema.Registry** instead of hardcoding arities. Eliminates the CoV risk.

4. **Extract the parse-resolve-desugar-plan pipeline** in main.go into a shared `compile()` function to eliminate the duplication between `compileAndEval` and `cmdCheck`.

5. **Add compile-time tests** that assert `stringBuiltins` keys match `builtinRegistry` keys (minus the prefix), and that `isPrimitive()` matches `primitiveTypes`.
