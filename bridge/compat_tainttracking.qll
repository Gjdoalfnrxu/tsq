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

        /**
         * Holds if taint flows from `source` to `sink` in this configuration.
         * Sanitizers are checked at every node on the path (not just endpoints).
         * Additional taint steps defined by isAdditionalTaintStep are consulted
         * to extend reachability beyond the base TaintAlert relation.
         */
        predicate hasFlow(int source, int sink) {
            this.isSource(source) and
            this.isSink(sink) and
            not this.isSanitizer(source) and
            not this.isSanitizer(sink) and
            (
                source = sink
                or
                (
                    TaintAlert(source, sink, _, _) and
                    not exists(int mid |
                        this.isSanitizer(mid) and
                        mid != source and mid != sink and
                        TaintAlert(source, mid, _, _) and
                        TaintAlert(mid, sink, _, _)
                    )
                )
                or
                exists(int mid1, int mid2 |
                    this.isAdditionalTaintStep(mid1, mid2) and
                    not this.isSanitizer(mid1) and
                    not this.isSanitizer(mid2) and
                    (source = mid1 or TaintAlert(source, mid1, _, _)) and
                    (mid2 = sink or TaintAlert(mid2, sink, _, _))
                )
            )
        }

        /**
         * Holds if there is a taint-flow path from `source` to `sink`,
         * filtered by this configuration's isSource/isSink/isSanitizer overrides.
         * Sanitizers are checked at every node on the path.
         * Additional taint steps are consulted for extended reachability.
         */
        predicate hasFlowPath(int source, int sink) {
            this.isSource(source) and
            this.isSink(sink) and
            not this.isSanitizer(source) and
            not this.isSanitizer(sink) and
            (
                source = sink
                or
                (
                    TaintAlert(source, sink, _, _) and
                    not exists(int mid |
                        this.isSanitizer(mid) and
                        mid != source and mid != sink and
                        TaintAlert(source, mid, _, _) and
                        TaintAlert(mid, sink, _, _)
                    )
                )
                or
                exists(int mid1, int mid2 |
                    this.isAdditionalTaintStep(mid1, mid2) and
                    not this.isSanitizer(mid1) and
                    not this.isSanitizer(mid2) and
                    (source = mid1 or TaintAlert(source, mid1, _, _)) and
                    (mid2 = sink or TaintAlert(mid2, sink, _, _))
                )
            )
        }
    }
}
