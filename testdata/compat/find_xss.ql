/**
 * Find potential cross-site scripting (XSS) vulnerabilities.
 *
 * Clean-room query written from scratch against public CodeQL API
 * documentation (https://codeql.github.com/docs/). Not derived from
 * CodeQL source code.
 *
 * Uses the Xss module from the compat security bridge to identify
 * HTTP input sources paired with XSS sinks that share a taint alert.
 */
import javascript
import semmle.javascript.security.dataflow.XssQuery

from Xss::XssSource source, Xss::XssSink sink, TaintAlert alert
where alert.getSrcKind() = "http_input" and alert.getSinkKind() = "xss"
select sink, "Potential XSS from user input."
