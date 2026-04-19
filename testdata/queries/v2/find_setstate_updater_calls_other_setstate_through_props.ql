/**
 * @name useState setter call (with prop-alias) whose updater calls another setter (with path)
 * @description Like find_setstate_updater_calls_other_setstate_md.ql, but also
 *              recognises setState calls that have been passed through one or
 *              more JSX prop hops to a child component, e.g.:
 *                <ZoomControl onConfigChange={setZoomConfig} />
 *                function ZoomControl({ onConfigChange }) {
 *                  onConfigChange(prev => { setOtherState(...); return prev; });
 *                }
 *              Either side of the updater pair (outer or inner) may be
 *              reached through prop aliasing. Direct identifier passing only
 *              for v1; wrapped arrows are out of scope.
 * @kind problem
 * @id js/tsq/setstate-updater-calls-other-setstate-through-props
 */

import tsq::react
import tsq::calls
import tsq::variables
import tsq::functions
import tsq::expressions
import tsq::jsx
import tsq::symbols
import tsq::imports
import tsq::base

from Call c, int line
where setStateUpdaterCallsOtherSetStateThroughProps(c, line)
select
  c.getCalleeNode().getFile().getPath() as "path",
  line as "line",
  c as "call"
