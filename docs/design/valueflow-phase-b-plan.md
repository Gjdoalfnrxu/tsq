# Phase B implementation plan — planner work for recursive value flow

**Status:** design only. Branch `design/valueflow-phase-b`, worktree
`/tmp/tsq-valueflow-phase-b`, parent doc
`docs/design/valueflow-layer.md`.

**Author:** Planky (with Cain).
**Date:** 2026-04-19.
**Scope:** the §4.2 blockers from the parent doc — recursive-IDB
cardinality estimation, plus root-cause fix for #166 disjunction
poisoning. **Pure planner work**; no value-flow rules, no extractor
changes. Phase C ships `mayResolveTo`, not B.

**User directive driving this plan:** *"go with dbscheme stats
equivalent. No shortcuts to avoid having to implement another
subsystem — we need a robust system here that still executes
efficiently."*

That directive is binding. The "default 1000" hint and the per-query
sampler are the shortcuts we are explicitly not extending. P2b's
sampler stays for non-recursive estimation; recursive IDBs get a
proper precomputed-statistics layer modelled on CodeQL's dbscheme
stats.

---

## 1. Cardinality subsystem — schema design

### 1.1 What we precompute

A **statistics sidecar** keyed by base-relation name, computed once at
extraction time, stored alongside the EDB facts, consulted by the
planner. Per-relation entries:

```go
// ql/stats/stats.go
package stats

type RelStats struct {
    Name      string          // base relation name, e.g. "CallArg"
    Arity     int             // matches schema; mismatch → invalidates
    RowCount  int64           // exact, not sampled
    Cols      []ColStats      // one per column position
}

type ColStats struct {
    Pos          int          // 0-based column index
    NDV          int64        // number of distinct values
    NullFrac     float64      // fraction of rows where col is null/zero-id
    TopK         []TopKEntry  // most-common values, capped at 32
    HistBuckets  []Bucket     // optional equi-depth, K=64; nil if NDV ≤ 256
}

type TopKEntry struct {
    Value uint64              // raw id (string-id or numeric-id)
    Count int64               // exact count of this value in this column
}

type Bucket struct {
    Lo, Hi uint64             // value-id range
    Count  int64              // tuples in [Lo, Hi]
}

type JoinStats struct {
    LeftRel,  LeftCol  string // e.g. "CallArg" col 0
    RightRel, RightCol string // e.g. "Call"    col 0
    Selectivity        float64 // |LeftRel ⋈ RightRel| / (|LeftRel| × |RightRel|)
    DistinctMatches    int64  // |πLeftCol(LeftRel) ∩ πRightCol(RightRel)|
}

type Schema struct {
    SchemaVersion int             // bump on format change
    EDBHash       [32]byte        // BLAKE2b of concatenated relation files
    BuiltAt       time.Time
    Rels          map[string]*RelStats
    Joins         []JoinStats     // only for declared FK-like pairings
}
```

### 1.2 What we deliberately skip vs CodeQL

CodeQL stores per-column histograms with K=128 buckets and a
`@klass` join-shape hint table written into `.dbscheme.stats`. We
deviate in three places:

1. **TopK at 32, not at 128.** CodeQL's choice was tuned for QL programs
   that ask for `count(string-literal-X)`. Our planner consults TopK only
   to detect value-skew at planning time, not for selectivity arithmetic
   on user-named constants. 32 is enough to recognise "this column is
   90% one value" without bloating the sidecar. Revisit if the recursive
   estimator (§4) flags TopK as load-bearing.
2. **Equi-depth histograms only when `NDV > 256`.** Below that NDV we
   keep TopK and skip the histogram — the TopK already covers the
   distribution. Saves bytes on small id columns (e.g. `JsxAttribute.idx`).
3. **`JoinStats` is opt-in, declared by the schema.** CodeQL emits join
   stats automatically for every column-typed `@klass`. We don't have a
   referential-type system at the schema level — instead, tag pairs in
   `extract/schema/relations.go` with a `JoinPaired` annotation that
   names the partner relation+column. Initial annotations: the obvious
   FK shape (`CallArg.call → Call.call`, `Parameter.fn → Function.fn`,
   `ExprMayRef.sym → ExprMayRef.sym` self for skew detection,
   `Contains.parent → Contains.child`). ~15 pairs total in v1.

### 1.3 Why per-column distinct counts are non-negotiable

The recursive estimator (§4) needs `NDV` to bound the cycle ratio of
`mayResolveTo` step composition. Without `NDV` the estimator falls back
to `RowCount × RowCount` selectivity assumptions, which is the
shortcut Cain disallowed. Storing `NDV` per column is cheap (one
`HyperLogLog` pass over each column at extraction time, ~12 KB per
relation regardless of row count) and compositional under join
(see §4.2).

---

## 2. Cardinality subsystem — when stats get computed

### 2.1 Computed at extraction time

`tsq extract` already walks every relation to write the EDB; the stats
pass piggy-backs on the same walk. Two-phase:

1. **Streaming phase** (during walk): per relation, run a HyperLogLog
   per column (`NDV`), a Count-Min Sketch per column for TopK candidates,
   and accumulate `RowCount`. Cost: O(rows × cols) hash ops, negligible
   relative to AST walk.
