# 02 — Enforce `private` predicate visibility

## Scope

Compat-plan item 1l shipped annotation parsing only. `private` is
parsed and stored on `PredicateDecl.Annotations` and
`MemberDecl.Annotations`, but the resolver does not enforce it — a
`private` predicate in one module is currently callable from any other
module. This PR adds visibility checking at name resolution.

This PR does NOT touch the other annotations (`deprecated`,
`pragma[...]`, `bindingset[...]`, `language[...]`). Those remain
parse-only; `deprecated` is plan 03, the rest are intentionally
no-ops.

## Dependencies

None. Parsing and storage already exist.

## Files to change

- `/tmp/tsq/ql/resolve/resolve.go` — add a `callerModule` tracking
  mechanism to each resolve context; when resolving a predicate call,
  reject `private` predicates declared in a different module.
- `/tmp/tsq/ql/resolve/resolve_test.go` — add positive (same module) and
  negative (cross module) tests.
- `/tmp/tsq/ql/ast/ast.go` — no change expected; read to confirm
  `Annotation` shape.

## Implementation steps

1. Read `ql/resolve/resolve.go` and find where predicate calls are
   resolved (search for `resolveCall` or similar — do not guess).
2. Add a helper `isPrivate(PredicateDecl) bool` that returns true iff
   any `Annotation` has `Name == "private"`.
3. Track the "current module path" through the resolver walk. Each
   `ModuleDecl` push/pop updates a stack. Top-level resolver context is
   the empty module.
4. When resolving a predicate call, if the target is private AND its
   declaring module path differs from the caller's module path, emit
   a resolve error: `"predicate X is private to module Y"`.
5. Apply the same rule to class member calls: `private` members are
   only callable from within the same class declaration (or its
   subclasses — check how CodeQL defines it; default to strict "same
   class" if unsure and note in risks).
6. Add tests:
   - `TestPrivatePredicateSameModule` — call succeeds.
   - `TestPrivatePredicateCrossModule` — resolve error.
   - `TestPrivateMemberSameClass` — call succeeds.
   - `TestPrivateMemberCrossClass` — resolve error.

## Test strategy

- `ql/resolve/resolve_test.go::TestPrivatePredicateSameModule`:
  define a module with a private predicate and a public predicate that
  calls it; assert resolve succeeds.
- `ql/resolve/resolve_test.go::TestPrivatePredicateCrossModule`:
  define two modules; call the private predicate from the second
  module; assert a `ResolveError` with a message containing "private".
- Full `go test ./ql/...` must pass.

## Acceptance criteria

- [ ] Private predicate calls across module boundaries produce a
  resolve error with a clear message.
- [ ] Private predicate calls within the same module succeed.
- [ ] Existing bridge files that use `private` (check `bridge/*.qll`
  with `grep private`) still resolve correctly — if they don't, the
  bridge was implicitly relying on lax visibility and must be fixed in
  the same PR.
- [ ] `go test ./...` green.

## Risks and open questions

- Bridge `.qll` files may already use `private` liberally assuming it
  is a no-op. Grep before implementing: if the bridge breaks,
  coordinate which is the bug.
- CodeQL's exact scoping rule for `private` class members (same class
  only vs. subclasses) needs confirmation from the spec. Default to
  the stricter rule and leave looser semantics to a follow-up.
- Module path comparison must handle nested modules. An inner module
  should be able to call its own `private` helpers — decide
  prefix-match vs exact-match.

## Out of scope

- `deprecated` warnings (plan 03).
- Visibility for `pragma[private]` or other non-standard forms.
- IDE/tooling hints; errors only surface at query compile time.
