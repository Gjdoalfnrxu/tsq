# Phase 4 Handover: Planner — Stratification and Join Ordering

## What was implemented

Four files in `ql/plan/`:

- `plan.go` — IR types (`ExecutionPlan`, `Stratum`, `PlannedRule`, `JoinStep`, `PlannedAggregate`, `PlannedQuery`) and the top-level `Plan()` entry point
- `stratify.go` — Tarjan's SCC + stratum assignment
- `join.go` — Greedy join ordering with eligibility rules
- `validate.go` — Safety validation for individual rules

Three test files:
- `stratify_test.go` — 8 stratification tests
- `join_test.go` — 6 join ordering tests
- `validate_test.go` — 5 validation tests

All 20 tests pass (`go test ./ql/plan/...`).

---

## Stratification Algorithm

### Overview

Stratification ensures that negation and aggregation are evaluated safely: a predicate that another rule negates or aggregates over must be fully computed before the negating/aggregating rule fires.

### Step 1: Build the dependency graph

For each rule `Head :- Body`:
- For each positive body literal with predicate Q: add edge `Head → Q` (positive)
- For each negative body literal (`not Q(...)`) : add edge `Head → Q` (negative)
- For each aggregate literal `count(T v | Q(v) | ...)`: add edge `Head → Q` (negative, treated like negation)
- Comparison literals (`Cmp != nil`) produce no predicate edges

Predicates that appear only as rule heads never appear as dependency targets — they are derived. Predicates that appear only in rule bodies (never as rule heads) are base fact relations; they have no rules and implicitly occupy stratum 0.

### Step 2: Tarjan's SCC

Standard lowlink algorithm (~60 lines). Finds all strongly connected components. Tarjan's output is in reverse topological order.

Each SCC is either:
- A single predicate (possibly with a self-loop)
- Multiple predicates in mutual positive recursion

### Step 3: Unstratifiability check

Any negative edge within an SCC means the predicate recursively depends on its own negation — this is undefined semantics. The planner returns an error: `"unstratifiable: predicate X has recursive negation"`.

Self-loops are checked separately: a predicate with a negative self-loop is caught even if the SCC has only one node.

### Step 4: Stratum assignment

After reversing Tarjan's output to get topological order (dependencies first), we assign stratum numbers via iterative propagation:

- For a positive dependency `Head → Dep` (different SCCs): `stratum[Head] >= stratum[Dep]`
- For a negative/aggregate dependency `Head → Dep` (different SCCs): `stratum[Head] > stratum[Dep]` (strictly greater)

The propagation loop runs until no stratum number changes (fixed point). This correctly handles chains.

### Worked example: 5-predicate chain with negation

```
base(x) — fact relation, no rules
A(x) :- base(x).
B(x) :- A(x).
C(x) :- base(x), not B(x).
D(x) :- C(x), not A(x).
E(x) :- D(x), A(x).
```

Dependency graph:
- A → base (positive)
- B → A (positive)
- C → base (positive), C → B (negative)
- D → C (positive), D → A (negative)
- E → D (positive), E → A (positive)

SCCs: {base}, {A}, {B}, {C}, {D}, {E} — all singleton (no mutual recursion).

Topological order (dependencies first): base, A, B, C, D, E.

