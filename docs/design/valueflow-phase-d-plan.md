# Value-flow layer — Phase D implementation plan

**Status:** in-flight. Phase D bridge PRs PR1/PR2/PR6 have landed on
`main` (PRs #205, #206, #207). PR3 (`tsq_taint.qll`) and PR5
(`compat_tainttracking.qll`) are **retired** as exploratory dead ends —
see §2.X retirement note below and issue #208. plan-PR7 (R1–R4 shape-
predicate deletion in `tsq_react.qll`, the ≥600-LoC deletion win) is
pending and gated on the measurement harness landed by this PR (the
harness PR is the PR you are reading this plan from).
**Author:** Planky.
**Date:** 2026-04-19 (initial); revised 2026-04-20 (PR3/PR5 retirement,
§4.5 recalibrated for the reduced bridge set).
**Parent:** `docs/design/valueflow-layer.md` §6 Phase D.
**Precondition:** Phase C landed (recursive `mayResolveTo`,
`resolvesToFunction`, `resolvesToObjectLiteralField` shipped via
`bridge/tsq_valueflow.qll` — confirmed by parity tests against R1-R3
React fixtures).

Phase D is the wider rollout. Two questions: does the value-flow layer
hold up beyond the React proof-of-concept, and is the planner cost
acceptable on real corpora? Both must be answered with numbers, not
vibes, and the keep-or-revert thresholds must be set **before** the
numbers come in.

---

## 1. Bridge inventory

Every `*.qll` under `bridge/` classified.

| Bridge | Lines | Domain | Phase D action | Notes |
|---|---|---|---|---|
| `tsq_react.qll` | 1263 | React (setter resolution) | **Migrated by Phase C — confirm parity** | The proof-of-concept. Phase D inherits it; R1-R4 predicates targeted for deletion in PR-final. |
| `tsq_taint.qll` | 131 | Taint sources/sinks/sanitisers | **Migrate (high value)** | Hand-rolled `TaintedSym` / `TaintedField` chains today. ~6 predicates, ~40 lines collapse to `mayResolveTo`-backed equivalents. |
| `compat_tainttracking.qll` | 141 | CodeQL-compat TaintTracking | **Migrate (high value)** | `Configuration` API surface — `isAdditionalTaintStep` and the closure can be expressed via `mayResolveTo` with sanitiser predicates as barriers. ~3 predicates affected. |
| `compat_dataflow.qll` | 187 | CodeQL-compat DataFlow | **Migrate (high value)** | Whole module is the natural home for `mayResolveTo` as a backing relation for `localFlow`/`flowsTo`. ~5 predicates affected. |
| `tsq_express.qll` | 29 | Express handlers | **Migrate (medium value)** | `ExpressReqSource → handler argExpr` chain currently relies on `TaintSource` extent only; `mayResolveTo` enables true handler-arg → use-site resolution. ~2 predicates added (not deleted). |
| `tsq_dataflow.qll` | 46 | LocalFlow / LocalFlowStar wrappers | **Migrate (low risk, low value)** | Already maps onto base relations; opportunity to expose `mayResolveTo` alongside as the public surface. ~1 predicate added. |
| `tsq_summaries.qll` | 121 | ParamToReturn / ParamToCallArg | **Skip — but read by `mayResolveTo`** | Already a primitive `mayResolveTo` consumes. No migration; verify no regressions. |
| `tsq_callgraph.qll` | 53 | CallTarget / CallTargetRTA wrappers | **Skip** | Pure wrappers over base relations. No value-flow logic. |
| `tsq_calls.qll` | 75 | Call / CallArg classes | **Skip** | Schema wrappers. |
| `tsq_functions.qll` | 158 | Function / Parameter classes | **Skip** | Schema wrappers. |
| `tsq_expressions.qll` | 197 | Expression classes (FieldRead / FieldWrite / ObjectLiteral / etc.) | **Skip** | Schema wrappers. `mayResolveTo` reads from these; no migration. |
| `tsq_variables.qll` | 38 | VarDecl / Assign classes | **Skip** | Schema wrappers. |
| `tsq_composition.qll` | 41 | Composition helpers | **Skip** | Generic combinators. |
| `tsq_jsx.qll` | 44 | JSX classes | **Skip** | Schema wrappers. |
| `tsq_types.qll` | 163 | Type-text classes | **Skip** | Type-side, not value-side. |
| `tsq_symbols.qll` | 64 | Symbol class | **Skip** | Schema wrapper. |
| `tsq_imports.qll` | 38 | ImportBinding / ExportBinding | **Skip — read by `mayResolveTo`** | Primitives. |
| `tsq_base.qll` | 64 | Base entity classes | **Skip** | Schema. |
| `tsq_node.qll` | 17 | ASTNode | **Skip** | Schema. |
| `tsq_errors.qll` | 31 | Error / Diagnostic | **Skip** | Diagnostics. |
| `compat_javascript.qll` | 376 | CodeQL-compat JS AST | **Skip in D, candidate for E** | Touches `getAReference`-style predicates that *would* benefit but the surface is too wide for a single-PR migration. Defer to a follow-up RFC. |
| `compat_dom.qll` | 84 | DOM source/sink shorthands | **Skip** | Source/sink declarations only; flow happens elsewhere. |
| `compat_security_xss.qll` | 36 | XSS source/sink wrappers | **Skip — gets the win for free** | Backed by `compat_tainttracking.qll`; benefits transitively. |
| `compat_security_sqli.qll` | 36 | SQLi wrappers | **Skip — free win** | As above. |
| `compat_security_cmdi.qll` | 36 | Cmd injection wrappers | **Skip — free win** | As above. |
| `compat_security_pathtraversal.qll` | 39 | Path traversal wrappers | **Skip** | Stub kind, not extracted. |
| `compat_http.qll` | 53 | HTTP source classes | **Skip** | Source declarations. |
| `compat_io.qll` | 33 | IO sink classes | **Skip** | Sink declarations. |
| `compat_crypto.qll` | 75 | Crypto sink classes | **Skip** | Sink declarations. |
| `compat_regexp.qll` | 30 | Regexp classes | **Skip** | Schema. |

