# Phase C implementation plan — full recursive `mayResolveTo` + bridge migration

**Status:** plan only. Branch `design/valueflow-phase-c`, worktree
`/tmp/tsq-valueflow-phase-c`. No code lands from this doc.

**Author:** Planky (with Cain).
**Date:** 2026-04-19.
**Parent:** `docs/design/valueflow-layer.md`.
**Assumes shipped:** Phase A (extractor primitives + non-recursive base
case) and Phase B (recursive-IDB cardinality estimator + #166 disjunction-
poisoning fix + (name, arity) sweep).

This document specifies **how Phase C is built**, not whether to build it
(decided in the parent) and not what the planner does to make it tractable
(decided in Phase B).

---

## 1. Relation hierarchy

`mayResolveTo` is **not** one monolithic predicate. The decomposition mirrors
CodeQL's `localFlowStep` / `localFlowStepPlus` / global-flow split, with
field sensitivity carried as an explicit access-path string rather than via a
separate AccessPath node hierarchy (we don't have the schema room for one).

### 1.1 Top-level shape

```
flowStep(int from, int to, string path)         // one step, may be intra or inter
mayResolveTo(int v, int s, string path)         // closure of flowStep
mayResolveTo(int v, int s) :- mayResolveTo(v, s, "").  // path-erased convenience

// Composed below from:
localFlowStep(int from, int to, string path)
interFlowStep(int from, int to, string path)
flowStep(f, t, p) :- localFlowStep(f, t, p).
flowStep(f, t, p) :- interFlowStep(f, t, p).
```

`from` and `to` are **expression IDs** (consistent with the parent design,
§2.1 — we stay expr/sym, no DataFlow::Node analogue). `path` is "" for
direct value flow and `"foo"` / `"foo.bar"` for field-qualified flow (§2).

### 1.2 The closure rule

```
mayResolveTo(v, s, p)         :- ExprValueSource(v, s), p = "".
mayResolveTo(v, s, p)         :- flowStep(v, mid, p1),
                                 mayResolveTo(mid, s, p2),
                                 pathCompose(p1, p2, p).
```

`pathCompose` is the only piece of cleverness. For unqualified flow it's
string concatenation (modulo identity for ""). For field reads/writes it
implements push/pop on the access path:

```
pathCompose("",  p,    p   ).               // identity left
pathCompose(p,   "",   p   ).               // identity right
pathCompose("." + f, p, q) :- popField(p, f, q).   // store(f) cancels load(f)
pathCompose(p, q, p ++ q)  :- noCancel(p, q).
```

Concretely: `obj.foo = x` emits `flowStep(<obj-write>, <x>, "foo")`; a
later `obj.foo` read emits `flowStep(<read>, <obj-write-base>, "foo")`;
the closure cancels the matched `foo`-push against the `foo`-pop and yields
unqualified flow from `<read>` to `<x>`. Mismatched paths (`foo` write,
`bar` read) **do not** compose — that's the entire point of field
sensitivity.

### 1.3 Local flow steps (Phase C / PR2)

One named predicate per step kind, all of arity `(from, to, path)`. Each
expects to be union'd into `localFlowStep`.

```ql
predicate lfsAssign(int from, int to, string path) {
  exists(int sym | Assign(_, from, sym) and ExprMayRef(to, sym) and path = "")
}

predicate lfsVarInit(int from, int to, string path) {
  exists(int sym | VarDecl(_, sym, from, _) and ExprMayRef(to, sym) and path = "")
}

predicate lfsParamBind(int from, int to, string path) {
  exists(int paramSym |
    ParamBinding(_, _, paramSym, from) and ExprMayRef(to, paramSym) and path = "")
}

predicate lfsReturnToCallSite(int from, int to, string path) {
  exists(int call, int fn |
    ReturnStmt(fn, _, from) and CallTarget(call, fn) and ExprIsCall(to, call)
    and path = "")
}

predicate lfsDestructureField(int from, int to, string path) {
  exists(int parent, string fld, int bindSym |
    DestructureField(parent, fld, _, bindSym, _)
    and parent = from and ExprMayRef(to, bindSym) and path = fld)
}

predicate lfsArrayDestructure(int from, int to, string path) {
  exists(int parent, int idx, int bindSym |
    ArrayDestructure(parent, idx, bindSym)
    and parent = from and ExprMayRef(to, bindSym) and path = idx.toString())
}

predicate lfsObjectLiteralStore(int from, int to, string path) {
  exists(string fld | ObjectLiteralField(to, fld, from) and path = fld)
}

predicate lfsSpreadElement(int from, int to, string path) {
  ObjectLiteralSpread(to, _, from) and path = ""    // spread carries all fields
}

predicate lfsFieldRead(int from, int to, string path) {
  exists(int baseSym, string fld |
    FieldRead(to, baseSym, fld) and ExprMayRef(from, baseSym) and path = fld)
}

predicate lfsFieldWrite(int from, int to, string path) {
  exists(int baseSym, string fld |
    FieldWrite(_, baseSym, fld, from) and ExprMayRef(to, baseSym) and path = fld)
}

predicate lfsAwait(int from, int to, string path) {
  AwaitExpr(to, from) and path = ""    // §5: treat await e as e
}

predicate localFlowStep(int from, int to, string path) {
  lfsAssign(from, to, path) or lfsVarInit(from, to, path) or
  lfsParamBind(from, to, path) or lfsReturnToCallSite(from, to, path) or
  lfsDestructureField(from, to, path) or lfsArrayDestructure(from, to, path) or
  lfsObjectLiteralStore(from, to, path) or lfsSpreadElement(from, to, path) or
  lfsFieldRead(from, to, path) or lfsFieldWrite(from, to, path) or
  lfsAwait(from, to, path)
}
```

The top-level `or` is exactly the disjunction-poisoning shape that bit R4.
**Phase B's #166 fix is load-bearing here.** If #166 is not actually fixed
when Phase C opens, fall back to **per-kind union via separate IDB heads**
— same pattern as `useStateSetterAliasV2` today. That increases predicate
count by ~10 but stays correct.

### 1.4 Inter-procedural flow steps (Phase C / PR3)

Symmetric: arity `(from, to, path)`. These are the steps that cross
function/module boundaries.

```ql
predicate ifsCallArgToParam(int from, int to, string path) {
  exists(int call, int fn, int idx, int paramSym |
    CallArg(call, idx, from) and CallTarget(call, fn)
    and Parameter(fn, idx, _, _, paramSym, _) and ExprMayRef(to, paramSym)
    and path = "")
}

predicate ifsRetToCall(int from, int to, string path) {
  // Same as lfsReturnToCallSite but call may cross modules.
  // Kept distinct so PR2 can ship without cross-module resolver.
  exists(int call, int fn |
    CallTargetCrossModule(call, fn) and ReturnStmt(fn, _, from)
    and ExprIsCall(to, call) and path = "")
}

predicate ifsImportExport(int from, int to, string path) {
  exists(int localSym, int exportedSym, string mod, string name |
    ImportBinding(to, mod, name) and ExportBinding(exportedSym, mod, name)
    and ExprMayRef(from, exportedSym) and path = "")
}

predicate ifsCallTargetRTA(int from, int to, string path) {
  // Method dispatch via RTA when CallTarget is ambiguous. Falls back to "".
  exists(int call, int fn |
    CallTargetRTA(call, fn) and ReturnStmt(fn, _, from) and ExprIsCall(to, call)
    and path = "")
}

predicate interFlowStep(int from, int to, string path) {
  ifsCallArgToParam(from, to, path) or ifsRetToCall(from, to, path) or
  ifsImportExport(from, to, path) or ifsCallTargetRTA(from, to, path)
}
```

Same disjunction caveat as §1.3.

---

## 2. Field sensitivity

### 2.1 Mechanism

Access paths are **strings** (CodeQL uses an algebraic AccessPath; we don't
have the schema budget for it and string concat is cheap on the planner).
Conventions:

- `""` — unqualified flow; the value itself flows from `from` to `to`.
- `"foo"` — `to`'s value is `from`'s value at field `foo`.
- `"foo.bar"` — `to`'s value is `from`'s value at field `foo`, sub-field
  `bar`.

A **store** (`obj.foo = x`) emits an edge with `path = "foo"` going from
the value to the object; a **load** (`obj.foo`) emits an edge with
`path = "foo"` going from the object to the read site. `pathCompose` matches
the load's `foo` against the store's `foo` and cancels.

### 2.2 Depth bound

**Cap access path depth at 2.** Justification:

1. **Combinatorial.** A naive 5-step chain with depth-3 paths can produce
   ~field-arity³ × step⁵ tuples. On Mastodon (~50k object literals,
   median ~3 fields per literal) depth-3 alone is borderline OOM.
2. **Empirical.** Every R3 fixture, every R4 fixture, and every real
   bridge query in `testdata/queries/` resolves within 2 levels
   (`{ setX, setY }` is depth 1; `{ actions: { setX } }` is depth 2).
3. **Consistent with existing `LocalFlow` semantics.** `LocalFlow` is
   field-name-only, no nesting — depth 2 is already a strict
   improvement.

When the closure would push a third field onto a path, **drop the field
qualifier** (treat the deeper read as path-insensitive). Document this as
unsoundness in the precision dial — same posture as the parent doc §5.

### 2.3 Spread interaction

`{ ...rest }` carries all fields with path `""`. Composition rule:

```
pathCompose("", p, p).
```

…which we already have. So `{ ...rest, foo: x }` followed by `obj.foo`
read finds the `foo: x` store directly (cancels), and `obj.bar` read
walks through the spread to whatever wrote `bar` upstream. Sound modulo
the depth cap.

Mismatched stores under spread (`{ ...a, ...b }` where both define `foo`)
**both flow** — last-write-wins is not modelled (parent §5: "Last-write-
wins, fn-scoped" — and this is *not* fn-scoped). Documented unsoundness
in §4.

---

## 3. Through-call resolution and recursion

### 3.1 Same-module direct calls

Handled by `ifsCallArgToParam` + `lfsReturnToCallSite`. The closure
naturally chains argument → parameter → function body → return → call site.

### 3.2 Cross-module via `ImportBinding`/`ExportBinding`

Two routes, both in Phase C:

1. **Symbol-level:** `ifsImportExport` bridges import sym ↔ export sym.
   Sound when names don't collide; over-bridges otherwise. Same posture
   as round-2's `importedFunctionSymbol` (which is name-keyed).
2. **Call-level:** `CallTargetCrossModule` (new helper, PR1) joins
   `Call × ImportBinding × ExportBinding × FunctionSymbol` once and
   exposes `(call, fn)` for any call whose callee resolves through one
   import hop. PR3 wires this into `ifsRetToCall`.

Multi-hop re-export (`A re-exports from B re-exports from C`) is **not**
covered in v1. Defer; the existing bridge doesn't cover it either.

### 3.3 Virtual / dynamic dispatch

**Punt.** Use `CallTargetRTA` if present (extractor schema), otherwise treat
as bottom (no inter-flow edge). Adding a real type-flow analysis is Phase
E or later. Document as known under-approximation; flag in the bridge
migration if any React fixture relies on virtual dispatch (none currently
do).

### 3.4 Recursive function calls

The closure handles cycles automatically via seminaive evaluation. Two
hazards:

- **Self-recursion through the same parameter** (`function f(x) { return f(x); }`)
  produces a fixpoint cycle that terminates because the `(from, to, path)`
  tuple set is finite.
- **Mutual recursion through accumulator parameters** can balloon paths
  if the depth cap (§2.2) doesn't fire — the path string keeps growing.
  Solution: depth-cap the path AND cap closure iteration count
  (§5).

### 3.5 Higher-order functions

`makeIncrementer(setN)` from the parent doc §3.3 example chains through
`ifsCallArgToParam` (setN → makeIncrementer's param0) then a `lfsParamBind`
inside the returned arrow's call to `setX` then back out via
`lfsReturnToCallSite` and `lfsVarInit`. The closure handles it; no
special-case rule needed. Test fixture in PR2.

---

## 4. Soundness vs precision posture

CodeQL's "may flow" semantics: `mayResolveTo(v, s)` overapproximates the
set of expressions whose value at runtime may be `s`'s value. Absence is a
proof of nothing; presence is a candidate, not a guarantee.

### 4.1 Documented deviations from full soundness

| Deviation | Where | User-visible consequence | Revisit when |
|-----------|-------|--------------------------|--------------|
| `let` reassignment treated as may-flow from any reachable Assign | `lfsAssign` | False positives on heavily-mutated locals | SSA pass lands (Phase E+) |
| Prototype chain ignored | (no rule) | Method calls via prototype assignment miss target fn | A real React lib fixture surfaces it |
| `eval` / `Function(...)` constructor | (no rule) | Any flow into eval'd code is dropped | Never (out of scope) |
| Dynamic property access (`obj[expr]`) | `lfsFieldRead`/`lfsFieldWrite` | We model only static-string field names | A bridge hits a `obj[k]` pattern with provable-constant `k` |
| `Object.assign(target, src)` | (no rule) | Treated as opaque; src fields don't flow to target | When a bridge query needs it |
| Array element flow (push/shift/map) | (no rule) | Array contents are bottom | Phase E |
| Async/Promise resolution | `lfsAwait` collapses `await e → e` | We miss `Promise.resolve(x).then(v => ...)` | Promise model phase (multi-month) |
| Getters/setters | (no rule) | Treated as plain field access, ignoring side effects | Indefinitely |

### 4.2 Precision deviations from CodeQL parity

| Deviation | User-visible | Cost of fixing |
|-----------|--------------|----------------|
| No flow-sensitivity (no SSA) | False positives through reassignment | Whole quarter of work |
| No context-sensitivity (one summary per fn) | Some FPs through fn called from multiple sites | k=1 CFA bumps cost ~Nx callers |
| Field paths capped at depth 2 | Loss of precision on deeply-nested record flow | Lift after Mastodon perf budget allows |
| `last-write-wins` not modelled | FPs from stale writes | SSA |

### 4.3 The contract

**Phase C ships as a refutation tool.** "If `mayResolveTo` returns nothing,
nothing reaches" — and this is the property the bridge migration relies
on for the parity tests. **It does not ship as a proof tool.** Bridge
authors who want "definitely reaches" must add their own filters.

---

## 5. Termination and depth caps

The closure terminates because `(from, to, path)` is finite (bounded
expression IDs, bounded path strings via §2.2 depth cap). But "terminates"
≠ "fits in memory."

### 5.1 What protects us

1. **Path depth cap (§2.2).** Caps the `path` co-domain at `O(fields²)`
   — a few thousand strings on Mastodon.
2. **Phase B's recursive-IDB cardinality estimator.** Sizes the IDB
   correctly so the planner picks demand-bound joins instead of
   Cartesian. **If Phase B's estimator regresses, every Phase C query
   will OOM on Mastodon.** This is the planner-side load-bearer.
3. **Magic-set propagation.** With `mayResolveTo` typically queried from
   a known `v`, the magic-set rewrite seeds the closure backwards from
   the use site. Demand-bound. The disj2-rounds 1-6 work was building
   exactly the propagation infrastructure this needs.

### 5.2 Iteration cap

Add a **hard iteration cap on the seminaive fixpoint loop**. Default 50;
configurable per-query. CodeQL does the same (`step-cap`). Implementation:
the `ql/eval/seminaive.go` loop already counts iterations for telemetry;
add a hard-stop with a logged warning emitting `MayResolveToCapHit(query)`
into the diagnostic relation.

Cap-hit semantics: **truncate, don't crash.** The closure returns
whatever it has at iteration 50; downstream bridge queries see fewer
edges (a missing-edge failure, not a wrong-answer failure). Same posture
as §4 — under-approximation by default.

### 5.3 What we explicitly do NOT cap

- **Recursion depth in the call graph.** That's bounded organically by
  the corpus (real call chains are short). Capping it forfeits soundness
  for a problem the iteration cap already solves.
- **Path string length.** Already capped at depth 2 (§2.2).
- **Number of facts per `mayResolveTo` head.** Let the planner handle it.

### 5.4 Pathological cycle protection

Round 3's circular VarDecl chains (`a = b; b = a;` patterns from object-
spread loops) are killed by the tuple-finiteness argument: the closure
visits each `(v, s, p)` once. The cycle detector is the seminaive
algorithm itself.

---

## 6. Bridge migration — what gets deleted

`bridge/tsq_react.qll` is 1263 lines, ~70 named predicates. Per-predicate
classification, walking the file top-to-bottom:

### 6.1 Untouched (kept verbatim — does something `mayResolveTo` doesn't cover)

- `functionContainsStar` (depth-3 unrolled `Contains`) — that's a
  containment query, not value flow. Stays.
- `isUseStateSetterSym` — definitional; identifies the *source* symbols
  for the closure. Stays.
- `UseStateSetterCall` class + `useStateSetterCallLine` — base call
  identification. Stays.
- `setStateUpdaterCallsFn` + `setStateUpdaterCallsOtherSetState`
  (direct-form rule) — the original direct match. Stays as the
  zero-hop case; arguably could be subsumed but no benefit.
- `DangerouslySetInnerHTML` (taint-sink class at end of file) — unrelated.
  Stays.

**Untouched total: 6 predicates / ~250 lines.**

### 6.2 Simplified (kept as a thin wrapper expressing intent over `mayResolveTo`)

- `setStateUpdaterCallsOtherSetStateThroughProps` — becomes a 5-line
  wrapper: "call where callee `mayResolveTo` something whose source
  expression is a `useState`-setter destructure binding."
- `setStateUpdaterCallsOtherSetStateThroughContext` (incl. `_outerCtx` /
  `_innerCtx` halves) — same wrapper pattern; the `_outerCtx`/`_innerCtx`
  split exists *only* because of #166. Phase B fix collapses it.
- `useStateSetterAliasCall` / `useStateSetterAliasCallV2` /
  `useStateSetterContextAliasCall` — collapse into one
  `useStateSetterReachableCall(int call) :- ExprIsCall(_, call) and
  CallCallee(call, callee) and mayResolveTo(callee, src) and
  isUseStateSetterSource(src)`.

**Simplified: 6 predicates collapse into 3, ~180 lines → ~40 lines.**

### 6.3 Deleted entirely (subsumed by `mayResolveTo`)

The R1 prop-alias family (lines ~223-450, **~230 lines**):
- `useStateSetterSym`, `jsxPropPassesIdentifier`,
  `componentDestructuredProp`, `jsxElementComponent`, `setterAliasStep`,
  `useStateSetterAlias` (with depth-1/2/3 unrolled bodies),
  `useStateSetterAliasCall`. All are syntactic-shape enumerations of
  the prop-pass step, which the new closure handles via `lfsParamBind`
  + `lfsDestructureField`.

The R3 object-literal family (lines ~579-720, **~150 lines**):
- `isObjectLiteralExprOwnField`, `isObjectLiteralExprSpread`,
  `isObjectLiteralExpr`, `objectLiteralFieldOwn`,
  `objectLiteralFieldSpreadD1Direct`, `objectLiteralFieldSpreadD1Var`,
  `objectLiteralFieldSpreadD1`, `objectLiteralFieldSpreadD2*` (×4),
  `objectLiteralFieldSpreadD2`, `objectLiteralFieldThroughSpread`. The
  whole spread-fanout family. Subsumed by `lfsObjectLiteralStore` +
  `lfsSpreadElement` + path composition.

The R3 context-provider field family (lines ~770-860, **~110 lines**):
- `contextProviderValueObject`, `contextProviderFieldR2`,
  `contextProviderFieldR3VarIndirect{Own,SpreadD1,SpreadD2}`,
  `contextProviderFieldR3VarIndirect`, `contextProviderFieldR3Direct{Own,
  SpreadD1,SpreadD2,Spread}`, `contextProviderField`. All subsumed.

The R4 hook-return family (added PR #171, **~60 lines**):
- `valueExprCallsHook`, `hookFnInvokedByValueExpr`,
  `resolveToObjectExprHookReturn{Direct,Var}`,
  `resolveToObjectExprHookReturn`, plus the
  `resolveToObjectExpr{Direct,Wrapped,VarD1,VarD2}` siblings and the
  `resolveToObjectExpr` union. All subsumed by call-return + var-init
  composition in the closure.

The context-symbol bridging (lines ~1017-1100, **~110 lines**):
- `contextSymLinkSame`, `contextSymLinkCrossFile`, `contextSymLink`,
  `contextSetterAliasStep{R2,R3DirectSpread,R3VarIndirect}`,
  `contextSetterAliasStep`, `setterAliasStepAny`,
  `useStateSetterAliasV2`, `isContextAliasedSetterSym`. All subsumed.

`hookIndirection{D1,D2}` + union: subsumed by call-return chaining.
`useContextCall`, `useContextCallSiteResolvesContext`: subsumed.
`importedFunctionSymbol`: subsumed by `ifsImportExport` (and the
name-collision over-bridging problem moves into the value-flow layer
where it's documented in §4.1).

### 6.4 Quantified migration outcome

| Bucket | Before | After |
|--------|--------|-------|
| Total predicates in `tsq_react.qll` | ~70 | ~12 |
| Lines | 1263 | ~350 |
| Lines deleted | — | ~660 |
| Lines simplified | — | ~140 → ~40 |
| Predicates deleted | — | ~52 |
| Predicates simplified | — | 6 → 3 |

This **comfortably exceeds the parent doc §6 Phase D target** of "≥ 600
lines deleted / -50 predicates" and we hit it as the proof-of-concept
inside Phase C, not in Phase D.

---

## 7. Sequencing inside Phase C — PRs

Each PR is independently mergeable and delivers stand-alone value. If
Phase C is paused mid-flight, every shipped PR remains useful.

### PR1 — extractor extensions

**Title:** `feat(extract): emit return-edge and parameter-binding facts at expression granularity for value-flow layer`

**Scope:** any flow primitives Phase A didn't ship. Specifically:
- `CallTargetCrossModule(call, fn)` — pre-joined helper for
  `ifsRetToCall`. Saves the closure body a 4-table join.
- `AwaitExpr(expr, innerExpr)` — if not already in schema.
- `FieldWrite` arity audit — confirm we have `(stmt, baseSym, fld, valExpr)`.
  Add `FieldWriteExpr(int writeExpr, int valExpr, string fld)` if needed
  for the expr-keyed entry.

**Depends on:** Phase A landed.

**Size:** ~250 lines (walker.go + schema/relations.go + tests). Small.

**Gate-to-merge:** new relations populated on the React fixtures with
non-empty row counts; manifest count bumped; `stdlib_coverage_test.go`
allowlists added.

**Standalone value:** these primitives are useful for any future bridge,
not just value flow. Bridge authors get cleaner joins immediately.

### PR2 — local flow steps (intra-procedural, no recursion)

**Title:** `feat(valueflow): local flow step kinds (intra-procedural)`

**Scope:** the ~10 `lfs*` predicates from §1.3. Each as a named predicate.
Top-level `localFlowStep` union. **No recursion yet** — just the step
predicates and their union.

**Depends on:** PR1.

**Size:** ~400 lines QL + ~300 lines Go test fixtures. Each `lfs*` gets
a dedicated unit fixture.

**Gate-to-merge:** per-step-kind unit fixtures pass; row counts on
existing React fixtures match hand-computed expectations; no perf change
to existing queries (we haven't wired anything yet).

**Standalone value:** intra-procedural flow alone is enough for several
hypothetical future bridges. Ships as a usable layer even without the
closure.

### PR3 — inter flow steps (call/return, import/export, no recursion)

**Title:** `feat(valueflow): inter-procedural flow step kinds`

**Scope:** the ~4 `ifs*` predicates from §1.4 + `interFlowStep` union +
`flowStep` top-level union. Still no recursion.

**Depends on:** PR2.

**Size:** ~250 lines QL + ~200 lines test.

**Gate-to-merge:** unit fixtures per ifs kind; cross-module fixture
exercises `ifsImportExport`; no regression on existing bridge queries.

**Standalone value:** with `flowStep` available as a 1-step relation,
bridge authors can manually depth-unroll (R1-R3 style) using a single
relation rather than writing the shape-matchers themselves.

### PR4 — closure into `mayResolveTo`

**Title:** `feat(valueflow): recursive mayResolveTo with iteration cap`

**Scope:** the closure rule from §1.2 (path-erased version first;
field sensitivity is PR5). Iteration cap from §5.2.
`MayResolveToCapHit` diagnostic relation.

**Depends on:** PR3 + Phase B's recursive-IDB cardinality estimator
must be live in `main`. **Hard gate.** If it isn't, PR4 cannot merge.

**Size:** ~200 lines QL + ~500 lines integration tests.

**Gate-to-merge:**
- All R1, R2, R3, R4 React fixtures produce **at least the same alert
  rows** under a `mayResolveTo`-based bridge predicate (NOT bit-identical
  yet — that's PR6).
- Mastodon wall-time within +50% of baseline.
- `MayResolveToCapHit` rate < 1% of queries on Mastodon.

**Standalone value:** this is the layer. After PR4, the bridge migration
can begin.

### PR5 — field-sensitivity layer

**Title:** `feat(valueflow): field-sensitive access-path composition`

**Scope:** add the `path` argument to `localFlowStep`/`interFlowStep`/
`flowStep`/`mayResolveTo`. Implement `pathCompose` per §1.2.
Depth-cap-at-2 enforcement (§2.2).

**Depends on:** PR4.

**Size:** ~150 lines QL + ~250 lines test fixtures (each composition
case: store-then-load-same-field, store-then-load-different-field,
spread-then-load, depth-cap-overflow).

**Gate-to-merge:** spread/destructure fixtures from R3 produce identical
alert sets to current bridge; **field-mismatch fixtures produce zero
spurious alerts** (the precision win); no perf regression > 20% over PR4
baseline.

**Standalone value:** without PR5 the layer is path-insensitive (still
useful, matches CodeQL's local-flow-only mode).

### PR6 — bridge migration

**Title:** `refactor(bridge): port React setState alias chain to mayResolveTo`

**Scope:** delete the §6.3 predicates. Rewrite the §6.2 predicates as
thin wrappers. Leave §6.1 untouched.

**Depends on:** PR5.

**Size:** -660 lines (deletions dominate); ~80 lines new wrappers.

**Gate-to-merge:**
- **Bit-identical CSV diff** of every existing bridge integration test
  (`setstate_prop_alias_integration_test.go`,
  `setstate_context_alias_integration_test.go`,
  `setstate_context_alias_r3_integration_test.go`, R4 test).
- Bridge manifest count drops appropriately; `manifest_test.go` updated.
- Mastodon wall-time within +50% of pre-Phase-C baseline.

**Standalone value:** delivers the §1 thesis of the parent doc — the
React bridge stops growing per-shape predicates.

### PR7 — integration tests + Mastodon benchmark

**Title:** `test(valueflow): full integration suite + Mastodon perf gate`

**Scope:**
- Whole-closure integration fixtures (each higher-order pattern from
  §3.5; the parent-doc §3.3 example as a canonical test).
- Adversarial fixtures: the round-3 circular VarDecl shape (cycle
  termination test); deep-spread (depth-cap test); name-colliding
  cross-module imports (over-bridging documented behaviour test).
- CI perf gate that fails the build if Mastodon wall-time exceeds
  +50% of pre-Phase-C baseline.
- `MayResolveToCapHit` alerting wired into the cain-nas bench.

**Depends on:** PR6.

**Size:** ~600 lines tests + ~100 lines CI YAML.

**Gate-to-merge:** CI green on Mastodon and Jitsi; perf gate active.

**Standalone value:** without PR7 we don't know if PR4-6 actually held
their performance budget on big corpora. This is the regression net.

---

## 8. Test strategy

### 8.1 Per-step-kind unit fixtures

Every `lfs*` and `ifs*` predicate ships with a minimal fixture in
`testdata/projects/valueflow-step-XX/` exercising **only** that step kind.
Row count assertions at the predicate level (not just the closure level)
— same pattern as `TestContextChain_LinkPredicates`'s per-link assertions
in R2.

### 8.2 Whole-closure integration fixtures

Curated end-to-end fixtures, one per pattern shape:
- Direct prop pass (R1 fixture, ported)
- Context provider with own-fields (R2 fixture, ported)
- Context with spread + computed key (R3 fixture, ported)
- Factory hook return (R4 fixture, ported)
- Higher-order function (`makeIncrementer` from parent doc §3.3)
- Recursive function call (cycle test)
- Multi-hop cross-module import

Each asserts the closure's row set against a hand-computed reference. The
reference is checked into the repo as a `.expected.csv`.

### 8.3 The bridge-migration parity test

**This is the load-bearing test for PR6.** For every existing bridge
fixture, run the OLD bridge (predicates from §6.2/6.3) and the NEW
bridge (mayResolveTo wrappers). Diff the CSV outputs. **Bit-identical
match required for PR6 merge.** Implementation:

```go
func TestBridgeMigrationParity_R1(t *testing.T) {
  oldRows := runQuery(t, "find_setstate_..._old.ql", fixture)
  newRows := runQuery(t, "find_setstate_..._new.ql", fixture)
  require.Equal(t, oldRows, newRows)  // exact diff
}
```

`_old.ql` is the pre-PR6 query saved as a frozen artifact for the
duration of Phase C; deleted after PR6 merges.

### 8.4 Adversarial / negative fixtures

- Field mismatch: `obj.foo = setX; call(obj.bar)` — must NOT alert.
- Cycle: `let a = b; let b = a;` — must terminate, no spurious source.
- Depth cap overflow: a 4-level-nested object — must drop deeper paths.
- Name collision: two modules export `setX`, one re-imports — documented
  over-bridging behaviour, asserted not silently fixed.

### 8.5 Property tests

Reuse `taint_monotone_property_test.go` shape: `mayResolveTo` is monotone
in source-set inclusion (adding fixtures monotonically grows the
alert set). Catches accidental closure-pruning bugs.

---

## 9. Performance budget

### 9.1 Targets

Pre-Phase-C baseline (post-PR #170, today's `main`): Mastodon ~48s,
Jitsi ~74s for the `_disj_2` query family, no cap-hits.

Post-PR4 (`mayResolveTo` live, before bridge migration):
- **Acceptable:** ≤ 2× baseline at parity result count. (Mastodon: ≤ 96s.)
- **Flag-for-follow-up:** 2-5× baseline.
- **Blocks merge:** > 5× baseline. (Mastodon: > 240s.)

Post-PR6 (bridge migrated to `mayResolveTo`):
- **Acceptable:** ≤ +50% over pre-Phase-C baseline at parity result
  count. (Mastodon: ≤ 72s.) — same as parent doc §6 Phase D budget.
- **Flag-for-follow-up:** +50% to +100%.
- **Blocks merge:** > +100%.

### 9.2 Why the bridge migration target is *tighter* than the layer target

The bridge migration removes ~660 lines of bridge code that the planner
currently has to evaluate. Replacing them with one `mayResolveTo` query
should be at most a small constant slower in good cases, and **faster**
in cases where the magic-set rewrite can demand-bind the closure
backwards from a small seed (the same dynamic R4 hit — locality of
bindings beats locality of names).

If post-PR6 numbers are *worse* than pre-Phase-C, the layer is failing
its core thesis and we revert per-bridge while keeping the layer for
new consumers (parent doc §7 minimum-viable plan).

### 9.3 What we measure

cain-nas bench:
- Wall time per query family (mastodon, jitsi, react fixtures).
- Cap-hit count (`_disj_2`, `mayResolveToCapHit` if PR7 wires it).
- Alert row count delta (must match parity).
- Memory peak.

---

## 10. Risks specific to Phase C

### 10.1 Bridge parity divergence

**Highest-probability risk.** The R3 fixtures may match cases that
`mayResolveTo` rejects (because R3 is over-approximating in ways the
new layer correctly excludes), or vice versa.

**Estimated probability:** ~50% on at least one fixture.

**Decision rule when they disagree:**

1. **R3 matches, `mayResolveTo` doesn't:** investigate the missing
   step kind. If R3 is correct (a real value flow exists), add the
   step kind to §1.3/1.4 — PR6 stays blocked until parity holds.
2. **`mayResolveTo` matches, R3 doesn't:** investigate the new match.
   If it's a true positive, accept the alert-count growth (parent doc
   §6 Phase D budget allows ≤10% growth). If it's a spurious flow,
   add a soundness deviation in §4 documenting it and either tighten
   the rule or accept the FP.
3. **Both match different rows:** treat as case 1 + case 2 superposed.

### 10.2 `pathCompose` correctness

Field-sensitivity composition is the trickiest piece of the closure
rule. Risk: cancellation logic admits flow that shouldn't compose, or
fails to compose flow that should.

Mitigation: §8.4 adversarial fixtures specifically targeting composition
edge cases. Field-mismatch fixture is the canary.

### 10.3 Phase B's recursive-IDB estimator regresses under load

Phase B's estimator may pass its own benchmarks but degrade under
Phase C's specific access pattern (deep recursion + magic-set
propagation through path-cancellation). Mitigation: PR4 adds a Phase B
exercise harness that runs the closure on Mastodon as part of CI;
estimator regressions are caught at the planner-PR level, not at
Phase C bringup.

### 10.4 Iteration cap fires on real corpora

If `MayResolveToCapHit` fires > 1% on Mastodon, we're under-approximating
worse than R1-R4 already do. Mitigation: PR7 wires alerting; if cap-hit
rate exceeds 1%, file a planner issue and consider raising the cap from
50 to 100 (cost: linear in corpus size).

### 10.5 Diagnostic relation pollution

`MayResolveToCapHit` per-query rows could blow up the diagnostic table
on big corpora. Mitigation: emit at-most-one row per top-level query, not
per inner-closure invocation.

---

## 11. What Phase C does NOT do

- **Does not migrate other tsq bridges.** Audit of `bridge/` shows the
  React bridge (`tsq_react.qll`) is the only one with R1-R4-style
  per-shape predicate inflation. The taint bridges (`tsq_taint.qll`,
  `compat_security_*.qll`) use a different idiom (taint-source/sink
  configuration) that doesn't benefit from `mayResolveTo` directly.
  Express, redux, GraphQL bridges — none exist yet. **Phase D** picks up
  any further bridge integration; in practice "Phase D" may just be
  "the next bridge author imports `tsq_valueflow.qll`."
- **Does not introduce SSA, k-CFA context-sensitivity, or a
  promise/effect graph.** All deferred per parent doc §5.
- **Does not change the planner.** All planner work is Phase B's
  responsibility. Phase C only consumes planner capabilities; if the
  planner regresses, Phase C reverts cleanly via PR sequencing.
- **Does not measure beyond Mastodon + Jitsi.** Wider corpus rollout
  (any-real-customer codebase) is Phase D measurement.
- **Does not touch the direct-form rule** (`setStateUpdaterCallsOtherSetState`).
  That predicate stays as written — it's the zero-hop case and it's
  already trivial.
- **Does not add a `Configuration` class.** Parent doc §8.2 — we
  deliberately diverge from CodeQL on this. Bridge authors filter
  `mayResolveTo` directly with their own predicates.
