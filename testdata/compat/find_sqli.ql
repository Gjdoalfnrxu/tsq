/**
 * Find potential SQL injection vulnerabilities.
 *
 * Clean-room query written from scratch against public CodeQL API
 * documentation (https://codeql.github.com/docs/). Not derived from
 * CodeQL source code.
 *
 * Uses the SqlInjection module from the compat security bridge to
 * identify HTTP input sources paired with SQL query sinks that share
 * a taint alert.
 */
import javascript
import semmle.javascript.security.dataflow.SqlInjectionQuery

from SqlInjection::SqlInjectionSource source, SqlInjection::SqlInjectionSink sink, TaintAlert alert
where alert.getSinkKind() = "sql"
select sink, "Potential SQL injection from user input."
