# Phase 5 Handover: Semi-Naive Evaluator

## What was implemented

Six files in `ql/eval/`:

- `relation.go` — `Value`, `Tuple`, `Relation`, `HashIndex` types
- `builtin.go` — `EvalComparison`, `EvalArithmetic`, `ValueToString`
- `join.go` — `EvalRule`, `EvalRuleDelta`, and the join execution internals
- `aggregate.go` — `EvalAggregate` and `evalLiterals`
- `seminaive.go` — `Evaluate` (semi-naive fixpoint loop), `ResultSet`
- `eval.go` — `Evaluator`, `NewEvaluator`, `loadBaseRelations`

Five test files covering all components, including the transitive closure correctness test.

---

## Semi-Naive Algorithm — Worked Example

**Problem:** Transitive closure of a 5-node chain.

```
Edge(1,2). Edge(2,3). Edge(3,4). Edge(4,5).
Path(x,y) :- Edge(x,y).
Path(x,z) :- Edge(x,y), Path(y,z).
```

**Bootstrap (iteration 0 — EvalRule):**

Both rules are evaluated using all available relations. `Path` doesn't exist yet so the recursive rule produces nothing. After bootstrap:
```
Path = {(1,2),(2,3),(3,4),(4,5)}
ΔPath = {(1,2),(2,3),(3,4),(4,5)}
```

**Iteration 1 (EvalRuleDelta — delta rule for recursive rule):**

