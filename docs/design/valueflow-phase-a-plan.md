# Value-flow Phase A — implementation plan

**Status:** design only. No code, no schema migration yet.
**Branch:** `design/valueflow-phase-a` off `design/valueflow-layer`.
**Parent doc:** `docs/design/valueflow-layer.md` (read first; this doc refines §3.2 + §6 Phase A only).
**Author:** Planky.
**Date:** 2026-04-19.

Phase A's contract from the parent doc: ship the extractor extensions plus a **non-recursive** `mayResolveTo` that the planner can size today, prove it on the React bridge by collapsing the easy `resolveToObjectExpr*` branches, and lay rails Phase B/C extend with recursion. Phase A is deliberately not interesting Datalog — it's a vocabulary change. Phase B does the planner work; Phase C adds recursion. **This doc does not design B/C.**

---

## 1. Extractor extensions

### 1.1 What the walker emits today (relevant subset)

`extract/schema/relations.go` registers — for the value-flow vocabulary — `VarDecl/4`, `Assign/3`, `ExprMayRef/2`, `ExprIsCall/2`, `Call/3`, `CallArg/3`, `CallCalleeSym/2`, `CallResultSym/2`, `FieldRead/3`, `FieldWrite/4`, `ObjectLiteralField/3`, `ObjectLiteralSpread/2`, `DestructureField/5`, `ArrayDestructure/3`, `ReturnStmt/3`, `ReturnSym/2`, `Parameter/6`, `LocalFlow/3`, `LocalFlowStar/3`, `InterFlow/2`, `FlowStar/2`, `ParamToReturn/2`. Both `walker.go` (vendored tree-sitter) and `walker_v2.go` (type-aware, tsgo-overlay) emit these via `tw.fw.emit("Name", ...)`.

The gap the parent doc identifies (§3.1 last paragraph): the bridge speaks **expr→expr**, the flow rels speak **sym→sym**. Phase A's job is to add the two thin shims that close this gap *without* introducing a recursive IDB.

### 1.2 New relations

#### `ExprValueSource(int expr, int sourceExpr)` — arity 2

Schema: one row per AST expression that is a **value-producing literal at its own location** — i.e. an expression whose runtime value is determined entirely by its own subtree. Concretely: object literals, array literals, function/arrow expressions, class expressions, primitive literals (string/number/bool/null/undefined/regex/template-without-substitutions), JSX elements. **Not** identifiers (those go through `ExprMayRef`), not calls, not member access, not binary ops, not `await`, not `as` casts.

Identity row: `expr == sourceExpr`. The relation looks redundant — why join `(v, v)` when `v = v` works? Because the planner needs a *grounded* base predicate for the recursive rule in Phase C: `mayResolveTo(v, s) :- ExprValueSource(v, s).` is a sized base case the trivial-IDB pre-pass can estimate. A bare equality wouldn't.

Worked example for `const f = () => 1; const o = { x: 5 };`:

```
// (using illustrative node ids)
ExprValueSource(<arrow-expr#10>, <arrow-expr#10>)
ExprValueSource(<obj-literal#20>, <obj-literal#20>)
ExprValueSource(<5-literal#21>, <5-literal#21>)
ExprValueSource(<1-literal#11>, <1-literal#11>)
```

Complexity: **trivial AST walk.** Decided per-node-kind in the existing `Enter` switch (`walker.go:Enter` and `walker_v2.go:emitV2Facts`). One new emit site per kind.

Estimated lines: **~40 LoC across both walkers + ~25 LoC in `relations.go` + tests.** Walker_v2 already has the kind switch; we slot in alongside the existing `case` arms.

#### `ParamBinding(int fn, int paramIdx, int paramSym, int argExpr)` — arity 4

Schema: one row per (call site × parameter slot) where `fn` is the **callee function id** (resolved via `CallTarget` or `CallTargetRTA`), `paramIdx` is the parameter position, `paramSym` is the symbol of that parameter inside the callee, and `argExpr` is the actual-argument expression at the call site. This is the join `CallTarget ⨝ CallArg ⨝ Parameter` materialised once at extraction time.

Why materialise: the bridge today needs this composition in hot bodies; it's a 4-table join the planner has to re-derive each query. Pre-rolling it out at extraction is "one literal in hot bodies" (parent doc §3.2), and gives Phase A a base relation that's already-bound on the call-site side.

Worked example for:

```ts
function inc(prev) { return prev + 1; }
const v = inc(7);
```

Tuples:

```
ParamBinding(<fn:inc>, 0, <sym:prev>, <7-literal-expr>)
```

Multi-arg, multi-call:

```ts
function add(a, b) { return a + b; }
add(1, 2);
add(x, 3);
```

```
ParamBinding(<fn:add>, 0, <sym:a>, <1-literal-expr>)
ParamBinding(<fn:add>, 1, <sym:b>, <2-literal-expr>)
ParamBinding(<fn:add>, 0, <sym:a>, <x-ident-expr>)
ParamBinding(<fn:add>, 1, <sym:b>, <3-literal-expr>)
```

