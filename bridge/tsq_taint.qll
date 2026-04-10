/**
 * Bridge library for taint analysis base relations (v2 Phase D placeholder).
 * These relations are empty until Phase D populates them with taint source
 * and sink definitions.
 */

/**
 * A taint sink expression. Holds when `sinkExpr` is a sink of the given `sinkKind`.
 * Empty until Phase D.
 */
class TaintSink extends @taint_sink {
    TaintSink() { TaintSink(this, _) }

    /** Gets the sink expression. */
    int getExpr() { result = this }

    /** Gets the sink kind. */
    string getSinkKind() { TaintSink(this, result) }

    /** Gets a textual representation. */
    string toString() { result = "TaintSink" }
}

/**
 * A taint source expression. Holds when `srcExpr` is a source of the given `sourceKind`.
 * Empty until Phase D.
 */
class TaintSource extends @taint_source {
    TaintSource() { TaintSource(this, _) }

    /** Gets the source expression. */
    int getExpr() { result = this }

    /** Gets the source kind. */
    string getSourceKind() { TaintSource(this, result) }

    /** Gets a textual representation. */
    string toString() { result = "TaintSource" }
}