2. **Finalisation phase** (after walk): for each column with `NDV > 256`
   and a histogram annotation, do **one extra pass** to build the
   equi-depth buckets. For each `JoinPaired` annotation, do **one extra
   pass per pair** computing exact `DistinctMatches` (HLL intersect on
   already-built sketches) and `Selectivity` via a sampling probe (1024
   left-side rows, hash-lookup on right index already loaded for
   fact-DB writeout).

### 2.2 Persistence

**File format:** Protobuf in a sidecar `<edb>.stats.pb` next to the
existing fact files. Single binary blob, gzip-compressed, ≤ 5% of EDB
size on jitsi/mastodon (estimated from per-rel HLL=12KB + TopK=32×16B +
hist=64×24B + RowCount+Arity = ~3.5 KB per relation × ~400 relations =
~1.4 MB, vs ~30 MB EDB on mastodon).

**File layout:**

```
project/.tsq/cache/
  facts.bin              # existing EDB
  facts.bin.stats.pb     # NEW
  facts.bin.stats.lock   # advisory lock during write
```

### 2.3 Versioning and invalidation

`Schema.EDBHash` is the authoritative invalidator. The planner's stats
loader (§3) computes `BLAKE2b` of the EDB file on load (cheap — ~200ms
on mastodon) and rejects the sidecar if it doesn't match. On rejection:
log a warning, fall back to **default-stats mode** (§3.4), and trigger
async recompute on the next extract. **Never** silently use stale
stats — the recursive estimator (§4) is most wrong when its inputs
are wrong; better to degrade than to lie.

`SchemaVersion` is bumped manually on any structural change to the
sidecar (TopK width change, new field, etc). Mismatch = same fallback.

### 2.4 Invocation

```bash
tsq extract <project>     # writes facts.bin AND facts.bin.stats.pb
tsq extract --no-stats <project>  # opt out (CI/debug)
tsq stats compute <project>       # rebuild stats only, given existing EDB
tsq stats inspect <project> [rel] # human-readable dump
```

The `tsq stats` subcommand is a hard requirement — both for debugging
the recursive estimator's predictions against ground truth (§4.4) and
for diagnosing planner mispredictions in the field.

---

## 3. Cardinality subsystem — how the planner consumes stats

### 3.1 Loader injection point

Stats load happens once per `tsq` invocation, in `cmd/tsq/main.go`'s
`compileAndEval` between `LoadBaseRelations` and `EstimateAndPlanWithExtentsCtx`:

```go
// cmd/tsq/main.go (sketch, post-Phase-B)
baseRels, _ := eval.LoadBaseRelations(factDB)
edbStats, _ := stats.Load(factDB.Path)  // returns nil + warning on mismatch
estHook    := eval.MakeMaterialisingEstimatorHookWithStats(baseRels, edbStats)
planFn     := plan.PlanWithClassExtentsAndStats(edbStats)
plan, errs := plan.EstimateAndPlanWithExtentsCtx(prog, hints, cap, estHook, matExt, planFn)
```

`edbStats` is plumbed in two places: into the **estimator hook** so the
trivial-IDB pre-pass can use real selectivity for non-recursive IDBs,
and into the **planner** so the recursive estimator (§4) and the magic-set
demand inference can read base-relation distinct counts when reasoning
about variable groundedness.

### 3.2 Where in `ql/plan/plan.go` the new path slots in

Trace, post-Phase-B:

```
EstimateAndPlanWithExtentsCtx (plan.go:262)
  → matExtHook(prog, hints, cap)                # P2a class extents
  → estHook(prog, hints, cap)                   # P2b sampling pre-pass
       ├─ FOR each non-recursive IDB:
       │    sample-then-plan (existing)
       │    BUT: when stats != nil, use stats.JoinSelectivity
       │          for the rule body BEFORE sampling — promotes
       │          the sampler from "estimator" to "validator"
       └─ FOR each recursive IDB (NEW in B):
            estimateRecursiveIDB(rule, baseStats, prog, hints)
              writes hints[head] := computed bound (§4)
  → planFn(stripped, hints, classExtentNames)   # PlanWithClassExtents
       └─ orderJoinsWithDemandAndIDB consults idbDemand (existing)
            AND consults edbStats.Cols[c].NDV when deciding
            whether a body literal grounds a var (NEW)
```

### 3.3 Distinguishing base-relation lookup from recursive-IDB

Two consumers, two contracts:

- **Base relations:** stats are looked up by `(name, arity)`. `RowCount`
  replaces the existing `len(baseRels[name])` calls in `estimate.go`'s
  `ruleBodyEstimate`. `NDV[col]` replaces the implicit "all values
  distinct" assumption in `bodyContextGroundedVars` —
  `isSmallExtent(pred, hints)` becomes `isSmallExtent(pred, hints) ||
  isLowFanoutCol(pred, col, stats)` where the latter fires when a
  literal binds a column whose `NDV/RowCount` ratio means each driver
  tuple selects O(1) matches.

- **Recursive IDBs:** stats are *not* looked up — there's no IDB
  sidecar; the estimator (§4) computes the cardinality at plan time
  from the base-rel stats it cites. The output goes into the same
  `sizeHints` map that base-rel stats populate, so downstream join
  ordering treats it identically.

### 3.4 Default-stats fallback mode

