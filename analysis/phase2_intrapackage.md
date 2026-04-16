# Phase 2: Intra-Package Coupling Analysis

## 2.1 extract/ Internals

### walker.go (964 LOC) vs walker_v2.go (1043 LOC) — Relationship

Both walkers are active. `TypeAwareWalker` (walker_v2.go) **wraps** `FactWalker` (walker.go) via composition — it holds a `*FactWalker` field (`tw.fw`) and delegates all v1 operations to it.

**Coupling pattern:** TypeAwareWalker calls `tw.fw.Enter()`, `tw.fw.Leave()`, `tw.fw.EnterFile()`, `tw.fw.LeaveFile()` for v1 facts, then overlays v2 facts by calling `tw.fw.emit()` directly. This creates:

- **CoN (Name):** walker_v2 references `fw.emit`, `fw.nid`, `fw.fileID`, `fw.filePath`, `fw.scope` — deeply coupled to FactWalker's internal field names and method signatures.
- **CoI (Identity):** Both share the *same* `*db.DB` instance via `tw.fw.db`. walker_v2 has no independent database reference — all writes flow through `fw.emit()`.
- **CoEx (Execution):** Strict ordering dependency — `tw.fw.Enter(node)` must execute before `tw.emitV2Facts(node)` because v1 populates the parent stack, scope, and Node/Contains tuples that v2 facts reference. Similarly, `tw.fw.Leave()` must run after v2 stack pops.
- **CoM (Meaning):** Both walkers must agree on what `uint32` IDs mean. walker_v2 computes IDs via `tw.fw.nid()` (delegating to walker.go's `NodeID()` call), ensuring identical semantics.

**Bidirectional coupling:** No. walker_v2 depends on walker.go but not vice versa. walker.go is unaware of walker_v2.

**Duplicated helpers:** walker_v2 reuses `childByField()` and `childByKind()` from walker.go (package-level free functions). No duplication here — good factoring.

**Risk:** walker_v2 reaches into `fw` internals (`fw.filePath`, `fw.fileID`, `fw.scope`, `fw.emit`). These are unexported fields accessed by same-package code, but any refactoring of FactWalker's internal state will break walker_v2.

### backend.go ↔ backend_treesitter.go ↔ backend_vendored.go

**backend.go** defines the `ExtractorBackend` interface (6 methods: Open, WalkAST, ResolveSymbol, ResolveType, CrossFileRefs, Close) plus `ASTNode` interface (8 methods) and supporting types (`ProjectConfig`, `ASTVisitor`, `SymbolRef`, `SymbolDecl`, `NodeRef`).

**backend_treesitter.go** implements `ExtractorBackend` with tree-sitter. Contains:
- `TreeSitterBackend` struct
- `tsASTNode` struct implementing `ASTNode`
- `kindMap` — the canonical kind mapping (120+ entries)
- `normalise()` — the snake_case-to-PascalCase converter

**backend_vendored.go** implements `ExtractorBackend` by embedding `*TreeSitterBackend` for AST walking and adding tsgo subprocess for semantic operations.

**Coupling forms:**

- **CoN (Name):** Both backends implement `ExtractorBackend` — must agree on all 6 method signatures. This is healthy interface-based CoN.
- **CoI (Identity):** `VendoredBackend` embeds `*TreeSitterBackend` — they share the same tree-sitter parser instances. `VendoredBackend.WalkAST` delegates directly to `b.treeSitter.WalkAST()`.
- **CoA (Algorithm):** `kindMap` in backend_treesitter.go and `tsgoKindMap` in tsgonode.go must produce the **same canonical PascalCase names** for equivalent AST concepts. Both map their respective parser's kind strings to the same canonical names (e.g., "CallExpression", "MemberExpression"). This is a duplicated algorithm — if the canonical name for a concept changes, both maps must be updated.
- **CoM (Meaning):** `ErrUnsupported` sentinel (defined in backend.go) carries semantic meaning — both backends return it from unimplemented methods, and callers check for it to degrade gracefully.

**vendored_scope.go** adds another layer: `VendoredScopeAdapter` wraps `*VendoredBackend` + `*ScopeAnalyzer`, implementing the same `Resolve(name, atNode)` interface. This creates CoN with scope.go's `Declaration` type.

### scope.go (498 LOC) — Consumers

**Primary consumer:** `FactWalker` (walker.go). The walker creates a `ScopeAnalyzer` per file (`fw.scope = NewScopeAnalyzer(path)`), calls `fw.scope.Build(node)` on the Program root, then calls `fw.scope.Resolve(name, node)` during fact emission for:
- `emitExprMayRef` — identifier resolution
- `emitCall` — callee symbol resolution (CallCalleeSym)
- `emitAssign` — LHS symbol resolution
- `emitFieldRead` / `emitFieldWrite` — base object resolution
- `emitJsxElement` — JSX tag resolution

**Secondary consumer:** `VendoredScopeAdapter` wraps scope.go's `ScopeAnalyzer` as a fallback.

**Coupling forms:**
- **CoI (Identity):** scope.go's `ScopeAnalyzer` holds a `nodeScope map[int]*Scope` that is populated during `Build()` and queried during `Resolve()`. The walker depends on scope being built on the same tree-sitter nodes that are being walked — if build and query happen on different parse trees, byte offsets won't match.
- **CoEx (Execution):** `Build()` must be called exactly once per file, on the Program root node, before any `Resolve()` calls. walker.go enforces this with the `rootSeen` flag.
- **CoM (Meaning):** scope.go uses `nodeStartByte()` which type-asserts `ASTNode` to `*tsASTNode` to access `n.StartByte()`. This creates an implicit coupling to the tree-sitter backend's concrete type. The fallback (line*10000 + col) is only approximate.
- **CoA (Algorithm):** scope.go has its own `childByField()` and `firstChildByKind()` methods on `ScopeAnalyzer` that are functionally identical to the free functions in walker.go. This is a minor code duplication (CoA) — both implement the same child-lookup algorithm.

### kinds.go — Cross-file Usage

`kinds.go` defines two constant sets: `FunctionKinds` (6 entries) and `ExpressionKinds` (31 entries), plus lookup functions `IsFunctionKind()` and `isExpressionKind()`.

**Consumers (5 files):**
1. **walker.go:** `IsFunctionKind(kind)` to decide when to call `emitFunction()`
2. **walker_v2.go:** `IsFunctionKind(kind)` for function stack management and `FunctionContains` emission; `isExpressionKind(kind)` for `ExprInFunction` emission
3. **scope.go:** Hardcoded list in `buildScope()` case statement: `"FunctionDeclaration", "FunctionExpression", "ArrowFunction", "MethodDefinition", "GeneratorFunction", "GeneratorFunctionDeclaration"` — this is a **duplicated constant set** (CoA) that must stay in sync with `FunctionKinds`
4. **kinds_test.go:** Tests the lookup functions

**Coupling forms:**
- **CoN (Name):** All consumers must agree on the exact string values ("FunctionDeclaration", "ArrowFunction", etc.)
- **CoA (Algorithm, critical):** scope.go's case statement duplicates the `FunctionKinds` list instead of using `IsFunctionKind()`. If a new function kind is added to `FunctionKinds`, scope.go must be updated manually. The comment in kinds.go says "scope.go all reference this slice" but scope.go actually hardcodes the values.
- **CoM (Meaning):** The kind strings are the canonical PascalCase names that `normalise()` / `normaliseTsgoKind()` produce from raw parser output. If canonical names change, kinds.go, backend_treesitter.go's `kindMap`, and tsgonode.go's `tsgoKindMap` must all agree.

### ids.go — ID Format Dependencies

`ids.go` defines 6 ID functions: `NodeID`, `SymID`, `ReturnSymID`, `FileID`, `TypeEntityID`, `PositionNodeID`. All use FNV-1a hashing truncated to uint32.

**Consumers (6 files):**
1. **walker.go:** `NodeID()`, `SymID()`, `FileID()` — used extensively for all fact emission
2. **walker_v2.go:** `SymID()`, `ReturnSymID()` — for Symbol, FunctionSymbol, ReturnSym emission
3. **scope.go:** No direct use — but scope.go's `Declaration` stores `StartLine`/`StartCol` which are later fed to `SymID()` by walker.go
4. **typecheck/enricher.go:** `TypeEntityID()`, `PositionNodeID()` — for tsgo enrichment
5. **walker_test.go:** Tests ID computation

**Coupling forms:**
- **CoA (Algorithm):** All callers must feed the same inputs in the same order. `NodeID` takes `(filePath, startLine, startCol, endLine, endCol, kind)` — if any caller permutes or omits fields, IDs won't match across the extraction pipeline.
- **CoV (Value):** `SymID` and the scope resolution must produce matching IDs. walker.go computes `SymID(fw.filePath, decl.Name, decl.StartLine, decl.StartCol)` using values from scope.go's `Declaration`. If scope resolution returns different position information than what walker.go would compute directly, the IDs diverge. This is a cross-file value constraint.
- **CoM (Meaning):** The uint32 IDs are used as entity references throughout the entire system — in the fact database (db.DB), in eval.go (as IntVal), and in QL queries. Everyone must agree that these are FNV-1a hashes of specific string representations.

---

## 2.2 ql/eval/ Internals

### eval.go ↔ seminaive.go ↔ join.go ↔ relation.go ↔ parallel.go

This is the Datalog evaluation engine. The relationships are:

**eval.go** (80 LOC): Entry point. `Evaluator` struct wraps `*plan.ExecutionPlan` + `*db.DB`. `loadBaseRelations()` converts db.DB tuples into eval.Relation objects. Calls `Evaluate()`.

**seminaive.go** (210 LOC): Core evaluation loop. `Evaluate()` implements stratified semi-naive bottom-up evaluation. Orchestrates:
- Calls `Rule()` and `RuleDelta()` from join.go
- Calls `Aggregate()` from aggregate.go
- Calls `parallelBootstrap()` / `parallelDelta()` from parallel.go
- Uses `relKey()` from relkey.go for all map lookups
- Uses `NewRelation()` and `Relation.Add()` from relation.go

**join.go** (379 LOC): Join implementation. Contains `Rule()`, `RuleDelta()`, `evalJoinSteps()`, `applyStep()`, `applyPositive()`, `applyNegative()`, `projectHead()`.

**relation.go** (220 LOC): Data structures. `Value` interface (`IntVal`, `StrVal`), `Tuple`, `Relation` (with hash indexes), `HashIndex`.

**parallel.go** (118 LOC): Parallel variants of bootstrap and delta evaluation.

**Coupling analysis:**

- **CoT (Type):** All files share the same type vocabulary: `Value`, `IntVal`, `StrVal`, `Tuple`, `Relation`, `binding`, `HashIndex`. This is healthy type-level coupling.
- **CoN (Name):** `relKey()` is called from seminaive.go (8 calls), join.go (4 calls), parallel.go (3 calls), aggregate.go (1 call). Every relation lookup must use this function — forgetting it was the source of the arity-shadow bug documented in relkey.go.
- **CoI (Identity):** `allRels map[string]*Relation` is the central shared mutable state. seminaive.go, parallel.go, and aggregate.go all read and write this map. In parallel mode, `parallelBootstrap()` and `parallelDelta()` read `allRels` concurrently (safe because they don't mutate it during the parallel phase) then merge results sequentially via `headRel.Add(t)`.
- **CoEx (Execution):** Strict stratum ordering — strata must be evaluated sequentially. Within a stratum: bootstrap before fixpoint. Aggregates after fixpoint. The `parallelBootstrap` goroutines must all complete before the sequential merge phase.
- **CoA (Algorithm):** `applyPositive()` in join.go and `applyNegative()` in join.go implement mirror-image logic (positive extends bindings, negative filters them). Both use the same index lookup pattern (`rel.Index(boundCols).Lookup(boundVals)`) and the same column matching verification loop — near-duplicate code.

### Join Strategy ↔ Relation Representation Coupling

**Tight coupling.** The join implementation in join.go directly depends on:
1. `Relation.Index(cols)` returns a `*HashIndex` — join.go uses this for bound-column probing
2. `HashIndex.Lookup(key)` returns `[]int` indices into `Relation.Tuples()`
3. `Relation.Tuples()` returns `[]Tuple` for index-based access
4. `partialKey()` / `tupleKey()` in relation.go must produce consistent keys for both index building and deduplication

**CoA (Algorithm):** The `HashIndex.Lookup` method rebuilds the key via `partialKey(Tuple(key), seqCols)` where `seqCols = [0, 1, ..., len(key)-1]`. This means the caller's key values must be in the same canonical column order as the index was built with. `sortedColKey()` ensures indexes are canonical, but `Lookup()` assumes key[i] maps to sorted cols[i] — the ordering contract is implicit.

**CoV (Value):** `Relation.Add()` panics if tuple arity doesn't match `r.Arity`. This is a hard runtime constraint linking the relation schema to every tuple producer (join.go's `projectHead`, aggregate.go's tuple construction, seminaive.go's bootstrap).

### builtins.go (691 LOC) + builtin.go (138 LOC) — Split

**builtin.go** (138 LOC): Infrastructure. Contains `Compare()`, `Arithmetic()`, `ValueToString()` — generic value operations used by the join engine.

**builtins.go** (691 LOC): Builtin predicate implementations. Contains:
- `builtinRegistry` map (string → builtinFunc)
- `IsBuiltin()` / `ApplyBuiltin()` dispatch functions
- 30+ individual builtin implementations (`builtinStringLength`, `builtinIntAbs`, etc.)
- Helper functions (`resolveStringArg`, `resolveIntArg`, `bindResult`)

**Coupling forms:**
- **CoT (Type):** Both files operate on `Value`/`IntVal`/`StrVal` from relation.go.
- **CoN (Name):** builtins.go calls `Compare()` from builtin.go (in `bindResult`), and `lookupTerm()` from join.go. join.go calls `IsBuiltin()` and `ApplyBuiltin()` from builtins.go.
- **CoM (Meaning):** All builtin predicate names must start with `__builtin_` — this prefix is the implicit convention. The registry keys and the QL desugarer must agree on exact names.
- **CoP (Position):** Every builtin's argument positions are hardcoded: e.g., `builtinStringLength` expects `atom.Args[0]` = this, `atom.Args[1]` = result. The QL desugarer must emit args in this exact order. This is positional connascence between the eval engine and the QL compiler.

### relkey.go — Key Representation Coupling

`relKey(name, arity)` returns `"name/arity"` as a string key. This is used by every file in eval/ that does relation lookups.

- **CoA (Algorithm):** The encoding `name + "/" + strconv.Itoa(arity)` is duplicated implicitly — any code that constructs or parses these keys must agree on the format. Currently only `relKey()` constructs them, which is good.
- **CoN (Name):** `relKey` is called 16+ times across 4 files. Renaming it or changing its signature would require touching every call site.
- **CoM (Meaning):** The `/` separator carries meaning — relation names cannot contain `/` or the encoding would be ambiguous. This is an undocumented constraint.

### aggregate.go — Own Machinery or Shared?

Aggregate evaluation uses **its own join machinery** (`evalLiterals`) that is simpler than the planner-ordered `evalJoinSteps`:

```go
func evalLiterals(lits []datalog.Literal, rels map[string]*Relation) []binding {
    // Mirrors evalJoinSteps but works on []datalog.Literal directly,
    // without planner-ordered JoinSteps.
}
```

**CoA (Algorithm, duplicated):** `evalLiterals` reimplements the join loop from `evalJoinSteps` but operates on `datalog.Literal` instead of `plan.JoinStep`. It calls the same `applyComparison`, `applyPositive`, `applyNegative` from join.go. The duplication is because aggregates bypass the planner — their body literals are evaluated naively.

- **CoN (Name):** aggregate.go calls `tupleKey()`, `NewRelation()`, `lookupTerm()` from other files.
- **CoT (Type):** Shares `Value`, `Tuple`, `Relation`, `binding` types.
- **CoEx (Execution):** Aggregates run after fixpoint (enforced by seminaive.go). The `Aggregate()` function is called in stratum order, after the fixpoint loop completes.

---

## 2.3 ql/parse/ Internals

### parser.go (1522 LOC) ↔ lexer.go (369 LOC) — Token Coupling

**Token interface:** The parser accesses the lexer through a clean, narrow interface:

1. `NewLexer(src, file)` — construction
2. `l.Next()` — returns a `Token` struct

The `Token` struct is the coupling point:
```go
type Token struct {
    Type TokenType  // enum (int)
    Lit  string     // literal text
    Line int
    Col  int
}
```

**Coupling forms:**

- **CoT (Type):** Parser depends on `Token` struct and `TokenType` enum. Both are defined in lexer.go. The parser uses `TokIdent`, `TokInt`, `TokString`, `TokKwFrom`, `TokKwSelect`, etc. — 59 distinct references to `p.current.Type` or `p.at(Tok...)` patterns.
- **CoN (Name):** All ~40 `TokenType` constants are shared names between lexer and parser. Adding a new keyword requires adding it to both lexer.go's `keywords` map and using it in parser.go.
- **CoM (Meaning):** The `Lit` field carries semantic content for `TokIdent` (the identifier text), `TokInt` (the numeric text), and `TokString` (the string content with escapes resolved). The parser relies on these conventions.

**Does the parser reach into lexer internals?**

**Yes, partially.** The `Parser.saveState()` / `restoreState()` methods directly access lexer internals for backtracking:
```go
func (p *Parser) saveState() parserState {
    return parserState{
        current: p.current,
        lexPos:  p.lexer.pos,    // internal field
        lexLine: p.lexer.line,   // internal field
        lexCol:  p.lexer.col,    // internal field
        lexErr:  p.lexer.err,    // internal field
    }
}
```

This is **CoI (Identity)** — the parser directly references and mutates the lexer's internal position state. The `Lexer` struct's fields (`pos`, `line`, `col`, `err`) are unexported but accessible because parser and lexer are in the same package. This is the tightest coupling in the parse package — the parser cannot use a different lexer implementation without matching these exact internal fields.

**No bidirectional coupling:** The lexer has no knowledge of the parser. Data flows strictly lexer → parser.

---

## 2.4 extract/rules/ Internals

### Rule Files — Independent Fact Emitters or Composing?

All rule files (`callgraph.go`, `localflow.go`, `taint.go`, `frameworks.go`, `summaries.go`, `higherorder.go`, `composition.go`) share the same pattern: each exports a function returning `[]datalog.Rule`. They are **independent fact emitters** with **implicit data coupling through shared relation names**.

**Coupling forms:**

- **CoN (Name, dominant):** All rules reference the same relation names as string literals: `"CallTarget"`, `"LocalFlow"`, `"LocalFlowStar"`, `"FlowStar"`, `"TaintedSym"`, `"ExprMayRef"`, `"Parameter"`, `"SymInFunction"`, etc. These must match exactly:
  - The schema definitions in `extract/schema/`
  - The fact emission in walker.go / walker_v2.go
  - Other rules that produce or consume the same relation
  - There is no compile-time enforcement — a typo in a relation name is a silent logic error.

- **CoP (Position):** Relation arguments are positional. `"Parameter"` is used as `Parameter(fn, idx, _, _, paramSym, _)` — the 6-argument positional convention must match across callgraph.go, localflow.go, summaries.go, frameworks.go, and higherorder.go. All files use `Parameter` with the same 6-arg layout. A schema change to `Parameter`'s column order would require updating every rule file.

- **CoM (Meaning):** String constants carry semantic meaning: `s("http_input")`, `s("xss")`, `s("sql")`, `s("command_injection")` must be used consistently between taint.go (which consumes `TaintSource` kinds), frameworks.go (which produces them), and any user-written QL queries. There is no enum — just string matching.

### Data Flow Between Rules (Implicit Composition)

The rules compose through **shared derived relations** rather than direct function calls:

```
callgraph.go    → produces: CallTarget, MethodDeclInherited, MethodDeclDirect, Instantiated, CallTargetRTA
localflow.go    → produces: LocalFlow, LocalFlowStar
                  consumes: Assign, ExprMayRef, VarDecl, FieldRead, FieldWrite, SymInFunction, DestructureField, ReturnStmt, ReturnSym
summaries.go    → produces: ParamToReturn, ParamToCallArg, ParamToFieldWrite, ParamToSink, SourceToReturn, CallReturnToReturn
                  consumes: Parameter, ReturnSym, LocalFlowStar, FunctionContains, CallArg, ExprMayRef, CallCalleeSym, TaintSink, TaintSource
composition.go  → produces: InterFlow, FlowStar
                  consumes: CallTarget, CallArg, ExprMayRef, ParamToReturn, CallResultSym, ParamToCallArg, FunctionSymbol, LocalFlowStar, InterFlow, AdditionalTaintStep, AdditionalFlowStep
taint.go        → produces: TaintedSym, SanitizedEdge, TaintedField, TaintAlert
                  consumes: TaintSource, ExprMayRef, FlowStar, SanitizedEdge(negation), CallResultSym, CallTarget, Sanitizer, FieldWrite, FieldRead, TaintSink, VarDecl, SymInFunction, ExprInFunction
frameworks.go   → produces: TaintSource, TaintSink, Sanitizer, ExpressHandler, HttpHandler, KoaHandler, FastifyHandler, LambdaHandler, NextjsHandler
                  consumes: FieldRead, Parameter, MethodCall, CallArg, ExprMayRef, FunctionSymbol, CallCalleeSym, ImportBinding, ExportBinding, JsxAttribute
higherorder.go  → produces: InterFlow
                  consumes: MethodCall, ExprMayRef, CallArg, FunctionSymbol, Parameter
```

**Key observation:** The dependency graph is acyclic at the file level (frameworks produces sources/sinks, taint consumes them, composition bridges local and inter-procedural flow). But within the Datalog evaluation, recursive rules (e.g., `FlowStar` in composition.go, `LocalFlowStar` in localflow.go, `MethodDeclInherited` in callgraph.go) create circular dependencies handled by the semi-naive fixpoint.

### merge.go — Rule Output Combination

`merge.go` provides two functions:

1. **`AllSystemRules()`** — concatenates all rule sets in a fixed order: CallGraph + LocalFlow + Summaries + Composition + Taint + Frameworks + HigherOrder
2. **`MergeSystemRules(prog, systemRules)`** — prepends system rules before user rules into a new `datalog.Program`

**Coupling forms:**
- **CoN (Name):** merge.go must know every rule-producing function name.
- **CoEx (Execution):** The concatenation order in `AllSystemRules()` doesn't affect correctness (the stratifier handles ordering), but it does affect the order rules are presented to the planner.

### higherorder.go ↔ callgraph.go Coupling

These files are **loosely coupled**. They share no direct function calls or types. Their coupling is entirely through shared relation names:

- Both produce `InterFlow` (higherorder.go directly, composition.go from callgraph's `CallTarget`)
- Both consume `MethodCall`, `ExprMayRef`, `FunctionSymbol`, `Parameter`
- higherorder.go is about callback flow (Array.map, Promise.then); callgraph.go is about direct/virtual call resolution

**No bidirectional coupling.** higherorder.go doesn't reference any callgraph-specific relations (CallTarget, MethodDeclInherited, etc.), and callgraph.go doesn't reference higherorder-specific patterns.

### Helper Function Duplication

All rule files share package-level helpers defined in callgraph.go:
- `v(name)` — creates `datalog.Var`
- `w()` — creates `datalog.Wildcard`
- `pos(pred, args...)` — creates positive literal
- `neg(pred, args...)` — creates negative literal
- `rule(headPred, headArgs, body...)` — creates a Rule

frameworks.go adds:
- `s(val)` — creates `datalog.StringConst`
- `intc(n)` — creates `datalog.IntConst`

These helpers are defined once and used across all files — good factoring, no duplication.

---

## Summary: Dominant Coupling Forms by Area

| Area | Dominant Forms | Highest Risk |
|------|---------------|--------------|
| extract/ walkers | CoI (shared db), CoEx (ordering), CoN (internal fields) | walker_v2 reaching into fw internals |
| extract/ backends | CoA (duplicate kind maps), CoN (interface) | kindMap ↔ tsgoKindMap divergence |
| extract/ scope | CoEx (build-before-resolve), CoI (same parse tree) | type assertion to *tsASTNode |
| extract/ kinds | CoA (scope.go hardcodes list), CoN (string constants) | scope.go's FunctionKinds duplication |
| extract/ ids | CoA (hash input format), CoV (scope→walker ID match) | SymID position mismatch between scope and walker |
| ql/eval/ core | CoI (shared allRels), CoEx (stratum ordering) | parallel mutation of allRels |
| ql/eval/ joins | CoA (positive/negative mirror), CoV (arity panics) | evalLiterals duplicating evalJoinSteps |
| ql/eval/ builtins | CoP (arg positions), CoM (name conventions) | __builtin_ name agreement with desugarer |
| ql/parse/ | CoI (parser saves lexer state), CoT (Token/TokenType) | backtracking via lexer internals |
| extract/rules/ | CoN (relation name strings), CoP (arg positions) | No compile-time check on relation name typos |

### Most Critical Cross-File Couplings

1. **scope.go's duplicated FunctionKinds** (CoA) — the case statement hardcodes the same list that kinds.go defines as `FunctionKinds`. Adding a new function kind to one without the other creates a silent bug where scope analysis mishandles that kind.

2. **walker_v2 reaching into FactWalker internals** (CoI/CoN) — `tw.fw.emit()`, `tw.fw.nid()`, `tw.fw.filePath`, `tw.fw.fileID`, `tw.fw.scope` are all accessed. Any FactWalker refactoring is high-risk.

3. **Dual kind maps** (CoA) — `kindMap` (backend_treesitter.go, 120 entries) and `tsgoKindMap` (tsgonode.go, 86 entries) must produce identical canonical names for the same AST concepts. No shared data structure enforces this.

4. **Relation name strings** (CoN across rules/) — ~30 distinct relation names used as bare strings across 7 rule files with no compile-time validation. A typo produces a silent empty-relation join.

5. **Parser ↔ Lexer backtracking** (CoI) — the parser directly saves and restores the lexer's internal position fields (`pos`, `line`, `col`, `err`). This is the strongest coupling in parse/ and prevents any lexer abstraction.

6. **aggregate.go's evalLiterals** (CoA) — reimplements the join loop from evalJoinSteps, creating a maintenance burden where changes to join semantics must be reflected in two places.
