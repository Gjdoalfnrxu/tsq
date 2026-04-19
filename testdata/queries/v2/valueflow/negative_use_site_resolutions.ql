/**
 * @name mayResolveTo — negative fixture, projected with locations
 * @description Selects every (valueExpr, sourceExpr) pair `mayResolveTo`
 *              emits, projected with file path + line numbers for both
 *              endpoints. The negative-fixture integration test uses this
 *              to make per-fixture pinned assertions: for each known
 *              use-site (file, line) the test confirms `mayResolveTo`
 *              does NOT join through to the unreachable literal line.
 *              See plan §4.1 — this is the per-fixture pinned assertion
 *              that the original aggregate-only ≤60 guard was missing.
 * @kind table
 * @id js/tsq/valueflow/negative-use-site-resolutions
 */

import tsq::valueflow
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
