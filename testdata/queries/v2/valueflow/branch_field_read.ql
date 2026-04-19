/**
 * @name mayResolveToFieldRead — branch isolation
 * @kind table
 * @id js/tsq/valueflow/branch-field-read
 */
import tsq::valueflow
import tsq::base
import tsq::expressions
import tsq::variables
import tsq::calls
import tsq::functions
import tsq::symbols
from int v, int s where mayResolveToFieldRead(v, s) select v, s
