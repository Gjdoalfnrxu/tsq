/**
 * Bridge library for React framework models (v2 Phase F).
 *
 * Provides QL classes for React component XSS detection via
 * `dangerouslySetInnerHTML` and predicates for detecting useState
 * setter call patterns (the "updater function" smells).
 */

/**
 * Transitive-ish `FunctionContains`: holds if `node` is contained,
 * directly or via nested function literals, by `fn`. The base
 * `FunctionContains` relation is innermost-only (emitted in
 * `extract/walker_v2.go:136-139`), so without this expansion the call
 * `setX(prev => arr.forEach(() => setY()))` would not pair the outer
 * `setX` updater with the inner `setY` call — the inner call's
 * `FunctionContains` row points at the `() => setY()` arrow, not at the
 * outer `prev => ...` arrow.
 *
 * This is hand-unrolled to a fixed depth of three function nestings
 * rather than written as a true recursive predicate, because at the time
 * of writing the v1 evaluator's recursive-predicate path through
 * `Function(mid, ...)` does not propagate as expected for this exact
 * shape. Three levels covers all known real-world useState updater
 * patterns. If a fourth nesting level becomes load-bearing, lift this to
 * a real recursive predicate and revisit the engine behaviour — see the
 * follow-up note in the PR description.
 */
predicate functionContainsStar(int fn, int node) {
    FunctionContains(fn, node)
    or
    exists(int mid1 |
        FunctionContains(fn, mid1) and
        Function(mid1, _, _, _, _, _) and
        FunctionContains(mid1, node)
    )
    or
    exists(int mid1, int mid2 |
        FunctionContains(fn, mid1) and
        Function(mid1, _, _, _, _, _) and
        FunctionContains(mid1, mid2) and
        Function(mid2, _, _, _, _, _) and
        FunctionContains(mid2, node)
    )
}

/**
 * Holds if `sym` is a setter symbol bound by destructuring the second
 * element of a `useState` call imported from `'react'`:
 *
 *   const [_, sym] = useState(...);
 */
predicate isUseStateSetterSym(int sym) {
    exists(int parent, int varDecl, int initExpr, int useStateSym |
        ArrayDestructure(parent, 1, sym) and
        Contains(varDecl, parent) and
        VarDecl(varDecl, _, initExpr, _) and
        CallCalleeSym(initExpr, useStateSym) and
        ImportBinding(useStateSym, "react", "useState")
    )
}

/**
 * Holds if call `c` is a call to a `useState` setter (the second element
 * of a destructuring binding from a call to react's `useState`).
 */
predicate isUseStateSetterCall(int c) {
    exists(int sym | CallCalleeSym(c, sym) and isUseStateSetterSym(sym))
}

/**
 * Holds if call `c` is a useState setter call and `line` is the start
 * line of its callee identifier.
 */
predicate useStateSetterCallLine(int c, int line) {
    isUseStateSetterCall(c) and
    exists(int callee |
        Call(c, callee, _) and
        Node(callee, _, _, line, _, _, _)
    )
}

// NOTE: the v1 `setStateUpdaterCallsFn` predicate (forward-enumerated
// `functionContainsStar`) was ripped out as part of issue #121 Phase A.2.
// The query that called it (`testdata/queries/v2/find_setstate_updater_calls_fn.ql`)
// has been rewritten in place to use `SetStateUpdaterTracker` below — a
// Configuration-class form whose `step` override walks the same
// containment graph but lets the planner magic-set-rewrite the small-IDB
// sink side. Row-set parity with the v1 baseline (3 rows on
// `testdata/projects/react-usestate`) is asserted by
// `TestIssue121_BackwardSetStateQueryParityWithV1`.

