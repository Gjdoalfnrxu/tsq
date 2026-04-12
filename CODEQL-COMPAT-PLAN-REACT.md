# CodeQL Compat — Verbatim React `useState` Query Plan

**Scope:** make the two queries in the brief parse and evaluate **verbatim** on tsq, returning sensible results against `/tmp/react-fixture/facts.db`. Subset of master plan Phase 2d (framework models) plus `DataFlow` API additions.

**Branch:** `codeql-compat-react-plan`. Concurrent branch `real-query-useState` (tsq-native version) is off-limits.

**Reference query:**

```ql
import javascript
import semmle.javascript.frameworks.React

from ReactComponent c, DataFlow::CallNode useStateCall, DataFlow::FunctionNode updater,
     DataFlow::CallNode innerCall
where useStateCall = DataFlow::moduleMember("react", "useState").getACall()
  and updater.flowsTo(useStateCall.getASuccessor+().(DataFlow::CallNode).getArgument(0))
  and innerCall.getEnclosingFunction() = updater.getFunction()
select updater, innerCall
```

---

## 1. Module path resolution

**Today:** `bridge/embed.go` `ImportLoader` is a flat string→filename map. Resolves `javascript`, `DataFlow::PathGraph`, `TaintTracking`, `semmle.javascript.security.dataflow.*`. The key `DataFlow::PathGraph` is misleading — it just maps the literal string to `compat_dataflow.qll`; nothing parses dotted/scoped paths structurally.

**Gap:** `import semmle.javascript.frameworks.React` is not in the map. There is no `import javascript` either as a separate canonical key (it is — works today). `DataFlow::moduleMember(...)` is a predicate reference inside the `DataFlow` module, not an import — that lives in compat_dataflow.qll once we add it.

**Change:**
- Add map entries in `bridge/embed.go` (lines ~80–86):
  - `"semmle.javascript.frameworks.React"` → `"compat_react.qll"` (new file).
- Add `compat_react.qll` to the `//go:embed` directive (line 5) and the `files` slice (lines 11–36).
- No changes to `ql/resolve/resolve.go` — `processImports` already calls the loader by literal path string and merges declarations from the loaded module into the env. Dotted paths are opaque to the resolver, which is what we want.

**No walker / parser changes needed for module paths.**

---

## 2. Bridge classes & predicates required

The query references the following symbols. Each must be backed by a tsq relation, either directly or via a derived datalog rule.

| Symbol | File | Backed by | Status today | Est. lines |
|---|---|---|---|---|
| `ReactComponent` | `compat_react.qll` (new) | `Function` + JSX-returning + capitalized name (datalog rule) | Missing — needs new `ReactComponent(fnId)` derived relation | 25 |
| `DataFlow::CallNode` | `compat_dataflow.qll` | `Call` + `ExprIsCall` (wraps a Call entity) | Missing | 30 |
| `DataFlow::FunctionNode` | `compat_dataflow.qll` | `Function` | Missing | 20 |
| `DataFlow::Node` (already exists) | `compat_dataflow.qll` | `Symbol` | Exists — needs `getASuccessor` predicate added | +15 |
| `DataFlow::moduleMember(string, string)` | `compat_dataflow.qll` | `ImportBinding` | Missing — class returning an "abstract" node whose `.getACall()` resolves to the Call set | 25 |
| `.getACall()` (on the moduleMember result) | `compat_dataflow.qll` | `ImportBinding` ⨝ `ExprMayRef` ⨝ `ExprIsCall` ⨝ `Call` | Missing | (in the 25 above) |
| `DataFlow::CallNode.getArgument(int)` | `compat_dataflow.qll` | `CallArg` | Missing | 8 |
| `DataFlow::CallNode.getASuccessor()` and `+` closure | `compat_dataflow.qll` | `LocalFlow` (intra-proc) — see §3 | Partially backed | 15 |
| `DataFlow::FunctionNode.getFunction()` | `compat_dataflow.qll` | identity over `Function` id | Missing | 5 |
| `DataFlow::FunctionNode.flowsTo(Node)` | `compat_dataflow.qll` | `LocalFlowStar` from the function's symbol to the target sym | Missing — see §3 | 15 |
| `innerCall.getEnclosingFunction()` | `compat_dataflow.qll` (on `CallNode`) | `FunctionContains(fnId, callNode)` | Backing fact exists | 8 |