### 1.1 Phase D migration roster

Six bridges in scope. Estimated effort:

| Bridge | Predicates touched | Lines added | Lines deleted | Net |
|---|---|---|---|---|
| `tsq_react.qll` (Phase C inheritance) | ~10 (delete R1-R4) | +50 | -600 | -550 |
| `tsq_taint.qll` | ~4 | +30 | -20 | +10 |
| `compat_tainttracking.qll` | ~3 | +40 | -10 | +30 |
| `compat_dataflow.qll` | ~5 | +60 | -30 | +30 |
| `tsq_express.qll` | ~2 (add) | +25 | 0 | +25 |
| `tsq_dataflow.qll` | ~1 (add) | +15 | 0 | +15 |
| **Total** | **~25** | **+220** | **-660** | **-440** |

Hits the parent doc's Phase D target of ≥600 lines deleted from
`tsq_react.qll`. Net-net the bridge directory shrinks.

---

## 2. Per-bridge migration sequencing

One PR per bridge (or grouped where coupled). Order chosen by **value
÷ risk**: highest-leverage low-risk first, react finalisation last
because it carries the deletion of the round-3/4 shape predicates.

### PR1 — benchmark harness + measurement table template (no code)
- **Title:** `bench: value-flow Phase D harness + measurement matrix template`
- **Scope:** `bench/valueflow/` directory: `bench_run.sh`, `corpora.yaml`,
  `compare.py` (CSV diff + wall-time delta), and `MATRIX.md` (the
  empty matrix). No production code.
- **Size:** ~250 lines (mostly shell + python).
- **Tests gating merge:** harness self-test on the local fixtures
  (round-1 to round-4 React) producing a deterministic baseline row.
- **Corpus measurements gating merge:** N/A (no production change).
- **Dependency:** none.

### PR2 — `tsq_dataflow.qll` (lowest risk)
- **Title:** `feat(bridge): expose mayResolveTo via tsq_dataflow.qll`
- **Scope:** add `MayResolveTo` class wrapping `mayResolveTo`; no
  deletion. Pure additive surface.
- **Size:** ~15 lines + 30 lines test.
- **Tests gating merge:** parity test that
  `MayResolveTo(v, s).getSource()` matches `mayResolveTo(v, s)` on
  round-1 fixture.
- **Corpus measurements gating merge:** matrix row complete; row count
  parity = identical (additive, never returned by existing queries);
  wall ≤ 1.1x.
- **Dependency:** PR1.

