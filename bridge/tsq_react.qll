/**
 * Bridge library for React framework models (v2 Phase F).
 *
 * Provides QL classes for React component XSS detection via
 * `dangerouslySetInnerHTML` and a class form for detecting useState
 * setter call patterns (the "updater function" smells).
 *
 * NOTE (2026-04-18): rewritten from int-parameter predicate form to
 * class form. The previous form caused the desugarer to synthesise
 * disjunction predicates (`_disj_2`) that the planner could not size,
 * blowing the 5M binding cap on Mastodon at join step 1. The class form
 * makes `UseStateSetterCall` an arity-1 extent that participates in the
 * P2a class-extent materialisation pre-pass — once materialised, every
 * downstream join over useState setter calls is anchored against a tiny
 * extent (~7 tuples on the Counter fixture, scaling with real call
 * sites in production), exactly mirroring CodeQL's class-extent join
 * pattern. The arity-shadow bug that previously blocked this rewrite
 * was fixed in P2a (`e11dbad`): class extent heads are now keyed by
 * (name, arity) rather than name alone, so `Call/1` (the `Call`
 * supertype's class extent) and `Call/3` (the base schema relation)
 * occupy independent slots.
 */

/**
 * Transitive `FunctionContains`: holds if `node` is contained, directly
 * or via nested function literals, by `fn`. The base `FunctionContains`
 * relation is innermost-only (emitted in
 * `extract/walker_v2.go:136-139`), so without this expansion the call
 * `setX(prev => arr.forEach(() => setY()))` would not pair the outer
 * `setX` updater with the inner `setY` call — the inner call's
 * `FunctionContains` row points at the `() => setY()` arrow, not at the
 * outer `prev => ...` arrow.
 *
 * Now written as true recursion (`Path :- Edge or Path, Edge`) and
 * evaluated by the seminaive engine. The previous depth-3 unroll was
 * historical — the engine's recursive-predicate path is exercised by
 * other rules and behaves correctly here. Recursion matches CodeQL's
 * `+` transitive-closure semantics and removes the silent depth ceiling.
 */
