/**
 * @name useState setter call (with context-alias) whose updater calls another setter (with path)
 * @description Round-2 sibling of find_setstate_updater_calls_other_setstate_through_props.ql.
 *              Recognises setState calls reached through React Context — i.e. setters
 *              passed via `<Ctx.Provider value={{ setX }}>` and read back via
 *              `useContext(Ctx)` (directly or through a wrapping hook) and a
 *              destructure binding. Either side of the updater pair (outer or
 *              inner) may be reached through context aliasing OR JSX-prop aliasing
 *              (round-1).
 *
 *              Limitations: namespace-import createContext (`React.createContext`)
 *              is out of scope; multi-Provider disambiguation is over-approximate
 *              (any Provider for the context is fused with any useContext); hook
 *              indirection is depth-limited to 2.
 * @kind problem
 * @id js/tsq/setstate-updater-calls-other-setstate-through-context
 */

import tsq::react
import tsq::valueflow
import tsq::calls
import tsq::variables
import tsq::functions
import tsq::expressions
import tsq::jsx
import tsq::symbols
import tsq::imports
import tsq::base

from Call c, int line
where setStateUpdaterCallsOtherSetStateThroughContext(c, line)
select
  c.getCalleeNode().getFile().getPath() as "path",
  line as "line",
  c as "call"