Complexity: **needs symbol resolution** — specifically, requires `CallTarget` / `CallTargetRTA` to be settled, which means it must emit in a **post-pass** after the main walker run. Walker_v2's `Run` already overlays after the inner walker; this is a third stage that consumes the populated DB. Spread args (`f(...rest)`) and rest params (`function f(...args)`) emit nothing in v1 — out of scope for Phase A. Document the carve-out in the relation comment.

Estimated lines: **~80 LoC** for the post-pass (DB scan over `CallTarget × CallArg × Parameter`, emit), **+ 15 LoC schema + tests.** No new tree walking.

#### `AssignExpr(int lhsSym, int rhsExpr)` — arity 2

Schema: the symmetric-to-`VarDecl` view of `Assign`. `Assign` today is `(lhsNode, rhsExpr, lhsSym)`; the bridge and the upcoming `mayResolveTo` only ever care about `(lhsSym, rhsExpr)`. Materialising it as a 2-column projection lets the planner key joins on `lhsSym` directly without dragging the unused `lhsNode` column through the binding inference.

Worked example for `let x; x = foo();`:

```
Assign(<assign-stmt#5>, <call:foo()#6>, <sym:x>)
AssignExpr(<sym:x>, <call:foo()#6>)
```

Complexity: **trivial** — pure projection; can emit at the same site `Assign` is emitted.

Estimated lines: **~10 LoC.**

#### Carve-outs (do NOT add in Phase A)

`FieldShape`, `ResolvesToImportedFunction`, `MethodResolvesTo`, `Effect` from parent doc §3.2 — all **deferred**. They either need a binding/scope pass or a real type query; both belong to Phase C extractor work, not A.

### 1.3 Schema-version bump

`ExprValueSource`, `ParamBinding`, `AssignExpr` are net-new relations — **no breaking change** to existing consumers. Bump `db.SchemaVersion` by 1; downgrade-compat is a write-only concern (older readers ignore unknown rels). No migration of existing rows.

---

## 2. Non-recursive `mayResolveTo`

Phase A ships **only the rules whose body has no `mayResolveTo` literal in it.** This is the base case + every one-step rule rewritten so the recursive call is replaced with a direct ground predicate. Non-recursive means the planner's existing P2b sampling estimator handles it — no recursive-IDB cardinality work needed (parent doc §4.2 #1 stays a Phase B blocker).

File: `ql/system/valueflow.qll` (new). The bridge consumes via `bridge/tsq_valueflow.qll`, which re-exports the rules and adds a thin `resolvesToFunctionDirect` helper.

### 2.1 Base case — `mayResolveTo` ≡ identity on value-source expressions

```ql
predicate mayResolveTo(int valueExpr, int sourceExpr) {
    ExprValueSource(valueExpr, sourceExpr)
}
```

