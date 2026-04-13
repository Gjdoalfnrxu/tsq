/**
 * Custom DataFlow::Configuration that finds parameters flowing to
 * themselves (testing the source = sink case).
 *
 * Clean-room query written from scratch against public CodeQL API
 * documentation (https://codeql.github.com/docs/). Not derived from
 * CodeQL source code.
 *
 * Tests Configuration subclass instantiation, override dispatch for
 * isSource/isSink, and basic flow checking. The hasFlow() method
 * is expressed inline to work around a known desugarer limitation
 * with disjunctions in inherited method bodies (tracked as A2-followup).
 */
import javascript
import DataFlow::PathGraph

class ParamFlowConfig extends DataFlow::Configuration {
    ParamFlowConfig() { any() }

    override predicate isSource(DataFlow::Node node) {
        node.isParameter()
    }

    override predicate isSink(DataFlow::Node node) {
        node.isParameter()
    }
}

from ParamFlowConfig config, DataFlow::Node source, DataFlow::Node sink
where config.isSource(source) and config.isSink(sink) and source = sink
select source, source.getName()
