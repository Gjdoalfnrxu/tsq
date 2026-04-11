/**
 * Bridge library for React framework models (v2 Phase F).
 * Provides QL classes for React component XSS detection via dangerouslySetInnerHTML.
 */

/**
 * Holds if `sym` is a setter symbol bound by destructuring the second
 * element of a `useState` call imported from `'react'`:
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
    exists(int callee, int arity, int file, string kind, int sc, int el, int ec |
        Call(c, callee, arity) and
        Node(callee, file, kind, line, sc, el, ec)
    )
}

/**
 * Holds if `c` is a useState setter call whose first argument is a function
 * literal (arrow or function expression) whose body contains at least one
 * inner Call. This is the "updater function calls a function" pattern:
 *   setX(prev => helper(prev))
 *   setX(prev => { mutate(); return prev; })
 *
 * Note: the inner call is required to have a callee node distinct from the
 * function literal itself, so we don't match the trivial outer call.
 */
predicate setStateUpdaterCallsFn(int c, int line) {
    isUseStateSetterCall(c) and
    exists(int argFn, int innerCall, int innerCallee, int innerArity |
        CallArg(c, 0, argFn) and
        Function(argFn, _, _, _, _, _) and
        FunctionContains(argFn, innerCall) and
        Call(innerCall, innerCallee, innerArity) and
        innerCall != c
    ) and
    exists(int callee, int arity, int file, string kind, int sc, int el, int ec |
        Call(c, callee, arity) and
        Node(callee, file, kind, line, sc, el, ec)
    )
}

/**
 * Holds if `c` is a useState setter call whose updater function literal
 * contains a Call to ANOTHER useState setter (different setter symbol):
 *   setX(prev => { setY(...); return prev; })
 */
predicate setStateUpdaterCallsOtherSetState(int c, int line) {
    isUseStateSetterCall(c) and
    exists(
        int argFn, int innerCall, int outerSym, int innerSym
    |
        CallArg(c, 0, argFn) and
        Function(argFn, _, _, _, _, _) and
        FunctionContains(argFn, innerCall) and
        innerCall != c and
        CallCalleeSym(c, outerSym) and
        CallCalleeSym(innerCall, innerSym) and
        isUseStateSetterSym(innerSym) and
        innerSym != outerSym
    ) and
    exists(int callee, int arity, int file, string kind, int sc, int el, int ec |
        Call(c, callee, arity) and
        Node(callee, file, kind, line, sc, el, ec)
    )
}

/**
 * A React XSS sink via dangerouslySetInnerHTML. These are TaintSink facts
 * with kind "xss" derived from JsxAttribute facts matching the attribute name
 * "dangerouslySetInnerHTML".
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
