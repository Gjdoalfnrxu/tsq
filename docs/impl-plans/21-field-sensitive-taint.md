# Plan 21 — Field-Sensitive Taint Chaining (Rule 6b)

**Status:** not started
**Phase:** Improves taint precision
**Dependencies:** none

## Problem

Rule 6b in `extract/rules/taint.go` chains taint through `VarDecl` assignments:
when `const q = req.query`, `q` becomes tainted. However, `req.query.id`
produces a `FieldRead` expression, not a `VarDecl`, so
`const id = req.query.id` does not chain taint to `id`.

This forces all fixtures to use `const q = req.query` (whole-object access)
instead of the more realistic `const id = req.query.id` (field access).

## Scope

Extend taint propagation so that:

1. `const x = req.query.id` — FieldRead on a tainted base taints `x`
2. `const { id } = req.query` — destructuring a tainted object taints `id`
3. `obj.field = tainted; use(obj.field)` — field write + read propagation
4. `const { a, b } = req.body` — multiple destructured bindings

## Approach

Add a new taint propagation rule:

```
TaintedSym(dstSym, kind) :-
    FieldRead(expr, baseSym, fieldName),
    VarDecl(_, dstSym, expr, _),
    TaintedSym(baseSym, kind).
```

And for destructuring:

```
TaintedSym(dstSym, kind) :-
    DestructuredBinding(dstSym, baseSym, fieldName),
    TaintedSym(baseSym, kind).
```

## Files to modify

- `extract/rules/taint.go` — add FieldRead/destructuring taint rules
- Update existing fixtures to use more realistic `req.query.id` patterns
  once this works

## Acceptance criteria

- New test fixture with `const id = req.query.id; db.query('...' + id)` produces
  a taint alert
- Existing tests still pass (the `req.query` whole-object pattern should
  continue to work)

## E2E test plan reference

This corresponds to `testdata/compat/projects/field-sensitive/` from the E2E
test plan (Phase 3).
