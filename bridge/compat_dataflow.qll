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

    /**
     * Holds if data flows from `source` to `sink` via local flow edges.
     * Uses the transitive closure (LocalFlowStar) relation.
     */
    predicate hasFlow(Node source, Node sink) {
        exists(int fnId |
            LocalFlowStar(fnId, source, sink)
        )
    }

    /**
     * Holds if there is a data-flow path from `source` to `sink`.
     * Equivalent to hasFlow for now; provided for API compatibility
     * with CodeQL path queries.
     */
    predicate hasFlowPath(PathNode source, PathNode sink) {
        exists(int fnId |
            LocalFlowStar(fnId, source, sink)
        )
    }
}
