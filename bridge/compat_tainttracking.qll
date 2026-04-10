/**
 * CodeQL-compatible TaintTracking framework.
 * Clean-room implementation providing CodeQL API surface
 * backed by tsq's fact relations.
 *
 * This file is NOT derived from CodeQL source code.
 * API surface documented from public CodeQL documentation.
 */

module TaintTracking {
    /**
     * A taint-tracking configuration. Users extend this class and override
     * isSource, isSink, isSanitizer, and isAdditionalTaintStep to define
     * a custom taint-tracking analysis.
     */
    abstract class Configuration extends @symbol {
        /** Holds if `node` is a taint source in this configuration. */
        predicate isSource(int node) { none() }

        /** Holds if `node` is a taint sink in this configuration. */
        predicate isSink(int node) { none() }

        /** Holds if `node` is a sanitizer that blocks taint flow. */
        predicate isSanitizer(int node) { none() }

        /** Holds if there is an additional taint step from `node1` to `node2`. */
        predicate isAdditionalTaintStep(int node1, int node2) { none() }
    }

    /**
     * Holds if taint flows from `source` to `sink` via the taint alert relation.
     * Backed by tsq's TaintAlert(srcExpr, sinkExpr, srcKind, sinkKind) fact.
     */
    predicate hasFlow(int source, int sink) {
        TaintAlert(source, sink, _, _)
    }

    /**
     * Holds if there is a taint-flow path from `source` to `sink`.
     * Equivalent to hasFlow for now; provided for API compatibility
     * with CodeQL path queries.
     */
    predicate hasFlowPath(int source, int sink) {
        TaintAlert(source, sink, _, _)
    }
}
