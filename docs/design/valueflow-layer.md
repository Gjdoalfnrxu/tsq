# Value-flow analysis layer for tsq — design

**Status:** design only. No code, no PRs, no schema changes. Branch
`design/valueflow-layer`, worktree `/tmp/tsq-valueflow-design`.

**Author:** Planky (with Cain).
**Date:** 2026-04-19.
**Reviewers:** anyone touching `bridge/tsq_react.qll`, `extract/`, `ql/plan/`.

## 1. Problem statement

The React bridge in `bridge/tsq_react.qll` is now **1263 lines** of QL whose
job is to answer one question: "for an expression `e` that ultimately reaches
the callee position of a call `c`, which `useState` setter symbol does `e`
resolve to?" Every round of bug fixes has added more named predicates because
each new syntactic carrier of the setter (JSX prop, React Context, object
literal, spread, var indirection, hook wrapper, cross-file import) needs its
own enumerated shape. Counts by round, all in `bridge/tsq_react.qll`:

| Round | Carrier added                                              | Predicates added | Lines added (approx) |
|-------|------------------------------------------------------------|------------------|----------------------|
| R1    | direct identifier through JSX prop, 1-2-3 hops unrolled    |  8               |  ~160                |
| R2    | React Context: createContext + Provider + useContext + hook | ~17              |  ~290                |
| R3    | object-literal spread, computed key, var-indirected value  | ~28              |  ~360                |
| R4    | disjunction-poisoning splits (`_outerCtx`/`_innerCtx`, `contextSymLinkSame`/`CrossFile`) | ~5 | ~80 |
| **Total** | **across rounds 1-4** | **~58 new predicates** | **~890 lines** |

Each round is essentially the same control flow ("walk one more step of value
provenance backwards from the call site") expressed by **enumerating the
syntactic shapes the value can take**. There are two compounding pressures:

1. **Real surface area is growing.** Round 5 (already lined up) is
   `wrapped-arrow passes` (`<Foo onChange={x => setX(x)} />`). Round 6 is
   single-named-props field-read (`function Foo(props) { props.onChange(...) }`).
   Round 7 is module re-export chains > 1 hop. Each is another fan of 5-10
   shapes.

2. **Each new shape costs ~N predicates because of issue #166.** Disjunction
   poisoning: `(A or B)` inside one predicate body returns 0 even when `A`
   alone returns 1 (planner drops bindings from one branch). Workaround is to
   split into named predicates union'd at top level — multiplies the predicate
   count by the disjunctive fan-out of every branching point. R4 split was a
   2-disjunct predicate becoming 3 predicates; the next will be worse.

**Project the trend:** at the current rate (~15 predicates per round) the
React bridge is on track to cross 2000 lines and 100 predicates within a
quarter, for a *single* code-smell rule. None of these predicates will be
reusable by the next bridge that wants to ask "what does this expression
resolve to" (express handler tracking, redux action shape, GraphQL resolver
arg, etc).

The bet of this design: a single `mayResolveTo` relation, with the
syntactic-shape coverage moved into one well-tested layer the bridge calls
once, ends the per-shape predicate inflation. The cost is a real
(under-)approximate value-flow analysis with the planner machinery to evaluate
it.

## 2. What "value flow" means here

### 2.1 Core relation

```
mayResolveTo(int valueExpr, int sourceExpr)
```

`mayResolveTo(v, s)` holds when the runtime value of expression `v` may be
the same value that expression `s` produces — across any number of
intermediate assignments, parameter bindings, returns, field writes/reads,
and destructuring steps that the analysis can prove are flow-compatible.

`s` is always an **expression that produces a value at a specific syntactic
location**: a function-expression literal, an object-literal, a `useState(...)`
call result component, a `createContext(...)` call, a string literal, etc.
`v` is the expression at the use site (the callee position of a call, the
sink position of a JSX attribute, the spread argument of a Provider value,
…).

A companion `resolvesToFunction(int callee, int fnId)` derives from
`mayResolveTo` + `FunctionSymbol`/`Function` to give the bridge layer the
"call-target" view it actually wants in 80% of cases. The bridge stops
asking "is the callee's symbol an alias of a setter sym?" and starts asking
"does the callee resolve to a function expression that came out of a
`useState(...)` initialiser?"

