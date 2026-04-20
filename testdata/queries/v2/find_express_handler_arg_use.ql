/**
 * @name Express handler arg uses
 * @description Phase D PR2. Projects every
 *              `ExpressHandlerArgUse(useExpr, fn, paramIdx)` row with
 *              the use-site expression's file path + start line. Keyed
 *              for fixture-grounded regression-guard assertions in
 *              `express_handler_arg_use_test.go`.
 * @kind table
 * @id js/tsq/express/handler-arg-use-located
 */

import tsq::express
import tsq::base

from ASTNode use, int fn, int paramIdx
where ExpressHandlerArgUse(use, fn, paramIdx)
select
  use.getFile().getPath() as "usePath",
  use.getStartLine() as "useLine",
  fn as "fn",
  paramIdx as "paramIdx"
