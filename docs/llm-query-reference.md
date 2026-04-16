# tsq LLM Query Reference

A concise reference for writing tsq QL queries. Written for LLMs and humans who know what they want to find but need to know which bridge classes and patterns to use.

---

## Quick-start

```bash
# 1. Extract facts from a TypeScript project
tsq extract -dir ./my-app -output app.db

# 2. Run a query
tsq query -db app.db my-query.ql

# 3. Choose output format
tsq query -db app.db -format json my-query.ql
tsq query -db app.db -format csv  my-query.ql
tsq query -db app.db -format sarif my-query.ql
```

---

## Query structure

Every query follows this skeleton:

```ql
import tsq::<module>        // one or more imports

from <Type> varName         // declare variables (type-filtered)
where <conditions>          // optional filter
select varName as "label"   // columns to output
```

Multiple `from` bindings are joined (cross-product filtered by `where`). You can `select` multiple columns.

---

## Module imports

| Import | Provides |
|--------|----------|
| `import tsq::base` | `ASTNode`, `File`, `Contains` |
| `import tsq::functions` | `Function`, `Parameter`, `FunctionContains`, `ReturnStmt` |
| `import tsq::calls` | `Call`, `CallArg` |
| `import tsq::symbols` | `Symbol`, `FunctionSymbol`, `SymInFunction` |
| `import tsq::variables` | `VarDecl`, `Assign` |
| `import tsq::expressions` | `ExprMayRef`, `ExprIsCall`, `FieldRead`, `FieldWrite`, `ArrayDestructure` |
| `import tsq::imports` | `ImportBinding`, `ExportBinding` |
| `import tsq::jsx` | `JsxElement`, `JsxAttribute` |
| `import tsq::types` | `ClassDecl`, `InterfaceDecl`, `MethodDecl`, `MethodCall` |
| `import tsq::react` | React-specific predicates (`isUseStateSetterCall`, `functionContainsStar`) |
| `import tsq::dataflow` | `LocalFlow`, `LocalFlowStar` |
| `import tsq::callgraph` | `CallTarget`, `CallTargetRTA` |
| `import tsq::taint` | `TaintAlert` |
| `import tsq::express` | `ExpressHandler` |
| `import tsq::summaries` | Dataflow summaries |

---

## Core patterns

### Find all calls to a named function

```ql
import tsq::calls
import tsq::expressions
import tsq::base

from Call c, ASTNode callee
where c.getCalleeNode() = callee
  and callee.getKind() = "Identifier"
select c as "call", callee.getStartLine() as "line"
```

### Find calls to a specific method (e.g. `mp.track`)

```ql
import tsq::calls
import tsq::expressions
import tsq::base

// MethodCall class (from tsq::types) is the cleanest path when available.
// Fallback: match on callee node kind "MemberExpression" and the right text shape.

from Call c
where exists(ASTNode callee |
    c.getCalleeNode() = callee and
    callee.getKind() = "MemberExpression"
)
select c as "call", c.getCalleeNode().getStartLine() as "line",
       c.getCalleeNode().getFile().getPath() as "file"
```

### Find calls by import binding — the right way for `mixpanel.track`

```ql
import tsq::calls
import tsq::expressions
import tsq::imports
import tsq::base

// Find the local symbol bound to the mixpanel import
// Then find calls whose callee references that symbol

from Call c, ImportBinding ib, ExprMayRef ref
where ib.getModuleSpec() = "mixpanel"
  and ref.getSym() = ib.getLocalSym()
  and c.getCalleeNode() = ref.getExpr()
select c as "call",
       c.getCalleeNode().getFile().getPath() as "file",
       c.getCalleeNode().getStartLine() as "line"
```

### Trace the call chain backwards from a tracking call to its event handler

This is the key pattern for Mixpanel → Playwright test generation.

