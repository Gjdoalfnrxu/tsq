/**
 * @name useState setter call whose updater calls another useState setter
 * @description Finds calls to a `useState` setter where the first argument is
 *              a function literal whose body invokes a DIFFERENT useState
 *              setter. This is a code smell:
 *                setCount(prev => { setName(""); return 0; })
 * @kind problem
 * @id js/tsq/setstate-updater-calls-other-setstate
 */

import tsq::react
import tsq::calls
import tsq::variables
import tsq::functions

from int c, int line
where setStateUpdaterCallsOtherSetState(c, line)
select c as "call", line as "line"