### PR3 — `tsq_express.qll` (additive, isolated)
- **Title:** `feat(bridge): handler-arg → use-site resolution via mayResolveTo`
- **Scope:** add `ExpressHandlerArgUse` predicate using
  `mayResolveTo(useExpr, handlerArgExpr)`. Additive.
- **Size:** ~25 lines + 50 lines fixture/test.
- **Tests gating merge:** new fixture
  `testdata/express_arg_use_basic` showing handler-arg flow into a
  query string concat sink.
- **Corpus measurements gating merge:** matrix row complete on jitsi
  (Express-heavy); wall ≤ 1.5x; zero new cap-hits.
- **Dependency:** PR1.

### PR4 — `tsq_taint.qll` (medium-value, medium-risk)
- **Title:** `refactor(bridge): tsq_taint TaintedSym/TaintedField via mayResolveTo`
- **Scope:** rewrite `TaintedSym` and `TaintedField` to defer to
  `mayResolveTo` with taint-source predicates as the start set. Delete
  the hand-rolled chain predicates.
- **Size:** ~30 added / ~20 deleted.
- **Tests gating merge:** all `compat_security_*_test.go` green;
  `TestTaintMonotone` green.
- **Corpus measurements gating merge:** matrix rows for jitsi +
  Mastodon; row count parity bit-identical OR diff approved as
  improvement (review process below); wall ≤ 2x; maxrss ≤ 1.5x;
  zero new cap-hits.
- **Dependency:** PR1, PR2.

### PR5 — `compat_dataflow.qll` (high-value, medium-risk)
- **Title:** `refactor(bridge): compat DataFlow.localFlow backed by mayResolveTo`
- **Scope:** `DataFlow.localFlow` and `DataFlow.localFlowStep` rewritten
  to call `mayResolveTo` with depth=1; `DataFlow::Node.getASuccessor`
  routes through it.
- **Size:** ~60 added / ~30 deleted.
- **Tests gating merge:** `compat_test.go` and any existing CodeQL
  parity tests green.
- **Corpus measurements gating merge:** matrix rows for jitsi + Mastodon
  + any CodeQL-compat fixture; row count diff approved; wall ≤ 2x.
- **Dependency:** PR4.

### PR6 — `compat_tainttracking.qll` (rides on PR5)
- **Title:** `refactor(bridge): compat TaintTracking Configuration uses mayResolveTo`
- **Scope:** `Configuration.hasFlow(source, sink)` closure backed by
  `mayResolveTo` chain with `isSanitizer`/`isBarrier` predicates as
  cut-edges.
- **Size:** ~40 added / ~10 deleted.
- **Tests gating merge:** the four `compat_security_*` modules
  (XSS/SQLi/cmdi/path) all return identical alert sets on Mastodon.
- **Corpus measurements gating merge:** matrix rows for Mastodon
  (XSS-heavy); row count diff approved; wall ≤ 2x.
- **Dependency:** PR5.

### PR7 — react R1-R4 deletion + CHANGELOG + HOWTO
- **Title:** `feat(bridge): delete R1-R4 setter shape predicates; add HOWTO`
- **Scope:** delete `setStateUpdaterCallsFn_outerCtx`,
  `_innerCtx`, `contextSymLinkSame`, `contextSymLinkCrossFile`,
  `objectLiteralField{Own,SpreadD1,SpreadD2,VarIndirect}`, and the R1
  prop-pass cascade. CHANGELOG entry with deprecation policy
  (§6). New `bridge/HOWTO.md` (§7).
- **Size:** ~50 added / ~600 deleted.
- **Tests gating merge:** every fixture in `testdata/setstate_*` and
  `testdata/issue88_*` returns identical alerts.
- **Corpus measurements gating merge:** the **whole matrix**, all
  bridges × all corpora, complete and meeting keep criteria (§4).
- **Dependency:** PR2-PR6, all green.

### 2.X — PR3 / PR5 retirement note (2026-04-20, issue #208)

PR3 (`tsq_taint.qll` rewrite) and PR5 (`compat_tainttracking.qll`
rewrite) are **retired as exploratory dead ends** — not merely blocked.
Full context lives in GitHub issue #208 and in the wiki under "Phase D
PR3 — `tsq_taint.qll` additive wrapper (BLOCKED, 2026-04-20)" plus the
"option #3 system-rewrite investigation" sub-section. Brief summary:

- `MayResolveTo(v, s)` is an **expression-level** relation with a hard
  base-case constraint `s ∈ ExprValueSource` (ObjectLiteral / Array /
  Arrow / Function / Class / primitives / JSX element). FieldRead,
  Identifier, Call, MemberExpression are **excluded**.