predicate functionContainsStar(int fn, int node) {
    FunctionContains(fn, node)
    or
    exists(int mid |
        functionContainsStar(fn, mid) and
        FunctionContains(mid, node)
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
 * A useState setter call: a call whose callee resolves to a symbol bound
 * by destructuring the second element of a `useState(...)` initialiser.
 *
 *   const [count, setCount] = useState(0);
 *   setCount(...);   // <-- this is a UseStateSetterCall
 *
 * Class extent eligibility: this class extends `@call` (the base entity
 * type) and the characteristic predicate body references only base
 * schema relations — `CallCalleeSym`, `ArrayDestructure`, `Contains`,
 * `VarDecl`, `ImportBinding`. That makes the rule body match
 * `plan.IsClassExtentBody` and the rule is materialised by P2a's
 * `MaterialiseClassExtents` pre-pass before the planner runs. After
 * materialisation, the extent is a small base-like relation that
 * downstream joins (e.g. `setStateUpdaterCallsFn`) anchor against
 * directly, instead of the planner having to derive cardinality through
 * the synthesised disjunction `_disj_2` that the predicate form
 * produced.
 *
 * Why `extends @call` and not `extends Call`: both work post-P2a, but
 * `@call` keeps the entity-type grounding explicit (one base relation
 * reference in the body) and avoids depending on `Call`'s class extent
 * being materialised first. The arity-shadow fix (P2a) ensures the
 * arity-1 head `UseStateSetterCall(this)` does not collide with any
 * arity-3 schema relation.
 */
class UseStateSetterCall extends @call {
    UseStateSetterCall() {
        exists(int sym, int parent, int varDecl, int initExpr, int useStateSym |
            CallCalleeSym(this, sym) and
            ArrayDestructure(parent, 1, sym) and
            Contains(varDecl, parent) and
            VarDecl(varDecl, _, initExpr, _) and
            CallCalleeSym(initExpr, useStateSym) and
            ImportBinding(useStateSym, "react", "useState")
        )
    }

    /** Gets the callee symbol of this useState setter call. */
    int getSetterSym() {
        CallCalleeSym(this, result)
    }

    /** Gets the start line of the callee identifier. */
    int getLine() {
        exists(int callee |
            Call(this, callee, _) and
            Node(callee, _, _, result, _, _, _)
        )
    }

    /** Gets the first argument node (the updater function literal, when present). */
    int getUpdaterArg() {
        CallArg(this, 0, result)
    }

    /** Gets a textual representation. */
    string toString() { result = "useState setter call" }
}

/**
 * Holds if call `c` is a useState setter call and `line` is the start
 * line of its callee identifier. Retained for backward compatibility
 * with existing query callers; new queries should use the
 * `UseStateSetterCall` class directly.
 */
predicate useStateSetterCallLine(UseStateSetterCall c, int line) {
    exists(int callee |
        Call(c, callee, _) and
        Node(callee, _, _, line, _, _, _)
    )
}

/**
 * Holds if `c` is a useState setter call whose first argument is a function
 * literal (arrow or function expression) whose body — including any
 * nested function literals — contains at least one inner Call. This is
 * the "updater function calls a function" pattern:
 *
 *   setX(prev => helper(prev))
 *   setX(prev => { mutate(); return prev; })
 *   setX(prev => arr.forEach(() => helper(prev)))   // nested case
 *
 * The transitive `functionContainsStar` is required because the
 * extractor emits the base `FunctionContains` relation only against the
 * *innermost* enclosing function. Without the transitive variant, the
 * nested-arrow positive case above would be silently missed.
 *
 * Implementation note: the explicit `c instanceof UseStateSetterCall`
 * guard is load-bearing. The desugarer does NOT inject a class-extent
 * type literal for predicate parameters — only for `from`-clause and
 * `exists`-clause declarations (`desugar.go:558,789`). Without the
 * `instanceof`, the planner sees `c` as a free integer and does not
 * anchor the join against the materialised `UseStateSetterCall` extent;
 * the seed becomes whichever base relation has the smallest hint, and
 * the optimisation is lost. Follow-on improvement: extend
 * `desugarTopLevelPredicate` to inject parameter-type constraints so
 * authors don't need this redundancy. Tracked as a planner-roadmap
 * follow-on in PR.
 */
predicate setStateUpdaterCallsFn(UseStateSetterCall c, int line) {
    c instanceof UseStateSetterCall and
    exists(int argFn, int innerCall, int callee |
        CallArg(c, 0, argFn) and
        Function(argFn, _, _, _, _, _) and
        functionContainsStar(argFn, innerCall) and
        Call(innerCall, _, _) and
        Call(c, callee, _) and
        Node(callee, _, _, line, _, _, _)
    )
}

/**
 * Holds if `c` is a useState setter call whose updater function literal
 * contains a Call to ANOTHER useState setter (different setter symbol),
 * directly or via nested function literals:
 *
 *   setX(prev => { setY(...); return prev; })
 *   setX(prev => arr.forEach(() => setY(...)))   // nested case
 */
predicate setStateUpdaterCallsOtherSetState(UseStateSetterCall c, int line) {
    c instanceof UseStateSetterCall and
    exists(UseStateSetterCall inner, int argFn, int callee, int outerSym, int innerSym |
        inner instanceof UseStateSetterCall and
        CallArg(c, 0, argFn) and
        Function(argFn, _, _, _, _, _) and
        functionContainsStar(argFn, inner) and
        Call(c, callee, _) and
        Node(callee, _, _, line, _, _, _) and
        CallCalleeSym(c, outerSym) and
        CallCalleeSym(inner, innerSym) and
        outerSym != innerSym
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
