/**
 * @name useState setter call with function-literal updater that calls a function
 * @description Finds calls to a `useState` setter where the first argument is
 *              a function literal whose body invokes another function. This is
 *              the "updater pattern":  setX(prev => helper(prev)).
 * @kind problem
 * @id js/tsq/setstate-updater-calls-fn
 *
 * v2 (issue #121 Phase A.2): rewritten on top of the `BackwardTracker`
 * Configuration surface. `SetStateUpdaterTracker` (in
 * `bridge/tsq_react.qll`) overrides `step` with structural containment
 * (`functionContainsStar`); the magic-set transform binds the small
 * `isSink` side and propagates the binding backward through `step`.
 * Forward-enumeration of the v1 predicate's `functionContainsStar`
 * literal OOM'd on Mastodon — see issue #130 / issue #121.
 */

import tsq::react
import tsq::dataflow_track
import tsq::calls
import tsq::variables
import tsq::functions

from int c, int line
where
    exists(int argFn, int innerCall, SetStateUpdaterTracker tracker |
        tracker.hasFlowTo(argFn, innerCall) and
        setStateSetterCallForArg(argFn, c, line)
    )
select c as "call", line as "line"
