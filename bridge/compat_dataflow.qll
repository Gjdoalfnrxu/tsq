/**
 * CodeQL-compatible DataFlow framework.
 * Clean-room implementation providing CodeQL API surface
 * backed by tsq's fact relations.
 *
 * This file is NOT derived from CodeQL source code.
 * API surface documented from public CodeQL documentation.
 */

module DataFlow {
    /**
     * A data-flow node. Wraps a symbol ID since tsq's dataflow is symbol-based.
     * A Node represents a program point where data flows.
     */
    class Node extends @symbol {
        Node() { Symbol(this, _, _, _) }

        /** Gets a textual representation of this node. */
        string toString() { Symbol(this, result, _, _) }

        /** Gets the file containing this node's declaration. */
        File getLocation() { Symbol(this, _, _, result) }

        /** Gets the declaration AST node for this symbol. */
        ASTNode asExpr() { Symbol(this, _, result, _) }

        /** Gets the name of the symbol this node represents. */
        string getName() { Symbol(this, result, _, _) }

        /** Gets a successor node via local data flow. */
        Node getASuccessor() { exists(int fnId | LocalFlow(fnId, this, result)) }

        /** Gets a predecessor node via local data flow. */
        Node getAPredecessor() { exists(int fnId | LocalFlow(fnId, result, this)) }

        /** Gets a source node that flows to this node via transitive flow. */
        Node getASourceNode() { FlowStar(result, this) }

        /** Holds if data flows from this node to `other`. */
        predicate flowsTo(Node other) { FlowStar(this, other) }

        /** Holds if data flows from `other` to this node. */
        predicate flowsFrom(Node other) { FlowStar(other, this) }

        /** Holds if this node represents a function parameter. */
        predicate isParameter() { Parameter(_, _, _, _, this, _) }
    }

    /**
     * A single local data-flow step between two nodes.
     * Wraps the LocalFlow fact relation, projecting out the function ID.
     */
    predicate localFlowStep(Node pred, Node succ) {
        exists(int fnId | LocalFlow(fnId, pred, succ))
    }

    /**
     * A data-flow configuration. Users extend this class and override
     * isSource, isSink, isBarrier, and isAdditionalFlowStep to define
     * a custom data-flow analysis.
     */
    abstract class Configuration extends @symbol {
        /** Holds if `node` is a source in this configuration. */
        predicate isSource(Node node) { none() }

        /** Holds if `node` is a sink in this configuration. */
        predicate isSink(Node node) { none() }

        /** Holds if `node` is a barrier (sanitizer) that blocks flow. */
        predicate isBarrier(Node node) { none() }

        /** Holds if there is an additional flow step from `pred` to `succ`. */
        predicate isAdditionalFlowStep(Node pred, Node succ) { none() }

        /**
         * Holds if there is a barrier node on the path from `source` to `sink`,
         * excluding the endpoints themselves.
         */
        predicate barrierOnPath(Node source, Node sink) {
            exists(Node mid |
                this.isBarrier(mid) and
                mid != source and mid != sink and
                exists(int fn1 | LocalFlowStar(fn1, source, mid)) and
                exists(int fn2 | LocalFlowStar(fn2, mid, sink))
            )
        }

        /**
         * Holds if data flows from `source` to `sink` via local flow edges only,
         * with no barrier nodes on the path.
         */
        predicate flowViaLocalFlow(Node source, Node sink) {
            exists(int fnId | LocalFlowStar(fnId, source, sink)) and
            not this.barrierOnPath(source, sink)
        }

        /**
         * Holds if data flows from `source` to `sink` via an additional flow step.
         */
        predicate flowViaAdditionalStep(Node source, Node sink) {
            exists(Node mid1, Node mid2 |
                this.isAdditionalFlowStep(mid1, mid2) and
                not this.isBarrier(mid1) and
                not this.isBarrier(mid2) and
                (source = mid1 or exists(int fn | LocalFlowStar(fn, source, mid1))) and
                (mid2 = sink or exists(int fn | LocalFlowStar(fn, mid2, sink)))
            )
        }

        /**
         * Holds if data flows from `source` to `sink` in this configuration.
         * Barriers are checked at every node on the path (not just endpoints).
         * Additional flow steps defined by isAdditionalFlowStep are consulted
         * to extend reachability beyond LocalFlow edges.
         */
        predicate hasFlow(Node source, Node sink) {
            this.isSource(source) and
            this.isSink(sink) and
            not this.isBarrier(source) and
            not this.isBarrier(sink) and
            (
                source = sink
                or
                this.flowViaLocalFlow(source, sink)
                or
                this.flowViaAdditionalStep(source, sink)
            )
        }

        /**
         * Holds if there is a data-flow path from `source` to `sink`,
         * filtered by this configuration's isSource/isSink/isBarrier overrides.
         * Barriers are checked at every node on the path.
         * Additional flow steps are consulted for extended reachability.
         */
        predicate hasFlowPath(PathNode source, PathNode sink) {
            this.isSource(source) and
            this.isSink(sink) and
            not this.isBarrier(source) and
            not this.isBarrier(sink) and
            (
                source = sink
                or
                this.flowViaLocalFlow(source, sink)
                or
                this.flowViaAdditionalStep(source, sink)
            )
        }
    }

    /**
     * A node on a data-flow path. Wraps a symbol for path queries,
     * providing the same interface as Node with path-query context.
     */
    class PathNode extends @symbol {
        PathNode() { Symbol(this, _, _, _) }

        /** Gets the underlying data-flow node. */
        Node getNode() { result = this }

        /** Gets a textual representation. */
        string toString() { Symbol(this, result, _, _) }

        /** Gets the file containing this node. */
        File getLocation() { Symbol(this, _, _, result) }
    }
}
