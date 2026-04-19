/**
 * @name mayResolveToAssign — branch isolation
 * @kind table
 * @id js/tsq/valueflow/branch-assign
 */
import tsq::valueflow
import tsq::base
import tsq::expressions
import tsq::variables
import tsq::calls
import tsq::functions
import tsq::symbols
from int v, int s where mayResolveToAssign(v, s) select v, s
