/**
 * @name resolvesToFunctionDirect — direct exercise
 * @description Exercises the `resolvesToFunctionDirect(callee, fnId)`
 *              derived helper from `tsq_valueflow.qll`. The Phase A bridge
 *              ships this predicate so PR3 can rewrite the easy
 *              `resolveToObjectExpr*` branches in `tsq_react.qll` onto it;
 *              shipping it untested means a slot-swap bug in
 *              `FunctionSymbol(sym, fnId)` would land silently and only
 *              surface in PR3. This query gives the integration test a
 *              direct call site so any regression here lights up
 *              immediately.
 * @kind table
 * @id js/tsq/valueflow/resolves-to-function-direct
 */

import tsq::valueflow
import tsq::base
import tsq::expressions
import tsq::variables
import tsq::calls
import tsq::functions
import tsq::symbols

from int callee, int fnId
where resolvesToFunctionDirect(callee, fnId)
select callee as "callee", fnId as "fnId"
