/**
 * Find taint alerts from HTTP input, testing sanitizer bypass behavior.
 * Correct sanitizers should block taint; wrong-kind sanitizers should not.
 * Clean-room query against public CodeQL API docs.
 */
import javascript

from TaintAlert alert
where alert.getSrcKind() = "http_input"
select alert.getSinkExpr(), alert.getSinkKind()
