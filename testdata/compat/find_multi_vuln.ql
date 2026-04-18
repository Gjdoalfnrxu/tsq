/**
 * Find all taint alerts from HTTP input sources.
 * Clean-room query against public CodeQL API docs.
 *
 * KNOWN FALSE NEGATIVE: Route 3 cmd-injection (`exec('ls ' + q)` with
 *   `const { exec } = require('child_process')`) does not alert because
 *   the command_injection sink rule in extract/rules/frameworks.go:30-40
 *   matches a local `Function` decl named "exec", not the destructured
 *   ImportBinding shape used here. Pre-existing extractor limitation —
 *   absence reads as "fixed by #113" but is unrelated. Tracked as
 *   issue #129.
 *
 * Route 4 path-traversal (`fs.readFile`) similarly never alerts because
 * there is no TaintSink rule for fs.readFile (separate gap, not yet filed).
 */
import javascript

from TaintAlert alert
where alert.getSrcKind() = "http_input"
select alert.getSinkExpr(), alert.getSinkKind()
