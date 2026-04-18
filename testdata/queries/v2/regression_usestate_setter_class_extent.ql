/**
 * @name Regression: UseStateSetterCall class extent materialisation
 * @description Selects every UseStateSetterCall instance and reports its
 *              callee symbol. This exercises the class extent's materialisation
 *              path (P2a) for `UseStateSetterCall extends @call` whose
 *              characteristic predicate body references only base relations
 *              (`CallCalleeSym`, `ArrayDestructure`, `Contains`, `VarDecl`,
 *              `ImportBinding`).
 *
 *              The query is the smallest possible probe of the class extent:
 *              if `UseStateSetterCall` is materialised correctly, the result
 *              is exactly the set of useState setter call sites. If the
 *              materialisation pre-pass regresses (e.g. the body fails
 *              `IsClassExtentBody` again, or the arity-shadow guard
 *              over-eagerly skips the rule), the query either returns 0
 *              rows or fans out to a Cartesian product of unrelated calls.
 *
 *              On the `react-usestate/Counter.tsx` fixture the expected
 *              row count is 7 (every `setX(...)` call site, regardless of
 *              whether the args match a deeper updater pattern).
 * @kind problem
 * @id js/tsq/regression-usestate-setter-class-extent
 */

import tsq::react

from UseStateSetterCall sc, int sym
where CallCalleeSym(sc, sym)
select sc as "call", sym as "setterSym"
