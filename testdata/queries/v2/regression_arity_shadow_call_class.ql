/**
 * @name Regression: Call class characteristic predicate vs Call/3 base relation
 * @description Imports `tsq::calls` so the bridge `Call` class is brought
 *              into scope. Selecting `Call` instances exercises the class's
 *              characteristic predicate `Call(this) :- Call(this, _, _)`,
 *              which under the old engine wrote 1-arity head tuples into
 *              the same relation as the 3-arity base schema relation
 *              `Call(c, callee, arity)`. The result was that subsequent
 *              joins on `Call(_, x, _)` saw col-1 as effectively
 *              unconstrained and produced cartesian-style overmatch.
 *
 *              With the (name, arity) keying fix in the eval engine,
 *              `Call/1` (the class characteristic) and `Call/3` (the base
 *              schema relation) live in independent slots, so this query
 *              returns the expected number of calls (no overmatch).
 *
 *              Concretely: the simple/Counter.tsx fixture has a known
 *              count of CallExpression nodes — the regression bites if
 *              the result is grossly larger than reality.
 * @kind problem
 * @id js/tsq/regression-arity-shadow-call-class
 */

import tsq::calls

from Call c
select c, c.getArity() as "arity"
