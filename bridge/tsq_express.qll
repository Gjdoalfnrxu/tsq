/**
 * Bridge library for Express.js framework models (v2 Phase F).
 * Provides QL classes for Express handler detection and HTTP input sources.
 */

/**
 * An Express route handler function. Derived from patterns like
 * `app.get("/path", handler)`, `app.post(...)`, etc.
 */
class ExpressHandler extends @express_handler {
    ExpressHandler() { ExpressHandler(this) }

    /** Gets the handler function ID. */
    int getFnId() { result = this }

    /** Gets a textual representation. */
    string toString() { result = "ExpressHandler" }
}

/**
 * An Express request source — an expression reading from req.query, req.params,
 * or req.body in an Express handler. These are TaintSource facts with kind "http_input".
 */
class ExpressReqSource extends TaintSource {
    ExpressReqSource() { this.getSourceKind() = "http_input" }

    /** Gets a textual representation. */
    override string toString() { result = "ExpressReqSource" }
}
