# tsq CodeQL Compatibility — Implementation Plans (Phase 2)

Based on a comprehensive audit of gaps between tsq and CodeQL query compatibility.
Organized into parallel work streams where possible.

---

## Work Stream A: DataFlow API Completion (Critical)

Blocks ~50+ queries. The DataFlow/TaintTracking API is the backbone of all security queries.

### Plan A1: DataFlow::Node predicate implementation
**Files**: `bridge/compat_dataflow.qll`, `ql/desugar/desugar.go`
**Goal**: Implement `DataFlow::Node.getASourceNode()`, `.getAPredecessor()`, `.getASuccessor()`, `.flowsTo()`, `.flowsFrom()`, `.asExpr()`, `.asParameter()`
**Approach**: These predicates map to existing relations (FlowStar, LocalFlowStar, ExprMayRef, Parameter). Wire them as QL class methods that desugar to the right Datalog joins.
**Est**: ~120 LoC
**Depends on**: Nothing — can start immediately

### Plan A2: DataFlow::Configuration user override support
**Files**: `ql/desugar/desugar.go`, `bridge/compat_dataflow.qll`, `extract/rules/taint.go`
**Goal**: Allow users to define custom `DataFlow::Configuration` subclasses with `isSource()`, `isSink()`, `isBarrier()` overrides that actually affect analysis.
**Approach**: Desugar user `isSource`/`isSink`/`isBarrier` overrides into additional TaintSource/TaintSink/Sanitizer facts before evaluation. This is the key gap — currently Configuration is a skeleton.
**Est**: ~200 LoC
**Depends on**: A1

### Plan A3: TaintTracking::Configuration.isAdditionalTaintStep()
**Files**: `bridge/compat_tainttracking.qll`, `extract/rules/taint.go`
**Goal**: Implement `isAdditionalTaintStep(src, sink)` override that adds custom FlowStar edges during taint propagation.
**Approach**: Desugar user-defined additional steps into supplementary FlowStar facts. Needs a new derived relation `AdditionalTaintStep` that gets merged into FlowStar before fixpoint.
**Est**: ~100 LoC
**Depends on**: A2

### Plan A4: DataFlow::PathGraph and path tracking
**Files**: `bridge/compat_dataflow.qll`, `extract/rules/taint.go`
**Goal**: Implement `DataFlow::PathNode` with actual source-to-sink paths (not just alerts). Support `edges(a, b)` predicate for path visualization.
**Approach**: Implement TaintPath relation (deferred in Phase 1 due to needing arithmetic for step counting). Use bounded recursion with max depth.
**Est**: ~150 LoC
**Depends on**: A1

---

## Work Stream B: Framework Models (Critical)

Blocks ~50+ queries. Each framework model is independent — maximally parallel.

### Plan B1: Node.js HTTP module
**Files**: `extract/rules/frameworks.go`
**Goal**: Model `http.createServer()` handler detection, `req`/`res` sources and sinks.
**Approach**: Add rules matching `CallCalleeSym` for `createServer` from `http`/`https` imports. Map callback parameters as sources/sinks like Express.
**Est**: ~80 LoC
**Depends on**: Nothing

### Plan B2: Koa.js framework model
**Files**: `extract/rules/frameworks.go`
**Goal**: Model Koa middleware pattern (`ctx.request.body`, `ctx.body`, `ctx.query`), router handlers.
**Approach**: Detect Koa `app.use()` middleware, map `ctx` parameter fields to TaintSource/TaintSink.
**Est**: ~80 LoC
**Depends on**: Nothing

### Plan B3: Fastify framework model
**Files**: `extract/rules/frameworks.go`
**Goal**: Model Fastify route handlers (`request.body`, `request.query`, `reply.send()`).
**Approach**: Detect `fastify.get/post/...()` patterns, map handler parameters.
**Est**: ~70 LoC
**Depends on**: Nothing

### Plan B4: AWS Lambda handler model
**Files**: `extract/rules/frameworks.go`
**Goal**: Model Lambda handler `event` parameter as taint source.
**Approach**: Detect `exports.handler = async (event, context)` pattern and common Lambda frameworks (Middy, SAM).
**Est**: ~60 LoC
**Depends on**: Nothing

### Plan B5: Next.js API route model
**Files**: `extract/rules/frameworks.go`
**Goal**: Model Next.js API routes (`req.query`, `req.body`) and App Router handlers.
**Approach**: Detect `export default function handler(req, res)` in `pages/api/` paths and App Router `GET/POST` exports.
**Est**: ~70 LoC
**Depends on**: Nothing

### Plan B6: Database driver models (pg, mysql, mysql2, mongoose, sequelize)
**Files**: `extract/rules/frameworks.go`
**Goal**: Replace heuristic `.query()` matching with import-aware database sink detection.
**Approach**: Track imports from `pg`, `mysql`, `mysql2`, `mongoose`, `sequelize`, `knex`. Map their query/exec methods to SQL sinks. Keep the heuristic `.query()` as fallback.
**Est**: ~150 LoC
**Depends on**: Nothing

