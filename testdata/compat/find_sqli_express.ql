/**
 * Find SQL injection in Express applications.
 * Clean-room query against public CodeQL API docs.
 */
import javascript
import semmle.javascript.security.dataflow.SqlInjectionQuery

from SqlInjection::SqlInjectionSink sink, TaintAlert alert
where alert.getSrcKind() = "http_input" and alert.getSinkKind() = "sql"
select sink, "SQL injection from user input."
