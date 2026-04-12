/**
 * Find tainted data flowing to eval() calls via a custom DataFlow
 * configuration.
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

class EvalSinkConfig extends DataFlow::Configuration {
    EvalSinkConfig() { exists(DataFlow::Node n | n = this) }

    override predicate isSource(DataFlow::Node node) {
        exists(TaintSource ts | ts = node)
    }

    override predicate isSink(DataFlow::Node node) {
        exists(Symbol s | s = node and s.getName() = "eval")
    }
}

from EvalSinkConfig config, DataFlow::Node source, DataFlow::Node sink
where config.hasFlow(source, sink)
select sink, "Tainted data flows to eval() from $@.", source, source.toString()