```ql
/**
 * For each mp.track("Event Name") call, find the enclosing
 * event handler function (onClick, onSubmit, etc.) and the
 * JSX element it is attached to.
 *
 * Step 1: find the track call
 * Step 2: find the function that contains it (FunctionContains)
 * Step 3: find JSX attributes whose value references that function
 */

import tsq::calls
import tsq::expressions
import tsq::imports
import tsq::functions
import tsq::jsx
import tsq::base
import tsq::react  // for functionContainsStar

from Call trackCall, int fnId, JsxAttribute attr
where
  // 1. trackCall is a call to mixpanel.track / mp.track / analytics.track
  exists(ImportBinding ib |
    ib.getModuleSpec().regexpMatch("mixpanel.*|@segment/analytics.*") and
    exists(ExprMayRef ref |
      ref.getSym() = ib.getLocalSym() and
      trackCall.getCalleeNode() = ref.getExpr()
    )
  ) and
  // 2. fnId is a function that contains the track call (transitive)
  functionContainsStar(fnId, trackCall.getCalleeNode()) and
  // 3. the function is referenced in a JSX attribute (onClick, onSubmit, etc.)
  exists(ExprMayRef handlerRef, FunctionSymbol fs |
    fs.getFunction() = fnId and
    handlerRef.getSym() = fs.getSymbol() and
    attr.getValueExpr() = handlerRef.getExpr() and
    attr.getName().regexpMatch("on[A-Z].*")
  )
select
  trackCall as "trackCall",
  trackCall.getCalleeNode().getFile().getPath() as "file",
  trackCall.getCalleeNode().getStartLine() as "line",
  attr.getName() as "eventHandler",
  attr.getElement() as "jsxElement"
```

### Find JSX elements with a specific attribute

```ql
import tsq::jsx

from JsxAttribute attr
where attr.getName() = "data-testid"
select attr.getElement() as "element",
       attr.getValueExpr() as "valueNode"
```

### Find all imports from a module

```ql
import tsq::imports

from ImportBinding ib
where ib.getModuleSpec() = "react"
select ib.getImportedName() as "name", ib.getLocalSym() as "localSym"
```

### Find async functions

```ql
import tsq::functions

from Function f
where f.isAsync()
select f.getName() as "name"
```

### Find functions with more than 3 parameters

```ql
import tsq::functions

from Function f, Parameter p
where p.getFunction() = f
  and p.getIndex() = 3  // 0-based: index 3 = 4th param
select f.getName() as "name"
```

### Find React useState setter calls with updater functions

```ql
import tsq::react

from int c, int line
where setStateUpdaterCallsFn(c, line)
select c as "call", line as "line"
```

### Local data flow: does a value from X reach Y?

```ql
import tsq::dataflow
import tsq::variables

from LocalFlowStar flow, VarDecl src, VarDecl dst
where flow.getSource() = src.getSym()
  and flow.getDestination() = dst.getSym()
select src as "source", dst as "destination"
```

### Find method calls by method name

```ql
import tsq::types

from MethodCall mc
where mc.getMethodName() = "setState"
select mc as "call", mc.getMethodName() as "method"
```

---

## Practical recipes

### Recipe: Find all Mixpanel events tracked in the codebase

```ql
import tsq::calls
import tsq::base
import tsq::expressions

// Works when mixpanel is called as mp.track("EventName", ...)
// First arg is the event name string literal — not directly accessible
// as a string from the bridge (you get the node), but file+line identifies it.

from Call c
where exists(ASTNode callee |
    c.getCalleeNode() = callee and
    callee.getKind() = "MemberExpression"
  and c.getArity() >= 1
)
select c as "call",
       c.getCalleeNode().getFile().getPath() as "file",
       c.getCalleeNode().getStartLine() as "line"
```

### Recipe: Find onClick handlers that wrap async operations

```ql
import tsq::jsx
import tsq::functions
import tsq::react

from JsxAttribute attr, int fnId
where attr.getName() = "onClick"
  and exists(ExprMayRef ref, FunctionSymbol fs |
    fs.getFunction() = fnId and
    ref.getSym() = fs.getSymbol() and
    attr.getValueExpr() = ref.getExpr()
  )
  and exists(Function f | f = fnId and f.isAsync())
select attr.getElement() as "element",
       attr.getValueExpr().getStartLine() as "line"
```