**Total new code in compat_*.qll:** ~165 lines across `compat_dataflow.qll` (+130) and `compat_react.qll` (+45 incl. header comment).

`compat_react.qll` will declare `ReactComponent` extending `@function` with a characteristic predicate that joins on a new `ReactComponent(fnId)` relation populated by datalog.

---

## 3. Dataflow predicates: `flowsTo` + `getASuccessor+()`

CodeQL semantics:
- `Node.getASuccessor()` = one local dataflow step out of this node.
- `Node.getASuccessor+()` = transitive closure of local dataflow steps.
- `Node.flowsTo(Node)` = local dataflow from this to that (essentially `getASuccessor*`).

**Backing in tsq:**
- Per-function: `LocalFlow(fnId, src, dst)` and `LocalFlowStar(fnId, src, dst)` (already computed in `extract/rules/localflow.go`).
- Whole-program: `FlowStar(src, dst)` — exists but no fnId.

**Mapping:**
- `Node.getASuccessor()` ⇒ `exists(int fn | LocalFlow(fn, this, result))`. Crosses no function boundaries — matches CodeQL "local" semantics.
- `Node.getASuccessor+()` ⇒ same with `LocalFlowStar`. tsq's `LocalFlowStar` is the transitive (NOT reflexive) closure — matches `+` exactly.
- `Node.flowsTo(Node)` ⇒ for compatibility with the query, defined as `LocalFlowStar(_, this, target)` (same-function reachability). The query uses `updater.flowsTo(useStateCall.getASuccessor+()...getArgument(0))`. The updater (a function-expression node) and the useState call live in the same component function — `LocalFlowStar` covers it.

**Precision call-out:** real CodeQL `flowsTo` is intra-procedural plus stores/loads modeling. tsq's `LocalFlow` covers assignment, var-decl init, return, destructuring (per `extract/rules/localflow.go`). It does NOT cover field stores/loads symbolically. For the fixture this is fine — the updater is passed directly. **Risk if a query uses an indirection through an object field — out of scope here.**

**`updater` as a `Node`:** `DataFlow::FunctionNode` has to project to a Node-shaped key so that `flowsTo` can join. Implementation: `FunctionNode` extends `@function`, and its `flowsTo(Node n)` joins via `FunctionSymbol(sym, this) and LocalFlowStar(_, sym, n)`. `FunctionSymbol` already exists.

**Argument cast `(DataFlow::CallNode).getArgument(0)`:** `CallNode` and `Node` both wrap symbol/call IDs. The `(...).(DataFlow::CallNode)` syntax is CodeQL's downcast — already supported by tsq's QL evaluator (instance-of join). We just need the characteristic predicate to be defined. The successor chain ends at a symbol → we re-bind via `ExprIsCall` / `CallArg` to recover a `CallNode` whose 0th arg sym is the successor target.

---

## 4. `ReactComponent` detection

**Approach:** datalog rule, no walker change.