### Plan B7: Sanitizer library models
**Files**: `extract/rules/frameworks.go`
**Goal**: Model common sanitizer libraries (DOMPurify, xss, validator, escape-html, shell-escape, sqlstring).
**Approach**: Track imports and map their sanitization functions to `Sanitizer(fn, kind)` facts by kind (xss, sql, command_injection).
**Est**: ~120 LoC
**Depends on**: Nothing

---

## Work Stream C: Extraction Completeness (High)

Each extraction pattern is independent walker work.

### Plan C1: Template literal extraction
**Files**: `extract/walker.go`, `extract/schema/relations.go`
**Goal**: Extract template literal parts and expressions. New relations: `TemplateLiteral(id, tag?)`, `TemplateElement(parentId, idx, rawText)`, `TemplateExpression(parentId, idx, exprId)`.
**Approach**: Handle `TemplateLiteral` and `TaggedTemplateExpression` nodes in walker. Emit part/expression children.
**Est**: ~100 LoC
**Depends on**: Nothing

### Plan C2: Enum declaration extraction
**Files**: `extract/walker_v2.go`, `extract/schema/relations.go`
**Goal**: New relations: `EnumDecl(id, name, file)`, `EnumMember(enumId, memberName, initExpr?)`.
**Approach**: Handle `EnumDeclaration` and `EnumMember` nodes in v2 walker.
**Est**: ~60 LoC
**Depends on**: Nothing

### Plan C3: Decorator extraction
**Files**: `extract/walker_v2.go`, `extract/schema/relations.go`
**Goal**: New relation: `Decorator(targetId, decoratorExpr)`.
**Approach**: Handle `Decorator` nodes, link to their target declaration (class, method, property, parameter).
**Est**: ~50 LoC
**Depends on**: Nothing

### Plan C4: Namespace/module declaration extraction
**Files**: `extract/walker_v2.go`, `extract/schema/relations.go`
**Goal**: New relations: `NamespaceDecl(id, name, file)`, `NamespaceMember(nsId, memberId)`.
**Approach**: Handle `ModuleDeclaration`/`NamespaceDeclaration` nodes in tree-sitter TS grammar.
**Est**: ~60 LoC
**Depends on**: Nothing

### Plan C5: Optional chaining and nullish coalescing flow
**Files**: `extract/walker.go`, `extract/rules/local_flow.go`
**Goal**: Model data flow through `?.` (short-circuit to undefined) and `??` (fallback).
**Approach**: Extract `OptionalChain(expr, baseExpr)` and `NullishCoalescing(expr, lhs, rhs)`. Add local flow rules that propagate taint through both branches.
**Est**: ~80 LoC
**Depends on**: Nothing

### Plan C6: TypeScript type guards and assertion functions
**Files**: `extract/walker_v2.go`, `extract/schema/relations.go`
**Goal**: New relation: `TypeGuard(fnId, paramIdx, narrowedType)` for `x is T` return types and `asserts x` patterns.
**Approach**: Parse return type annotations in function declarations for `is` and `asserts` keywords.
**Est**: ~70 LoC
**Depends on**: Nothing

---

## Work Stream D: QL Language & Builtins (Medium)

### Plan D1: Additional string/regex builtins
**Files**: `ql/eval/builtins.go`
**Goal**: Add `.regexpFind()`, `.regexpReplaceAll()`, `.splitAt()` (full), `.prefix()`, `.suffix()`.
**Approach**: Implement as Go functions in the builtins dispatch table.
**Est**: ~80 LoC
**Depends on**: Nothing

### Plan D2: `unique` aggregate
**Files**: `ql/parse/parser.go`, `ql/ast/ast.go`, `ql/eval/aggregate.go`
**Goal**: Support `unique(Type x | formula | result)` aggregate — returns the single value satisfying formula, or fails if 0 or 2+ values exist.
**Approach**: Add to parser keyword list, AST node, and aggregate evaluator.
**Est**: ~50 LoC
**Depends on**: Nothing

### Plan D3: Integer builtins
**Files**: `ql/eval/builtins.go`
**Goal**: Add `.abs()`, `.bitAnd()`, `.bitOr()`, `.bitXor()`, `.bitShiftLeft()`, `.bitShiftRight()`, `.toHexString()`.
**Approach**: Direct Go implementations.
**Est**: ~40 LoC
**Depends on**: Nothing

---

## Work Stream E: CodeQL Standard Library Stubs (High)