/**
 * Configuration-class form of the v1 `setStateUpdaterCallsFn` predicate
 * (issue #121, Phase A.2). Re-expresses the "useState setter call whose
 * arrow-fn arg contains an inner call" pattern as a `BackwardTracker`
 * subclass that overrides `step` with structural containment instead of
 * dataflow.
 *
 * Why this shape:
 *   - The v1 predicate forward-enumerated `functionContainsStar` (438k
 *     tuples on Mastodon) and OOM'd. Issue #121 introduces a query
 *     surface where the planner can magic-set-rewrite the small-IDB sink
 *     side and walk the containment graph backward from the bound sink
 *     symbol.
 *   - `step` is overridden to `functionContainsStar(s, t) and Call(t, _, _)`
 *     — a structural step rather than the default `flowsTo` dataflow
 *     step. The magic-set inference is agnostic to which binary relation
 *     `step` expands to (see `BackwardTracker` qldoc for the contract).
 *   - `isSink` is the tiny IDB of inner-call symbols, on the order of
 *     dozens-to-thousands of tuples even on Mastodon. Magic-set
 *     propagation binds the sink position and prunes the containment
 *     walk to those targets.
 *   - `isSource` is the set of arrow-fn arg symbols of useState setter
 *     calls — also tiny.
 *
 * Result tuples are `(srcArgFn, innerCall)` pairs. The v2 query
 * (`testdata/queries/v2/find_setstate_updater_calls_fn.ql`) projects
 * these back to (callSym, line) for row-set parity with the v1
 * baseline.
 */
/**
 * Helper for the v2 `setStateUpdaterCallsFn` query rewrite: maps a useState
 * setter call's first-arg arrow-fn symbol back to the `(call, line)` pair
 * the v1 query returned. Lives in the bridge because base relations
 * (`CallArg`, `Call`, `Node`) are not in scope inside `.ql` query bodies
 * directly — bridge predicates are the API surface.
 */
predicate setStateSetterCallForArg(int argFn, int c, int line) {
    isUseStateSetterCall(c) and
    CallArg(c, 0, argFn) and
    exists(int callee |
        Call(c, callee, _) and
        Node(callee, _, _, line, _, _, _)
    )
}

class SetStateUpdaterTracker extends BackwardTracker {
    override predicate isSink(int innerCall) {
        Call(innerCall, _, _) and
        exists(int argFn |
            // Constrain sinks to inner calls reachable from some useState
            // setter's arrow-fn arg under containment. This is what keeps
            // the sink IDB small (~dozens on real corpora) — without it,
            // every Call in the program is a candidate sink.
            isUseStateSetterCall(_) and
            functionContainsStar(argFn, innerCall) and
            Function(argFn, _, _, _, _, _)
        )
    }

    override predicate isSource(int argFn) {
        exists(int c |
            isUseStateSetterCall(c) and
            CallArg(c, 0, argFn) and
            Function(argFn, _, _, _, _, _)
        )
    }

    override predicate step(int argFn, int innerCall) {
        functionContainsStar(argFn, innerCall) and
        Call(innerCall, _, _)
    }
}

/**
 * Holds if `c` is a useState setter call whose updater function literal
 * contains a Call to ANOTHER useState setter (different setter symbol),
 * directly or via nested function literals:
 *
 *   setX(prev => { setY(...); return prev; })
 *   setX(prev => arr.forEach(() => setY(...)))   // nested case
 */
predicate setStateUpdaterCallsOtherSetState(int c, int line) {
    isUseStateSetterCall(c) and
    exists(int argFn, int innerCall, int outerSym, int innerSym |
        CallArg(c, 0, argFn) and
        Function(argFn, _, _, _, _, _) and
        functionContainsStar(argFn, innerCall) and
        CallCalleeSym(c, outerSym) and
        CallCalleeSym(innerCall, innerSym) and
        isUseStateSetterSym(innerSym) and
        innerSym != outerSym
    ) and
    exists(int callee |
        Call(c, callee, _) and
        Node(callee, _, _, line, _, _, _)
    )
}

/**
 * A React XSS sink via dangerouslySetInnerHTML. These are TaintSink facts
 * with kind "xss" derived from JsxAttribute facts matching the attribute
 * name "dangerouslySetInnerHTML".
 */
class DangerouslySetInnerHTML extends TaintSink {
    DangerouslySetInnerHTML() {
        this.getSinkKind() = "xss" and
        exists(int elem | JsxAttribute(elem, "dangerouslySetInnerHTML", this))
    }

    /** Gets a textual representation. */
    override string toString() { result = "DangerouslySetInnerHTML" }
}

// NOTE: A class form `class UseStateSetterCall extends Call { ... }` was
// considered but removed because it triggers the v1 engine's
// arity-shadowing bug — materialising `Call/1` head facts into the same
// relation as the base `Call/3` schema corrupts joins on `Call`.
// Use the int-parameter predicates above (`isUseStateSetterCall`,
// `useStateSetterCallLine`, `setStateUpdaterCallsFn`,
// `setStateUpdaterCallsOtherSetState`) instead.
