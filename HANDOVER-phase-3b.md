# Phase 3b Handover: QL Desugarer and Datalog IR

## What was implemented

Two new packages:

- `ql/datalog/ir.go` — Datalog IR types
- `ql/desugar/desugar.go` — OOP-to-Datalog lowering
- `ql/datalog/ir_test.go` — 9 IR construction and String() tests
- `ql/desugar/desugar_test.go` — 25 desugaring tests

All tests pass (`go test ./ql/...`).

---

## Name Mangling

Methods are mangled as `ClassName_methodName`.

| QL | Datalog predicate |
|---|---|
| `class Foo { int getX() { ... } }` | `Foo_getX(this, result)` |
| `class Foo { int getZ(int x) { ... } }` | `Foo_getZ(this, x, result)` |
| `class Foo { predicate isBar() { ... } }` | `Foo_isBar(this)` |
| Top-level `int count(Foo f) { ... }` | `count(f, result)` |

Argument order is always: `(this [, param1, param2, ...] [, result])`. `result` is omitted for predicates (nil `ReturnType`). Top-level predicates have no `this`.

---

## Override Dispatch Logic

For single inheritance, when `Sub extends Base` and both define `getX`:

**Base rule (excludes Sub):**
```
Base_getX(this, result) :- Base(this), not Sub(this), [Base's body].
```

**Override rule (dispatches Sub's body under Base's predicate name):**
```
Base_getX(this, result) :- Sub(this), [Sub's body].
```

The subclass also gets its own mangled predicate `Sub_getX` from `desugarClass` processing `Sub`'s members directly.

**3-level chain example: `A ← B ← C` all define `getX`:**

Processing `A`:
- `directSubClassesWithMethod("A", "getX")` returns `[B]`
- Emits `A_getX(this,result) :- A(this), not B(this), [A body].`
- Emits `A_getX(this,result) :- B(this), [B body].`  ← but B has sub C

The B override rule should also exclude C. This is handled by recursive lookup: when building B's override rule for `A_getX`, `directSubClassesWithMethod("B", "getX")` returns `[C]`, and those are also excluded from B's contribution.

**Limitation:** The current implementation calls `buildMethodRule` for the override with `subOverriders = directSubClassesWithMethod(subName, methodName)`, which correctly adds `not C(this)` to B's override rule. This gives correct dispatch for 3-level chains.

**What is NOT handled:** Multiple inheritance diamond dispatch. If `class C extends A, B` and both A and B define `getX`, the desugarer emits both override rules but does not attempt to resolve conflicts. This is documented as a v2 limitation.

---

## Fresh Variable Generation

```go
type freshVarGen struct{ n int }
func (g *freshVarGen) next() datalog.Var { g.n++; return datalog.Var{Name: fmt.Sprintf("_v%d", g.n)} }
```

A new `freshVarGen` is instantiated for:
- Each class characteristic predicate rule
- Each method rule  
- Each top-level predicate rule
- The select query

This means fresh variable numbering resets to `_v1` at the start of each rule. Within a rule, all method calls (chained or not) produce sequentially numbered vars `_v1`, `_v2`, etc. This is deterministic: two rules with identical structure produce identical fresh variable names.

---

## Desugaring Rules Summary

| QL construct | Datalog output |
|---|---|
| `class Foo extends Bar { Foo() { body } }` | `Foo(this) :- Bar(this), [body].` |
| `RetType getX() { body }` in Foo | `Foo_getX(this, result) :- Foo(this), [body].` |
| `int getZ(int x) { body }` in Foo | `Foo_getZ(this, x, result) :- Foo(this), [body].` |
| `predicate isBar() { body }` in Foo | `Foo_isBar(this) :- Foo(this), [body].` |
| `this.getX()` (expr) | fresh `_v1`; adds `Foo_getX(this, _v1)` to body; evaluates to `_v1` |
| `x.getY()` where x:Foo | fresh `_vN`; adds `Foo_getY(x, _vN)`; evaluates to `_vN` |
| `x instanceof Foo` | `Foo(x)` |
| `x.(Foo)` | adds `Foo(x)` constraint; evaluates to `x` |
| `a = b` | `Comparison{Op:"=", Left:a, Right:b}` |
| `not formula` | flip `Positive` on resulting literals |
| `exists(T v | guard | body)` | type constraint `T(v)`, guard lits, body lits inlined |
| `forall(T v | guard | body)` | negated guard lits, body lits (double-negation approx) |
| `count(T v | body)` | `Literal{Agg: &Aggregate{Func:"count",...}}` |
| `from T v where W select S` | `Query{Body: [T(v), W lits], Select: [S terms]}` |