Definition (matches CodeQL's `ReactComponent` for the function-component case): a function whose name starts with an uppercase letter and which contains a JSX element OR returns JSX.

**Rule** (`extract/rules/frameworks.go`, new function `reactComponentRules()`):

```
ReactComponent(fn) :-
    Function(fn, name, _, _, _, _),
    __builtin_string_charAt(name, 0, c),  // first char
    __builtin_string_toUpperCase(c, c),    // is uppercase
    FunctionContains(fn, jsxNode),
    JsxElement(jsxNode, _, _).
```

`__builtin_string_*` predicates already exist (Phase 1e). The "starts with uppercase" check uses charAt + toUpperCase equality. If that turns out to be too clunky in datalog, fall back to a tiny walker pass populating `ReactComponent(fnId)` directly during AST walking (cleaner; ~15 lines in `extract/walker_v2.go`).

**New relation** in `extract/schema/relations.go`:
```go
RegisterRelation(RelationDef{Name: "ReactComponent", Version: 2, Columns: []ColumnDef{
    {Name: "fnId", Type: TypeEntityRef},
}})
```

Schema count bumps 72 → 73.

---

## 5. Walker / fact gaps

| Gap | Resolution | Where |
|---|---|---|
| `ReactComponent(fnId)` | New derived relation, datalog rule (or walker pass) | §4 |
| Mapping a call expression node to its enclosing function | Already covered by `FunctionContains(fnId, nodeId)` | none |
| `useState` import resolution | Already covered by `ImportBinding(sym, "react", "useState")` | none |
| `useState(arg0)` call site | Already covered by `Call`, `CallArg`, `CallCalleeSym`, `ExprMayRef` | none |
| Function expression as updater (the inline `prev => ...`) | `Function` + `FunctionSymbol` already emitted by walker for arrow funcs (verified: `extract/walker_v2.go` walks arrow_function); the function flows to the call-arg sym via `LocalFlow` rule for "anon function passed as arg" | verify in PR1 — if missing, add a `LocalFlow(fn, fnSym, argSym)` rule |
| `LocalFlow` from anon function expr to enclosing call's argument symbol | Likely missing — `localflow.go` rules cover var-decl init and assignment but may not cover an inline arrow as a `CallArg` whose `ExprMayRef` introduces a fresh sym. Audit needed. | §6 PR2 |

**Most likely walker change:** none. Most likely datalog change: a one-rule addition for inline-function-as-argument flow, IF the audit shows it isn't already covered transitively via `ExprMayRef`.

---

## 6. PR sequence

Each PR rebases on `main`, runs the pre-commit hook, ships green CI, and is mergeable on its own.

### PR 1 — `ReactComponent` relation + walker/datalog backing
- **Title:** "compat: ReactComponent fact + datalog rule"
- **Files:**
  - `extract/schema/relations.go` (+5)
  - `extract/rules/frameworks.go` (+25)
  - `extract/rules/frameworks_test.go` (+60)
  - `bridge/manifest.go` count bump
- **Acceptance:** unit test asserts `Counter` in fixture is in `ReactComponent`, `helper` is not.
- **Size:** ~100 lines.

### PR 2 — Audit & fix LocalFlow for inline-function arguments
- **Title:** "localflow: anon function expression as call argument"
- **Files:**
  - `extract/rules/localflow.go` (+10 if needed)
  - `extract/rules/localflow_test.go` (+50)
- **Acceptance:** test asserts `LocalFlow(counterFn, updaterArrowSym, useStateArg0Sym)` exists for the fixture's `setCount(prev => ...)` site.
- **Size:** ~60 lines. **Skip if audit shows it's already covered.**

### PR 3 — `compat_dataflow.qll` extensions: `CallNode`, `FunctionNode`, `getASuccessor`
- **Title:** "compat: DataFlow::CallNode, FunctionNode, getASuccessor"
- **Files:**
  - `bridge/compat_dataflow.qll` (+90 inside the existing `module DataFlow { }`)
  - `bridge/compat_dataflow_test.go` (+80) — runs queries against react fixture, asserts result counts
- **Acceptance:** standalone QL snippets `from DataFlow::CallNode c select c` and `from DataFlow::FunctionNode f select f` return correct counts on `/tmp/react-fixture/facts.db`.
- **Size:** ~170 lines.

### PR 4 — `compat_dataflow.qll`: `moduleMember(...).getACall()` + flowsTo
- **Title:** "compat: DataFlow::moduleMember + flowsTo"
- **Files:**
  - `bridge/compat_dataflow.qll` (+40)
  - `bridge/compat_dataflow_test.go` (+60)
- **Acceptance:** `DataFlow::moduleMember("react", "useState").getACall()` returns 2 calls on the fixture.
- **Size:** ~100 lines.

### PR 5 — `compat_react.qll` + import map wiring
- **Title:** "compat: semmle.javascript.frameworks.React module"
- **Files:**
  - `bridge/compat_react.qll` (new, ~45 lines)
  - `bridge/embed.go` (+3)
  - `bridge/embed_test.go` (+10) — asserts the new path resolves
- **Acceptance:** `import semmle.javascript.frameworks.React` parses and resolves; `from ReactComponent c select c` returns `Counter`.
- **Size:** ~60 lines.

### PR 6 — Verbatim end-to-end golden test (the queries from the brief)
- **Title:** "compat: golden test for verbatim React useState query"
- **Files:**
  - `testdata/compat/react_useState_q1.ql` (the exact Query 1 verbatim)
  - `testdata/compat/react_useState_q2.ql` (Query 2 — see §7 note)
  - `testdata/compat/react_useState.expected` (golden output)
  - `bridge/compat_test.go` (+50) — wires golden run
- **Acceptance:**
  - Q1 returns `(updaterArrow_onClick, helperCall)` and `(updaterArrow_onReset, setNameCall)`.
  - Q2 returns only `(updaterArrow_onReset, setNameCall)` — the inner call must itself be a setState from another useState destructuring.
- **Size:** ~120 lines including .ql and golden.

**Cumulative estimate: ~610 lines across 6 PRs.**

---

## 7. Acceptance test specifics

**Fixture:** `/tmp/react-fixture/facts.db` (built from `/tmp/react-fixture/src/app.tsx`, content shown below).

```tsx
const [count, setCount] = useState(0);
const [name, setName]   = useState("");

const onClick = () => { setCount(prev => helper(prev)); };           // Q1 + Q2 (helper, not setState)  → Q1 only
const onReset = () => { setCount(prev => { setName(""); return 0; }); }; // Q1 + Q2
const onClear = () => setCount(0);            // not an updater function — no match
const onBump  = () => setCount(prev => prev + 1); // updater w/o inner call — no match
```

**Q1 expected rows (2):**
- `(prev => helper(prev), helper(prev))`
- `(prev => { setName(""); ... }, setName(""))`

**Q2 expected rows (1):** the inner call's callee symbol must resolve back to a binding from a useState destructure — only `setName(...)` qualifies. Q2 verbatim text needs to be written; the brief leaves it as a comment. **Plan PR6 to write Q2 as the natural extension** — `innerCall.getCalleeSym() = setterSym and ArrayDestructure(_, 1, setterSym) and VarDecl(_, setterSym, useStateInit, _) and ExprIsCall(useStateInit, otherUseState) and otherUseState matches DataFlow::moduleMember("react","useState").getACall()`.

**Golden file** captures both result sets exactly.

---

## 8. Risks

1. **Inline-function-expression dataflow.** If `LocalFlow` does not currently produce an edge from the arrow-function symbol to the call-argument symbol, Q1's `flowsTo` clause silently returns nothing. PR2 audits and fixes. **Risk: medium — high impact if missed.**

2. **`getASuccessor+()` projecting to a `CallNode` via downcast.** tsq's QL evaluator handles `expr.(Type)` casts via class characteristic predicate joins, but the cast happens *inside* a transitive-closure expression — not exercised before. May expose a desugar bug in `pred+(x,y).(Type).getArgument(0)`. **Risk: medium.** Mitigation in PR4: rewrite as a let-binding helper predicate if the inline form fails to desugar.

3. **`ReactComponent` overdetection.** Capitalized + contains-JSX is broad. The fixture has only one such function so it's fine for the golden, but a real codebase might pull in HOCs that don't take props. Documented as a known limitation; refinement is out of scope for this plan. **Risk: low for this query, low overall.**

4. **`CallArg` entity collapse** — known v1 caveat noted in `tsq_calls.qll` (col-0 keying collapses sibling args into one entity). The query uses `getArgument(0)` specifically, which is unaffected (idx 0 disambiguates), but any future query iterating arguments could hit this. **Risk: low for this query.**

5. **`DataFlow::moduleMember` is a predicate, not a class.** Real CodeQL implements it as a predicate returning a `SourceNode`. tsq's compat layer needs to expose it as either a predicate that returns a node-typed value or a class with a static factory pattern. The QL syntax `DataFlow::moduleMember("react","useState").getACall()` requires the result of `moduleMember` to itself be a typed entity with a `getACall` member predicate. **Implementation: model `moduleMember(string, string)` as a predicate returning an `ImportBinding`-backed node, with `getACall` defined on a wrapper class.** **Risk: medium — the cleanest model needs care to round-trip through tsq's resolver.**

---

## 9. Files touched (summary)

| File | PRs | Net lines |
|---|---|---|
| `extract/schema/relations.go` | 1 | +5 |
| `extract/rules/frameworks.go` (+test) | 1 | +85 |
| `extract/rules/localflow.go` (+test) | 2 | ~+60 (conditional) |
| `bridge/compat_dataflow.qll` (+test) | 3, 4 | +270 |
| `bridge/compat_react.qll` (new) | 5 | +45 |
| `bridge/embed.go` (+test) | 5 | +13 |
| `testdata/compat/react_useState_*` | 6 | ~+120 |
| `bridge/compat_test.go` | 6 | +50 |
| **Total** | **6 PRs** | **~610 lines** |
