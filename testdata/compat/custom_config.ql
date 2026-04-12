/**
 * Find HTTP input sources flowing to any tainted symbol via a custom
 * DataFlow configuration.
 *
 * Clean-room query written from scratch against public CodeQL API
 * documentation (https://codeql.github.com/docs/). Not derived from
 * CodeQL source code.
 *
 * Demonstrates defining a user-level DataFlow::Configuration subclass
 * with custom isSource/isSink predicates, then querying for flows.
 */
import javascript
import DataFlow::PathGraph

class HttpInputFlowConfig extends DataFlow::Configuration {
    HttpInputFlowConfig() { exists(DataFlow::Node n | n = this) }

    override predicate isSource(DataFlow::Node node) {
        exists(int srcExpr |
            TaintSource(srcExpr, "http_input") and
            VarDecl(_, node, srcExpr, _)
        )
    }

    override predicate isSink(DataFlow::Node node) {
        FlowStar(node, node)
    }
}

from HttpInputFlowConfig config, DataFlow::Node source, DataFlow::Node sink
where config.hasFlow(source, sink)
select sink, "HTTP input flows to $@.", source, source.toString()