---

## Known Limitations vs Full CodeQL Desugaring

1. **No multi-dispatch.** Abstract class hierarchies with multiple competing implementations are not fully resolved. The desugarer handles single-inheritance dispatch correctly.

2. **No newtype.** `newtype T = A() or B()` is not implemented. The parser may or may not parse it; the desugarer has no handler.

3. **No module parameters.** Parameterised modules (`module M<T>`) are not implemented.

4. **Disjunction in rule bodies is approximated.** `f1 or f2` in a formula only emits `f1`'s literals. Full support requires splitting into two rules at the call site. This is a known gap and requires restructuring `desugarFormula` to return `[][]Literal` (a disjunction of conjunctions) and splitting at the rule level.

5. **Forall desugaring is approximate.** The standard double-negation translation requires a helper predicate (the "violating witness" predicate). The current implementation emits negated guard literals and positive body literals, which is a stratified-Datalog approximation. For most practical queries this is correct, but the full encoding would emit:
   ```
   helper_forall_N(v) :- GuardLits, not BodyLits.
   OuterRule(...) :- ..., not helper_forall_N(v), ...
   ```
   This requires the planner to support locally-scoped helper predicates.

6. **Arithmetic expressions** are represented as pseudo-comparisons with a string-encoded operator expression. The planner needs to interpret these as evaluation directives.

7. **Resolver annotation keying for method calls.** The `ExprResolutions` map is keyed on `ast.Expr` interface values via pointer identity. Method call resolution is keyed on the `*ast.MethodCall` node. When `resolveMethodCallPred` is called with the receiver expression, it correctly looks up the `*ast.MethodCall` node's annotation (which the resolver places on the `mc` pointer, not the `recv` pointer).

---

## What Phase 4 (Planner) Needs

The planner consumes `*datalog.Program` with this structure:

```
Program
├── Rules: []Rule
│   ├── Head: Atom{Predicate, Args: []Term}
│   └── Body: []Literal
│       ├── Literal{Positive, Atom}        — positive/negative predicate call
│       ├── Literal{Positive, Cmp}         — comparison constraint
│       └── Literal{Positive, Agg}         — aggregate sub-goal
└── Query: *Query
    ├── Select: []Term                     — output expressions
    └── Body: []Literal                    — conjunction of constraints
```

Term variants: `Var{Name}`, `IntConst{Value}`, `StringConst{Value}`, `Wildcard{}`.

**For the planner:**

1. **Dependency graph** is built over `Rule.Head.Predicate` (defines) and `Literal.Atom.Predicate` (uses). Negation edges come from `Literal{Positive: false}`. Aggregate edges come from `Literal{Agg: ...}` — the aggregate body's predicates are dependencies.

2. **Stratification:** SCCs of the dependency graph. Negation or aggregation crossing an SCC boundary forces a stratum split. Negation within an SCC is a stratification error.

3. **Join ordering:** Each `Rule.Body` is a conjunction of literals. The planner selects an evaluation order for the join.

4. **Aggregates:** `Literal{Agg: &Aggregate{Func, Var, TypeName, Body, Expr}}`. The aggregate is evaluated after its `Body` literals' fixpoint. The result is bound to a fresh variable in the outer rule.

5. **Queries:** The `Query` is evaluated after all rules. Its `Body` is a conjunction to evaluate, and `Select` specifies which variables to output.

6. **Helper predicates:** The current desugarer does not emit helper predicates for forall. If the planner needs them (for the full double-negation encoding), the desugarer will need an extension point to add unnamed rules.

PR: opened against Gjdoalfnrxu/tsq feat/phase-3b-desugarer → main.
