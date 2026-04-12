/**
 * Coverage probe: touches bridge classes not exercised by other compat queries.
 * This query exists solely to expand stdlib coverage and is not expected to
 * return meaningful results from the basic fixture project.
 *
 * Clean-room query written from scratch against public CodeQL API
 * documentation (https://codeql.github.com/docs/). Not derived from
 * CodeQL source code.
 */
import javascript

// Expression / statement classes
from Call c, VarDecl v
where
    c.getCallee() = v.getName()
select c, v.getName()
