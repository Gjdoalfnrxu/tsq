/**
 * @name mayResolveToObjectField — branch isolation
 * @kind table
 * @id js/tsq/valueflow/branch-object-field
 */
import tsq::valueflow
import tsq::base
import tsq::expressions
import tsq::variables
import tsq::calls
import tsq::functions
import tsq::symbols
from int v, int s where mayResolveToObjectField(v, s) select v, s
