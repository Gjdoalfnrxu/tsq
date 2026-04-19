/**
 * @name useState setter call whose updater calls another useState setter (with path)
 * @description Same as find_setstate_updater_calls_other_setstate.ql, but the
 *              select exposes `path` so `tsq query --format markdown` can
 *              render file:line headers and source snippets. The bridge
 *              predicate itself is unchanged; we just resolve the call site's
 *              file path via the Call → callee Node → File chain.
 * @kind problem
 * @id js/tsq/setstate-updater-calls-other-setstate-md
 */

import tsq::react
import tsq::calls
import tsq::variables
import tsq::functions
import tsq::base

from Call c, int line
where setStateUpdaterCallsOtherSetState(c, line)
select
  c.getCalleeNode().getFile().getPath() as "path",
  line as "line",
  c as "call"