Stratum assignment:
- base: 0 (fact, no rules)
- A: 0 (positive dep on base)
- B: 0 (positive dep on A)
- C: 1 (negative dep on B, so stratum[C] > stratum[B]=0 → 1)
- D: 2 (negative dep on A stratum=0, positive dep on C stratum=1 → max is 2 from negative A? No: D→A is negative, stratum[D] > stratum[A]=0 → 1; D→C is positive, stratum[D] >= stratum[C]=1 → 1; combined → stratum[D]=2 because D→A negative forces >0 and D→C positive forces >=1, so D=2? Let's trace: initially all 0. Pass 1: D→C positive: 0>=1 fails → D=1. D→A negative: 1>0 ok. Pass 2: D→C positive: 1>=1 ok. Stable. D=1.)

Wait — let me retrace more carefully:
- Pass 1: C→B negative: C=0, B=0, 0>0 fails → C=1. D→C positive: D=0, C=1, 0>=1 fails → D=1. D→A negative: D=1, A=0, 1>0 ok.
- Pass 2: C→B negative: C=1, B=0, 1>0 ok. D→C positive: D=1, C=1, 1>=1 ok. D→A negative: D=1, A=0, 1>0 ok. E→D positive: E=0, D=1, 0>=1 fails → E=1.
- Pass 3: E→D positive: E=1, D=1, ok. E→A positive: E=1, A=0, ok. Stable.

Final strata: {base=fact, A=0, B=0, C=1, D=1, E=1}.

Evaluation order: stratum 0 (A, B rules), stratum 1 (C, D, E rules). base is a fact, always available.

---

## Join Ordering Heuristic

### Overview

For each rule body (a conjunction of literals), the planner determines the order in which to evaluate them. Good ordering minimises intermediate relation sizes.

### Algorithm: Greedy most-bound-first, smallest-size tie-break

At each step:

1. **Eligibility check** — a literal is eligible to be placed if:
   - It is a positive atom: always eligible (scans the relation)
   - It is a negative atom (`not Q(...)`): eligible only when all its variables are already bound
   - It is a comparison (`x < y`): eligible only when both operand variables are bound
   - It is an aggregate: always eligible (self-contained sub-computation)

2. **Score eligible literals** — score = `(-boundCount, size)` where:
   - `boundCount` = number of the literal's variables already bound (more is better)
   - `size` = relation size from `sizeHints`, default 1000 if unknown
   - Lower score = placed earlier (`-boundCount` so more-bound = lower score)

3. **Place the best-scored literal**, mark its variables as bound, add it to `JoinOrder`.

4. Repeat until all literals are placed.

### Worked example

Rule: `P(x, y, z) :- A(x), B(x, y), C(y, z).`  
Size hints: A=10, B=100, C=50.

Step 1: All eligible. Scores: A=(-0, 10), B=(-0, 100), C=(-0, 50). Best: A (smallest size). Place A. Bound: {x}.

Step 2: B eligible (x bound, 1 bound var). C eligible (0 bound vars). Scores: B=(-1, 100), C=(-0, 50). Best: B (more bound vars wins). Place B. Bound: {x, y}.

Step 3: C eligible (y bound, 1 bound var). Scores: C=(-1, 50). Place C.

Result: A → B → C.

### Why negatives and comparisons are deferred

Negation in Datalog requires the negative relation to be fully computed before the check. At the join level, we enforce this by requiring all variables in a negative literal to be bound before it is placed — which means a positive literal binding those variables must appear earlier. The same logic applies to comparisons, which are filter predicates that require both operands to be ground.

---

## What the Evaluator (Phase 5) Needs

### Iterating strata

```go
for _, stratum := range ep.Strata {
    // Run fixpoint for this stratum's rules.
    for _, plannedRule := range stratum.Rules {
        evaluateRule(plannedRule, db)
    }
    // After fixpoint: evaluate aggregates.
    for _, agg := range stratum.Aggregates {
        evaluateAggregate(agg, db)
    }
}
```

Strata are already in evaluation order — no sorting needed.

### Executing JoinSteps

Each `JoinStep` has:
- `Literal` — the literal to evaluate at this step
- `IsFilter` — if true, all variables were already bound when this step was placed; it is a filter, not a scan
- `JoinCols` — currently empty (v1 placeholder); the evaluator uses variable names to correlate across steps

For v1, the evaluator can maintain a binding map `varName → value` and for each step:

```
for each binding-set in current partial results:
    if step.Literal is positive atom:
        scan step.Literal.Atom.Predicate, filter rows matching already-bound vars, extend binding
    if step.Literal is negative atom:
        check that no row in step.Literal.Atom.Predicate matches current bindings
    if step.Literal.Cmp != nil:
        evaluate comparison with current bindings, keep or discard
    if step.Literal.Agg != nil:
        compute aggregate over its Body with current outer bindings, bind ResultVar
```

### PlannedAggregate

```go
type PlannedAggregate struct {
    ResultRelation string        // variable name that holds result (from Agg.ResultVar.Name)
    Agg            datalog.Aggregate
    GroupByVars    []datalog.Var // head variables excluding the result variable
}
```

The evaluator computes `Agg.Func` (count/min/max/sum/avg) over `Agg.Body` literals, grouped by `GroupByVars`, and stores results in the named relation/variable.

### PlannedQuery

After all strata are evaluated:

```go
if ep.Query != nil {
    for each binding in evaluateJoinOrder(ep.Query.JoinOrder, db):
        emit tuple [evaluate term t with binding for t in ep.Query.Select]
}
```

---

## Performance

- **Stratification:** O(V+E) for Tarjan's SCC, plus O(R×L) for stratum propagation where R = number of rules and L = average body size. In practice, the propagation converges in 1-2 passes for typical programs.
- **Join ordering:** O(n²) per rule where n = number of body literals. For typical rules (2-8 literals) this is negligible.
- **Space:** O(V+E) for the dependency graph.

## Known limitations

- `JoinCols` in `JoinStep` is always empty in v1. For index-accelerated evaluation, the evaluator should compute this from variable positions at eval time.
- Forall helper predicates (full double-negation encoding) are not generated by the desugarer — this is a known Phase 3b limitation.
- Disjunction in rule bodies is approximated by the desugarer; the planner handles whatever rules it receives.
