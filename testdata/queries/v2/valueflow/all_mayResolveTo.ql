/**
 * @name mayResolveTo — all rows
 * @description Selects every (valueExpr, sourceExpr) pair the Phase A
 *              non-recursive `mayResolveTo` predicate emits. Used by the
 *              valueflow-base integration test to assert per-branch
 *              coverage and by the union/sum-of-branches consistency check
 *              for disjunction-poisoning regression detection.
 * @kind table
 * @id js/tsq/valueflow/all-may-resolve-to
 */

import tsq::valueflow
import tsq::base
import tsq::expressions
import tsq::variables
import tsq::calls
import tsq::functions
import tsq::symbols

from int v, int s
where mayResolveTo(v, s)
select v as "valueExpr", s as "sourceExpr"