### 2.2 Soundness/precision posture

**Under-approximate by default.** `mayResolveTo` lists a finite set of
candidate sources; if no rule fires, no fact is emitted. Missing-edge
failures are the dominant failure mode — same posture as CodeQL's
`localFlow` and the v2 `LocalFlow`/`FlowStar` already in tsq. The
alternative (over-approximating with type-level "may flow anywhere of this
type") is more expensive AND noisier; we'd lose the bridge's whole reason
for existing.

**The honest framing:** `mayResolveTo` is "may-resolve" in the same sense
that CodeQL's dataflow is "may-flow" — sound *as a refutation tool* (if
nothing resolves, definitely nothing reaches), unsound as a proof tool
(absence of `mayResolveTo(v, s)` doesn't prove `v` can't carry `s`'s
value). We accept false negatives; we hard-budget false positives.

### 2.3 Reference point: CodeQL JS/TS DataFlow

CodeQL models a separate **data flow graph** distinct from the AST: nodes are
expressions, parameters, SSA defs, property reads/writes; edges are local
flow steps; `flowsTo` is the transitive closure plus a `Configuration`
class for source/sink/barrier framing. Local flow is "data flow within a
function, no calls, no property reads/writes." Global flow adds inter-procedural
+ stores/loads + sacrifices precision (their docs are explicit:
*"the analysis may report spurious flows that cannot in fact happen"*).

We deliberately diverge in three places (§8): no separate dataflow-node
type system at the schema level, no SSA representation, no `Configuration`
class as a *performance* contract (P3a in the planner roadmap retired the
`BackwardTracker` performance role).

## 3. Base primitives — what the extractor already emits, what's missing

### 3.1 Already emitted (per `extract/schema/relations.go`)

The extractor today emits a healthy intra-procedural value-flow vocabulary.
Key already-present relations:

- `Assign(lhsNode, rhsExpr, lhsSym)`
- `VarDecl(varDecl, sym, initExpr, _)`
- `ExprMayRef(expr, sym)` — expression may reference symbol
- `ExprIsCall(expr, call)` / `Call(call, callee, _)`
- `CallArg(call, idx, argNode)`
- `CallCalleeSym(call, calleeSym)` / `CallTarget(call, fn)` /
  `CallTargetRTA(call, fn)` (RTA-resolved)
- `CallResultSym(call, resultSym)`
- `FieldRead(expr, baseSym, fieldName)`
- `FieldWrite(stmt, baseSym, fieldName, valueExpr)`
- `DestructureField(parent, sourceField, bindName, bindSym, idx)`
- `ArrayDestructure(parent, idx, bindSym)`
- `ReturnStmt(fn, _, retExpr)` + `ReturnSym(fn, returnSym)`
- `ObjectLiteralField(parent, fieldName, valueExpr)` (added round 2)
- `ObjectLiteralSpread(parent, idx, sourceExpr)`
- `JsxElement(elem, tagNode, tagSym)` + `JsxAttribute(elem, name, valueExpr)`
- `Parameter(fn, idx, name, paramNode, sym, typeText)`
- `FunctionSymbol(sym, fn)` + `Function(fn, ...)` + `Contains(parent, child)`
- `ImportBinding(sym, module, name)` / `ExportBinding(sym, module, name)`
- `LocalFlow(fnId, srcSym, dstSym)` + `LocalFlowStar(fnId, srcSym, dstSym)`
  (intra-procedural, sym→sym)
- `InterFlow(srcSym, dstSym)` + `FlowStar(srcSym, dstSym)` (inter-procedural)

This is most of what `mayResolveTo` needs. The current bridge is **not**
using `LocalFlow`/`FlowStar` because they are sym→sym and the bridge
question is expr→expr — a value-source expression is literally not a
symbol. That gap is the whole reason rounds 1-4 exist as expression-shape
enumeration.

### 3.2 What's missing — list

Trivial-extension-of-the-walker class:

- `ExprValueSource(int expr, int sourceExpr)` — direct extractor edge
  emitted at every expression that immediately produces a value, with
  `expr == sourceExpr`. One row per value-producing AST node. ~5-10 lines
  in `walker.go` and `walker_v2.go`.
  Example for `const x = 5;` — emit `ExprValueSource(<5-literal-expr>,
  <5-literal-expr>)`.

