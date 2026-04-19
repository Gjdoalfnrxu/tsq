/**
 * @name mayResolveToJsxWrapped — sourceExpr is an ObjectLiteral
 * @kind table
 * @id js/tsq/valueflow/branch-jsx-wrapped-object-source
 *
 * Tightened probe for the JSX-wrapper-tolerant branch. Filters the raw
 * mayResolveToJsxWrapped(v, s) result to rows whose `s` is an object-literal
 * expression (per `isObjectLiteralExpr`). The jsx_wrapped.tsx fixture has a
 * single `theme` VarDecl initialised to `{ color: 'red' }`, so this probe
 * MUST return at least one row. A bare `>0 rows` check on the unfiltered
 * branch would tolerate a regression where the wrapper resolves to the wrong
 * sourceExpr kind (e.g. an Identifier) — this filter pins the semantics.
 */
import tsq::valueflow
import tsq::base
import tsq::expressions
import tsq::variables
import tsq::calls
import tsq::functions
import tsq::symbols
import tsq::react
from int v, int s where mayResolveToJsxWrapped(v, s) and isObjectLiteralExpr(s) select v, s