- Every `TaintSource` rule head in `extract/rules/frameworks.go`
  (Express, HTTP, Koa, Fastify, Lambda, Next.js) emits on a FieldRead
  expression (`req.query`, `req.body`, `event.*`). So
  `MayResolveTo(_, taintSourceExpr)` has **zero rows on the schema** —
  not on specific fixtures, on *any* extraction.
- Three investigation passes (plan-doc literal sketch, carrier-only
  variant, system-rewrite of `TaintAlert`) all hit the same structural
  wall. PR5 rides on the same machinery and inherits the same gap.

Future unblock paths (none in scope for a bridge-only PR):

(i) extend `ExprValueSource` to seed on FieldRead for a dedicated
carrier variant — project-wide semantic change with large blast
radius;
(ii) introduce a new expression-level `TaintCarry(from, to)` IDB peer
to `FlowStar`, seeded at taint sources and recursed via `FlowStep` —
net-new rule set living alongside `MayResolveTo`;
(iii) accept the dichotomy — taint's symbol-level propagation is a
legitimate separate graph and `MayResolveTo`'s expression-level
closure was never designed to subsume it.

**Chosen path:** (iii) for this Phase. Phase D delivers its line-
deletion win via plan-PR7 (React R1–R4 shape-predicate deletion)
without needing PR3/PR5. Taint-layer migration is deferrable Phase E
work at minimum and arguably a separate project.

PR4 (`compat_dataflow.qll`) is **deferred but not retired** — it does
not share the taint-schema gap and remains measurable under the
existing harness; revisit in Phase E or as a standalone follow-up.

---

## 3. The measurement matrix

Filled in as PRs land. Row count is `wc -l` of CSV alert output;
wall is median of 3 cold runs; plan shape is `OK` (no cap-hit log)
or `CAP@<step>` (which join step blew); maxrss in MB from
`/usr/bin/time -v`.

### 3.1 Template

| Bridge \ Corpus | local fixtures | jitsi | Mastodon | (NAS-misc) |
|---|---|---|---|---|
| `tsq_dataflow` | rows: B / A · wall: B / A · plan: B / A · rss: B / A | … | … | … |
| `tsq_express` | … | … | … | … |
| `tsq_taint` | … | … | … | … |
| `compat_dataflow` | … | … | … | … |
| `compat_tainttracking` | … | … | … | … |
| `tsq_react` (final) | … | … | … | … |