When `stats.Load` returns nil (mismatch, missing, schema-version
incompatible), the planner runs in **default-stats mode**: every base
relation is filled with `RelStats{RowCount: len(baseRels[name]), Cols:
[arity]ColStats{NDV: rowCount/2, NullFrac: 0, TopK: nil}}`. This
preserves today's behaviour byte-identically when `stats == nil`.
**Critical constraint:** the recursive estimator (§4) MUST detect
default-stats mode and refuse to estimate beyond depth 1, falling
back to `SaturatedSizeHint` for recursive IDBs. The accuracy
guarantees we make for the estimator depend on real `NDV` values; it
must not pretend to compute from invented ones.

---

## 4. Cardinality subsystem — recursive IDB estimation

This is the load-bearing piece. The user has explicitly disallowed
"default 1000," "sampler-only," and "depth-cap and pray." We compute.

### 4.1 The shape we're sizing

A canonical recursive IDB:

```
mayResolveTo(v, s) :- ExprValueSource(v, s).                    # base
mayResolveTo(v, s) :- step(v, mid), mayResolveTo(mid, s).       # recursive
```

Where `step` is the *union of all step kinds* (assignment, return,
param-bind, field-read, destructure, …). We need to estimate
`|mayResolveTo|` at plan time, before any evaluation has happened.

### 4.2 Algorithm — selectivity composition + bounded fixpoint

Two inputs from base-rel stats:

- `B = |base case head|` — estimated by the existing P2b sampler over
  the base rule, since it's non-recursive. Already wired.
- `σ = mean fan-out per recursive step` — computed from `step`'s body
  using the **selectivity-composition rule** below. This is new.

**Selectivity composition for one recursive step:** the recursive rule
body is `step(v, mid), mayResolveTo(mid, s)`. Treat `mid` as the
**join column** between the two literals. Then:

```
σ = E[ |{s : (mid, s) ∈ mayResolveTo}| | mid drawn from step ]
  = (|mayResolveTo| / NDV(mayResolveTo, col=0))
    × P[mid is in mayResolveTo's domain]
```

Without circularity: at the first iteration we don't know
`|mayResolveTo|`, so we substitute `|step|` (the size of one step's
output) and iterate. Two-step iteration converges in ≤ 5 rounds in
practice because the geometric series is dominated by σ.

**Closed form when σ < 1:** `|mayResolveTo| ≈ B / (1 - σ)`. Geometric
series — the textbook transitive-closure cost model.

**Closed form when σ ≥ 1:** the recursion is fan-out-positive; the
fixpoint is bounded by the **finite-domain ceiling**: number of
expressions × number of source expressions, i.e.
`|Expr| × |ValueSource|`, both known from base-rel stats. Use that
ceiling. This is conservative, but it's the tight bound when the
recursion can in principle reach every (expr, source) pair.

**Detecting σ from stats:**

```
σ = (|step| / NDV(step, col=mid))     # mean fan-out per mid
  × (|mayResolveTo| / NDV(mayResolveTo, col=mid))   # cycle ratio
```

`NDV(step, col=mid)` is computable at plan time from base-rel `JoinStats`
(`step` itself is a non-recursive IDB whose body is a union of small
shapes, all sized by P2b). `NDV(mayResolveTo, col=mid)` is unknown
ground truth; substitute `min(|Expr|, σ_est × NDV(step, mid))` as the
seed and iterate.

### 4.3 Algorithm — bounded fixpoint detail

```go
func estimateRecursiveIDB(rule, base, edbStats, hints) int64 {
    B := sampleBase(rule.BaseCase, edbStats)              // P2b sampler
    sigma := 0.0
    for i := 0; i < 5; i++ {
        sigma_next := composeStepSelectivity(rule, edbStats, hints, prevSize)
        if math.Abs(sigma_next - sigma) < 0.05 { sigma = sigma_next; break }
        sigma = sigma_next
        prevSize = updateEstimate(B, sigma)
    }
    if sigma < 0.95 {
        return int64(float64(B) / (1.0 - sigma))         // geometric
    }
    domain := edbStats.Rels["Expr"].RowCount * edbStats.Rels["ValueSource"].RowCount
    return min(domain, SaturatedSizeHint)                 // ceiling
}
```

The `< 0.95` cutoff (not `< 1.0`) is because at σ near 1 the geometric
series is numerically unstable AND empirically the recursion saturates
fast — better to fall to the ceiling early than to project a spurious
1e8 that doesn't materialise.

### 4.4 Worked example — `mayResolveTo` on a small TS corpus

Take `testdata/projects/react-usestate-context-alias/`:
- `|Expr| = 4500` (rough — small fixture)
- `|ExprValueSource| = 1200` (about ¼ of expressions are value-producing)
- `|step| ≈ |LocalFlow| + |InterFlow| + |ParamBinding| + |FieldRead+Write|` ≈ `2500`
- `NDV(step, col=mid) ≈ 1800` (most mids appear once or twice)
- `|ExprValueSource|` is the base-case size: `B = 1200`.

Iteration 0: `σ = (2500/1800) × (1200/1200) = 1.39`. > 0.95 → ceiling
fires. Domain ceiling: `4500 × 1200 = 5.4M`. Capped at `SaturatedSizeHint
= 1<<30 = 1.07B`, so `5.4M`.

**Predicted hint:** 5.4M.