### Recipe: Find Express route handlers

```ql
import tsq::express

from ExpressHandler h
select h.getFnId() as "fnId"
```

### Recipe: Find all components that render dangerouslySetInnerHTML

```ql
import tsq::jsx

from JsxAttribute attr
where attr.getName() = "dangerouslySetInnerHTML"
select attr.getElement() as "element",
       attr.getValueExpr().getFile().getPath() as "file",
       attr.getValueExpr().getStartLine() as "line"
```

---

## Limitations to know

| Limitation | Impact | Workaround |
|-----------|--------|-----------|
| **Cross-file symbol resolution requires `tsgo`** | `ImportBinding.getLocalSym()` tracks the local symbol ID but can't follow it to its declaration in another file without `tsgo` on PATH | Pair with `ExprMayRef` to find uses of the import binding's `localSym` |
| **`CallArg` entity collapse** | Multiple args to the same call share the same QL entity (col-0 = call id). `getIndex()` distinguishes them but treat with care | Use `getArgument(0)` for first arg, avoid `getAnArgument()` when you need specific indices |
| **`Parameter` entity collapse** | Same as CallArg — parameters share col-0 (fn id). `getParameter(idx)` is safe; `getAParameter()` may give unexpected joins | Prefer `getParameter(idx)` |
| **No string literal values** | String literal content (e.g. the event name string passed to `mp.track`) is not directly queryable — you get the node id, not the string. Use file+line to locate it manually | Use file/line output, then read the source |
| **`functionContainsStar` depth limit** | Hand-unrolled to 3 nesting levels. Misses track calls inside deeply nested lambdas (rare in real code) | For deep nesting, write a recursive predicate using `FunctionContains` directly |
| **Structural-only extraction by default** | Type-aware relations (`ExprType`, `Implements`, `ResolvedType`) are empty without `tsgo` | Run `tsq extract -tsgo /path/to/tsgo` to populate type relations |

---

## Running against a React codebase — end-to-end example

```bash
# Extract from a Next.js app
tsq extract -dir ./my-next-app -output my-next-app.db

# Find all mixpanel track calls
tsq query -db my-next-app.db -format csv \
  testdata/queries/find_all_calls.ql

# Write a custom query for tracking calls with handlers
tsq query -db my-next-app.db -format json \
  ./my-queries/mp_track_with_handler.ql
```

---

## Writing a new query — checklist

1. **What are you looking for?** Identify the QL class that represents it (Call, JsxElement, Function, etc.)
2. **What constraints narrow it?** Method name? Import source? Containing function? JSX attribute name?
3. **Import only what you need** — each import adds relations to the evaluation; keep imports minimal
4. **Use `functionContainsStar` for transitive containment** — `FunctionContains` is innermost-only
5. **Prefer `getArgument(0)` over `getAnArgument()`** when you want a specific arg
6. **Output file+line** for any result you'll act on — entity IDs are internal integers; file+line is what humans and tools need

---

## Module path reference

The `import tsq::X` paths map to `.qll` files in the `bridge/` directory:

```
tsq::base          → bridge/tsq_base.qll
tsq::functions     → bridge/tsq_functions.qll
tsq::calls         → bridge/tsq_calls.qll
tsq::symbols       → bridge/tsq_symbols.qll
tsq::variables     → bridge/tsq_variables.qll
tsq::expressions   → bridge/tsq_expressions.qll
tsq::imports       → bridge/tsq_imports.qll
tsq::jsx           → bridge/tsq_jsx.qll
tsq::types         → bridge/tsq_types.qll
tsq::react         → bridge/tsq_react.qll
tsq::dataflow      → bridge/tsq_dataflow.qll
tsq::callgraph     → bridge/tsq_callgraph.qll
tsq::taint         → bridge/tsq_taint.qll
tsq::express       → bridge/tsq_express.qll
tsq::summaries     → bridge/tsq_summaries.qll
```
