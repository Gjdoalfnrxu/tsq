/**
 * @name mayResolveTo — dataflow surface, all rows with locations
 * @description Phase D PR1 parity query. Selects every (valueExpr,
 *              sourceExpr) pair returned by the additive
 *              `mayResolveTo` predicate re-exported via
 *              `tsq::dataflow`, projected with file path + line for
 *              both endpoints. Consumed by the Phase D PR1 parity
 *              test that compares this projection against the
 *              pre-existing `mayResolveToRec` projection
 *              (all_mayResolveToRec_located.ql) — the row sets must
 *              be identical because both surfaces wrap the same
 *              system `MayResolveTo` relation.
 * @kind table
 * @id js/tsq/valueflow/all-may-resolve-to-dataflow-located
 */

import tsq::dataflow
import tsq::base
import tsq::expressions
import tsq::variables
import tsq::calls
import tsq::functions
import tsq::symbols

from ASTNode v, ASTNode s
where mayResolveTo(v, s)
select
  v.getFile().getPath() as "valuePath",
  v.getStartLine() as "valueLine",
  s.getFile().getPath() as "sourcePath",
  s.getStartLine() as "sourceLine"
