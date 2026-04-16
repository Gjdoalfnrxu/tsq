# Phase 1: Inter-Package Boundary Analysis

## Methodology

Each boundary was analysed by reading source files, tracing import paths, identifying shared types, and classifying coupling points by connascence form. "Degree" is the count of distinct coupling points (shared names, types, or conventions) across the boundary.

---

## 1.1 extract/ <-> extract/schema/

**Imports:** `extract/db` imports `extract/schema`; `extract/` does not import `extract/schema/` directly. The coupling is indirect: `extract/` -> `extract/db/` -> `extract/schema/`.

**What crosses the boundary:**
- `schema.RelationDef`, `schema.ColumnDef`, `schema.ColumnType` (TypeInt32, TypeEntityRef, TypeString) -- used by `db.Relation` and `db.DB` to validate tuples, determine column storage format, and serialize/deserialize data.
- `schema.Registry` (global slice) -- iterated by `db.Encode()` for relation ordering and by `db.ReadDB()` via `schema.Lookup()` for deserialization.
- `schema.Lookup(name)` -- called by `db.DB.Relation()` to find definitions by name.
- Relation names as strings -- `extract/walker.go` calls `fw.emit("Node", ...)` where "Node" must match `schema.RelationDef.Name` exactly.

**Dominant connascence form:** **CoN (Name)** at the surface (70+ relation name strings that must match between `walker.go`'s `emit()` calls and `relations.go`'s `RegisterRelation()` calls), with **CoT (Type)** underneath (column types must match the Go types passed to `AddTuple`).

**Degree:** ~70 relation names + 3 column type constants + 5 structural types = ~78 coupling points.

**Direction:** Unidirectional. `extract/schema/` is a pure definition package; it knows nothing about its consumers.

