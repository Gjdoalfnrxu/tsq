/**
 * @name useState setter call with function-literal updater that calls a function
 * @description Finds calls to a `useState` setter where the first argument is
 *              a function literal whose body invokes another function. This is
 *              the "updater pattern":  setX(prev => helper(prev)).
 * @kind problem
 * @id js/tsq/setstate-updater-calls-fn
 */

// NOTE: We deliberately import ONLY tsq::react. Importing tsq::calls,
// tsq::variables, tsq::functions, etc. would materialise their
// characteristic predicates (e.g. `Call(this) :- Call(this,_,_)`) into
// relations whose names collide with the underlying schema relations
// (`Call/3`, `VarDecl/4`, ...) — a known v1 engine bug
// (arity-shadowing in seminaive eval). The bridge predicates in
// tsq::react use raw atoms only, so this single import is sufficient.
import tsq::react

from int c, int line
where setStateUpdaterCallsFn(c, line)
select c as "call", line as "line"
