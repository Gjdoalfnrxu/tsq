/**
 * @name useState setter call with function-literal updater that calls a function
 * @description Finds calls to a `useState` setter where the first argument is
 *              a function literal whose body invokes another function. This is
 *              the "updater pattern":  setX(prev => helper(prev)).
 * @kind problem
 * @id js/tsq/setstate-updater-calls-fn
 */

import tsq::react
import tsq::calls
import tsq::variables
import tsq::functions

from int c, int line
where setStateUpdaterCallsFn(c, line)
select c as "call", line as "line"