- Consumes: `ExprValueSource/2`.
- Cardinality (Mastodon): one row per value-producing AST node. Mastodon corpus is ~100k symbols → ~300k–500k value-producing exprs. **~10^5 rows.**
- Planner sanity: trivial-IDB pre-pass sizes this directly from the EDB row count of `ExprValueSource`. No recursion. Sampling pre-pass not needed (it's a 1:1 projection).

### 2.2 Var-init step (one hop, non-recursive)

```ql
predicate mayResolveToVarInit(int valueExpr, int sourceExpr) {
    exists(int sym, int initExpr, int varDecl |
        ExprMayRef(valueExpr, sym) and
        VarDecl(varDecl, sym, initExpr, _) and
        ExprValueSource(initExpr, sourceExpr)
    )
}
```

Reads "`valueExpr` references a sym whose `VarDecl` initialiser is itself a value-source." Note the inner rel is `ExprValueSource`, **not** `mayResolveTo` — this is the Phase A simplification that keeps it non-recursive.

- Consumes: `ExprMayRef`, `VarDecl`, `ExprValueSource`.
- Cardinality (Mastodon): bounded by `VarDecl × ExprMayRef-on-init-target`. Most VarDecls initialise from non-source exprs (calls, member access). Pessimistic estimate **~10^5 rows**.
- Planner: 3-table join, all base relations sized; P3a backward-binding inference will adorn from `valueExpr` → `sym` → `varDecl` → `initExpr` in order. Benefits from the (name, arity)-keyed magic-set propagation already in place after disj2 round 6.

### 2.3 Assign step (one hop)

```ql
predicate mayResolveToAssign(int valueExpr, int sourceExpr) {
    exists(int sym, int rhsExpr |
        ExprMayRef(valueExpr, sym) and
        AssignExpr(sym, rhsExpr) and
        ExprValueSource(rhsExpr, sourceExpr)
    )
}
```

- Consumes: `ExprMayRef`, `AssignExpr`, `ExprValueSource`.
- Cardinality: assignments are rarer than var-decls in modern TS — **~10^4 rows** Mastodon-scale.
- Planner: same shape as 2.2.

### 2.4 Param-binding step (one hop)

```ql
predicate mayResolveToParamBind(int valueExpr, int sourceExpr) {
    exists(int sym, int fn, int idx, int argExpr |
        ExprMayRef(valueExpr, sym) and
        ParamBinding(fn, idx, sym, argExpr) and
        ExprValueSource(argExpr, sourceExpr)
    )
}
```

- Consumes: `ExprMayRef`, `ParamBinding`, `ExprValueSource`.
- Cardinality: bounded by call sites passing a value-source literal directly (`f({...})`, `f(() => x)`). On Mastodon, **~10^4–10^5 rows** (React codebases pass arrow literals into JSX a lot).
- Planner: ParamBinding's 4-arity carries the binding hint cleanly. Magic-set propagation must key on (name=ParamBinding, arity=4); confirmed-OK after disj2 round 5.

### 2.5 Field-read of immediately-adjacent field-write (one hop, no shape recursion)

```ql
predicate mayResolveToFieldRead(int valueExpr, int sourceExpr) {
    exists(int baseSym, string fld, int rhsExpr, int writeNode |
        FieldRead(valueExpr, baseSym, fld) and
        FieldWrite(writeNode, baseSym, fld, rhsExpr) and
        ExprValueSource(rhsExpr, sourceExpr)
    )
}
```

Field-name + base-sym match only; no shape recursion (parent doc §5: "Field-named, no shape" is the v1 default). Last-write-wins is *not* enforced — all writes are may-occur.

- Consumes: `FieldRead`, `FieldWrite`, `ExprValueSource`.
- Cardinality: **~10^4 rows** Mastodon. FieldWrites in the corpus are dominated by class field init and reducer-style assignments.
- Planner: 3-table join keyed on `(baseSym, fld)` after P3b projection-pushdown.

### 2.6 Object-literal field projection (one hop)

```ql
predicate mayResolveToObjectField(int valueExpr, int sourceExpr) {
    exists(int objExpr, string fld, int fieldValExpr |
        // valueExpr is a FieldRead of fld on a sym whose VarDecl init is objExpr
        exists(int baseSym, int varDecl |
            FieldRead(valueExpr, baseSym, fld) and
            VarDecl(varDecl, baseSym, objExpr, _) and
            ObjectLiteralField(objExpr, fld, fieldValExpr) and
            ExprValueSource(fieldValExpr, sourceExpr)
        )
    )
}
```

This is the Phase A version of "the easy `resolveToObjectExpr` cases." Single VarDecl indirection, own field only. **No spread, no depth-2 var indirection, no computed key** — those need recursion through `mayResolveTo` (Phase C).

- Consumes: `FieldRead`, `VarDecl`, `ObjectLiteralField`, `ExprValueSource`.
- Cardinality: **~10^4 rows.** Object-literal-field reads through a single VarDecl are common in React (props destructure, theme objects).
- Planner: 4-table join. The disjunction-poisoning bug (#166) does NOT apply here because every Phase A rule is a separate top-level predicate; the union is `or`-of-calls below.

### 2.7 Top-level union — `or`-of-calls

```ql
predicate mayResolveTo(int valueExpr, int sourceExpr) {
    mayResolveToBase(valueExpr, sourceExpr)
    or mayResolveToVarInit(valueExpr, sourceExpr)
    or mayResolveToAssign(valueExpr, sourceExpr)
    or mayResolveToParamBind(valueExpr, sourceExpr)
    or mayResolveToFieldRead(valueExpr, sourceExpr)
    or mayResolveToObjectField(valueExpr, sourceExpr)
}
```

(Where `mayResolveToBase` wraps §2.1.) This shape is exactly the disj2 round-4 pattern: one head, six named branches, top-level disjunction is `or`-of-calls — sidesteps #166 by construction. Aggregate cardinality on Mastodon: **~10^5–10^6 rows.** Sits comfortably inside the planner's non-recursive sizing.

### 2.8 Derived helper — `resolvesToFunctionDirect`

```ql
predicate resolvesToFunctionDirect(int callee, int fnId) {
    exists(int sourceExpr |
        mayResolveTo(callee, sourceExpr) and
        FunctionSymbol(_, sourceExpr) and
        sourceExpr = fnId
    )
}
```

Phase A surface for the bridge: "is this callee's resolved value-source a function expression node?" Phase C will replace with the recursive-aware version.

---

## 3. Bridge migration scope (Phase A only)

Phase A migrates **only the directly-resolvable `resolveToObjectExpr*` branches** in `bridge/tsq_react.qll`. The recursion-dependent ones (Wrapped + VarD2) stay until Phase C.

### 3.1 Predicates that collapse

| Predicate | Lines | Phase A action |
|---|---|---|
| `resolveToObjectExprDirect` (L512–515) | 4 | **Delete.** Subsumed by `mayResolveToObjectField` callers asking "did this resolve to an object literal." Bridge call sites swap to the new helper. |
| `resolveToObjectExprVarD1` (L534–541) | 8 | **Delete.** Exactly the pattern of §2.6's body without the field-read step — the bridge's downstream consumer of `objExpr` becomes a `mayResolveTo`-driven join. |
| `objectLiteralFieldOwn` (L615–617) | 3 | **Delete.** Trivially `ObjectLiteralField(objExpr, fld, valueExpr)` — bridge consumers inline the EDB. |
| `contextProviderFieldR3DirectOwn` (L817–823) | 7 | **Delete.** Top-level disjunct that only consumed `objectLiteralFieldOwn` + `resolveToObjectExprDirect`. |
| `contextProviderFieldR3VarIndirectOwn` (L770–779) | 10 | **Delete.** Same — it's the §2.6 pattern with one extra ExprMayRef hop on the consumer side. |

That's **~32 LoC and 5 predicates** deleted directly from rounds 3/4 of the bridge.

### 3.2 Predicates that survive Phase A (deferred to Phase C)

- `resolveToObjectExprWrapped` (L523) — depends on `Contains` walking; recursive shape, defer.
- `resolveToObjectExprVarD2` (L550) — two-hop var indirection, requires recursive `mayResolveTo`.
- `objectLiteralFieldSpreadD1*` / `D2*` family (L624–700+) — spread chains, recursive by nature.
- `contextProviderFieldR3*Spread*` family — same reason.
- All `_outerCtx`/`_innerCtx`/`contextSymLink*` R4 splits — these are disjunction-poisoning workarounds for the *recursive* alias chain; Phase A doesn't touch them, Phase C deletes them when the recursive `mayResolveTo` arrives.

### 3.3 Quantified target

- **Lines deleted from `bridge/tsq_react.qll` in Phase A:** ~30–50 (target floor: 30).
- **Predicates removed:** 5.
- **No predicate count reduction in R4 splits.** That comes in Phase C.

This is *deliberately* a small migration. Phase A's win is the vocabulary, not the deletions. The parent doc's "≥ 600 lines deleted" target is a Phase D goal.

---

## 4. Test strategy

### 4.1 New fixtures

Under `testdata/projects/`:

- `valueflow-base/` — minimal TS project covering each Phase A rule in isolation:
  - `var_init.ts` — `const x = {...}; use(x)` exercises §2.2.
  - `assign.ts` — `let x; x = () => 1; x()` exercises §2.3.
  - `param_bind.ts` — `function f(g) { g(); } f(() => 1);` exercises §2.4.
  - `field_read_write.ts` — `class C { x = () => 1; } new C().x()` exercises §2.5.
  - `obj_field.ts` — `const o = { f: () => 1 }; o.f()` exercises §2.6.

Each file ≤ 20 LoC, hand-checkable expected `mayResolveTo` rows.

- `valueflow-negative/` — patterns Phase A must NOT resolve (would require recursion):
  - Two-hop var indirection.
  - Object spread carrier.
  - Field write through aliased base.

  Assertion: `mayResolveTo` returns 0 rows for the use-site expression. These are guards against accidental recursive leakage.

### 4.2 Extractor unit tests

In `extract/walker_test.go` and `extract/walker_v2_test.go`: per new relation, a fixture-tuple test that asserts the exact emitted row set. Borrows the existing `assertRows` helper pattern.

### 4.3 Regression invariant — round-1 to round-4 fixtures unchanged

Phase A introduces new rules; it does **not** rewrite the bridge's R1–R4 logic (only deletes the 5 predicates in §3.1, all of which are pure subsumption). Strategy:

1. Capture golden output for `react-component`, `react-usestate`, `react-usestate-context-alias`, `react-usestate-context-alias-r3`, `react-usestate-prop-alias` **before** the Phase A PR series starts (one commit on `design/valueflow-phase-a` saving `testdata/expected/phase-a-baseline/*.txt`).
2. After each Phase A PR lands, re-run the same queries; diff must be **empty**. The 5 deleted predicates are subsumption-only; their removal must not change query results.
3. Also run `compat_test.go` and `regression_*_test.go` end-to-end — these cover round-1/2/3/4 setter alias detection. Both must pass unmodified.

If a diff appears, the deletion was wrong (not subsumption). Revert the deletion; defer to Phase C.

---

## 5. Sequencing — 4 PRs

Each PR is independently mergeable, gated, and reverts cleanly.

### PR 1 — `feat(extract): ExprValueSource + AssignExpr base relations`

- Scope: schema rows in `relations.go`; emit sites in `walker.go` and `walker_v2.go` for both rels; unit tests.
- Dep: none.
- Size: **~100 LoC** (incl. tests).
- Delivers: two new EDB relations populated for every project. No QL consumer yet.
- Merge gate: existing extractor tests green; new walker tests green; `compat_test.go` unchanged.

### PR 2 — `feat(extract): ParamBinding post-pass relation`

- Scope: post-pass in walker_v2 (and walker.go via shared helper); schema row; carve-outs documented (no spread, no rest); unit tests.
- Dep: PR 1 (shares schema-version bump).
- Size: **~150 LoC** (post-pass logic is the largest single chunk).
- Delivers: third Phase A EDB relation.
- Merge gate: extractor tests green; ParamBinding row count on `react-usestate` fixture matches a hand-derived expected count (added test).

### PR 3 — `feat(ql): non-recursive mayResolveTo + tsq_valueflow.qll`

- Scope: `ql/system/valueflow.qll` with the 6 named branches + `mayResolveTo` union; `bridge/tsq_valueflow.qll` re-exporting + `resolvesToFunctionDirect`; new fixtures `valueflow-base/` and `valueflow-negative/`; QL tests.
- Dep: PR 1, PR 2.
- Size: **~250 LoC** (QL + fixtures + tests).
- Delivers: a sized, runnable, non-recursive `mayResolveTo`. Bridge is still on R1–R4.
- Merge gate: per-rule fixture rows match expected; negative fixtures return 0; planner sizing on `valueflow-base` shows non-default (i.e. real) cardinality estimates (assert via `tsq plan --explain` snapshot test).

### PR 4 — `refactor(bridge): collapse easy resolveToObjectExpr branches onto mayResolveTo`

- Scope: delete the 5 predicates listed in §3.1; rewrite their call sites to use `mayResolveTo` / `mayResolveToObjectField` / direct EDB. Save the golden baseline before opening PR.
- Dep: PR 3.
- Size: **~80 LoC removed, ~40 LoC added** at call sites. Net **−40 LoC**.
- Delivers: first measurable bridge collapse; baseline established for Phase C.
- Merge gate: golden-baseline diff is **empty** across all 5 React fixtures; `compat_test.go`, `regression_*_test.go`, `setstate_*_test.go` all pass; Mastodon bench wall-time delta within ±10% of pre-PR baseline.

**Total Phase A:** 4 PRs, ~620 LoC net add (~530 incl. test fixtures), elapsed estimate **2 weeks** as the parent doc projects.

---

## 6. What Phase A explicitly does NOT do

Comprehensive list — Phase B/C inheritors should expect none of these:

1. **No recursion in `mayResolveTo`.** Every rule is depth-1 from a `*ValueSource`-grounded leaf. No rule body contains a `mayResolveTo` literal.
2. **No two-hop var indirection.** `const a = b; const b = {...};` is unresolved in Phase A. Bridge's `resolveToObjectExprVarD2` survives.
3. **No spread resolution.** `{ ...base }` is unmodelled. All `objectLiteralFieldSpread*` predicates survive in the bridge.
4. **No JSX-wrapper unwrap.** `value={{...}}` where the JsxAttribute valueExpr is the JsxExpression wrapper, not the inner Object — `resolveToObjectExprWrapped` survives.
5. **No call-return composition.** `function f() { return {...}; } f()` does not resolve through Phase A. (Needs `mayResolveTo(retExpr, s)` recursion.)
6. **No destructure-source resolution.** `const { a } = o;` does not resolve `a` to `o.a`'s value-source. Needs recursion.
7. **No cross-module symbol resolution.** `ImportBinding`/`ExportBinding` chains are out of scope.
8. **No method/inheritance resolution.** `MethodResolvesTo` deferred entirely.
9. **No type-driven resolution.** `ExprType` is not consulted; we stay structural.
10. **No effect modelling.** Getters/setters/Proxy ignored, per parent doc §5.
11. **No await/promise chain.** `await e` is not unwrapped in Phase A even though parent doc §5 lists it as v1; the Phase A rules don't include the unwrap clause. Add in Phase C as a one-line rule.
12. **No `as` cast unwrap, no `!` non-null unwrap, no parenthesised-expr unwrap.** All deferred — they're each one rule in Phase C.
13. **No depth bound.** Phase A is depth-1 by construction; the `MayResolveDepth` parameter from parent doc §6 Phase C does not exist yet.
14. **No bridge R4 split deletions.** `_outerCtx`/`_innerCtx` and `contextSymLink*` survive untouched.
15. **No planner work.** Recursive-IDB sizing (parent §4.2 #1) and disjunction-binding-loss (#166) remain Phase B's job.

---

## 7. Risks specific to Phase A

### 7.1 Extractor complexity creep on `ParamBinding`

The post-pass design is clean *as written*, but JS/TS argument-passing has a long tail: spread args, rest params, default params, destructured params, optional params, overload resolution. The Phase A carve-out (skip spread + rest) is correct but easy to slide on under reviewer pressure. **Mitigation:** the relation comment in `relations.go` lists the carve-outs explicitly, and the unit test asserts a 0-row outcome for spread/rest fixtures so any drift breaks CI.

### 7.2 Schema bump breaks downstream consumers

`db.SchemaVersion` bump means older tsq binaries reading newly-extracted DBs won't know `ExprValueSource`/`ParamBinding`/`AssignExpr`. The current schema is forward-compat (unknown rels ignored) — verify before PR 1 lands by running an old binary against a new DB and confirming it doesn't panic. **Mitigation:** add a `compat_test.go` case that loads a Phase-A-extracted DB with an artificially-stale binary version and asserts the existing queries still work.

### 7.3 ParamBinding cardinality blow-up

If RTA-resolved `CallTarget` rows are noisy on Mastodon (one call site → many candidate fns), `ParamBinding` row count grows multiplicatively. A 3x blow-up on a 100k-symbol corpus pushes EDB into the millions. **Mitigation:** PR 2 includes a Mastodon row-count budget assertion in its bench output. If `ParamBinding` exceeds 5x the `CallArg` row count, PR 2 doesn't merge — switch to `CallTarget`-only (drop `CallTargetRTA`) and document the precision loss.

### 7.4 Subsumption assumption in §3.1 wrong

The 5 predicates marked "delete in PR 4" are claimed to be exactly subsumed by Phase A rules. If the bridge has a query path that exploits the *non*-resolution of one of them (an under-approximation the bridge silently relies on), deletion changes results. **Mitigation:** the golden-baseline diff in §4.3 catches this. Kill-switch: revert PR 4 only — PRs 1–3 stay landed and inert.

### 7.5 Disjunction-poisoning re-emerges in §2.7's union

The `or`-of-calls top-level shape is the known-good workaround pattern, but if any of the 6 branches happens to share a shape that triggers a *new* binding-loss case in the planner's disjunction rewrite, Phase A's rule returns 0 in production. **Mitigation:** PR 3's QL tests include per-branch isolation assertions (call each branch directly, confirm row count) AND a union assertion (call `mayResolveTo`, confirm row count = sum of branches modulo expected dedup). If they diverge on Mastodon, the workaround failed and we're hitting #166 again — escalate to Phase B.

### 7.6 Kill-switch / rollback story

Per-PR rollback model:

- **Revert PR 4** → bridge is back to current state; `tsq_valueflow.qll` exists but is unused; no functional regression.
- **Revert PR 3** → no consumer of new EDB rels; extractor still emits them harmlessly.
- **Revert PR 2** → `ParamBinding` gone; PR 3's `mayResolveToParamBind` branch returns 0 (other branches still work). Phase A is degraded but functional.
- **Revert PR 1** → schema-version rollback required; coordinated revert across all four PRs.

Phase A has no shared mutable global state with the rest of the planner — revert is mechanical. The schema bump in PR 1 is the one non-clean piece; everything downstream is additive.

---

## Appendix — open questions surfaced by Phase A

1. **Is `ExprValueSource` worth its row count?** It's `(v, v)` — pure identity. The planner-sizing argument (need a grounded base for Phase C) is real but un-measured. If P2b sampling can size a bare-equality base case adequately, `ExprValueSource` is dead weight on Mastodon (~500k rows of no information).
2. **Should `ParamBinding` use `CallTarget` only, or `CallTarget ∪ CallTargetRTA`?** RTA is more precise but multiplies. No data on Mastodon yet.
3. **Schema-version policy for additive relations.** Currently every new rel bumps `db.SchemaVersion` once. Three new rels in Phase A = three bumps or one? No documented policy; PR 1 should pick one.

---

## PR3 amendment — JSX wrapper subsumption gap (2026-04-19)

**Status:** landed alongside the bridge migration (this PR).

### The gap

Plan §3.1 lists `resolveToObjectExprVarD1` (bridge `tsq_react.qll` L534)
as cleanly subsumed by `mayResolveToVarInit` (plan §2.2). That subsumption
is wrong as designed.

`resolveToObjectExprVarD1` is **JsxExpression-wrapper-tolerant**:

```ql
predicate resolveToObjectExprVarD1(int valueExpr, int objExpr) {
    exists(int identExpr, int sym, int varDecl |
        (identExpr = valueExpr or Contains(valueExpr, identExpr)) and
        ExprMayRef(identExpr, sym) and
        VarDecl(varDecl, sym, objExpr, _) and
        isObjectLiteralExpr(objExpr)
    )
}
```

The `(identExpr = valueExpr or Contains(valueExpr, identExpr))` idiom
accepts the canonical `<Provider value={X} />` shape. The walker emits the
JsxAttribute's `valueExpr` column pointing at the `JsxExpression {X}`
wrapper (per `extract/walker.go:emitJsxAttr` field-or-fallback rule), not
at the inner identifier `X`. The `Contains` clause unwraps the wrapper
one level so `ExprMayRef(identExpr, sym)` can fire on the inner `X`.

`mayResolveToVarInit` as shipped in PR2 (commit `28bcda5`) is
value-expr-rooted:

```ql
predicate mayResolveToVarInit(int valueExpr, int sourceExpr) {
    exists(int sym, int initExpr, int varDecl |
        ExprMayRef(valueExpr, sym) and       // <-- direct on valueExpr, no unwrap
        VarDecl(varDecl, sym, initExpr, _) and
        ExprValueSource(initExpr, sourceExpr)
    )
}
```

`ExprMayRef(jsxExpr, sym)` is empty for the JsxExpression wrapper, so
substituting `mayResolveToVarInit` for `resolveToObjectExprVarD1` silently
drops every `value={X}` case — the entire IndirectValue + ComputedKey
path of the R3 fixture, which is the load-bearing test corpus.

The first PR3 attempt landed this substitution and the
`TestSetStateUpdaterCallsOtherSetStateThroughContext_R3` parity gate
caught it: 2 of 3 expected matches dropped to 0 (Indirect=0,
Computed=0). The PR3 attempt was halted, scope re-examined.

### The resolution

A `JsxExpression` wrapping an inner expression IS a real value-flow edge.
The fix is principled: extend `mayResolveTo` with wrapper-tolerant
handling, then redo the bridge migration.

#### Design — single wrapper branch over the core union

Two shapes were considered:

1. **Per-branch wrapped variants.** Add `mayResolveToVarInitWrapped`,
   `mayResolveToAssignWrapped`, …, six new predicates each unwrapping
   then re-running its underlying base. Bloats the union to 12 branches.
2. **One wrapper branch over a lifted core union.** Lift the existing
   six-branch union to `mayResolveToCore`, add a single
   `mayResolveToJsxWrapped` that calls `mayResolveToCore` on the
   unwrapped inner. Two-branch top-level union (`Core` ∪ `JsxWrapped`),
   one new helper, one new predicate.

PR3 picked shape (2). Both preserve the `or`-of-calls discipline at the
top level (each disjunct is a named-head call, not a literal disjunction
inside one rule body). Shape (2) keeps the union clean and makes the
wrapper extension a single contained change.

```ql
predicate jsxExpressionUnwrap(int jsxExpr, int innerExpr) {
    Node(jsxExpr, _, "JsxExpression", _, _, _, _) and
    Contains(jsxExpr, innerExpr)
}

predicate mayResolveToCore(int valueExpr, int sourceExpr) {
    mayResolveToBase(valueExpr, sourceExpr)
    or mayResolveToVarInit(valueExpr, sourceExpr)
    or mayResolveToAssign(valueExpr, sourceExpr)
    or mayResolveToParamBind(valueExpr, sourceExpr)
    or mayResolveToFieldRead(valueExpr, sourceExpr)
    or mayResolveToObjectField(valueExpr, sourceExpr)
}

predicate mayResolveToJsxWrapped(int valueExpr, int sourceExpr) {
    exists(int innerExpr |
        jsxExpressionUnwrap(valueExpr, innerExpr) and
        mayResolveToCore(innerExpr, sourceExpr)
    )
}

predicate mayResolveTo(int valueExpr, int sourceExpr) {
    mayResolveToCore(valueExpr, sourceExpr)
    or mayResolveToJsxWrapped(valueExpr, sourceExpr)
}
```

`jsxExpressionUnwrap` deliberately uses **direct** `Contains` (one
parent→child hop), not transitive walking. Arbitrary parent unwrap would
over-approximate: it would resolve `<Provider value={f({...})} />`
through the inner spread, when the bridge's existing semantics
intentionally stop there. Direct-only matches the existing
`resolveToObjectExprVarD1` semantics exactly: tree-sitter's
`jsx_expression` directly wraps a single inline expression.

Non-recursive, end to end. Call graph is

```
mayResolveTo  →  { mayResolveToCore, mayResolveToJsxWrapped }
mayResolveToJsxWrapped  →  mayResolveToCore
mayResolveToCore  →  { mayResolveToBase, …VarInit, …Assign, …ParamBind, …FieldRead, …ObjectField }
each branch  →  EDB only
```

No back-edge. Trivial-IDB pre-pass sizes the depth-3 stack the same way
it sized the original union.

### Migration scope, post-amendment

Plan §3.1 listed five predicates for deletion. After re-examining the
full call graph, PR3 ships a tighter scope:

| Predicate | Plan §3.1 | PR3 outcome | Rationale |
|---|---|---|---|
| `resolveToObjectExprDirect` | Delete | **Deleted** | Subsumed by `mayResolveToObjectExpr` (mayResolveToBase fires on the identity row of every object literal). |
| `resolveToObjectExprVarD1` | Delete | **Deleted** | Subsumed by `mayResolveToObjectExpr` once `mayResolveToJsxWrapped` lands. |
| `objectLiteralFieldOwn` | Delete | **Deferred** | Inlining its body (`ObjectLiteralField(o, f, v)`) into `objectLiteralFieldThroughSpread`'s union breaks the `or`-of-calls #166 workaround discipline at that union — first disjunct would become an EDB literal, the rest named calls. Deferred to a follow-up that addresses the discipline cost separately. |
| `contextProviderFieldR3DirectOwn` | Delete | **Deferred** | Composes with `objectLiteralFieldOwn`; deletion is contingent on the previous row. Same `or`-of-calls discipline concern at `contextProviderFieldR3DirectSpread`. |
| `contextProviderFieldR3VarIndirectOwn` | Delete | **Deferred** | Same — composes with `objectLiteralFieldOwn`, gated on the same discipline concern at `contextProviderFieldR3VarIndirect`. |

PR3 ships **2 of 5** plan-targeted predicates, plus the wrapper extension
that wasn't in the original plan. The remaining three are tracked for a
follow-up PR that also addresses the named-call wrapping pattern at
`*ThroughSpread` / `contextProviderFieldR3*` unions to preserve the #166
workaround.

### Helper introduced

`bridge/tsq_react.qll`:

```ql
predicate mayResolveToObjectExpr(int valueExpr, int objExpr) {
    mayResolveTo(valueExpr, objExpr) and
    isObjectLiteralExpr(objExpr)
}
```

Used by `resolveToObjectExpr`'s union in place of `Direct + VarD1`. Net
LoC: −12 deleted, +5 new helper definition + comment block, +1 union
disjunct change.

### Parity gate evidence

R3 + R4 through-context test results, baseline (origin/main + wrapper
extension only) vs. post-migration:

| Test | Baseline | Post-migration |
|---|---|---|
| `TestSetStateUpdaterCallsOtherSetStateThroughContext_R3` | total=3 (Indirect=1, Spread=1, Computed=1, Negative=0) | total=3 (Indirect=1, Spread=1, Computed=1, Negative=0) |
| `TestSetStateUpdaterCallsOtherSetStateThroughContext_R4` | total=2 (Consumer=1, DirectReturn=1, Negative=0) | total=2 (Consumer=1, DirectReturn=1, Negative=0) |

Empty diff. Parity gate held.

`TestR3_LinkPredicates` end-to-end consumers (the row counts that
matter for downstream callers — the union of resolveToObjectExpr's
output flowing into contextProviderField, etc.):

| Predicate | Baseline | Post-migration |
|---|---|---|
| `resolveToObjectExpr` | 11 | 16 (precision gain — wrapper extension widens reach) |
| `contextProviderFieldR3VarIndirect` | 6 | 6 |
| `contextProviderField` | 6 | 6 |
| `useStateSetterAliasV2` | 13 | 13 |
| `useStateSetterContextAliasCall` | 6 | 6 |

The widened `resolveToObjectExpr` row count is filtered by the
field-name + ExprMayRef downstream join, so the externally-visible
predicates (`contextProviderField`, `useStateSetterAliasV2`,
`useStateSetterContextAliasCall`) match baseline exactly.

### Test query import requirement

`bridge/tsq_react.qll`'s `mayResolveToObjectExpr` calls into
`mayResolveTo` from `bridge/tsq_valueflow.qll`. Bridge files don't carry
`import` statements; the resolver merges only what the query imports.
Queries that consume `tsq::react`'s React-context family must now also
import `tsq::valueflow`, otherwise `mayResolveTo` resolves to nothing and
the union silently returns just the surviving non-valueflow disjuncts
(`resolveToObjectExprWrapped`, `…VarD2`, `…HookReturn`).

`testdata/queries/v2/find_setstate_updater_calls_other_setstate_through_context.ql`
adds `import tsq::valueflow` for this reason. The other React-family
queries (find_setstate_updater_calls_other_setstate.ql, the props
variant, the regression query, the markdown variant, the mixpanel query)
do not consume `resolveToObjectExpr` and remain unchanged.

### Carry-forward — open questions for PR4 (or follow-up)

1. **Inline `objectLiteralFieldOwn` cleanly.** The `or`-of-calls
   discipline at `objectLiteralFieldThroughSpread` and the
   `contextProviderFieldR3*` unions can survive deletion if every
   disjunct stays a named-call. Options: keep `objectLiteralFieldOwn`
   permanently as a thin wrapper (defeating "delete" but trivial), or
   thread `mayResolveToObjectField`-like helpers everywhere (more
   substantive refactor). Defer until PR-shape decided.

2. **Can `resolveToObjectExprWrapped` go away too?** PR3 keeps it
   verbatim (plan §3.2 deferred it). Empirically, every Wrapped row is
   already produced by `mayResolveToJsxWrapped → mayResolveToBase` once
   the inner is the literal directly. Need Mastodon-scale parity check
   before claiming subsumption — JsxExpression wrappers in real code may
   carry intermediate kinds tree-sitter labels differently across
   backends.
