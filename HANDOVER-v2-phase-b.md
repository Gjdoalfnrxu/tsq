# v2 Phase B Handover: Call Graph Construction (CHA + RTA)

## What was built

### 1. Call graph rules (`extract/rules/callgraph.go`)

New `extract/rules` package with `CallGraphRules()` returning 7 programmatically constructed Datalog rules:

1. **Direct resolution** — `CallTarget(call, fn)` via `CallCalleeSym` + `FunctionSymbol`
2. **Concrete class dispatch** — `CallTarget(call, fn)` via `MethodCall` + `ExprType` + `ClassDecl` + `MethodDecl`
3. **CHA interface dispatch** — `CallTarget(call, fn)` via `MethodCall` + `ExprType` + `InterfaceDecl` + `Implements` + `MethodDecl`
4. **Inheritance** — `MethodDeclInherited(childId, name, fn)` via `Extends` + `MethodDecl` + `not MethodDeclDirect`
5. **Direct method base** — `MethodDeclDirect(classId, name, fn)` via `MethodDecl` + `ClassDecl`
6. **Instantiated** — `Instantiated(classId)` via `NewExpr`
7. **RTA** — `CallTargetRTA(call, fn)` like CHA but filtered by `Instantiated(classId)`

All rules are built using `datalog.Rule`, `datalog.Atom`, `datalog.Literal`, `datalog.Var` types. All pass validation, stratification (negation in rule 4 is safe — `MethodDeclDirect` is defined in a lower stratum than `MethodDeclInherited`), and produce correct results through the planner + evaluator.

### 2. System rules injection (`extract/rules/merge.go`)

`MergeSystemRules(prog, systemRules)` returns a new `*datalog.Program` combining system rules with user rules, preserving the original program. System rules are prepended so they're available to user queries.

### 3. Bridge file (`bridge/tsq_callgraph.qll`)

User-facing QL classes: `CallTarget`, `CallTargetRTA`, `Instantiated`. Registered in `embed.go` (embed directive + file list + import path mapping) and `manifest.go` (available classes list including `MethodDeclDirect` and `MethodDeclInherited`).

### 4. Schema additions (`extract/schema/relations.go`)

Five derived relations registered: `CallTarget`, `CallTargetRTA`, `Instantiated`, `MethodDeclDirect`, `MethodDeclInherited`.

### 5. Tests (`extract/rules/callgraph_test.go`)

12 tests covering:
- Direct resolution
- Concrete class method dispatch
- CHA interface dispatch (multiple implementors)
- RTA filtering (only instantiated classes)
- Inheritance (parent method inherited by child)
- Override blocking (child override prevents inheritance)
- **CHA superset RTA property** — verifies every RTA target exists in CHA
- `MergeSystemRules` correctness
- `Instantiated` deduplication
- Rule count (7)
- All rules pass `ValidateRule`
- All rules stratify successfully via `Plan()`

## Files changed

- `extract/rules/callgraph.go` — NEW: call graph rules
- `extract/rules/merge.go` — NEW: system rules merge function
- `extract/rules/callgraph_test.go` — NEW: 12 tests
- `bridge/tsq_callgraph.qll` — NEW: bridge classes
- `bridge/embed.go` — updated embed + loader
- `bridge/manifest.go` — added 5 available classes
- `bridge/embed_test.go` — updated expected file count (10 -> 11)
- `bridge/manifest_test.go` — updated expected class count (45 -> 50)
- `extract/schema/relations.go` — added 5 derived relations
- `extract/schema/relations_test.go` — updated expected relation count (45 -> 50)

## Integration notes

To use call graph rules in the pipeline:
```go
import "github.com/Gjdoalfnrxu/tsq/extract/rules"

// After desugaring user query:
merged := rules.MergeSystemRules(userProg, rules.CallGraphRules())
ep, errs := plan.Plan(merged, sizeHints)
```

## Next steps

- Wire `MergeSystemRules` into the CLI pipeline (cmd/tsq) so call graph rules are automatically injected
- Add end-to-end tests extracting TypeScript fixtures with classes/interfaces and verifying call targets
- Consider adding `MethodDeclInherited` to `MethodDecl` unification so inherited methods participate in call resolution