We generate one "delta variant" per body literal that has a non-empty delta. The recursive rule has `Edge(x,y)` and `Path(y,z)`. The delta for `Path` is non-empty; Edge has no delta (it's a base fact). So we use the `Path` delta only:

```
For each (x,y) in Edge, look up (y,z) in ΔPath:
  Edge(1,2), ΔPath(2,3) → Path(1,3) [new]
  Edge(2,3), ΔPath(3,4) → Path(2,4) [new]
  Edge(3,4), ΔPath(4,5) → Path(3,5) [new]
  Edge(4,5), ΔPath: no match
```

New: `ΔPath = {(1,3),(2,4),(3,5)}`

**Iteration 2:**
```
Edge(1,2), ΔPath(2,4) → Path(1,4) [new]
Edge(2,3), ΔPath(3,5) → Path(2,5) [new]
Edge(3,4), ΔPath(4,...): no match
Edge(4,5), ΔPath: no match
```

New: `ΔPath = {(1,4),(2,5)}`

**Iteration 3:**
```
Edge(1,2), ΔPath(2,5) → Path(1,5) [new]
Others: no new tuples.
```

New: `ΔPath = {(1,5)}`

**Iteration 4:**
```
Edge(1,2), ΔPath(1,5): 5 ≠ 2, no join match
Others: no new tuples.
```

`ΔPath = {}`. Fixpoint reached. Final `Path` has 10 tuples — all (i,j) reachable pairs.

**Why semi-naive is correct:** At each iteration, at least one body literal uses the delta — ensuring we only process newly added tuples. This avoids re-deriving the same tuples via the same derivation path (which naïve evaluation would do), while the full relations for other body literals ensure we don't miss combinations involving older tuples.

---

## Index Strategy

**Type:** Hash index (map from encoded key to list of tuple indices).

**Key encoding:** `partialKey(tuple, cols)` — encodes values of the specified columns using type tags (`i` for IntVal, `s` for StrVal) and `\x00` as a separator. The `\x00` separator is safe because `\x00` is not a valid UTF-8 character in normal strings.

**Index key for Lookup:** `Lookup([]Value)` takes a slice of values (one per indexed column in order). It re-encodes these using sequential indices (0, 1, 2...) to produce the same key format as the builder.

**Lazy construction:** Indexes are built on first access via `Relation.Index(cols)`. After construction, new tuples added via `Relation.Add()` are incrementally inserted into all existing indexes — so an index fetched before `Add` calls remains consistent.

**Multi-column indexes:** The column bitmask is used as the map key in `Relation.indexes`. Columns are identified by bitmask (uint64), so up to 64 columns are supported. For more than 64 columns, the bitmask would alias — a v1 limitation.

**When indexes are used:**
- Positive literals with at least one bound argument use the index for that column set.
- Positive literals with no bound arguments (full scan) iterate all tuples linearly.
- Negative literals (anti-join) use the index to check for existence; if any match is found, the binding is pruned.

---

## Memory Characteristics

- **Tuples:** Stored as `[]Tuple` (slice of `[]Value` slices). No sharing between tuples.
- **Deduplication set:** `map[string]struct{}` keyed by the full tuple encoding. Grows linearly with the number of unique tuples.
- **Indexes:** `map[string][]int` per index. The int slices accumulate tuple indices; on large relations they can be significant (O(n) per index per column set).
- **Delta relations:** Separate `Relation` objects (same structure). At any time the working set is: full relations + current delta relations. After each fixpoint iteration the previous delta is discarded.
- **Strings:** Loaded from the DB as Go strings (no interning at the eval layer). Each `StrVal` holds a reference-counted Go string.

**Estimate:** A relation with n tuples and k columns uses O(n·k) for tuples, O(n) for the dedup set (key length ≈ k·avg_value_size), and O(n·i) for i indexes. For typical Datalog queries over a 100k-node TypeScript project (fact DB ~50MB), the working set is 100-500MB depending on query complexity.

---

## What Phase 6 (Bridge) Needs

Phase 6 is the bridge layer — `.qll` files that map fact schema relations to QL predicates. The evaluator is already bridge-agnostic: it evaluates whatever Datalog rules it receives. The bridge doesn't need anything new from the evaluator.

**However**, Phase 6 needs to:
1. Produce `*datalog.Program` instances that the planner (Phase 4) can stratify and order.
2. Trust that the evaluator will load base relations from the DB by their schema names — these must match the relation names used in the bridge's Datalog rules.

## What Phase 7 (CLI) Needs

Phase 7 is the CLI entry point. It needs:

1. **`eval.NewEvaluator(execPlan, factDB)`** — takes a `*plan.ExecutionPlan` and a `*db.DB`. The `*db.DB` is obtained by reading a fact DB file with `db.ReadDB`.
2. **`evaluator.Evaluate(ctx)`** — returns `(*eval.ResultSet, error)`.
3. **`ResultSet.Columns []string`** — column names for output headers.
4. **`ResultSet.Rows [][]Value`** — rows; each `Value` is either `IntVal` or `StrVal`.
5. **`eval.ValueToString(v)`** — for serialising values to CSV/JSON/SARIF.

The pipeline from CLI perspective:
```
[.ql source] → parse → resolve → desugar → plan → eval.NewEvaluator → Evaluate → ResultSet
[fact DB file] ─────────────────────────────────────────────────────→ (loaded inside Evaluate)
```

Size hints for the planner can be obtained from the DB by calling `factDB.Relation(name).Tuples()` for each relation name before planning.

---

## Known Limitations (v1)

- **Aggregate body is evaluated without planner ordering** (`evalLiterals` in aggregate.go does a naive left-to-right scan, not planner-ordered). This is correct but potentially slow for complex aggregate bodies. Phase 6/7 can fix by pre-planning aggregate bodies.
- **EvalRuleDelta generates one variant per body literal** — the full semi-naive expansion. For rules with many body literals, this is O(n) variants per rule per iteration. In practice, most rules have 2-5 body literals; this is negligible.
- **Arithmetic evaluation in rules** is not yet wired (EvalArithmetic exists but is not called from the join engine — arithmetic terms in atoms are not supported). The comparison engine (EvalComparison) is fully wired.
- **Column bitmask for indexes** uses uint64 — relations with >64 columns would alias. All current fact relations have ≤7 columns.
- **String separator** (`\x00`) is assumed safe. If a StrVal contains `\x00`, key collisions could occur. All current fact schema strings come from source code identifiers and paths, which don't contain `\x00`.

PR: https://github.com/Gjdoalfnrxu/tsq/pull/10