- `ParamBinding(int fn, int paramIdx, int paramSym, int argExpr)` —
  augments `Parameter`/`CallArg` with the per-call edge *at the binding
  site*. Today the bridge has to join `CallTarget × CallArg × Parameter`
  to compose this; it's hot enough that rolling it out as a base relation
  is a planner kindness.
  Example: `setX(prev => …)` with callee `useState`'s param0 — emit
  `ParamBinding(<useState-fn>, 0, <prev-paramSym>, <arrow-expr>)`. (Yes,
  this overlaps `CallTarget`+`CallArg`+`Parameter`; the win is one
  literal in hot bodies.)

- `AssignExpr(int lhsSym, int rhsExpr)` — Assign minus the lhsNode,
  symmetric to `VarDecl(_, sym, init, _)`. Today both shapes need
  separate join paths in `LocalFlow`. Helper relation; deferable.

Real-pass class (NOT a trivial extension; needs a binding/scope or type
pass):

- `ResolvesToImportedFunction(int useExpr, int targetFn)` — cross-module
  function resolution. Today `tsq_react.qll` does it via
  `ImportBinding(name) ↔ ExportBinding(name)` wildcard-on-module. Sound
  only when names don't collide across modules. To do better needs the
  **module resolver** to participate (tsgo can do this; the vendored
  walker cannot).

- `FieldShape(int objExpr, string fieldName, int valueExpr)` — the
  object-literal model collapsed across `own`/`spreadD1`/`spreadD2`/
  `var-indirect`. The 5 shape predicates in R3 (`objectLiteralField*`)
  collapse into one relation if the *extractor* (which has the parser)
  unrolls spread-of-known-object-literal during emission. Spreads of
  unknown expressions stay unmodelled.

- `MethodResolvesTo(int methodCallExpr, int methodFn)` — needs
  inheritance + RTA, both already emitted (`MethodDeclInherited`,
  `CallTargetRTA`); composition not yet exposed at expression level.

- `Effect(int expr, …)` — getter/setter and Proxy effects. Drop
  entirely from v1; document deferral.

### 3.3 Worked tiny example

For:

```ts
function makeIncrementer(setX) { return prev => setX(prev + 1); }
const [n, setN] = useState(0);
const inc = makeIncrementer(setN);
inc();
```

Wanted: `mayResolveTo(<inc-callee>, <useState-call-result-component-1>)`,
i.e. the callee at `inc()` may resolve to the setter symbol returned
by `useState`. Path:

1. `inc` is a VarDecl initialised from `makeIncrementer(setN)` —
   `LocalFlow(makeIncrementer-result-sym, inc-sym)` already present.
2. `makeIncrementer` returns an arrow whose body calls `setX`. Param0 of
   `makeIncrementer` is `setX`. The summary `ParamToReturn(makeIncrementer,
   0)` is in scope (Phase C2).
3. The actual arg at the call site is `setN` — `ExprMayRef(setN-arg, setN-sym)`.
4. Compose: `mayResolveTo(<inc-callee>, ?)` should chain `inc`'s value
   back through `LocalFlow*`, hit a call to `makeIncrementer`,
   pass through `ParamToReturn` mapped to `setN` (the actual arg at the
   call site), then trace `setN` back through `ArrayDestructure` to the
   `useState(0)` call.

Today rounds 1-4 cover **none of this**. Round 5 (wrapped arrows) wouldn't
either. With `mayResolveTo` it's the same recursive predicate that handles
every other case.

## 4. Planner blockers

`mayResolveTo` is recursive by nature — flow chains are unbounded depth.
The planner roadmap is mid-flight on the exact gaps that block this. Status
as of `d08d52d` (today):

### 4.1 Phases that have landed and DO unblock value flow