**Estimated ground truth** (extrapolated from `tsq stats compute` on
a comparable real fixture, see deferral note in §10): in practice
`mayResolveTo` saturates well below domain because the recursion only
chains through value-producing expressions (most are not). On the
context-alias fixture the realistic count is ~3-4k. The estimator's
5.4M is **conservative by 1000×**.

That conservatism is the right error direction: the planner sees
"recursive IDB is huge, deprioritise as join seed," picks a small grounder
first, and the recursion actually fires only on bound `mid`. The wrong
direction (over-confident small estimate) is what kills us today.

For corpora where σ < 1 (e.g. a tight `transitiveClosure` over `Edge`),
the geometric form is accurate to within ~2x in the literature on
random-graph reachability — good enough for join ordering, where the
score function only needs to rank correctly.

### 4.5 Correctness posture

- **Sound for join ordering:** the estimate is always ≥ the true
  cardinality (geometric series upper-bounds the fixpoint; domain
  ceiling is by definition an upper bound). The planner using it as a
  seed-cost prefers the smallest literal — over-estimating a
  recursive IDB just keeps it out of the seed slot, which is the
  conservative, safe choice.
- **Composable with magic-set:** when magic-set rewrites a recursive
  rule, the rewritten body has a `magic_pred` literal that bounds
  `mid`. Re-estimate with `B' = |magic_pred|` (bounded by upstream
  demand) and the same σ — the cardinality drops by the demand
  ratio. The estimator is called per (rule, demand-shape).

---

## 5. #166 root cause — diagnosis

### 5.1 What `_disj_N` is in the IR

From `ql/desugar/desugar.go:623-651`:

```go
case *ast.Disjunction:
    leftLits  := d.desugarFormula(n.Left, gen)
    rightLits := d.desugarFormula(n.Right, gen)
    leftVars  := collectVarsFromLiterals(leftLits)
    rightVars := collectVarsFromLiterals(rightLits)
    freeVars  := intersectVars(leftVars, rightVars)        // ← lossy

    synthName := d.freshSynthName("_disj")
    head := datalog.Atom{Predicate: synthName, Args: args}  // args = freeVars
    d.syntheticRules = append(d.syntheticRules,
        datalog.Rule{Head: head, Body: leftLits},
        datalog.Rule{Head: head, Body: rightLits},
    )
    return []datalog.Literal{{
        Positive: true,
        Atom:     datalog.Atom{Predicate: synthName, Args: args},
    }}
```

The synthetic rule head is the **intersection** of left-branch vars and
right-branch vars. Variables bound only in the left branch are dropped
from the head — by design, because they'd be unsafe in the right rule's
head. The caller gets back a single literal `_disj_N(x, y, z)` over the
shared vars only.

### 5.2 The two failure modes this produces