### Plan E1: HTTP abstraction layer
**Files**: `bridge/compat_http.qll` (new)
**Goal**: Implement `HTTP::ServerRequest`, `HTTP::ResponseBody`, `HTTP::HeaderDefinition`, `HTTP::CookieDefinition` as abstract classes that framework models contribute to.
**Approach**: Create abstract bridge classes that Express, Koa, Fastify etc. extend. This decouples security queries from specific frameworks.
**Est**: ~100 LoC
**Depends on**: B1-B5 (framework models provide concrete implementations)

### Plan E2: DOM class stubs
**Files**: `bridge/compat_dom.qll` (new)
**Goal**: Stub `DOM::Element`, `DOM::DocumentNode`, `DOM::InnerHtmlWrite`, `DOM::AttributeWrite` for DOM XSS queries.
**Approach**: Map to JsxAttribute and FieldWrite patterns that target innerHTML, outerHTML, document.write.
**Est**: ~80 LoC
**Depends on**: Nothing

### Plan E3: CryptographicOperation and sensitive data classes
**Files**: `bridge/compat_crypto.qll` (new)
**Goal**: Stub `CryptographicOperation`, `CleartextLogging`, `SensitiveDataExpr` for crypto/logging queries.
**Approach**: Pattern-match on crypto library imports (crypto, bcrypt, argon2) and logging calls (console.log, winston, pino).
**Est**: ~100 LoC
**Depends on**: Nothing

### Plan E4: DatabaseAccess and FileSystemAccess stubs
**Files**: `bridge/compat_io.qll` (new)
**Goal**: Abstract `DatabaseAccess`, `FileSystemAccess` classes.
**Approach**: Map to existing SQL sink facts and add file system sink patterns (fs.readFile, fs.writeFile, path operations).
**Est**: ~80 LoC
**Depends on**: B6

### Plan E5: RegExp class
**Files**: `bridge/compat_regexp.qll` (new)
**Goal**: Stub `RegExp::Term`, `RegExp::Quantifier`, `RegExp::Group` for regex analysis queries.
**Approach**: Extract regex literals from AST, parse regex syntax into component terms.
**Est**: ~150 LoC (regex parsing is inherently complex)
**Depends on**: C1 (template literals may contain regex)

---

## Work Stream F: Rule 6b Cross-Product Fix (Critical, targeted)

### Plan F1: Add ExprInFunction relation and fix Rule 6b
**Files**: `extract/schema/relations.go`, `extract/walker_v2.go`, `extract/rules/taint.go`
**Goal**: Fix the known cross-product false positive in Rule 6b by adding an `ExprInFunction` relation that scopes sink expressions to functions.
**Approach**: 
1. Register `ExprInFunction(exprId, fnId)` relation
2. Emit it in walker for all expression nodes inside functions
3. Constrain Rule 6b: `ExprInFunction(srcExpr, fn), ExprInFunction(sinkExpr, fn)` or use FlowStar linkage on the sink side
**Est**: ~60 LoC
**Depends on**: Nothing

---

## Parallel Execution Plan

```
Phase 2a (can all start immediately, fully parallel):
  Stream B: B1, B2, B3, B4, B5, B6, B7  (7 independent framework models)
  Stream C: C1, C2, C3, C4, C5, C6       (6 independent extraction patterns)
  Stream D: D1, D2, D3                    (3 independent language features)
  Plan A1                                  (DataFlow predicates)
  Plan F1                                  (Rule 6b fix)
  Plan E2                                  (DOM stubs)
  Plan E3                                  (Crypto stubs)

Phase 2b (depends on 2a completions):
  Plan A2  (depends on A1)
  Plan E1  (depends on B1-B5)
  Plan E4  (depends on B6)
  Plan E5  (depends on C1)

Phase 2c (depends on 2b):
  Plan A3  (depends on A2)
  Plan A4  (depends on A1)

Total: 25 plans
  Phase 2a: 20 plans (fully parallel)
  Phase 2b: 4 plans
  Phase 2c: 2 plans
```

## Priority Order (if serializing)

1. **F1** — Rule 6b fix (correctness bug, 60 LoC)
2. **A1** — DataFlow::Node predicates (unlocks A2-A4)
3. **A2** — Configuration override support (unlocks custom queries)
4. **B6** — Database driver models (highest-value framework gap)
5. **B7** — Sanitizer library models (precision improvement)
6. **B1** — Node.js HTTP module
7. **B4** — AWS Lambda (common in production)
8. **B5** — Next.js API routes (common in modern apps)
9. **C1** — Template literals (blocks string analysis)
10. **E1** — HTTP abstraction layer
11. **A3** — Additional taint steps
12. **B2, B3** — Koa, Fastify
13. **C2-C6** — Remaining extraction patterns
14. **D1-D3** — Language features
15. **E2-E5** — Remaining stdlib stubs
16. **A4** — Path tracking (nice-to-have)

## Estimated Total Effort

~3100 LoC across 25 plans. With parallel execution, the critical path is:
A1 (120) -> A2 (200) -> A3 (100) = 420 LoC serial dependency.
Everything else can execute in parallel around that chain.
