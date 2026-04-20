/**
 * Bridge library for Express.js framework models (v2 Phase F).
 * Provides QL classes for Express handler detection and HTTP input sources.
 *
 * Phase D PR2 (additive): `ExpressHandlerArgUse` predicate links a use
 * expression back to the Express route handler whose parameter it
 * resolves to via value-flow. Thin wrapper over the system
 * `MayResolveTo` relation (Phase C PR4) + schema `Parameter` /
 * `ExprMayRef` / `ExpressHandler`. Non-recursive on the QL side —
 * recursion lives in the system Datalog rules.
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

/**
 * Holds when `useExpr` is a use-site expression referencing parameter
 * `paramIdx` of an Express route handler function `fn`, where `fn`
 * was registered via value-flow-resolvable callback passed to an
 * `app.{get,post,put,delete,patch,use}(...)` method call.
 *
 * # How the linkage is established
 *
 * Given an `app.<method>(...)` call, the predicate takes one
 * `CallArg(call, _, handlerArgExpr)` and uses
 * `MayResolveTo(handlerArgExpr, fn)` (the Phase C recursive closure)
 * to follow the callback expression back to the function node that
 * it resolves to at runtime. `fn` is that function node id.
 * `Parameter(fn, paramIdx, _, _, paramSym, _)` picks out the symbol
 * of the `paramIdx`-th parameter, and `ExprMayRef(useExpr, paramSym)`
 * asserts `useExpr` is an Identifier that references it.
 *
 * # Why this is the value-add over `ExpressHandler`
 *
 * The existing `ExpressHandler(fn)` rule (see
 * `extract/rules/frameworks.go` `expressHandlerRule`) fires only when
 * the callback is a named-variable identifier:
 * `ExprMayRef(cbExpr, cbSym) + FunctionSymbol(cbSym, fn)`. It MISSES
 * the inline-arrow case entirely: in `app.get('/x', (req, res) => …)`
 * the callback argument is an `ArrowFunction` node, not an
 * `Identifier`, so `ExprMayRef(cbExpr, _)` is empty and the handler
 * is never registered. This predicate substitutes `MayResolveTo` for
 * the `ExprMayRef + FunctionSymbol` leg, which:
 *   - Identity-base-hits the inline-arrow case (an `ArrowFunction`
 *     is an `ExprValueSource`, so `MayResolveTo(arrow, arrow)` fires
 *     via the base rule in `extract/rules/mayresolveto.go`).
 *   - Handles the named-variable case transitively via `lfsVarInit`.
 *   - Will scale to future wrapper patterns (`app.get('/x', wrap(h))`,
 *     `app.get('/x', handlers.create)`, etc.) as the closure body
 *     grows in subsequent PRs — additive-only wrt this bridge.
 *
 * # Semantics and scope
 *
 *   - `paramIdx` is 0-indexed (0 = req, 1 = res in Express convention).
 *   - Method filter: `get|post|put|delete|patch|use`. No receiver
 *     check — matches any MethodCall with those method names, because
 *     Express apps are often aliased (`app`, `router`, `r`, etc.) and
 *     `ExpressHandler`'s own rule also matches on method name only.
 *   - No CallArg idx pin: for `app.get(path, cb)` the cb is at idx=1;
 *     for `app.use(cb)` it is at idx=0. Any slot whose argNode
 *     value-flow-resolves to a function node with a matching
 *     `paramIdx`-th parameter counts. This is the same idx-wildcard
 *     posture as `expressHandlerRule`.
 *   - Non-recursive QL body: the recursion lives inside `MayResolveTo`.
 *     The planner's Phase B recursive-IDB sizing handles it.
 *
 * # Deviation from Phase D plan (acknowledged)
 *
 * The plan (§2 PR3) sketched the linkage as
 * `mayResolveTo(useExpr, handlerArgExpr)` — a use→arg direction. The
 * current `MayResolveTo(v, s)` has `s ∈ ExprValueSource`, i.e. the
 * resolved side must be a value-source literal (function, object,
 * primitive). A parameter Identifier is NOT a value source, so the
 * literal plan sketch produces zero rows on every fixture. This
 * implementation inverts the flow direction: we go arg→fn via
 * `MayResolveTo(handlerArgExpr, fn)` (which DOES hold — `fn` is an
 * `ArrowFunction`/`FunctionExpression` node, a value source), then
 * attach `ExprMayRef + Parameter` for the param-use side. Same
 * linkage, flow-direction-corrected for the as-shipped closure
 * semantics. Noted in the PR description for adversarial review.
 *
 * # Additive
 *
 * No existing predicate is modified. Callers that don't reference
 * `ExpressHandlerArgUse` see zero behaviour change. `ExpressHandler`
 * still fires only on the named-variable case; future work may
 * migrate it onto the same closure, but PR2 scope ends here.
 *
 * Example:
 *   from int useExpr, int fn
 *   where ExpressHandlerArgUse(useExpr, fn, 0)
 *   select useExpr, fn
 */
predicate ExpressHandlerArgUse(int useExpr, int fn, int paramIdx) {
    exists(int call, int handlerArgExpr, int paramSym, string method |
        MethodCall(call, _, method) and
        (
            method = "get"
            or method = "post"
            or method = "put"
            or method = "delete"
            or method = "patch"
            or method = "use"
        ) and
        CallArg(call, _, handlerArgExpr) and
        MayResolveTo(handlerArgExpr, fn) and
        Parameter(fn, paramIdx, _, _, paramSym, _) and
        ExprMayRef(useExpr, paramSym)
    )
}
