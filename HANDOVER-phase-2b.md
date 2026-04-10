# Handover: Phase 2b — QL Name Resolver

## What Was Implemented

`ql/resolve/resolve.go` — full name resolution pass for QL AST modules.
`ql/resolve/resolve_test.go` — 21 table-driven tests covering all specified cases.

---

## Annotation Mechanism (Pointer-Keyed Side Tables)

Resolution results are stored in `Annotations`, which uses two maps keyed by pointer identity:

```go
ExprResolutions map[ast.Expr]*Resolution      // keyed on ast.Expr interface value (pointer)
VarBindings     map[*ast.Variable]VarBinding  // keyed on *ast.Variable pointer
```

Because every AST node is allocated as a distinct pointer during parsing (or test construction), pointer identity uniquely identifies each node. There is no need for node IDs. Two AST nodes that happen to have the same field values are still different entries in the map if they are different allocations.

**How to look up:** after Resolve returns, index into `rm.Annotations.ExprResolutions[someExprPtr]` or `rm.Annotations.VarBindings[someVarPtr]`. The expression pointer must be the same pointer that appeared in the AST — not a copy of the struct.

---

## How the Desugarer (Phase 3b) Should Access Resolution Results

The desugarer receives a `*ResolvedModule` and should:

1. **Class → predicate lowering:** For each `*ast.ClassDecl` in `rm.Env.Classes`, generate a unary Datalog predicate from the class's characteristic predicate. The resolver has already validated supertypes, so the desugarer can trust `cd.SuperTypes` are resolvable.

2. **Method → binary predicate lowering:** For each `*ast.MemberDecl`, emit a predicate with explicit `this` and (if `ReturnType != nil`) `result` arguments. Use `rm.Env.Classes[className]` to traverse the inheritance chain for override union semantics.

3. **Method call sites:** When the desugarer encounters a `*ast.MethodCall` in an expression, look up `rm.Annotations.ExprResolutions[mc]`. The `Resolution.DeclClass` is the defining class and `Resolution.DeclMember` is the member. Use these to emit the correct predicate reference (e.g., `Bar_getY(this, result)` rather than `Foo_getY`).

4. **Variable binding:** When lowering quantified formulas (exists/forall), look up `rm.Annotations.VarBindings[v]` to confirm which declaration a variable refers to, and emit the correct Datalog variable.

5. **`this` and `result`:** These are resolved by the resolver (errors emitted for invalid use). The desugarer can treat `this` as the first argument and `result` as the last argument of every method predicate without further validation.

---

## Partial Resolution Behaviour

Resolve never short-circuits. All passes run to completion regardless of errors:

- **First pass** registers as many classes and predicates as it can (duplicates produce an error but the first declaration is registered).
- **Cycle detection** marks cycles and continues — the second pass still runs on non-cyclic classes.
- **Second pass** resolves all bodies it can reach. An undefined predicate or variable produces an error but does not halt resolution of other predicates or bodies.
- **Import failures** produce an error for the missing import; resolution of the rest of the module continues using whatever was already in the environment.

`rm.Errors` collects all errors. `rm.Env` and `rm.Annotations` are always populated with whatever was successfully resolved. The desugarer should check `len(rm.Errors) > 0` and refuse to proceed if there are errors (partial annotations may be incomplete).

---

## QL Constructs NOT Supported in v1 Resolver

| Construct | Status | Notes |
|---|---|---|
| Module parameters | Not supported | `module Foo<T> { ... }` — parameterised modules are not parsed or resolved |
| `newtype` | Not supported | Required for IPA dataflow; deferred to v2 |
| Abstract classes with multi-dispatch | Partial | Single supertype chain only; multiple supertypes in extends are structurally supported but dispatch semantics are not enforced |
| `pragma[noopt]` / `pragma[noinline]` | Not supported | Parser drops pragmas; resolver ignores them |
| `module` scoping / qualified imports | Partial | Import paths are stored as strings; `DataFlow::Node` style qualified names resolve only the leaf name within a flat env |
| `instanceof` type checks | Parsed, not fully resolved | InstanceOf type is validated (class must exist) but not used for type inference in ExprResolutions |
| `forex` | Parsed as Exists/Forall variant | The quantifier body is resolved correctly; forex semantics (exactly-one) are not enforced at resolution time |
| Chained method calls (`x.getA().getB()`) | Partial | Return type of `getA()` is inferred from ExprResolutions if already resolved; ordering dependency means the inner call must be resolved before the outer. Works in practice for left-to-right resolution. |
| Overload resolution | Not supported | If two members share a name with different arities, the first one found in the member list wins |
| `any()` / `none()` formulas | Supported | Resolved as no-ops (no variables to bind) |
| Aggregate expressions | Supported | `count`/`min`/`max`/`sum`/`avg` — decls are bound in inner scope, guard and body resolved |

---

## File Locations

- `ql/resolve/resolve.go` — resolver implementation
- `ql/resolve/resolve_test.go` — 21 tests