**Necessary vs accidental:** Mostly necessary -- a relational schema registry is the right abstraction for a fact database. The string-based relation name lookup (`fw.emit("Node", ...)`) is **accidental CoN**: a typo in a relation name string causes a runtime panic (via `db.Relation()`'s call to `schema.Lookup()`), not a compile-time error.

**Refactoring opportunity:** Generate typed emit helpers (e.g., `fw.emitNode(id, file, kind, ...)`) from the schema registry to convert runtime CoN to compile-time CoT. This would eliminate the ~70 string-name coupling points.

---

## 1.2 extract/ <-> extract/rules/

**Imports:** `extract/rules/` imports `ql/datalog` (not `extract/` directly). `extract/` does not import `extract/rules/`. The coupling between these two packages is **indirect through shared relation names**.

**What crosses the boundary:**
- Relation name strings: `rules/localflow.go` references predicates like `"Assign"`, `"ExprMayRef"`, `"SymInFunction"`, `"VarDecl"`, `"LocalFlow"`, `"LocalFlowStar"`, etc. These must exactly match the names registered in `extract/schema/relations.go` and the tuples emitted by `extract/walker.go` and `extract/walker_v2.go`.
- Column positional semantics: `rules/` constructs `datalog.Literal` with args at specific positions (e.g., `pos("Assign", w(), v("rhsExpr"), v("lhsSym"))` assumes column 0=lhsNode, 1=rhsExpr, 2=lhsSym). These positions must match the `ColumnDef` order in `schema/relations.go`.

**Dominant connascence form:** **CoP (Position)** -- the Datalog rule arguments are positional, and must match the column ordering in the schema. This is stronger than CoN because reordering columns in a `RelationDef` silently breaks all rules that reference that relation. Secondary: **CoN** on predicate/relation names.

**Degree:** ~30 distinct relation name references across 7 rule files, each with 2-6 positional arguments = ~120 positional coupling points.

**Direction:** Unidirectional. `extract/rules/` depends on the schema defined in `extract/schema/` (via shared string names). `extract/` does not depend on `extract/rules/`.

**Necessary vs accidental:** The positional coupling (CoP) is **accidental** -- Datalog is inherently positional, but the system could use named-column rule construction (e.g., `col("Assign", "rhsExpr", v("rhsExpr"))`) to convert CoP to CoN. The name coupling is necessary.

**Refactoring opportunity:** Introduce a schema-aware rule builder that maps column names to positions, turning CoP into CoN. Would catch column reordering bugs at rule construction time rather than at query evaluation time.

---

## 1.3 extract/ <-> extract/typecheck/

**Imports:** `extract/typecheck/` is self-contained (no imports from `extract/`). `extract/` (via `cmd/tsq/main.go`) imports `extract/typecheck/` for enrichment.

**What crosses the boundary:**
- `typecheck.Client` -- created by `cmd/tsq/` and passed to `typecheck.NewEnricher()`.
- `typecheck.Enricher` -- called by `cmd/tsq/` to query tsgo for type info.
- `typecheck.Position` -- a simple `{Line, Col int}` struct used to specify query positions.
- `typecheck.TypeFact` -- the enrichment result struct containing `{Line, Col, TypeDisplay, TypeHandle}`.
- `typecheck.DetectTsgo()` -- auto-detection utility called by `cmd/tsq/`.
- `typecheck.WriteTypeFacts()` -- callback-based function accepting `emit`, `posNodeID`, `symID`, `typeEntityID` callbacks. These callbacks are implemented in `cmd/tsq/main.go` using `extract.PositionNodeID`, `extract.SymID`, `extract.TypeEntityID`.

**Dominant connascence form:** **CoT (Type)** -- the boundary is typed structs and function signatures. The callback pattern in `WriteTypeFacts` introduces mild **CoP** (callback argument order).

**Degree:** ~8 exported types/functions crossing the boundary.

**Direction:** Unidirectional. `typecheck/` is a leaf package; `cmd/tsq/` and indirectly `extract/` consume it.

**Necessary vs accidental:** Mostly necessary. The callback-based `WriteTypeFacts` is slightly accidental -- it exists to decouple `typecheck/` from `extract/db`, but the callbacks create an indirect coupling that could be simplified.

**Refactoring opportunity:** Minor. The `WriteTypeFacts` callback pattern could be replaced by having `typecheck/` return structured data and letting the caller write it, which is essentially what `EnrichFile` already does. `WriteTypeFacts` is redundant with the direct write path in `cmd/tsq/main.go`.

---

## 1.4 extract/ <-> extract/db/

**Imports:** `extract/db` imports `extract/schema`. `extract/walker.go` imports `extract/db`.

**What crosses the boundary:**
- `db.DB` -- created by the caller, passed to `NewFactWalker(database)`, used throughout extraction.
- `db.Relation` -- returned by `db.DB.Relation(name)`, used to add tuples.
- `db.Relation.AddTuple(db, vals...)` -- the primary write API. Arguments are `interface{}` values that must match the column types from the schema.
- `db.SchemaVersion` -- referenced by `walker.go` to emit the SchemaVersion tuple.
- `db.NewDB()` -- called by `cmd/tsq/` to create the database.
- `db.DB.Encode(w)` / `db.ReadDB(r, size)` -- serialization/deserialization.
- `db.Relation.Tuples()`, `db.Relation.GetInt()`, `db.Relation.GetString()` -- read API used by `cmd/tsq/` for enrichment position collection.

**Dominant connascence form:** **CoT (Type)** on the `db.DB` and `db.Relation` types. **CoM (Meaning)** on `AddTuple`'s `interface{}` variadic args -- the caller must know which positions expect `int32` vs `uint32` vs `string`, and what constitutes an entity ref vs a plain int. A wrong Go type (e.g., passing `int` where `string` is expected) produces a runtime error, not a compile-time one.

**Degree:** ~12 API surface points (2 types, ~10 methods).

**Direction:** Bidirectional in a limited sense. `extract/walker.go` depends on `db.DB` (writes). `cmd/tsq/main.go` depends on `db.DB` (reads/writes). `db/` depends on `schema/`.

**Necessary vs accidental:** The `interface{}` variadic API for `AddTuple` is **accidental CoM** -- it trades type safety for convenience. The boundary types themselves are necessary.

**Refactoring opportunity:** Generate typed `AddXxx()` methods per relation (e.g., `r.AddNodeTuple(id, file, kind, startLine, startCol, endLine, endCol)`) to convert CoM to CoT.

---

## 1.5 ql/parse/ -> ql/ast/

**Imports:** `ql/parse` imports `ql/ast`.

**What crosses the boundary:**
- The entire `ast` package -- all AST node types (`Module`, `ClassDecl`, `PredicateDecl`, `Formula` interface, `Expr` interface, and ~25 concrete node types).
- `ast.Span` -- attached to every node.
- `ast.TypeRef`, `ast.VarDecl`, `ast.ParamDecl`, `ast.Annotation` -- structural types.
- The parser's return type: `func (p *Parser) Parse() (*ast.Module, error)`.

**Dominant connascence form:** **CoT (Type)** -- the parser constructs and returns `ast.*` types. This is a classic producer-consumer relationship. Every AST node type is a coupling point.

**Degree:** ~30 AST types constructed by the parser.

**Direction:** Strictly unidirectional. `parse/` produces `ast.*` nodes; `ast/` has no knowledge of `parse/`.

**Necessary vs accidental:** Entirely necessary. A parser must produce an AST; the type coupling is inherent to the architecture.

**Refactoring opportunity:** None needed. This is clean, well-separated coupling.

---

## 1.6 ql/ast/ -> ql/resolve/

**Imports:** `ql/resolve` imports `ql/ast`.

**What crosses the boundary:**
- `ast.Module` -- input to `resolve.Resolve()`.
- All AST types -- the resolver traverses the full AST, type-switching on `Formula` and `Expr` interface implementations.
- `ast.Span` -- used for error/warning location reporting.
- `ast.ClassDecl`, `ast.PredicateDecl`, `ast.MemberDecl` -- stored by pointer in `resolve.Environment`.
- `ast.ParamDecl` -- stored in `resolve.VarBinding`.
- `resolve.ResolvedModule` -- contains `*ast.Module` plus resolution side-tables.
- `resolve.Environment` -- maps class/predicate names to `*ast.ClassDecl` / `*ast.PredicateDecl`.
- `resolve.Annotations` -- maps `ast.Expr` and `*ast.Variable` to resolution results.
- `ast.MemberDefiningClass()` -- shared utility in `ast/heritage.go` called by both `resolve/` and `desugar/`.

**Dominant connascence form:** **CoT (Type)** -- heavy use of AST types. Some **CoI (Identity)** -- `resolve.Annotations` uses pointer identity of AST nodes as map keys (`map[ast.Expr]*Resolution`, `map[*ast.Variable]VarBinding`), meaning the resolver and the desugarer must operate on the **same** AST objects, not copies.

**Degree:** ~20 AST types referenced + 6 resolve output types + identity-keyed maps = ~28 coupling points.

**Direction:** Unidirectional. `resolve/` depends on `ast/`; `ast/` has a small reverse dependency via `heritage.go` which provides `MemberDefiningClass` (a shared utility, but defined in `ast/` to be importable by both `resolve/` and `desugar/`).

**Necessary vs accidental:** Mostly necessary. The **CoI** (pointer identity for annotation maps) is **accidental** and fragile -- any AST transformation that creates new node objects (e.g., cloning for immutability) would silently break resolution annotations.

**Refactoring opportunity:** Use node IDs or spans as map keys instead of pointer identity to eliminate CoI.

---

## 1.7 ql/resolve/ -> ql/desugar/

**Imports:** `ql/desugar` imports `ql/ast`, `ql/datalog`, and `ql/resolve`.

**What crosses the boundary:**
- `resolve.ResolvedModule` -- the primary input to `desugar.Desugar()`.
- `resolve.Annotations` -- accessed for `ExprResolutions` and `VarBindings` (using the same pointer-identity keys as resolve).
- `resolve.Environment` -- accessed for class hierarchy (`Classes`, `Predicates`, `Imports`).
- All `ast.*` types -- the desugarer walks the AST, constructing Datalog rules from each predicate/class.
- `ast.MemberDefiningClass()` -- shared algorithm for supertype member lookup.

**Dominant connascence form:** **CoI (Identity)** -- the desugarer accesses `resolve.Annotations` maps keyed by AST node pointers. It **must** receive the exact same AST object graph that the resolver annotated. **CoA (Algorithm)** -- `MemberDefiningClass` is a shared algorithm between resolve and desugar (already deduplicated into `ast/heritage.go`, which is good).

**Degree:** ~15 types from resolve + ~20 AST types + 1 shared algorithm = ~36 coupling points.

**Direction:** Unidirectional. `desugar/` depends on `resolve/` and `ast/`.

**Necessary vs accidental:** The CoI is accidental (see 1.6). The algorithm sharing via `ast/heritage.go` is a good pattern that eliminates what was previously CoA.

**Refactoring opportunity:** Same as 1.6 -- eliminate pointer-identity-based annotation maps.

---

## 1.8 ql/desugar/ -> ql/datalog/

**Imports:** `ql/desugar` imports `ql/datalog`.

**What crosses the boundary:**
- `datalog.Program` -- the output of `Desugar()`.
- `datalog.Rule`, `datalog.Atom`, `datalog.Literal`, `datalog.Query` -- constructed by the desugarer.
- `datalog.Term` interface and its implementations: `datalog.Var`, `datalog.IntConst`, `datalog.StringConst`, `datalog.Wildcard`.
- `datalog.Comparison`, `datalog.Aggregate` -- constructed for comparison and aggregate subgoals.

**Dominant connascence form:** **CoT (Type)** -- the desugarer is a pure producer of `datalog.*` IR types. Clean producer-consumer pattern.

**Degree:** ~12 datalog types constructed.

**Direction:** Strictly unidirectional. `desugar/` produces `datalog.*` structures; `datalog/` is a pure data definition package.

**Necessary vs accidental:** Entirely necessary.

**Refactoring opportunity:** None needed. Clean separation.

---

## 1.9 ql/datalog/ -> ql/plan/

**Imports:** `ql/plan` imports `ql/datalog`.

**What crosses the boundary:**
- `datalog.Program` -- input to `plan.Plan()` and `plan.WithMagicSet()`.
- `datalog.Rule` -- iterated for validation, stratification, and join ordering.
- `datalog.Atom`, `datalog.Literal`, `datalog.Term` -- inspected during planning (variable binding analysis, join column computation).
- `datalog.Aggregate` -- extracted from rule bodies for aggregate planning.
- `plan.ExecutionPlan` -- the output, containing `plan.Stratum` (which wraps `plan.PlannedRule`).
- `plan.PlannedRule.Head` is `datalog.Atom` and `plan.JoinStep.Literal` is `datalog.Literal` -- the plan embeds datalog types directly.

**Dominant connascence form:** **CoT (Type)** -- plan types embed datalog types. This is tighter than the parse->ast boundary because the plan doesn't just construct new types -- it **embeds** datalog types within its own structures (`PlannedRule.Head` is `datalog.Atom`).

**Degree:** ~10 datalog types referenced + 5 plan output types = ~15 coupling points.

**Direction:** Unidirectional. `plan/` depends on `datalog/`.

**Necessary vs accidental:** Mostly necessary. The embedding of `datalog.Atom` and `datalog.Literal` inside plan types is a reasonable design choice -- creating wrapper types would add boilerplate without benefit.

**Refactoring opportunity:** None needed.

---

## 1.10 ql/plan/ -> ql/eval/

**Imports:** `ql/eval` imports `ql/plan`, `extract/db`, and `extract/schema`.

**What crosses the boundary:**
- `plan.ExecutionPlan` -- input to `eval.NewEvaluator()` and `eval.Evaluate()`.
- `plan.Stratum`, `plan.PlannedRule`, `plan.JoinStep`, `plan.PlannedQuery`, `plan.PlannedAggregate` -- traversed during evaluation.
- `datalog.Literal`, `datalog.Atom`, `datalog.Term` (via plan types) -- matched against during join execution.
- `db.DB` -- input to `eval.NewEvaluator()` for loading base facts.
- `schema.Registry` and `schema.ColumnType` -- used by `loadBaseRelations()` to convert `db.Relation` tuples into `eval.Relation` tuples.
- `eval.ResultSet` -- the output type (containing `eval.Value`, `eval.IntVal`, `eval.StrVal`).

**Dominant connascence form:** **CoT (Type)** on plan types. **CoM (Meaning)** on the value mapping: `schema.TypeInt32`/`schema.TypeEntityRef` -> `eval.IntVal`, `schema.TypeString` -> `eval.StrVal`. The evaluator must agree with the schema on what column types mean. This is a secondary mapping layer between the extract-side type system and the eval-side type system.

**Degree:** ~15 plan types + ~8 eval types + ~5 schema/db types = ~28 coupling points.

**Direction:** Unidirectional from plan to eval. Eval also has a direct dependency on `extract/db` and `extract/schema` (for base fact loading), creating a cross-subsystem dependency.

**Necessary vs accidental:** The cross-subsystem dependency (`ql/eval` -> `extract/db`, `extract/schema`) is **accidental** -- it means the query engine has a hard dependency on the extraction layer, preventing independent testing without the extract package. The plan->eval coupling is necessary.

**Refactoring opportunity:** Extract `loadBaseRelations` into a separate adapter package (or make it a function of `cmd/tsq/`) so `ql/eval/` depends only on `ql/plan/` and its own `Relation` type. The fact-loading logic would move to the orchestrator.

---

## 1.11 extract/schema/ <-> bridge/

**Imports:** `bridge/manifest.go` imports `extract/schema`.

**What crosses the boundary:**
- `schema.Registry` -- iterated by `CapabilityManifest.AllRelationsCovered()` to verify that every schema relation has a bridge class.
- Relation name strings -- the `AvailableClass.Relation` field in the manifest must exactly match `RelationDef.Name` values from the schema.
- Import path strings -- `bridge/embed.go` defines a mapping from QL import paths (e.g., `"tsq::base"`) to `.qll` filenames. These import paths are used by `ql/resolve/` during import loading.
- `.qll` file contents -- the bridge files define QL classes whose predicates reference relation names that must match the schema.

**Dominant connascence form:** **CoN (Name)** -- three-way name agreement between (1) `schema.RelationDef.Name`, (2) `AvailableClass.Relation` in the manifest, and (3) predicate references inside `.qll` files. This is high-degree CoN because adding a new relation requires updating all three.

**Degree:** ~80 relation name references in the manifest + import path mapping (~30 entries) = ~110 coupling points.

**Direction:** `bridge/` depends on `extract/schema/` (unidirectional in Go imports). But the `.qll` file contents create an implicit bidirectional coupling: schema changes require `.qll` updates.

**Necessary vs accidental:** Partially accidental. The manifest duplicates schema information. The `.qll` files duplicate relation names. This triple redundancy is a maintenance burden.

**Refactoring opportunity:** Generate the manifest and `.qll` stubs from the schema registry. Would eliminate ~80 manually-maintained name couplings.

---

## 1.12 bridge/ <-> ql/ (via import loading)

**Imports:** `bridge/embed.go` defines `ImportLoader()` which returns a function matching the signature expected by `resolve.Resolve()`. In `cmd/tsq/main.go`, `makeBridgeImportLoader()` duplicates the path-to-file mapping and returns `func(path string) (*ast.Module, error)`.

**What crosses the boundary:**
- Import path strings -- must match between user `.ql` files' `import` declarations, `bridge.ImportLoader`'s path-to-file map, and `resolve.Resolve`'s import processing.
- `ast.Module` -- bridge `.qll` files are parsed into `*ast.Module` by the parser, then resolved recursively by `resolve.Resolve`.
- The import loader callback signature: `func(path string) (*ast.Module, error)`.
- QL class and predicate names defined in `.qll` files -- these must match what user queries reference.

**Dominant connascence form:** **CoN (Name)** on import paths and QL identifiers. The path-to-file mapping in `bridge/embed.go` is **duplicated** in `cmd/tsq/main.go` (`makeBridgeImportLoader()`), introducing **CoV (Value)** -- both maps must contain the same entries.

**Degree:** ~30 import path mappings (duplicated in two locations) + QL identifier names across ~30 `.qll` files.

**Direction:** Bidirectional. `bridge/` provides the `.qll` content and path mapping; `ql/resolve/` consumes it via the callback. The duplication in `cmd/tsq/` adds a third coupling point.

**Necessary vs accidental:** The duplication of the path-to-file map between `bridge/embed.go` and `cmd/tsq/main.go` is **clearly accidental CoV**. Both must stay in sync manually.

**Refactoring opportunity:** Eliminate the duplication: have `cmd/tsq/main.go` use `bridge.ImportLoader()` directly (adapting its signature), or extract the path map into a shared constant. This would halve the coupling surface.

---

## 1.13 cmd/tsq/ -> everything

**Imports:** `cmd/tsq/main.go` imports: `bridge`, `extract`, `extract/db`, `extract/typecheck`, `output`, `ql/ast`, `ql/desugar`, `ql/eval`, `ql/parse`, `ql/plan`, `ql/resolve`.

**What crosses the boundary:**
- All major types from every package: `extract.ProjectConfig`, `extract.ExtractorBackend`, `extract.TreeSitterBackend`, `extract.VendoredBackend`, `extract.TypeAwareWalker`, `db.DB`, `db.ReadDB`, `typecheck.Client`, `typecheck.Enricher`, `typecheck.Position`, `parse.Parser`, `ast.Module`, `resolve.Resolve`, `desugar.Desugar`, `plan.Plan`, `eval.NewEvaluator`, `eval.ResultSet`, `output.WriteSARIF`, `output.WriteJSONLines`, `output.WriteCSV`, `bridge.LoadBridge`, `bridge.V1Manifest`.
- Pipeline orchestration: `cmd/tsq/` drives the entire pipeline: parse -> resolve -> desugar -> plan -> eval -> output.
- The `nonTaintablePrimitives` map in `cmd/tsq/main.go` -- domain knowledge about TypeScript type semantics that arguably belongs in `extract/` or `bridge/`.
- `extract.FileID`, `extract.SymID`, `extract.PositionNodeID`, `extract.TypeEntityID` -- ID generation functions called during enrichment.

**Dominant connascence form:** **CoEx (Execution)** -- the CLI must execute pipeline stages in the correct order (parse before resolve, resolve before desugar, etc.). Also heavy **CoT** on the types from every imported package.

**Degree:** ~40+ imported types and functions across 11 packages.

**Direction:** Strictly unidirectional. `cmd/tsq/` depends on everything; nothing depends on `cmd/tsq/`.

**Necessary vs accidental:** The CoEx is mostly necessary -- a pipeline orchestrator inherently has execution order dependencies. Some coupling is accidental: `nonTaintablePrimitives` is domain knowledge living in the wrong place, and the duplicated import path map (see 1.12) adds unnecessary coupling.

**Refactoring opportunity:** Move `nonTaintablePrimitives` to `extract/schema/` or `bridge/`. Extract the compilation pipeline (`parse -> resolve -> desugar -> plan`) into a `ql.Compile()` helper to reduce the surface area of `cmd/tsq/`.

---

## 1.14 output/ <- ql/eval/

**Imports:** `output/` imports `ql/eval`.

**What crosses the boundary:**
- `eval.ResultSet` -- the primary input to all three formatters (`WriteSARIF`, `WriteJSONLines`, `WriteCSV`).
- `eval.Value` interface and its implementations `eval.IntVal`, `eval.StrVal` -- type-switched in the formatters for value extraction.
- `eval.ValueToString()` -- utility function called by CSV and SARIF formatters.
- `eval.ResultSet.Columns` (column names as `[]string`) -- used for CSV headers, JSON keys, and SARIF location heuristics.
- `eval.ResultSet.Rows` (`[][]Value`) -- iterated by all formatters.

**Dominant connascence form:** **CoT (Type)** -- the formatters depend on the `ResultSet` and `Value` types. Some **CoM (Meaning)** -- SARIF formatting uses column name heuristics (e.g., column named "file" is treated as a URI, "line" as a line number). This is **semantic coupling**: the formatter assigns meaning to column names that is not declared anywhere in the schema.

**Degree:** ~6 types/functions crossing the boundary.

**Direction:** Strictly unidirectional. `output/` depends on `ql/eval/`; `eval/` has no knowledge of `output/`.

**Necessary vs accidental:** The CoT is necessary. The CoM (column name heuristics) is **accidental** -- it creates an implicit contract between query authors and the SARIF formatter about what column names mean.

**Refactoring opportunity:** Define a formal `Location` annotation type in the evaluation result (or allow queries to explicitly mark location columns) instead of relying on name heuristics.
