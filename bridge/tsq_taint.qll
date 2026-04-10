/**
 * Bridge library for taint analysis relations (v2 Phase D).
 * Provides QL classes for taint sources, sinks, sanitizers, tainted symbols,
 * tainted fields, sanitized edges, and taint alerts.
 */

/**
 * A taint sink expression. Holds when `sinkExpr` is a sink of the given `sinkKind`.
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

/**
 * A sanitizer function. Holds when `fnId` is a sanitizer for the given `kind` of taint.
 */
class Sanitizer extends @sanitizer {
    Sanitizer() { Sanitizer(this, _) }

    /** Gets the function ID. */
    int getFnId() { result = this }

    /** Gets the taint kind this function sanitizes. */
    string getKind() { Sanitizer(this, result) }

    /** Gets a textual representation. */
    string toString() { result = "Sanitizer" }
}

/**
 * A tainted symbol. Holds when `sym` carries taint of the given `kind`.
 * Derived by the taint propagation rules from TaintSource facts and FlowStar edges.
 */
class TaintedSym extends @tainted_sym {
    TaintedSym() { TaintedSym(this, _) }

    /** Gets the tainted symbol. */
    int getSym() { result = this }

    /** Gets the taint kind. */
    string getKind() { TaintedSym(this, result) }

    /** Gets a textual representation. */
    string toString() { result = "TaintedSym" }
}

/**
 * A tainted field on an object. Holds when `baseSym.fieldName` carries taint of `kind`.
 */
class TaintedField extends @tainted_field {
    TaintedField() { TaintedField(this, _, _) }

    /** Gets the base symbol (the object). */
    int getBaseSym() { result = this }

    /** Gets the tainted field name. */
    string getFieldName() { TaintedField(this, result, _) }

    /** Gets the taint kind. */
    string getKind() { TaintedField(this, _, result) }

    /** Gets a textual representation. */
    string toString() { result = "TaintedField" }
}

/**
 * A sanitized edge. Holds when the flow from `srcSym` to `dstSym` is sanitized
 * for the given `kind` of taint because `dstSym` is a parameter of a sanitizer function.
 */
class SanitizedEdge extends @sanitized_edge {
    SanitizedEdge() { SanitizedEdge(this, _, _) }

    /** Gets the source symbol. */
    int getSrcSym() { result = this }

    /** Gets the destination symbol (sanitizer parameter). */
    int getDstSym() { SanitizedEdge(this, result, _) }

    /** Gets the taint kind being sanitized. */
    string getKind() { SanitizedEdge(this, _, result) }

    /** Gets a textual representation. */
    string toString() { result = "SanitizedEdge" }
}

/**
 * A taint alert. Produced when tainted data from a source reaches a sink.
 */
class TaintAlert extends @taint_alert {
    TaintAlert() { TaintAlert(this, _, _, _) }

    /** Gets the source expression. */
    int getSrcExpr() { result = this }

    /** Gets the sink expression. */
    int getSinkExpr() { TaintAlert(this, result, _, _) }

    /** Gets the source taint kind. */
    string getSrcKind() { TaintAlert(this, _, result, _) }

    /** Gets the sink kind. */
    string getSinkKind() { TaintAlert(this, _, _, result) }

    /** Gets a textual representation. */
    string toString() { result = "TaintAlert" }
}