- **P1** (estimate-first pipeline, PR #140) — landed.
- **P2a** (class-extent materialisation, PR #141) — landed.
- **P2b** (sampling estimator, PR #142) — landed.
- **P3a** (rule-body backward-binding inference, PR #143) — landed.
- **P3b** (projection pushdown, PR #144) — landed.
- **disj2 rounds 1-6** (#149/#156/#158/#161/#162/#168/#170) — landed; the
  composition path through magic-set rewrite + class-extent stripping +
  arity-keyed propagation is now functional on Mastodon for the
  through-context query.

### 4.2 Real and load-bearing — must-fix before value flow ships

1. **Recursive-IDB cardinality estimation.** The P2b sampling estimator is
   non-recursive only. For an IDB whose body refers to itself (the textbook
   `mayResolveTo` shape: `mayResolveTo(v, s) :- mayResolveTo(v, mid),
   step(mid, s)`), the planner falls back to the **default 1000-row hint**
   — and immediately picks Cartesian-heavy join orders downstream. Until
   recursive IDBs get a real cardinality estimate, every value-flow query
   is a roll of the dice on Mastodon.

   Fix path: extend the trivial-IDB pre-pass to seed recursive IDBs from
   their **base case** estimate × an estimated fixpoint multiplier
   (a constant, or a bounded sampling pass that runs δ for K iterations
   and projects). This is a planner-roadmap-level item, not a value-flow
   item, but it's on the critical path.

2. **Disjunction poisoning (#166).** The R4 split exists because
   `(A or B)` drops bindings from one branch. A real value-flow rule has
   ~10 disjuncts (one per step kind: assignment, return, param-bind,
   field-read-of-known-write, destructure, …). If we have to manually
   split each, we've recreated rounds 1-4 inside the value-flow layer.

   Fix path: root-cause the binding-loss in the planner's disjunction
   rewrite. Probably a magic-set seed that doesn't propagate to the
   second branch's adornment. Filed in the R2 wiki note as "planner
   issue to file"; still open.

### 4.3 Incidental — not blocking, but will become blocking

3. **Magic-set propagation through deep recursion.** `pickMagicAnchor`
   (R6) anchored magic literals at slot 0 of body ordering, which
   handles *one* recursive level. A 5-step `mayResolveTo` chain has
   `magic_mayResolveTo` literals appearing inside rules that themselves
   produce `magic_*` propagation rules. Adversarial review on the first
   real value-flow PR will surface ordering pathologies the round-6 fix
   doesn't cover.

4. **Termination / fixpoint cost modelling.** Today seminaive
   evaluation runs to fixpoint regardless of cost. A `mayResolveTo`
   that explodes (cycle in bindings, object-with-self-spread) will
   simply OOM. We need either (a) a depth bound configurable at the
   query level, or (b) a wall-time bound at fixpoint level. CodeQL
   resolves this with explicit `step` count caps in the dataflow
   library; we can do the same.

5. **(name, arity) keying everywhere.** Rounds 1-6 of disj2 keep
   re-discovering name-only lookups. A value-flow layer will add ~10
   more IDB heads; pre-emptively sweep every name-only IDB lookup in
   `ql/plan/` to (name, arity) before the value-flow PR opens. Audit
   item — file as a janky issue.

### 4.4 Critical-path verdict

To ship even Phase A (intra-procedural `mayResolveTo`) we need #1 and
#2 fixed. #3 and #4 can land as the value-flow layer matures. **#1 is
the hardest** — recursive cardinality estimation has no clean answer
in the literature short of "run it and find out."

## 5. Soundness/precision dial

Each axis: v1 default → v2 stretch.

| Axis              | v1 default                  | v2 stretch                            | Why v1 |
|-------------------|------------------------------|----------------------------------------|--------|
| Field sensitivity | **Field-named, no shape.** Track `FieldRead/Write` by string field name; ignore base-object identity. (Same as `TaintedField` today.) | Per-allocation-site shape (CodeQL's "AccessPath"). | Matches the existing taint layer; field-name collisions are rare in real React code. |
| Flow sensitivity  | **Insensitive.** Treat all writes to a symbol as equally live. | SSA-form (SsaDefinitionNode-equivalent). | SSA pass is its own quarter of work; CodeQL took years. |
| Context sensitivity | **Insensitive.** A function called from two sites yields one set of summary edges. | k-CFA bounded at k=1 or k=2. | Mastodon already strains the planner at insensitive; context bumps cost ~Nx callers. |
| Cross-call depth  | **Bounded at 3.** Match the existing R1-R3 unrolled depths in `tsq_react.qll`. | Unbounded (true recursion + planner sizing). | Hand-unrolled is the proven pattern; lift after planner blocker #1 lands. |
| Mutation          | **Last-write-wins, fn-scoped.** Within a function, rewrite-aware via `LocalFlow` chain; across function boundaries, treat all writes as may-occur. | Heap-shape-aware. | Matches LocalFlow semantics. |
| Reassignment      | **Tracked intra-procedurally** via existing `LocalFlow` rule for Assign. | Inter-procedural via `InterFlow` extension. | Free with §3.1. |
| Async             | **Treat `await e` as `e`.** No promise-graph. | Promise resolution chain. | Promise modelling is its own multi-month project. |
| Dynamic dispatch  | **Use `CallTargetRTA` when present, fall back to `CallCalleeSym` symbol resolution.** | Type-flow-driven. | RTA already in extractor schema (`CallTargetRTA/2`). |
| Prototype chains  | **Ignore.** Treat all method calls as resolving via the class's `MethodDeclInherited` extent. | Full prototype-chain walk. | Real React code rarely uses prototype assignment outside libs. |
| Getters/setters   | **Ignore — treat as fields.** | Effect-system extension. | Defer indefinitely; not seen in any failing query so far. |

The v1 settings line up with what the **existing R1-R4 bridge already
assumes implicitly** — we are not weakening anything by being explicit.
The exercise here is making the assumptions one named contract instead of
58 named predicates that each have their own implicit assumptions and
disagree at the edges.

## 6. Phased rollout

### Phase A — extractor extensions + simplest intra-procedural `mayResolveTo`

**Scope:** the §3.2 trivial extensions, plus a non-recursive
`mayResolveTo` that handles 1-step `LocalFlow`, `ParamBinding` at depth
1, and direct expression-source attribution. Field reads of
**syntactically-adjacent** field writes only (no recursive object-shape
walk).

**Deliverables:**
- `ExprValueSource`, `ParamBinding`, `AssignExpr` schema relations,
  emitted by both `walker.go` and `walker_v2.go`.
- `ql/system/valueflow.go` (new) — system Datalog rules for
  `mayResolveTo` non-recursive case.
- `bridge/tsq_valueflow.qll` (new) — class wrapper, ~50 lines.
- Tests: parity test that the depth-1 `mayResolveTo` reproduces the R1
  prop-pass detection on the existing fixture.

**Size:** 2-3 PRs, ~2 weeks. No planner work needed (non-recursive +
small intermediate sizes → trivial-IDB pre-pass handles cardinality).

**Unblocks:** the bridge can start migrating the *direct* (non-aliased)
case as a measurement smoke test.

### Phase B — planner work needed for recursive value flow

**Scope:** the §4.2 blockers. **Not value-flow code** — pure planner.

**Deliverables:**
- Recursive-IDB cardinality estimator: extend
  `EstimateNonRecursiveIDBSizes` with a recursive-case path that bounds
  the seed × fixpoint multiplier. ~3 weeks. Critical path.
- Disjunction-binding-loss root cause + fix (#166). ~1-2 weeks once the
  reproducer is minimised.
- (name, arity) sweep for residual name-keyed lookups in `ql/plan/`. ~3
  days.

**Size:** ~5-7 weeks total. This is the riskiest phase. Could
silently bloat to a quarter if the recursive estimator turns out hard.

**Unblocks:** Phase C.

### Phase C — full `mayResolveTo` (cross-call, recursive, cross-module)

**Scope:** the recursive Datalog rule for `mayResolveTo`, with
`InterFlow` composition at call boundaries and `ImportBinding`/
`ExportBinding` resolution at module boundaries. Field-write+field-read
matching at full recursion depth (bounded by the recursive-IDB sizing
work in Phase B). Cross-module symbol bridging via the same name-keyed
resolution the existing bridge uses.

**Deliverables:**
- ~10 recursive Datalog rules in `ql/system/valueflow.go` covering each
  step kind.
- `bridge/tsq_valueflow.qll` extended with `mayResolveTo` /
  `resolvesToFunction` / `resolvesToObjectLiteralField` predicates.
- Configurable depth bound (`MayResolveDepth = 5` initial).
- Tests: parity tests against R1, R2, R3 React fixtures (each existing
  fixture must produce the same alert under the new layer).

**Size:** 3-4 PRs, ~4 weeks (assumes Phase B landed clean).

**Unblocks:** Phase D.

### Phase D — bridge migration + measurement

**Scope:** rewrite `bridge/tsq_react.qll` to use `mayResolveTo` /
`resolvesToFunction` for the setter-resolution work. Delete the R1-R4
predicates. Run the full bench (cain-nas, mastodon + jitsi).

**Measurement targets:**
- Lines deleted from `tsq_react.qll`: target ≥ 600 of 1263 (the
  R1-R4 alias chain).
- Predicate count: target -50 (from ~70 to ~20).
- Mastodon corpus row-count delta: ≤ 10% growth in alert set (some
  legitimate new alerts expected from chains R1-R4 didn't cover);
  ≤ 5% spurious alerts in spot-check.
- Wall-time on cain-nas mastodon: budget +50% of baseline; flag for
  follow-up if worse.

**Size:** 1 PR + bench validation, ~1 week.

**Unblocks:** the next bridge that wants resolves-to (Express handler
tracking, redux, etc) — they import `tsq_valueflow.qll` instead of
re-doing rounds 1-4.

### Phase ordering

A and B can run in parallel (different people). C depends on both. D is
sequential after C. Worst case: 12 weeks elapsed if B goes well, 20+ if
B stalls.

## 7. Risks and what could go wrong

1. **Recursive cardinality estimation has no clean answer.** This is the
   #1 risk. Literature is thin (Soufflé uses profile data; CodeQL has
   dbscheme statistics). If the estimator is too conservative, every
   recursive IDB is treated as huge and the planner takes a worst-case
   cap-hit path. If too aggressive, the planner picks Cartesian. The
   honest probability this turns into a 2-3 month rabbit hole: **~40%**.

2. **The bridge migration finds the parity tests passing but real
   queries silently regressing.** Existing R1-R4 predicates were tuned
   for the specific fixtures; `mayResolveTo` at depth 5 may produce
   false positives on shapes the bridge had silent under-approximations
   for. Mitigation: golden-row test on every committed query before
   merging Phase D.

3. **Mastodon wall-time blows out.** 50% headroom may not be enough.
   Recursive IDB on a 100k-symbol corpus is genuinely expensive. If
   wall goes 3x, we revert Phase D and keep the layer for new bridges
   only — the React bridge stays on the per-shape predicates.

4. **Disjunction-poisoning fix is harder than expected.** If #166 needs
   a deep magic-set rewrite redesign, Phase B grows by another 4-6 weeks.

5. **Soundness: a real bug in the type system → silent FNs.** A
   `ParamToCallArg` summary that is wrong will silently drop a chain;
   the user sees no alert and no error. Mitigation: reuse the existing
   monotone-property tests (`taint_monotone_property_test.go`) on the
   value-flow rules.

### Minimum viable if forced to ship early

If Phase B stalls and we have to ship something, **Phase A's
non-recursive `mayResolveTo` is independently useful**: it covers the
single-step direct cases (R1's depth-1 form) with a fraction of the
predicates and no planner work. The bridge keeps R1-R4's depth-2/3
unrolling for the recursive cases. Net win: ~150 lines deleted, no
parity risk. This is the abandon-and-fall-back position.

## 8. Comparison with CodeQL's DataFlow library

### 8.1 What theirs does (from the JS/TS docs)

- **DataFlow::Node**: separate type system from the AST. Hierarchy of
  ValueNode, SsaDefinitionNode, ParameterNode, PropRead/PropWrite,
  InvokeNode, etc. Each is a wrapper class with predicates that map
  back to AST locations.
- **Local flow** (`localFlowStep`/`localFlowStepPlus`): within a
  function, sym-to-sym, no calls, no property reads/writes.
- **Global flow**: composed from local flow + call-return + store/load
  edges. Documented as "may report spurious flows."
- **Configuration class**: `isSource`/`isSink`/`isBarrier`/
  `isAdditionalFlowStep`. User-facing performance idiom — and per the
  CodeQL source, *also* a planner contract (CodeQL specialises the
  pruned global graph to each Configuration).
- **Field sensitivity**: tracks `getAPropertyRead("x")`/`getAPropertyWrite("x")`
  by string field name. Closer to v1 §5 default than the SSA-form
  alternative.
- **Precision/cost call-out**: "global data flow analysis typically
  requires significantly more time and memory than local analysis."

### 8.2 Where we deliberately diverge

- **No separate dataflow node type.** tsq's schema is already
  expression+symbol; introducing `DataFlow::Node` doubles the
  vocabulary the bridge author has to learn. We stay expr/sym.
- **No SSA pass.** Cost is precision — we will report spurious flows
  through reassigned variables that CodeQL wouldn't. Acceptable for v1
  given the existing taint layer is already insensitive.
- **`Configuration` is documentation, not a planner contract.** The
  planner roadmap explicitly retired the `BackwardTracker` performance
  role in P3a. CodeQL's Configuration-as-perf-contract is exactly the
  trust-channel hazard P1+P3a fixed. We keep the QL idiom (it's
  legible) but the planner doesn't read it.
- **No global-flow / local-flow split as a user-facing toggle.** One
  `mayResolveTo`, with a depth bound. Users who want local-only set
  depth=1; users who want global set depth=5. Removes the "did I
  remember to use the right flow?" footgun.

### 8.3 Lessons from CodeQL's iteration history

- **They started field-insensitive, added field-sensitivity later.**
  We do the same.
- **The `Configuration` class is their biggest user-facing churn —
  they've revised it 3 times.** Keep our equivalent (`tsq_valueflow.qll`)
  thin and stable from day one; expand outward.
- **Their dataflow took ~a decade to reach current precision.** We
  will not match it. The goal is "deletes more bridge code than it
  adds, and unblocks the next bridge," not "matches CodeQL."
- **Their step caps are explicit and measurable.** Mirror this — don't
  let recursion run unbounded; expose the depth bound at the QL surface.

---

## Appendix: relation sketches

```
// Existing — reused unchanged
LocalFlow(int fn, int srcSym, int dstSym)
LocalFlowStar(int fn, int srcSym, int dstSym)
InterFlow(int srcSym, int dstSym)
FlowStar(int srcSym, int dstSym)
ExprMayRef(int expr, int sym)
ParamToReturn(int fn, int paramIdx)
ParamToCallArg(int fn, int paramIdx, int calleeSym, int argIdx)

// New — Phase A
ExprValueSource(int expr, int sourceExpr)
ParamBinding(int fn, int paramIdx, int paramSym, int argExpr)

// New — Phase C (system Datalog rules, IDB)
mayResolveTo(int valueExpr, int sourceExpr)        // recursive
resolvesToFunction(int callee, int fnId)           // derived
resolvesToObjectLiteralField(int valueExpr, int fieldName, int sourceExpr)
```

Sketch of the recursive rule (informal — actual rules will need
demand-aware shaping per §4):

```
mayResolveTo(v, s) :- ExprValueSource(v, s).                     // base
mayResolveTo(v, s) :- ExprMayRef(v, sym), VarDecl(_, sym, init, _),
                      mayResolveTo(init, s).                      // var init
mayResolveTo(v, s) :- ExprMayRef(v, sym), Assign(_, rhs, sym),
                      mayResolveTo(rhs, s).                       // assignment
mayResolveTo(v, s) :- ExprMayRef(v, sym), ParamBinding(_, _, sym, argExpr),
                      mayResolveTo(argExpr, s).                   // param binding
mayResolveTo(v, s) :- ExprMayRef(v, sym), CallResultSym(call, sym),
                      CallTarget(call, fn), ReturnStmt(fn, _, retExpr),
                      mayResolveTo(retExpr, s).                   // call return
mayResolveTo(v, s) :- FieldRead(v, baseSym, fld),
                      FieldWrite(_, baseSym, fld, writeExpr),
                      mayResolveTo(writeExpr, s).                 // field
mayResolveTo(v, s) :- ObjectLiteralField(parent, fld, valExpr),
                      // …spread/destructure resolution chained here
                      mayResolveTo(valExpr, s).
mayResolveTo(v, s) :- DestructureField(parent, srcFld, _, sym, _),
                      ExprMayRef(v, sym),
                      // resolve parent's source object, then read srcFld
                      …
                      mayResolveTo(parentValExpr, s).
```

Everything in `tsq_react.qll` rounds 1-4 is one of these step kinds
re-encoded as a syntactic shape. The bet is that writing the step kinds
once costs less than writing the syntactic shapes forever.
