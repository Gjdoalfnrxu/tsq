/**
 * Find all taint alerts from HTTP input sources.
 * Clean-room query against public CodeQL API docs.
 */
import javascript

from TaintAlert alert
where alert.getSrcKind() = "http_input"
select alert.getSinkExpr(), alert.getSinkKind()
