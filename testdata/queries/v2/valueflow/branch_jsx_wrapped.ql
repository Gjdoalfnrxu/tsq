/**
 * @name mayResolveToJsxWrapped — branch isolation
 * @kind table
 * @id js/tsq/valueflow/branch-jsx-wrapped
 *
 * The JSX-wrapper-tolerant branch added in PR3. Re-runs `mayResolveToCore`
 * on the inner expression of a JsxExpression wrapper. Designed to make the
 * canonical `<Provider value={X} />` shape resolve through the value-flow
 * layer (the JsxAttribute valueExpr column points at the JsxExpression
 * `{X}` node, not at `X` directly).
 */
import tsq::valueflow
import tsq::base
import tsq::expressions
import tsq::variables
import tsq::calls
import tsq::functions
import tsq::symbols
from int v, int s where mayResolveToJsxWrapped(v, s) select v, s