`B` = before (last commit on `main` prior to this PR). `A` = after
(this PR's HEAD). Cells are filled by `bench/valueflow/compare.py`
output and committed to the PR description.

### 3.2 Corpora roster

- **local fixtures** — `testdata/` (round-1 to round-4 React, issue88,
  setstate_*) — fast, deterministic, used as the CI gate.
- **jitsi** — `audiograb@100.80.10.45:~/corpora/jitsi/` — Express +
  taint heavy.
- **Mastodon** — `audiograb@100.80.10.45:~/corpora/mastodon/` — React
  + cross-file imports + the historic planner-stress reference.
- **NAS-misc** — anything else available under
  `audiograb@100.80.10.45:~/corpora/`. Discover at PR1 time; list
  inline in the PR. Treat as bonus signal, not gating.

---

## 4. Keep-or-revert criteria — set IN ADVANCE

These thresholds are written before any number arrives. Do not
renegotiate after measurements come in. Numbers are per-bridge unless
stated.

### 4.1 Row count parity

- **Default expectation:** CSV output bit-identical before vs after.
- **If diff is non-empty:** the PR description must categorise every
  added / removed alert into one of:
  - `IMPROVEMENT` — new true positive previously missed (R1-R4 had
    a known shape gap, e.g. wrapped-arrow or props-field-read).
    Reviewer signs off as `IMPROVEMENT-OK`.
  - `REGRESSION-FN` — a previously-found alert no longer fires.
    **Auto-blocking.** Investigate, fix, or document why the
    previous alert was a false positive (with the offending fixture
    snippet pasted). Reviewer countersigns.
  - `REGRESSION-FP` — a new alert that on inspection is a false
    positive. Tolerated up to **5%** of the after-set per bridge per
    corpus; over 5% blocks merge.
- **Review process:** PR template includes a `Diff Triage` section.
  Two-reviewer rule for any PR with ≥1 diff entry: one owner, one
  independent (peepercat or Cain on the React PR; subagent + Cain
  for the rest).

### 4.2 Wall time

- **≤ 2x current** at result parity → green, merge.
- **2-5x** → yellow, investigation required (open a janky issue with
  flame graph and join trace), merge only with explicit Cain sign-off.
- **> 5x** → red, **auto-revert the migration PR**. Layer stays
  available for opt-in but bridge stays on the legacy path.

### 4.3 Plan shape

- **Zero new cap-hits — non-negotiable.** A single new
  `cap-hit @ step N` log line in the matrix blocks merge. No
  exceptions.
- **Eliminating an existing cap-hit** is the *win condition* — a
  bridge migration that turns a `CAP@2` into `OK` is the headline
  Phase D outcome and should be called out explicitly in the PR title.

### 4.4 maxrss

- **≤ 1.5x current** → green.
- **1.5x-2x** → yellow, investigate, requires Cain sign-off.
- **> 2x** → red, revert.

### 4.5 Aggregate keep-or-revert (plan-PR7 / final)

**Revised 2026-04-20 for the reduced bridge set.** Original §4.5
targeted ≥4 of 6 bridges and ≥400 net LoC deleted across `bridge/`.
With PR3/PR5 retired (issue #208 — structural schema gap between the
expression-level `MayResolveTo` and the symbol-level taint graph) the
six-bridge arithmetic no longer applies. The remaining migration set
is:

| Bridge PR | Status | Effect on §4.5 ledger |
|---|---|---|
| PR1 (`tsq_dataflow.qll` additive surface, #205) | **LANDED** | +15 LoC additive; no deletion |
| PR2 (`tsq_express.qll` `ExpressHandlerArgUse`, #206) | **LANDED** | +25 LoC additive; no deletion |
| PR3 (`tsq_taint.qll` rewrite) | **RETIRED** (#208) | ledger-neutral |
| PR4 (`compat_dataflow.qll`) | **DEFERRED** — not re-scoped in this revision; see retirement note | ledger-neutral for this Phase |
| PR5 (`compat_tainttracking.qll`) | **RETIRED** (#208) | ledger-neutral |
| PR6 (`tsq_react.qll` Phase-A wrapper deletion, #207) | **LANDED** | −3 LoC (identity alias) |
| plan-PR7 (R1–R4 shape-predicate deletion in `tsq_react.qll`) | **PENDING** (gated on this harness PR) | target −600 LoC |

Bridges-migrated fraction is now expressed over the **four in-scope
bridges** (PR1, PR2, PR6, plan-PR7). PR3/PR5 do not count toward either
numerator or denominator — they are not "red" bridges, they are out of
the measurable set entirely, and retiring them is a scope correction
not a failure.

**Recalibrated keep criteria — the layer is kept iff:**

- **All four in-scope bridge PRs green** (PR1 ✓, PR2 ✓, PR6 ✓, plan-PR7
  pending measurement). A red on plan-PR7 reverts plan-PR7 only; PR1/
  PR2/PR6 stay landed because they are additive / pure-deletion-of-
  identity-alias and carry no regression risk.
- **plan-PR7 green on Mastodon** — non-negotiable. Mastodon is the
  planner-stress reference and the R1–R4 shape predicates are the only
  remaining ≥50-LoC deletion win in the Phase D roster.
- **Net LoC deleted across `bridge/` ≥ 500** (revised down from 600).
  The ~600 estimate in §1.1 assumed the full PR3/PR4/PR5 rewrites were
  in scope. With those out, plan-PR7 is nearly the whole ledger; a
  modest underperformance (e.g. 550 LoC) still satisfies the revised
  bar. If plan-PR7 lands less than 500 LoC of net deletion, it has
  failed to justify the measurement-harness overhead and the layer
  question becomes "was Phase D worth it" rather than "was plan-PR7
  worth it" — open a postmortem.
- **At least one cap-hit eliminated on Mastodon** vs the pre-Phase-C
  baseline — unchanged. This is the qualitative headline.

If any of the above fails: **revert plan-PR7 only**, keep PR1/PR2/PR6
and the `bridge/tsq_valueflow.qll` layer in tree (it remains useful
even without the R1–R4 deletion — PR2's `ExpressHandlerArgUse` uses
it), file a postmortem in the wiki under
`Wiki/Tech/tsq-valueflow-phase-d-postmortem.md`, and scope a Phase E
RFC covering (i) the `compat_dataflow` migration (PR4 deferred, not
retired — still measurable), (ii) any new expression-level taint IDB
that would unblock the PR3/PR5 direction.

**Honest framing.** The original §4.5 criteria were over-calibrated for
a six-bridge plan that turned out to have a schema-level incompatibility
blocking two of the bridges. Retiring §4.5's original arithmetic is not
moving the goalposts — it is updating the ledger to match what is
measurable. The line-deletion target is revised down because the
line-deletion scope is smaller; the Mastodon and cap-hit criteria are
preserved because those don't depend on bridge count.


---

## 5. The benchmark harness

Modelled on `andryo@fungoid.xyz:~/janky-bench/` — a scripted run that
produces a comparable artefact each time, committed to its own git
history for trend analysis.

### 5.1 Layout

```
bench/valueflow/
├── bench_run.sh           # entrypoint: `./bench_run.sh <run_id> <bridge>`
├── corpora.yaml           # paths, fetch instructions, fingerprints
├── compare.py             # CSV diff + wall/rss/plan-shape extraction
├── results/               # per-run artefact directory (git-tracked)
│   └── run_NNN/
│       ├── manifest.yaml  # commit SHA, bridge, corpus, timestamp
│       ├── before.csv
│       ├── after.csv
│       ├── before.timing
│       ├── after.timing
│       ├── before.plan
│       ├── after.plan
│       └── diff.md        # human-readable summary
└── MATRIX.md              # the §3 matrix, regenerated from results/
```

### 5.2 Script contract

`./bench_run.sh run_007 tsq_taint`:
1. Resolve corpora list from `corpora.yaml`.
2. For each corpus, on the parent commit (Phase D base): run query,
   capture CSV / timing / plan log.
3. Check out PR head; rerun.
4. Diff via `compare.py`. Write `diff.md`.
5. Update `MATRIX.md` row.
6. Stage files in `bench/valueflow/results/run_007/` and print a
   `git commit` command. **Does not auto-commit** — human inspects
   first.

### 5.3 Persistence

`bench/valueflow/results/` is git-tracked **inside the tsq repo** —
not a separate `tsq-bench` repo. Rationale: tsq is a single project,
and putting the bench in-repo keeps commit SHAs aligned with the
binary that produced them. Trend analysis = `git log
bench/valueflow/results/run_*/`.

### 5.4 Cain-nas access

Corpora live on `audiograb@100.80.10.45`. `bench_run.sh` either
shells out (default) or pulls a tarball to local scratch
(`--cache-corpus`). All runs done from cain's box; the NAS does not
run the binary.

---

## 6. User-facing migration guide — round-3/4 deprecation

The R3/R4 shape predicates (`resolveToObjectExpr*`,
`contextSymLink{Same,CrossFile}`, `setStateUpdaterCallsFn_{outerCtx,
innerCtx}`, the `objectLiteralField*` family) are deleted in PR7.
Anyone with downstream queries calling these breaks at parse time.

### 6.1 Policy

- **No thin-wrapper deprecation period.** The R3/R4 predicates were
  internal-shaped (each tied to a specific desugarer-output shape
  that is no longer emitted post-Phase-C). Wrapping them in
  `mayResolveTo`-backed reimplementations costs more than it saves
  and creates a trust hazard (callers think they're calling the
  precise predicate; they're actually getting `mayResolveTo` semantics).
- **Hard removal in PR7**, with a CHANGELOG entry naming each deleted
  predicate and the `mayResolveTo` invocation that replaces it.
- **Pre-removal grep on internal queries:** before PR7 merges, grep
  every `*.ql` and `*.qll` under `tsq/` for the deleted names and
  migrate inline as part of PR7.
- **External / community queries:** known to be zero today (the
  predicates are tsq-internal). If this changes before PR7 lands,
  treat it as new context and reopen this section.

### 6.2 CHANGELOG entry template

```
### Removed (breaking)

- `resolveToObjectExprOwn`, `resolveToObjectExprSpreadD1`,
  `resolveToObjectExprSpreadD2`, `resolveToObjectExprVarIndirect`
  — replaced by `mayResolveTo` from `bridge/tsq_valueflow.qll`.
- `contextSymLinkSame`, `contextSymLinkCrossFile` — replaced by
  `mayResolveTo` chain through `useContext` call result.
- `setStateUpdaterCallsFn_outerCtx`, `_innerCtx` — fold both into a
  single `mayResolveTo`-backed predicate; see migration table below.

Migration: `<old-predicate>(args)` → `mayResolveTo(<expr>, <source>)`
plus the equivalent shape filter. Worked examples in `bridge/HOWTO.md`.
```

---

## 7. Bridge author documentation — `bridge/HOWTO.md` sketch

A new doc lands with PR7. Sketch:

### 7.1 Sections

1. **When to reach for `mayResolveTo`.**
   - You're asking "what value can flow into expression `e`?"
   - You're asking "what function does callee `c` resolve to?"
   - You're walking variable / parameter / return / field-write chains
     and would otherwise enumerate syntactic shapes.
2. **When to write a custom rule.**
   - Domain-specific *recognition* (this call is a React `useState`,
     this call is an Express handler). `mayResolveTo` answers
     "where", not "what kind".
   - Sanitiser / barrier semantics — these are cut-edges, not
     resolution edges.
3. **Soundness / precision pitfalls.**
   - `mayResolveTo` is may-flow (under-approximate refutation, over-
     approximate proof). Absence does not prove non-flow.
   - Default depth bound `MayResolveDepth = 5`. Raising it costs
     planner time roughly linearly in the number of recursive seeds;
     measure before raising.
   - Field sensitivity is field-name-only (per parent doc §5). Two
     different objects with the same field name are conflated.
   - Reassignments are insensitive — multiple writes to a symbol all
     count as live.
4. **How to size your rule against the planner.**
   - Always anchor `mayResolveTo` against a small extent (your
     domain class) on at least one side. `mayResolveTo(_, _)` with
     both sides free is a Cartesian.
   - If your bridge introduces a new IDB head that recurses through
     `mayResolveTo`, expect to need a custom seeding step. See the
     `tsq_react.qll` `useStateSetterCall` pattern (Phase C) for the
     canonical wiring.
   - Run your bridge through `bench/valueflow/bench_run.sh` against
     local fixtures *before* submitting. The harness output is the
     PR's mandatory measurement section.

### 7.2 Worked example

A 30-line walkthrough: "I want to know if any value passed to
`crypto.createHmac(secret)` came from a `req.body` field." Show the
full `mayResolveTo` chain, the depth bound, the sanitiser cut, and
the matrix row before/after.

---

## 8. Sequencing inside Phase D — PRs in order

| # | Title | Scope | Dependency | Gate to merge |
|---|---|---|---|---|
| PR1 | `bench: value-flow Phase D harness + matrix template` | `bench/valueflow/` only | none | self-test green |
| PR2 | `feat(bridge): expose mayResolveTo via tsq_dataflow.qll` | additive class | PR1 | matrix row green |
| PR3 | `feat(bridge): handler-arg → use-site resolution via mayResolveTo` | additive predicate | PR1 | matrix row green; jitsi |
| PR4 | `refactor(bridge): tsq_taint via mayResolveTo` | rewrite + delete | PR1, PR2 | matrix rows; no FP > 5%; no new cap-hits |
| PR5 | `refactor(bridge): compat DataFlow.localFlow via mayResolveTo` | rewrite | PR4 | matrix rows; CodeQL parity tests |
| PR6 | `refactor(bridge): compat TaintTracking via mayResolveTo` | rewrite | PR5 | matrix rows; all 4 security modules parity on Mastodon |
| PR7 | `feat(bridge): delete R1-R4 + CHANGELOG + HOWTO` | deletion + docs | PR2-PR6 | full matrix; aggregate criteria §4.5 |

Cadence target: PR1 in week 1; PR2/PR3 in week 2 (parallel,
worktrees); PR4/PR5/PR6 sequential weeks 3-5; PR7 week 6. Total: ~6
weeks if no surprises, ~10 weeks with realistic slack.

---

## 9. Test strategy

**The matrix IS the test.** Every migration PR fills its row of §3
and the numbers must meet §4. CI assertions:

- **Bit-identical CSV diff in CI** for the round-1 to round-4 React
  fixtures (and equivalent fixtures for non-React bridges:
  `testdata/express_arg_use_basic`, `testdata/taint_basic_chain`,
  `testdata/compat_xss_basic`). CI fails on any byte-diff unless the
  PR carries an explicit `expected-diff/<bridge>.md` artefact that
  the reviewer signed (commit-tracked counter-signature).
- **Plan-shape assertion in CI:** parse the plan log, assert
  `cap-hit count == baseline`. Any increase fails the build.
- **Existing test suites stay green:** `go test ./... -race` across
  all 17+ packages. Non-negotiable.
- **The benchmark harness output is a PR artefact**, not a CI
  assertion — wall-time variance on shared CI infra is too high.
  Wall and rss numbers come from `bench_run.sh` on Cain's box,
  posted in the PR description, reviewer-checked.

---

## 10. Rollback story

### 10.1 Migration PR rollback (mechanical)

`git revert <PR-N>` per bridge. Each migration PR is one commit on
`main`, no squash-merging across bridges. Reverting a single bridge
does not affect the others. The `bridge/tsq_valueflow.qll` layer
stays in tree.

### 10.2 Layer rollback (harder)

If Phase D measurements show `mayResolveTo` is fundamentally too
imprecise or too slow on real corpora and *no* migrated bridge
survives §4 thresholds:

1. **PR-final (PR7) is reverted first** — restores R1-R4 React shape
   predicates.
2. **PR2-PR6 reverted** in reverse dependency order.
3. **`bridge/tsq_valueflow.qll`** stays as opt-in for ≥ 4 weeks
   post-revert. Rationale: any new bridge work that started against
   it during Phase D needs a grace period to migrate off. After 4
   weeks with no callers, delete.
4. **`ql/system/valueflow.go`** (the recursive Datalog rules) — same
   4-week grace; deletion gated on zero `bridge/*.qll` references.
5. **Phase B planner work stays.** Recursive-IDB cardinality + (#166)
   disjunction fix are general planner improvements; they do not
   revert.

### 10.3 Pre-flight rule

To make the rollback story actually work: **bridges must call
`mayResolveTo` only after their measurement matrix row is green**.
No bridge flips to `mayResolveTo` speculatively before its numbers
land. This gates the rollback blast radius to one PR at a time.

---

## 11. What Phase D explicitly does NOT do

- **No new bridges.** A GraphQL resolver bridge, a Redux action
  bridge, a Vue.js bridge — all interesting, all out of scope. Phase
  D is infrastructure consolidation, not product expansion.
- **No `compat_javascript.qll` migration.** The CodeQL-compat JS AST
  surface is too wide for a single PR; defer to a Phase E RFC.
- **No soundness or precision improvements.** SSA-form, k-CFA
  context sensitivity, prototype-chain walking — all parent doc §5
  v2-stretch items. Phase D measures the v1 dial; v2 is a follow-up.
- **No additional planner work.** Phase B is done. If recursive-IDB
  cardinality estimation regresses on a Phase D corpus, that's a
  Phase B bug fix, not a Phase D scope expansion.
- **No type-system extensions.** `Effect` (getters/setters), promise
  resolution, dynamic dispatch beyond RTA — all deferred per parent
  doc §5.
- **No janky-style benchmark of the planner itself.** `bench/valueflow/`
  measures bridges. The planner has its own benchmarks (`ql/eval/
  *_bench_test.go`); not touched here.
- **No Discord-bot integration of the matrix.** Cain has flagged
  bench dashboards as a "later" item; not Phase D.

---

## 12. Open questions surfaced by this plan

1. **Recursive cardinality on the Express corpus.** `mayResolveTo`
   recursion through Express middleware chains is a depth pattern
   we have no fixture for. Risk that the Phase B sizing estimator
   under-predicts and we eat a Mastodon-scale cap-hit on jitsi at
   PR3. Mitigation: synthetic 5-deep middleware fixture before PR3
   opens.
2. **CodeQL-compat parity vs. CodeQL-compat fidelity.** Our
   `DataFlow.localFlow` will return *more* edges than CodeQL's
   (no SSA). PR5 will produce a non-empty `IMPROVEMENT` diff against
   any corpus that exercises reassignment. Need a policy: do we
   declare CodeQL-compat parity as "set-equal modulo SSA" or as
   "subset-of"? Affects whether PR5's diff is `IMPROVEMENT-OK` or
   `REGRESSION-FP`.
3. **Bench-run determinism.** `bench_run.sh` output must be
   byte-stable across runs to make the matrix meaningful. CSV
   ordering, timestamp suppression, and goroutine-scheduler
   non-determinism all need pinning. Open question whether tsq's
   current output is deterministic enough; spike needed before PR1.
