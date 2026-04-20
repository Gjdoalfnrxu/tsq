/**
 * @name mayResolveToRec — all rows, projected with locations
 * @description Selects every (valueExpr, sourceExpr) pair the Phase C
 *              recursive `mayResolveToRec` predicate emits, projected
 *              with file path + line numbers for both endpoints. The
 *              Phase C PR7 whole-closure integration tests consume
 *              this to make file+line-pinned assertions that are
 *              stable across walker-order changes (unlike raw
 *              node-id-keyed assertions).
 * @kind table
 * @id js/tsq/valueflow/all-may-resolve-to-rec-located
 */

import tsq::valueflow
import tsq::base
import tsq::expressions
import tsq::variables
import tsq::calls
import tsq::functions
import tsq::symbols

from ASTNode v, ASTNode s
where mayResolveToRec(v, s)
select
  v.getFile().getPath() as "valuePath",
  v.getStartLine() as "valueLine",
  s.getFile().getPath() as "sourcePath",
  s.getStartLine() as "sourceLine"