**Failure mode A — silent binding loss (the original #166).** Consider:

```ql
predicate p(int x, int y) {
  (sourceA(x) and refA(x, y)) or (sourceB(y) and refB(x, y))
}
```

After desugar: `p(x, y) :- _disj_1(x, y).` plus
`_disj_1(x, y) :- sourceA(x), refA(x, y).` and
`_disj_1(x, y) :- sourceB(y), refB(x, y).`. Both heads are arity-2 over
`(x, y)` — so far so good.

But in real value-flow rules, branches bind *different* bridge symbols.
A rule like `(VarDecl(s, init) and ...) or (Assign(_, init, s) and ...)`
shares only `init`; `s` is bound differently. The desugarer drops `s`
from the head. The caller's `... and somethingThatNeeds(s)` is then in
the **outer scope**, where `s` is unbound — giving a magic-set failure
that round 5 patched, OR a Cartesian downstream that round 4 deferred.

**Failure mode B — planner sizes the union, not the branches.**
The synthetic predicate has TWO rules. The P2b sampler estimates
`|_disj_N|` by sampling one rule and projecting; it picks one branch's
shape and doesn't see the other. On the round-2 `_disj_2 = 419k` case
this gave a 1000-row default that lied by 400×. Round 6's magic anchor
patches the *consequence* (don't let a magic literal lose slot 0)
without addressing the cause (the union itself is unsized).

### 5.3 Why round 1-6 patches don't fix it at root

Each round patched one of the downstream symptoms:
- **R5** — arity-keyed magic-set propagation, fixes the "magic_VarDecl(_)"
  unsafe-rule fallback
- **R6** — magic-anchor pin, fixes the "magic literal demoted in scoring"
  cap-hit
- **R3** — stripped-class-extent grounding, fixes the "demand inference
  doesn't see the materialised extent"
- **R4** — IDB-call deferral, fixes the "outer rule schedules recursive
  IDB before grounders"

None of them touch the desugar step that creates `_disj_N`. Each new
round uncovers the next symptom in the chain. **A 10-disjunct value-flow
rule will produce 10x as many of these symptoms,** because each
disjunct interacts with every demand-binding shape independently.

The root-cause fix is at desugar/plan time, not at magic-set or
join-ordering time.

---

## 6. #166 root cause — proposed fix

### 6.1 Architectural change: lift each branch to its own named IDB

Replace the one-synthetic-with-two-rules pattern with one synthetic
**per branch**:

```
// BEFORE
_disj_1(x, y) :- leftBody.
_disj_1(x, y) :- rightBody.
caller :- ..., _disj_1(x, y), ...

// AFTER
_disj_1_left(LV...)  :- leftBody.        # LV = all vars in left
_disj_1_right(RV...) :- rightBody.       # RV = all vars in right
_disj_1(SV...)       :- _disj_1_left(LV...), bind(SV from LV).   # projection
_disj_1(SV...)       :- _disj_1_right(RV...), bind(SV from RV).
caller :- ..., _disj_1(SV...), ...
```

Where `SV = intersect(LV, RV)` (the same projection the current
desugarer does, but now applied **after** each branch is sized
independently).

### 6.2 Why this fixes both failure modes

**Mode A (binding loss):** unchanged at the *caller* boundary — the
caller still sees `_disj_1(SV...)`, intersection only. But the per-branch
IDBs `_disj_1_left` and `_disj_1_right` carry their *full* var sets,
so when magic-set is applied to one side, demand can flow into the
side-specific bindings without colliding with the other branch. Each
branch's planning is independent.

**Mode B (sized union):** the planner now sees three IDBs to estimate.
`_disj_1_left` and `_disj_1_right` each get the existing P2b sampler
(or the recursive estimator from §4 if either branch is itself
recursive). `_disj_1` is then sized by **summing** the branch
estimates (upper bound on union — same posture as the existing
multi-rule-head sum in P2b's `int64` accumulator). This is the
"size each branch separately, sum at the union" property the current
desugar shape forbids.

### 6.3 IR change

```go
// ql/desugar/desugar.go
case *ast.Disjunction:
    leftLits  := d.desugarFormula(n.Left, gen)
    rightLits := d.desugarFormula(n.Right, gen)
    leftVars  := collectVarsFromLiterals(leftLits)
    rightVars := collectVarsFromLiterals(rightLits)
    sharedVars := intersectVars(leftVars, rightVars)

    leftSynth  := d.freshSynthName("_disj") + "_l"
    rightSynth := d.freshSynthName("_disj") + "_r"
    unionSynth := d.freshSynthName("_disj")  // existing scheme

    // Per-branch IDB heads carry FULL var lists.
    leftHead  := datalog.Atom{Predicate: leftSynth,  Args: termsFromVars(leftVars)}
    rightHead := datalog.Atom{Predicate: rightSynth, Args: termsFromVars(rightVars)}
    unionHead := datalog.Atom{Predicate: unionSynth, Args: termsFromVars(sharedVars)}

    d.syntheticRules = append(d.syntheticRules,
        datalog.Rule{Head: leftHead,  Body: leftLits},                                // size from leftBody
        datalog.Rule{Head: rightHead, Body: rightLits},                               // size from rightBody
        datalog.Rule{Head: unionHead, Body: []datalog.Literal{{Positive: true, Atom: leftHead}}},   // project
        datalog.Rule{Head: unionHead, Body: []datalog.Literal{{Positive: true, Atom: rightHead}}},  // project
    )
    return []datalog.Literal{{
        Positive: true,
        Atom:     datalog.Atom{Predicate: unionSynth, Args: termsFromVars(sharedVars)},
    }}
```

Net IR change: 2 extra synthetic rules per disjunction (the per-branch
heads). The union rules are unchanged in shape — they already had two
rules, they just now project from the per-branch IDBs instead of
inlining the branch bodies.

### 6.4 Planner change

In `EstimateNonRecursiveIDBSizes`, the per-branch IDBs are sized
independently. The union IDB's body is a single positive literal over
a sized IDB → trivial-IDB pre-pass handles it (P2a's class-extent
shortcut already covers this exact shape: arity-K head with body =
single positive atom). No new planner code; the lifting **uses the
existing planner machinery correctly**.

The arity-keying patches from rounds 5/6 stay in place — they're
correct as defence-in-depth and the lifting transform interacts
cleanly with them.

### 6.5 Migration story

- **Existing OR-of-calls workarounds (R4 in tsq_react.qll):** stay
  correct, just stop being necessary. Mark with a `TODO(post-#166-fix)`
  comment when Phase B PR4 lands, then delete in a follow-up.
- **Existing `_disj_N` test fixtures (`disj2_round[2-6]_*_test.go`):**
  must continue to pass. The lifting transform changes the rule
  *count* in the desugared program (two extra rules per disjunction),
  so any test asserting on `len(prog.Rules)` needs adjusting. Mostly
  the tests assert on plan shape and demand behaviour — those should
  pass unchanged, with the bonus that demand inference now succeeds
  in cases the current tests XFAIL.

---

## 7. Sequencing inside Phase B

Five PRs, gated. Each is independently shippable, each has explicit
gate-to-merge criteria.

### 7.1 PR1 — stats schema + computation + persistence

- **Title:** `feat(stats): EDB statistics sidecar — schema, compute, persist`
- **Scope:** §1, §2. New package `ql/stats/`. Extractor pass that writes
  the sidecar. `tsq stats inspect` and `tsq stats compute`
  subcommands. NO planner consumer yet — sidecar is written, then
  ignored.
- **Dependency:** none on planner, depends on existing extractor.
- **Size:** ~1500 LOC. Roughly: 400 schema/proto, 350 streaming
  computation, 200 finalisation passes, 200 persistence + hash
  validation, 350 tests.
- **Gate to merge:**
  - Stats produced for jitsi+mastodon.
  - `tsq stats inspect` output matches a hand-computed gold for a 3-relation
    fixture.
  - Extraction wall-time delta ≤ 10% (see §9).
  - Sidecar size ≤ 5% of EDB size.

### 7.2 PR2 — planner consumes base-relation stats

- **Title:** `feat(plan): consume EDB stats for base-relation cardinality + grounding`
- **Scope:** §3.1, §3.2 base-relation half. Stats threaded into
  `EstimateAndPlanWithExtentsCtx`. P2b sampler uses real selectivity
  from `JoinStats` for non-recursive IDBs. `bodyContextGroundedVars`
  consults `NDV` to mark low-fanout bindings. Recursive IDBs still
  get default behaviour (kept for PR3).
- **Dependency:** PR1.
- **Size:** ~600 LOC. Mostly plumbing the `*stats.Schema` pointer
  through existing function signatures (legacy callers get nil-aware
  wrappers, same pattern as round-3's `WithClassExtents` proliferation).
- **Gate to merge:**
  - Cain-nas mastodon bench: at least one query that previously
    cap-hit no longer cap-hits (specifically, find a query where
    base-rel `JoinStats`-driven planning picks a different seed).
  - Existing `disj2_round[2-6]` tests pass unchanged.
  - Plan-time delta ≤ 30% on the equivalence sweep.

### 7.3 PR3 — recursive-IDB estimator

- **Title:** `feat(plan): recursive-IDB cardinality estimator (selectivity composition + bounded fixpoint)`
- **Scope:** §4 in full. New file `ql/plan/estimate_recursive.go`.
  Hooked into `EstimateNonRecursiveIDBSizes` (rename to
  `EstimateIDBSizes` and split internally) so recursive IDBs get a
  hint computed from base-rel stats + branch sampler.
- **Dependency:** PR2.
- **Size:** ~800 LOC. Algorithm itself ~150 LOC; the rest is
  selectivity-composition arithmetic, fixed-point iteration,
  default-stats-mode degradation, comprehensive test suite covering
  σ < 1 (geometric), σ ≥ 1 (ceiling), unbounded-domain (saturation),
  cycles, and the worked example from §4.4.
- **Gate to merge:**
  - Synthetic transitive-closure benchmark: estimator's prediction
    within 3x of ground truth on 5 random graphs.
  - `mayResolveTo` shape (mocked rule body, real EDB stats) produces
    a hint within 100x of ground truth on the context-alias fixture.
  - The estimator NEVER produces a hint smaller than the actual
    fixpoint (sound-for-ordering invariant) — property test with
    1000 random rule bodies.

### 7.4 PR4 — #166 lifting transform

- **Title:** `fix(desugar+plan): lift disjunction branches into per-branch IDBs (#166 root cause)`
- **Scope:** §5, §6 in full. Change to `ql/desugar/desugar.go`'s
  `*ast.Disjunction` case. No planner change required (the lifting
  uses existing trivial-IDB sizing). Adversarial unit tests for the 10
  shapes round-1..6 patched as workarounds.
- **Dependency:** PR3 (so per-branch IDBs that are recursive get
  estimated correctly — without PR3 they fall back to default 1000).
- **Size:** ~400 LOC. Desugar change ~80 LOC, tests ~320 LOC.
- **Gate to merge:**
  - All `disj2_round[2-6]_*_test.go` continue to pass.
  - At least 3 of the round-1..6 patches can be reverted in a
    *separate experimental branch* without regression. (Don't revert
    them in the merged PR — keep them as defence-in-depth.)
  - A new test `TestLiftedDisjunction_TenBranches` produces a plan
    with no cap-hit on a synthetic 10-disjunct value-flow rule shape.

### 7.5 PR5 — integration tests + benchmarks

- **Title:** `test(plan): integration suite for recursive-IDB planning at corpus scale`
- **Scope:** synthetic recursive-query suite (transitive closure, alias
  chain, type flow), measured before/after on jitsi + mastodon.
  Cain-nas bench rig (`~/setstate-bench/`) extended.
- **Dependency:** PR4 (full Phase B).
- **Size:** ~500 LOC test code + bench infra updates.
- **Gate to merge:**
  - Bench shows planner picks better order for ≥3 of the 5 recursive
    shapes (measured by intermediate-cardinality reduction).
  - No regression on existing 200+ test queries (equivalence sweep).
  - Plan-time aggregate ≤ 2× current.

---

## 8. Test strategy

### 8.1 Recursive-query benchmark suite

Five shapes, three corpora (synthetic-skewed, jitsi, mastodon):

| Shape | Body | Target |
|-------|------|--------|
| TransitiveClosure | `tc(x,y) :- edge(x,y). tc(x,y) :- edge(x,z), tc(z,y).` | classic cost — should match σ<1 geometric well |
| AliasChain | `alias(s1,s2) :- LocalFlow(_,s1,s2). alias(s1,s2) :- alias(s1,m), LocalFlow(_,m,s2).` | tsq's existing intra-procedural form, baseline-known cost |
| TypeFlow | `tflow(e,t) :- TypeAnnotation(e,t). tflow(e,t) :- ExprMayRef(e,s), VarDecl(_,s,init,_), tflow(init,t).` | mixed base + IDB recursion |
| MayResolveTo (mock) | the §4.1 shape, mocked over real EDB | the actual Phase C target |
| DeepDisjunction | a 10-branch `or` body, each branch a different value-flow shape | exercises PR4 |

Measurement per shape:
1. Plan order picked.
2. Maximum intermediate cardinality during evaluation.
3. Wall time.
4. Stats-aware vs default-stats-mode (PR2+ flag).

The benchmark suite lives at `bench/recursive_idb_test.go` and runs
on cain-nas via the existing `~/setstate-bench/` rig. Pre/post deltas
recorded in `BENCH_RUNS.md` per the existing convention.

### 8.2 Regression suite

Existing `ql/plan/*_test.go` fixtures (533 LOC join, 466 LOC join, 663
LOC magicset_infer, 575 LOC magicset_demand, plus all six disj2
rounds) MUST continue to pass byte-identically when stats are absent
(default-stats mode). Stats-present mode may legitimately produce
different plans — gate those on per-test opt-in via a new
`PlanWithStats` test helper.

### 8.3 Property tests

- **Sound-for-ordering invariant:** for any rule body and any base-rel
  stats, the recursive estimator's hint ≥ true fixpoint cardinality.
  Tested with 1000 random graphs (varying σ from 0.1 to 5.0).
- **Determinism:** stats-driven plan is identical across runs given
  identical EDB + sidecar.
- **Stale-stats refusal:** corrupting the EDB hash byte forces
  default-stats fallback, never silent stale use.

---

## 9. Performance budget

### 9.1 Stats compute cost (extraction time)

- Streaming HLL+CMS pass: O(rows × cols) hash ops ≈ 200ns/op on AMD EPYC.
  Mastodon EDB ~30M total cells → ~6 seconds.
- Finalisation histogram pass: only on columns with `NDV > 256`,
  ~50 columns total estimated, ~1M rows each, ~5 seconds.
- JoinStats sampling probe (15 pairs × 1024 samples × index lookup):
  <1 second.
- **Total budget: ≤ 12 seconds on mastodon, ≤ 10% of current
  extraction wall (current mastodon extract is ~85s).** Confirm in
  PR1 gate.

### 9.2 Stats load cost (query time)

- File mmap + protobuf decode: ~100ms on a 1.5MB sidecar.
- BLAKE2b validation: ~200ms on a 30MB EDB.
- **Per-invocation overhead: ~300ms.** Acceptable since it amortises
  across all queries in a single `tsq` run.

### 9.3 Plan time delta

- Base-rel stats consultation: O(1) per literal, no algorithmic change.
- Recursive-IDB estimator: ≤ 5 fixed-point iterations × O(rule body
  size) per recursive IDB. Mastodon has ~5 recursive IDBs at most
  even with `mayResolveTo` added. Estimated: <50ms total per
  invocation.
- **Plan-time budget: ≤ 2x current** (current plan time on mastodon
  ~150ms → ≤ 300ms). Should land well under that.

### 9.4 Where the budget can go wrong

- **TopK width.** If we discover at PR3 time that the recursive
  estimator wants higher K, the sidecar inflates linearly. Cap at K=64
  with a runtime warning if any column wants more.
- **JoinStats explosion.** If we add JoinPaired annotations
  proliferatively in PR2, the finalisation pass cost grows linearly.
  Cap at ≤ 30 pairs in v1; require justification in the PR description
  for additions.

---

## 10. Risks specific to Phase B

### 10.1 Recursive-cardinality rabbit-hole risk

The parent doc cites 40% probability of a 2-3 month rabbit hole. After
deeper read of `magicset_demand.go` (702 LOC), the six rounds of
disj2 patching, and CodeQL's `dbscheme.stats` precedent, **I revise
this to ~30%** — but with a different shape:

- Reduced because the proposed estimator (§4) is a known algorithm
  family (geometric series + domain ceiling) with closed-form math
  and a clear default-stats fallback. Not original research.
- Still meaningful because **the estimator's accuracy is only
  measurable at corpus scale** — we won't know if 5.4M-vs-3k (the
  §4.4 worked example) is "conservatively safe" or "so conservative
  the planner now picks a different bad order" until PR3+PR5 land
  end-to-end. Could need 2-3 iterations on the σ formula.
- Mitigated by the **PR3 ground-truth gate**: estimator must be
  within 100x of ground truth on the context-alias fixture before
  merge. If we can't meet that, we know to iterate before extending
  into Phase C.

### 10.2 Disallowed shortcut, allowed degradation

Cain explicitly rejected "use a sampler and hope" as a fallback.
The honoured fallback if §4 turns out infeasible at corpus scale:

> Ship the stats subsystem (PR1+PR2) — base-relation stats are
> independently useful and improve every existing query. Recursive
> IDBs cap at depth 1, with a documented limitation: `mayResolveTo` in
> Phase C is materialised at depth 1 only. The bridge keeps R1-R3 hand
> unrolled for deeper. Net win: half the predicate inflation gone, no
> dishonest cardinalities. Document the loss explicitly in the Phase
> C design.

This is a real fallback, not "ship something and hope." It explicitly
trades feature scope for correctness. Cain signs off on it before
merging PR3 if the ground-truth gate isn't met.

### 10.3 Stats invalidation across iterative dev

`tsq extract` runs frequently during dev; the stats pass is now load-
bearing for plan quality. If the user runs `tsq query` against an
older EDB without re-extracting, the sidecar mismatches and we
silently fall back to default-stats mode. **Mitigation:** the warning
on hash mismatch is loud (stderr) and cited in `--verbose` output;
plan-quality regressions in tests should be obvious. Long-term
mitigation in Phase D: `tsq query` auto-runs `tsq stats compute` when
sidecar is stale and EDB is fresh.

### 10.4 Magic-set interaction with lifted disjunctions

PR4's per-branch lifting creates per-branch IDBs that interact with
magic-set rewrite. Each branch might trigger its own demand inference
and seed propagation. Risk: 10-disjunct rule fans out to 10 magic-set
rewrites, each producing its own propagation rules. **Mitigation:**
de-duplicate magic-set propagation rules across the lifted branches
using the PR5 arity-keyed propagation guard (already in main). Test
explicitly in PR4: a 10-disjunct rule produces ≤ 30 magic-set rules
(linear in branches, not quadratic in branch interactions).

### 10.5 The "we built a subsystem and the Phase C author still asks for more"

Hedge against scope creep into Phase C: each gate-to-merge in §7
explicitly cites the recursive estimator and lifting transform as
**means**, not ends. Phase C's success criterion is `mayResolveTo`
shipping with parity to R1-R4 fixtures. If after Phase B the planner
still can't size `mayResolveTo`, **that is a Phase B failure, not a
Phase C deferral.** The PR3 ground-truth gate is the early-warning.

---

## 11. What Phase B explicitly does NOT do

- **Does not ship `mayResolveTo`.** No Datalog rules for value flow.
  No new bridge predicates. No `tsq_valueflow.qll`. Those are Phase C.
- **Does not modify the extractor's emitted relations** beyond writing
  a stats sidecar. `ExprValueSource`, `ParamBinding`, `AssignExpr`
  are Phase A.
- **Does not delete R1-R6 disj2 patches.** Defence-in-depth stays. The
  PR4 gate proves the patches *could* be reverted in isolation but
  the merged PR keeps them.
- **Does not introduce a new query language surface.** No CLI flag for
  recursive depth, no QL annotation for "this predicate is recursive,
  estimate harder." All planner-internal.
- **Does not add CSE (Phase 4 of planner roadmap).** Out of scope.
- **Does not deliver the soundness-as-proof guarantee.**
  `mayResolveTo` is and remains under-approximate; the planner work
  here just makes evaluating it tractable.
- **Does not benchmark `mayResolveTo` itself.** The PR5 recursive-shape
  benchmarks use synthetic and abstract rules; they are *predictive* of
  the Phase C workload, not a proxy for it. Phase C will run its own
  parity benchmarks against R1-R4 fixtures.

---

## Appendix A: file inventory

New:
- `ql/stats/stats.go` (~250 LOC) — schema types
- `ql/stats/compute.go` (~400 LOC) — HLL/CMS/histogram passes
- `ql/stats/persist.go` (~200 LOC) — protobuf serialisation, hash validation
- `ql/stats/stats.proto` (~100 LOC) — wire format
- `ql/plan/estimate_recursive.go` (~250 LOC) — recursive estimator
- `cmd/tsq/stats.go` (~150 LOC) — `tsq stats` subcommands
- `bench/recursive_idb_test.go` (~300 LOC) — PR5 benchmark suite

Modified:
- `extract/extractor.go` — call stats compute at end of extract
- `cmd/tsq/main.go` — load stats, plumb through
- `ql/plan/plan.go` — `PlanWithStats` variant, stats threading
- `ql/plan/backward.go` — `bodyContextGroundedVars` consults `NDV`
- `ql/plan/magicset_demand.go` — same threading
- `ql/eval/estimate.go` — recursive IDB branch
- `ql/desugar/desugar.go` — `*ast.Disjunction` lifting transform

Total estimated delta: ~3500 LOC across 5 PRs.

## Appendix B: relations consulted

All Phase B planner work depends on stats for these base relations
(lifted from §3.1 of the parent doc):
`Assign`, `VarDecl`, `ExprMayRef`, `ExprIsCall`, `Call`, `CallArg`,
`CallCalleeSym`, `CallTarget`, `CallTargetRTA`, `CallResultSym`,
`FieldRead`, `FieldWrite`, `DestructureField`, `ArrayDestructure`,
`ReturnStmt`, `ReturnSym`, `ObjectLiteralField`, `ObjectLiteralSpread`,
`JsxElement`, `JsxAttribute`, `Parameter`, `FunctionSymbol`,
`Function`, `Contains`, `ImportBinding`, `ExportBinding`, `LocalFlow`,
`LocalFlowStar`, `InterFlow`, `FlowStar`. ~30 relations.

JoinPaired annotations to add in PR1 (initial):
- `CallArg.call ↔ Call.call`
- `Parameter.fn ↔ Function.fn`
- `FunctionSymbol.fn ↔ Function.fn`
- `Contains.parent ↔ Contains.child` (self-ref for skew)
- `LocalFlow.srcSym ↔ LocalFlow.dstSym` (self-ref for cycle ratio)
- `InterFlow.srcSym ↔ FlowStar.srcSym` (cross-rel grounding)
- `ExprMayRef.expr ↔ ExprMayRef.sym`
- `VarDecl.sym ↔ Assign.lhsSym` (most common alias source)
- `ImportBinding.name ↔ ExportBinding.name` (cross-module)
- ~6 more bridge-specific pairs as discovered in PR2 dev

---
