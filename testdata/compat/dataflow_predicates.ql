/**
 * Test DataFlow::Node predicate methods: flowsTo, getASuccessor, getAPredecessor.
 *
 * Clean-room query written from scratch against public CodeQL API
 * documentation (https://codeql.github.com/docs/). Not derived from
 * CodeQL source code.
 *
 * Selects pairs of DataFlow::Node where src has a direct flow successor,
 * restricted to nodes with a non-empty name.
 */
import javascript
import DataFlow::PathGraph

from DataFlow::Node src, DataFlow::Node succ
where succ = src.getASuccessor() and not src.getName() = "" and not succ.getName() = ""
select src, succ
