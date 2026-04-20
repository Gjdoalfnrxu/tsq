/**
 * @name mayResolveTo — dataflow class surface, all rows with locations
 * @description Phase D PR1 parity query (class-surface variant).
 *              Exercises `class MayResolveTo` in `tsq::dataflow` —
 *              its char pred and `getSource()` getter — rather than
 *              the sibling predicate. Projects every (valueExpr,
 *              sourceExpr) pair with file path + line. Consumed by
 *              the Phase D PR1 class-surface parity test that
 *              cross-checks row-set equality against the
 *              predicate-surface parity query. If char pred or
 *              getter regresses, the class-surface row set will
 *              diverge from the predicate-surface row set even
 *              though both wrap the same system relation.
 * @kind table
 * @id js/tsq/valueflow/all-may-resolve-to-dataflow-class-located
 */

import tsq::dataflow
import tsq::base
import tsq::expressions
import tsq::variables
import tsq::calls
import tsq::functions
import tsq::symbols

from MayResolveTo v, ASTNode va, ASTNode s
where va = v and s = v.getSource()
select
  va.getFile().getPath() as "valuePath",
  va.getStartLine() as "valueLine",
  s.getFile().getPath() as "sourcePath",
  s.getStartLine() as "sourceLine"
