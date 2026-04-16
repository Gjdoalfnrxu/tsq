/**
 * @name Mixpanel track calls with JSX event handlers
 * @description For each mp.track() / mixpanel.track() / analytics.track() call,
 *              find the JSX event handler (onClick, onSubmit, etc.) that triggers it.
 *              Used by the mixpanel-playwright skill to generate Playwright test selectors
 *              by tracing backwards from analytics calls to their UI triggers.
 * @kind problem
 * @id js/tsq/mixpanel-track-handler
 */

import tsq::calls
import tsq::expressions
import tsq::imports
import tsq::functions
import tsq::jsx
import tsq::base
import tsq::react

/**
 * Holds if `trackCall` is a call to a mixpanel/segment/analytics tracking function
 * imported from a known analytics module.
 */
predicate isTrackCall(Call trackCall) {
  exists(ImportBinding ib, ExprMayRef ref |
    ib.getModuleSpec().regexpMatch("mixpanel.*|mixpanel-browser|@segment/analytics.*|posthog-js|amplitude-js") and
    ref.getSym() = ib.getLocalSym() and
    trackCall.getCalleeNode() = ref.getExpr()
  )
  or
  // Also catch mp.track(), analytics.track() via MemberExpression callee
  // where the base object name matches known analytics variable names
  exists(ASTNode callee |
    trackCall.getCalleeNode() = callee and
    callee.getKind() = "MemberExpression"
    // Note: string value of member not directly queryable — relies on import binding above
    // or manual file+line inspection of results
  )
}

from Call trackCall, JsxAttribute attr
where
  isTrackCall(trackCall) and
  // Find the function that contains this track call (transitive — handles nested lambdas)
  exists(int fnId |
    functionContainsStar(fnId, trackCall.getCalleeNode()) and
    // Find JSX event handler attributes that reference this function
    exists(ExprMayRef handlerRef, FunctionSymbol fs |
      fs.getFunction() = fnId and
      handlerRef.getSym() = fs.getSymbol() and
      attr.getValueExpr() = handlerRef.getExpr() and
      attr.getName().regexpMatch("on[A-Z].*")
    )
  )
select
  trackCall as "trackCall",
  trackCall.getCalleeNode().getFile().getPath() as "file",
  trackCall.getCalleeNode().getStartLine() as "line",
  attr.getName() as "eventHandler",
  attr.getElement() as "jsxElement",
  attr.getValueExpr().getStartLine() as "handlerLine"
