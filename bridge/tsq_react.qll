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
 * Transitive-ish `FunctionContains`: holds if `node` is contained,
 * directly or via nested function literals, by `fn`. The base
 * `FunctionContains` relation is innermost-only (emitted in
 * `extract/walker_v2.go:136-139`), so without this expansion the call
 * `setX(prev => arr.forEach(() => setY()))` would not pair the outer
 * `setX` updater with the inner `setY` call — the inner call's
 * `FunctionContains` row points at the `() => setY()` arrow, not at the
 * outer `prev => ...` arrow.
 *
 * Hand-unrolled to depth 3 rather than written as true recursion. A
 * recursive form was tried first and works correctly on small
 * fixtures, but on the Mastodon corpus the recursive
 * `functionContainsStar` IDB grows too large to size correctly
 * pre-evaluation: the planner defaults its hint to 1000 (recursive IDBs
 * are not materialised by the trivial-IDB pre-pass) and then picks a
 * join order that blows the binding cap downstream. The unrolled form
 * is a non-recursive IDB and gets a real cardinality estimate from the
 * trivial-IDB pre-pass + sampling estimator (P2b), which keeps the
 * planner honest. Three levels covers all known real-world useState
 * updater patterns. If a fourth nesting level becomes load-bearing,
 * lift this to a real recursive predicate AFTER fixing the recursive-IDB
 * sizing path so the planner gets a real estimate (planner-roadmap
 * follow-on).
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
 * guard is now redundant — PR #146 taught `desugarTopLevelPredicate`
 * to inject class-extent type literals for predicate parameters, so
 * the planner anchors the join against the materialised
 * `UseStateSetterCall` extent without it. The guard is kept defensively
 * because it documents intent at the call site and is a no-op in plan
 * shape (same literal as the auto-injected one — deduplicated by the
 * planner). Output equivalence to the flat form is verified by
 * bench run_008 (bit-identical CSVs across both corpora).
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
 * Holds if `sym` is a useState setter symbol bound directly by destructuring
 * the second element of a `useState(...)` call (the base case for the alias
 * tracking below). This is the predicate form of `UseStateSetterCall`'s own
 * setter-symbol guard, exposed so the alias closure has a non-recursive base
 * case that the trivial-IDB pre-pass can size.
 */
predicate useStateSetterSym(int sym) {
    exists(int parent, int varDecl, int initExpr, int useStateSym |
        ArrayDestructure(parent, 1, sym) and
        Contains(varDecl, parent) and
        VarDecl(varDecl, _, initExpr, _) and
        CallCalleeSym(initExpr, useStateSym) and
        ImportBinding(useStateSym, "react", "useState")
    )
}

/**
 * Holds if a JSX attribute named `attrName` on element `elem` passes the
 * value of symbol `valueSym` directly through (i.e. `<Foo attr={valueSym} />`
 * with `valueSym` an Identifier expression).
 *
 * For v1 we ONLY recognise the direct-identifier case. Wrapped passes such
 * as `<Foo attr={() => valueSym(...)}>` or `<Foo attr={x => valueSym(x)}>`
 * are intentionally OUT OF SCOPE — they require either a function-literal
 * "passes through" analysis or a may-call summary. They are tracked as a
 * follow-up.
 */
predicate jsxPropPassesIdentifier(int elem, string attrName, int valueSym) {
    exists(int valueExpr |
        JsxAttribute(elem, attrName, valueExpr) and
        ExprMayRef(valueExpr, valueSym)
    )
}

/**
 * Holds if `paramSym` is the symbol bound by destructuring the field
 * `propName` from the first parameter of `componentFn`.
 *
 *   function ZoomControl({ onConfigChange }) { ... }
 *
 * Here `componentFn` is the function, `propName = "onConfigChange"`, and
 * `paramSym` is the binding symbol of `onConfigChange` inside the body.
 *
 * Limitations: only the destructured-first-param shape is recognised. A
 * single-named first-parameter (`function Foo(props) { props.onConfigChange(...) }`)
 * is NOT recognised in v1 — that form requires field-read tracking through
 * the `props` symbol. Tracked as follow-up.
 */
predicate componentDestructuredProp(int componentFn, string propName, int paramSym) {
    exists(int paramNode |
        Parameter(componentFn, 0, _, paramNode, _, _) and
        DestructureField(paramNode, propName, _, paramSym, _)
    )
}

/**
 * Holds if `componentFn` is the function declaration that JSX element
 * `elem` instantiates. Resolves `elem`'s tag symbol through `FunctionSymbol`.
 *
 * Example:
 *   function ZoomControl({ ... }) { ... }      // componentFn
 *   <ZoomControl onConfigChange={setX} />      // elem
 */
predicate jsxElementComponent(int elem, int componentFn) {
    exists(int tagSym |
        JsxElement(elem, _, tagSym) and
        FunctionSymbol(tagSym, componentFn)
    )
}

/**
 * One-hop alias step: holds if `paramSym` is a destructured prop binding
 * inside a component function, and the JSX site that instantiates that
 * component passes `valueSym` to that prop directly (identifier pass).
 *
 * In other words, `paramSym` aliases `valueSym` for callers that invoke
 * `paramSym(...)` in the component body.
 */
predicate setterAliasStep(int valueSym, int paramSym) {
    exists(int elem, string propName, int componentFn |
        jsxPropPassesIdentifier(elem, propName, valueSym) and
        jsxElementComponent(elem, componentFn) and
        componentDestructuredProp(componentFn, propName, paramSym)
    )
}

/**
 * Holds if `sym` is either a useState setter symbol directly OR a parameter
 * symbol that receives a setter symbol (or a transitively-aliased setter
 * symbol) through JSX prop passing.
 *
 * Why hand-unrolled to depth 3 instead of recursive: the same planner
 * pathology that motivated the unrolled `functionContainsStar` (see comment
 * above) applies here. The class-extent / trivial-IDB pre-pass cannot size
 * a recursive IDB pre-evaluation, so the planner falls back to the default
 * 1000-tuple hint and may pick a Cartesian-heavy join order. Three hops
 * covers the realistic prop-drilling depths we have seen in production
 * React code (Viewer → Toolbar → Button, etc). If a fourth hop becomes
 * load-bearing on a real corpus, lift to a real recursive predicate AFTER
 * the recursive-IDB sizing path is fixed.
 *
 * Termination: the unrolled form terminates trivially. Each `setterAliasStep`
 * is a finite extent over base relations; composing it 0/1/2/3 times yields
 * a finite IDB.
 */
predicate useStateSetterAlias(int sym) {
    useStateSetterSym(sym)
    or
    exists(int s0 |
        useStateSetterSym(s0) and
        setterAliasStep(s0, sym)
    )
    or
    exists(int s0, int s1 |
        useStateSetterSym(s0) and
        setterAliasStep(s0, s1) and
        setterAliasStep(s1, sym)
    )
    or
    exists(int s0, int s1, int s2 |
        useStateSetterSym(s0) and
        setterAliasStep(s0, s1) and
        setterAliasStep(s1, s2) and
        setterAliasStep(s2, sym)
    )
}

/**
 * A call whose callee symbol may-refs a `useStateSetterAlias` symbol —
 * i.e. either a direct `useState` setter call OR a call through a
 * prop-aliased parameter inside a child component.
 *
 *   const [_, setX] = useState(0);
 *   setX(prev => ...);                        // direct (useStateSetterCall too)
 *   <Child onChange={setX} />
 *   function Child({ onChange }) {
 *     onChange(prev => ...);                  // alias call
 *   }
 */
predicate useStateSetterAliasCall(int call) {
    exists(int sym |
        CallCalleeSym(call, sym) and
        useStateSetterAlias(sym)
    )
}

/**
 * Sibling of `setStateUpdaterCallsOtherSetState` that follows JSX-prop
 * setter aliases on EITHER the outer or the inner setter. Catches the
 * Viewer → ZoomControl pattern from the motivating bug:
 *
 *   function Viewer() {
 *     const [zoomConfig, setZoomConfig] = useState(initial);
 *     return <ZoomControl onConfigChange={setZoomConfig} />;
 *   }
 *   function ZoomControl({ onConfigChange }) {
 *     onConfigChange(prev => ({ ...prev, zoom: prev.zoom + 1 }));
 *   }
 *
 * The outer `onConfigChange(...)` call inside `ZoomControl` is recognised
 * as a setter-alias call (it transitively refers to `setZoomConfig`) and
 * its updater-arg body is searched for any other setter-alias call with a
 * DIFFERENT callee symbol — exactly mirroring the direct-form predicate.
 *
 * `line` is the start line of the OUTER call's callee identifier, for
 * markdown rendering parity with `_md` queries.
 */
predicate setStateUpdaterCallsOtherSetStateThroughProps(int call, int line) {
    useStateSetterAliasCall(call) and
    exists(int innerCall, int argFn, int callee, int outerSym, int innerSym |
        useStateSetterAliasCall(innerCall) and
        CallArg(call, 0, argFn) and
        Function(argFn, _, _, _, _, _) and
        functionContainsStar(argFn, innerCall) and
        Call(innerCall, _, _) and
        Call(call, callee, _) and
        Node(callee, _, _, line, _, _, _) and
        CallCalleeSym(call, outerSym) and
        CallCalleeSym(innerCall, innerSym) and
        outerSym != innerSym
    )
}

/* -----------------------------------------------------------------------
 * Round-2 of setState alias tracking — through React Context
 * -----------------------------------------------------------------------
 *
 * Extends `useStateSetterAlias` to follow setters that arrive at a call
 * site through `createContext` + `<Ctx.Provider value={{ setX }}>` +
 * `useContext(Ctx)` (or a hook wrapping useContext) + destructure.
 *
 * Motivating shape:
 *
 *   const ViewerStateActions = createContext<...>(null);
 *
 *   function ViewerProvider({ children }) {
 *     const [zoom, setZoom] = useState(1);
 *     return <ViewerStateActions.Provider value={{ setZoom }}>{children}</...>;
 *   }
 *
 *   function useViewerActions() { return useContext(ViewerStateActions); }
 *
 *   function ZoomButton() {
 *     const { setZoom } = useViewerActions();
 *     setZoom(prev => prev + 1);   // <-- recognised as a setter-alias call
 *   }
 *
 * Soundness vs. precision call-outs (deferrals documented in the round-2
 * wiki page):
 *  - createContext recognition is name-based on the callee identifier
 *    binding (`ImportBinding(_, "react", "createContext")`). Aliased
 *    `import { createContext as cc }` works because the callee sym still
 *    resolves to the imported binding, but a wholesale `import * as React`
 *    + `React.createContext(...)` callee shape is NOT covered in v1.
 *  - Multi-Provider disambiguation is intentionally over-approximate —
 *    a useContext(Ctx) resolves to the value of ANY Provider for Ctx in
 *    the program. A program with two unrelated Providers for the same
 *    context will produce false positives.
 *  - Hook-indirection depth is hand-unrolled to two levels (useContext,
 *    useFoo() = useContext, useBar() = useFoo()). Same planner-sizing
 *    rationale as `functionContainsStar` and `useStateSetterAlias`.
 *  - Object-literal field tracking treats `value={{ a, b }}` as exposing
 *    BOTH `a` and `b`; field-name binding to destructure-key is enforced
 *    on the consumer side via `DestructureField(parent, propName, _, _, _)`
 *    matched against `ObjectLiteralField(obj, propName, valueExpr)`.
 */

/**
 * Holds if `sym` is a symbol bound to a `createContext(...)` call result —
 * i.e. the context handle.
 *
 *   const ViewerStateActions = createContext<...>(null);
 *
 * Recognition is name-based on the callee identifier binding via
 * `ImportBinding(_, "react", "createContext")`. Namespace-import call sites
 * (e.g. `React.createContext(...)`) are NOT recognised in v1 — same
 * deferral as the namespace shape for useState in the round-1 base case.
 */
predicate contextSym(int sym) {
    exists(int initExpr, int createCtxSym |
        VarDecl(_, sym, initExpr, _) and
        CallCalleeSym(initExpr, createCtxSym) and
        ImportBinding(createCtxSym, "react", "createContext")
    )
}

/**
 * Holds if `objExpr` is contained, directly or transitively via the
 * JsxExpression wrapper, by `valueAttrExpr`. Used to find the object
 * literal expression inside a Provider's `value={{ ... }}` attribute
 * (the JsxAttribute valueExpr column points at the `{ ... }` JsxExpression
 * node, not at the inner Object literal directly).
 */
predicate jsxAttrValueObject(int valueAttrExpr, int objExpr) {
    valueAttrExpr = objExpr
    or
    Contains(valueAttrExpr, objExpr)
}

/**
 * Holds if a JSX element `elem` is a Provider for context symbol `ctxSym`
 * — i.e. its tag is the member access `<ctxSym.Provider ...>` — and its
 * `value` attribute carries object literal `objExpr`.
 *
 * The tag of a `<Foo.Provider />` JSX element is a MemberExpression node;
 * the walker emits a FieldRead row for it with `baseSym = Foo` and
 * `fieldName = "Provider"`. We pivot on that to identify the context.
 *
 * Limitations:
 *  - `Contains(valueAttrExpr, objExpr)` over-approximates: nested object
 *    literals inside a non-object value (e.g. `value={makeActions({...})}`)
 *    would also match. The downstream field-name match constrains this.
 *  - Only direct object-literal values are recognised; values built by a
 *    helper function call (`value={makeActions()}`) are out of scope.
 */
predicate contextProviderValueObject(int ctxSym, int objExpr) {
    exists(int elem, int tagNode, int valueAttrExpr |
        JsxElement(elem, tagNode, _) and
        FieldRead(tagNode, ctxSym, "Provider") and
        JsxAttribute(elem, "value", valueAttrExpr) and
        jsxAttrValueObject(valueAttrExpr, objExpr) and
        ObjectLiteralField(objExpr, _, _)
    )
}

/**
 * Holds if a Provider for context symbol `ctxSym` exposes field `fieldName`
 * bound to a value expression that may-refs `valueSym`.
 *
 * Combines `contextProviderValueObject` with `ObjectLiteralField`. The
 * shorthand form `{ setX }` is the load-bearing case — the walker emits
 * the field with valueExpr pointing at the Identifier node and the
 * Identifier emit-pass produces the ExprMayRef row we then look up.
 */
predicate contextProviderField(int ctxSym, string fieldName, int valueSym) {
    exists(int objExpr, int valueExpr |
        contextProviderValueObject(ctxSym, objExpr) and
        ObjectLiteralField(objExpr, fieldName, valueExpr) and
        ExprMayRef(valueExpr, valueSym)
    )
}

/**
 * Holds if `call` is a direct `useContext(ctxSym)` invocation, where the
 * argument may-refs the given context symbol. `useContext` is recognised
 * by the same import-name shape as `useState` and `createContext`.
 */
predicate useContextCall(int call, int ctxSym) {
    exists(int useCtxSym, int argNode |
        CallCalleeSym(call, useCtxSym) and
        ImportBinding(useCtxSym, "react", "useContext") and
        CallArg(call, 0, argNode) and
        ExprMayRef(argNode, ctxSym)
    )
}

/**
 * Holds if `hookFn` is a function whose body has a return statement whose
 * expression is a `useContext(ctxSym)` call. This is the "hook indirection"
 * pattern:
 *
 *   function useViewerActions() {
 *     return useContext(ViewerStateActions);
 *   }
 *
 * Hand-unrolled to depth 2 (useContext directly, OR a hook returning a
 * call to a hook returning useContext). Same planner-sizing rationale as
 * `useStateSetterAlias`. Deeper chains (3+) are deferred follow-up.
 */
predicate hookIndirectionD1(int hookFn, int ctxSym) {
    exists(int retExpr, int innerCall |
        ReturnStmt(hookFn, _, retExpr) and
        ExprIsCall(retExpr, innerCall) and
        useContextCall(innerCall, ctxSym)
    )
}

predicate hookIndirectionD2(int hookFn, int ctxSym) {
    exists(int retExpr, int innerCall, int innerCalleeSym, int innerHookFn |
        ReturnStmt(hookFn, _, retExpr) and
        ExprIsCall(retExpr, innerCall) and
        CallCalleeSym(innerCall, innerCalleeSym) and
        FunctionSymbol(innerCalleeSym, innerHookFn) and
        innerHookFn != hookFn and
        hookIndirectionD1(innerHookFn, ctxSym)
    )
}

predicate hookIndirection(int hookFn, int ctxSym) {
    hookIndirectionD1(hookFn, ctxSym)
    or
    hookIndirectionD2(hookFn, ctxSym)
}

/**
 * Holds if `call` is a call site that resolves (directly or via a hook
 * indirection) to a useContext(ctxSym) value, i.e. its result symbol carries
 * the Provider's value object for context `ctxSym`.
 *
 * Direct path: `useContext(ctx)` — `call` is the useContext call itself.
 * Indirect path: `useFoo()` where `useFoo` is a `hookIndirection` for ctx.
 */
/**
 * Holds if `localSym` (a callee binding seen at a call site) resolves —
 * possibly across module boundaries via an import/export pair on the same
 * exported name — to function symbol `targetFnSym`. v1 cross-module
 * resolution: import-binding name matches an export-binding name, and the
 * export-binding symbol is the target function's defining symbol.
 *
 * The module-spec path string from `ImportBinding` is NOT compared against
 * the export-binding's file path (different shapes), so this is loosely
 * over-approximated by name. Disambiguation is left to downstream filters
 * (only callers using a specific imported name will match).
 */
predicate importedFunctionSymbol(int localSym, int targetFn) {
    exists(string importedName, int exportSym |
        ImportBinding(localSym, _, importedName) and
        ExportBinding(importedName, exportSym, _) and
        FunctionSymbol(exportSym, targetFn)
    )
}

predicate useContextCallSiteResolvesContext(int call, int ctxSym) {
    useContextCall(call, ctxSym)
    or
    exists(int hookSym, int hookFn |
        CallCalleeSym(call, hookSym) and
        FunctionSymbol(hookSym, hookFn) and
        hookIndirection(hookFn, ctxSym)
    )
    or
    exists(int hookSym, int hookFn |
        CallCalleeSym(call, hookSym) and
        importedFunctionSymbol(hookSym, hookFn) and
        hookIndirection(hookFn, ctxSym)
    )
}

/**
 * Holds if `paramSym` is the symbol bound by destructuring the field
 * `fieldName` from the result of a `useContextCallSiteResolvesContext`
 * call for context `ctxSym`.
 *
 *   const { setZoom } = useViewerActions();
 *   //      ^^^^^^^ paramSym; fieldName = "setZoom"
 *
 * Matches the shape:
 *   VarDecl(varDecl, _, initExpr, _) where initExpr is (transitively via
 *   non-null assertion / cast) a `useContextCallSiteResolvesContext` call,
 *   and the destructure pattern is contained in the same VarDecl.
 *
 * Implementation note: we tolerate a single Cast hop on the initExpr to
 * cover the common `useContext(...)!` non-null assertion shape.
 */
predicate contextDestructureBinding(int ctxSym, string fieldName, int paramSym) {
    exists(int varDecl, int parent, int initExpr, int callExpr, int call |
        VarDecl(varDecl, _, initExpr, _) and
        Contains(varDecl, parent) and
        DestructureField(parent, fieldName, _, paramSym, _) and
        // initExpr resolves to a useContextCallSite call, possibly via a
        // single non-null assertion / cast hop.
        (
            callExpr = initExpr
            or
            Cast(initExpr, callExpr)
        ) and
        ExprIsCall(callExpr, call) and
        useContextCallSiteResolvesContext(call, ctxSym)
    )
}

/**
 * One-hop CONTEXT alias step: holds if `paramSym` is a destructured
 * binding from a useContext call site for context `ctxSym`, AND a Provider
 * for `ctxSym` exposes a field of that name bound to symbol `valueSym`.
 *
 * Mirrors `setterAliasStep` but for the context channel. `paramSym`
 * therefore aliases `valueSym` for callers that invoke `paramSym(...)` in
 * the consumer body.
 *
 * The field-name match between `contextDestructureBinding` and
 * `contextProviderField` is what disambiguates which provided field the
 * destructure is reading — even though a single Provider's value object
 * may expose many fields, only the one matching the destructure binding
 * name participates in this alias step.
 */
/**
 * Links a Provider-side context symbol to a Consumer-side context symbol
 * across the import/export boundary (or trivially, the same symbol when
 * Provider and Consumer share a module). Cross-module link is by exported
 * name match. v1 doesn't try to disambiguate module paths — same-name
 * collisions are over-approximated.
 */
predicate contextSymLink(int providerCtxSym, int consumerCtxSym) {
    providerCtxSym = consumerCtxSym
    or
    exists(string name |
        ExportBinding(name, providerCtxSym, _) and
        ImportBinding(consumerCtxSym, _, name)
    )
}

predicate contextSetterAliasStep(int valueSym, int paramSym) {
    exists(int providerCtxSym, int consumerCtxSym, string fieldName |
        contextSym(providerCtxSym) and
        contextProviderField(providerCtxSym, fieldName, valueSym) and
        contextSymLink(providerCtxSym, consumerCtxSym) and
        contextDestructureBinding(consumerCtxSym, fieldName, paramSym)
    )
}

/**
 * Extension of `useStateSetterAlias` to ALSO follow the context-provided
 * setter chain. Adds disjuncts of the form
 *   "useStateSetterSym(s0) and contextSetterAliasStep(s0, sym)"
 * up to depth 3, mirroring the JSX-prop-alias unrolling. Mixed chains
 * (prop hop then context hop, or context then prop) are also covered up
 * to combined depth 3.
 *
 * Why hand-unrolled: identical planner-sizing rationale as the round-1
 * `useStateSetterAlias`. The base relation set is finite (createContext
 * call results, Provider JSX elements, useContext / hook calls) so each
 * step is a finite extent.
 *
 * Note: the round-1 `useStateSetterAlias` predicate is REPLACED rather
 * than wrapped — this avoids defining two predicates with the same name
 * and keeps a single point-of-truth for the alias closure.
 */
predicate setterAliasStepAny(int valueSym, int paramSym) {
    setterAliasStep(valueSym, paramSym)
    or
    contextSetterAliasStep(valueSym, paramSym)
}

/**
 * Round-2 alias closure. SUPERSEDES the round-1 `useStateSetterAlias`
 * disjunction body; round-1's predicate is preserved above for source
 * compatibility but new code should reference this one.
 *
 * Hand-unrolled to depth 3 over `setterAliasStepAny`, which mixes JSX prop
 * hops and Context hops freely.
 */
predicate useStateSetterAliasV2(int sym) {
    useStateSetterSym(sym)
    or
    exists(int s0 |
        useStateSetterSym(s0) and
        setterAliasStepAny(s0, sym)
    )
    or
    exists(int s0, int s1 |
        useStateSetterSym(s0) and
        setterAliasStepAny(s0, s1) and
        setterAliasStepAny(s1, sym)
    )
    or
    exists(int s0, int s1, int s2 |
        useStateSetterSym(s0) and
        setterAliasStepAny(s0, s1) and
        setterAliasStepAny(s1, s2) and
        setterAliasStepAny(s2, sym)
    )
}

/**
 * V2 sibling of `useStateSetterAliasCall`. Recognises calls whose callee
 * symbol may-refs ANY useStateSetterAliasV2 symbol — i.e. a direct
 * useState setter, a JSX-prop-aliased parameter, OR a context-aliased
 * destructure binding.
 */
predicate useStateSetterAliasCallV2(int call) {
    exists(int sym |
        CallCalleeSym(call, sym) and
        useStateSetterAliasV2(sym)
    )
}

/**
 * Holds if `sym` is a context-aliased setter binding — i.e. a paramSym
 * produced by `contextSetterAliasStep` for some chain rooted at a
 * useStateSetterSym. This is the "at least one context hop" filter used
 * to keep the through-context query diagnostically distinct from the
 * direct-form and through-props queries. Hand-unrolled to depth 3 to
 * mirror the alias closure.
 */
predicate isContextAliasedSetterSym(int sym) {
    exists(int s0 |
        useStateSetterSym(s0) and
        contextSetterAliasStep(s0, sym)
    )
    or
    exists(int s0, int s1 |
        useStateSetterSym(s0) and
        setterAliasStepAny(s0, s1) and
        contextSetterAliasStep(s1, sym)
    )
    or
    exists(int s0, int s1 |
        useStateSetterSym(s0) and
        contextSetterAliasStep(s0, s1) and
        setterAliasStepAny(s1, sym)
    )
    or
    exists(int s0, int s1, int s2 |
        useStateSetterSym(s0) and
        setterAliasStepAny(s0, s1) and
        contextSetterAliasStep(s1, s2) and
        setterAliasStepAny(s2, sym)
    )
}

/**
 * Holds if `call` is a setter-alias call AND its callee chain involves at
 * least one Context hop. Filters out direct + pure-prop alias matches so
 * the through-context query reports only matches that are diagnostically
 * about the context channel.
 */
predicate useStateSetterContextAliasCall(int call) {
    exists(int sym |
        CallCalleeSym(call, sym) and
        isContextAliasedSetterSym(sym)
    )
}

/**
 * Sibling of `setStateUpdaterCallsOtherSetStateThroughProps` that requires
 * at least ONE side of the outer/inner setter pair to be reached through
 * React Context aliasing. The other side may be a direct setter, a
 * prop-aliased setter, or another context-aliased setter. Covers the
 * canonical motivating shape:
 *
 *   function ZoomButton() {
 *     const { setZoom, setPan } = useViewerActions();
 *     setZoom(prev => { setPan(p => ...); return ...; });
 *   }
 *
 * The "at least one context hop" filter (`useStateSetterContextAliasCall`)
 * keeps this query diagnostically distinct from the direct-form and the
 * through-props queries — a pure direct-form match would not surface here
 * even though the underlying `useStateSetterAliasV2` closure is a strict
 * superset.
 *
 * `line` is the start line of the OUTER call's callee identifier.
 */
predicate setStateUpdaterCallsOtherSetStateThroughContext_outerCtx(int call, int line) {
    useStateSetterAliasCallV2(call) and
    useStateSetterContextAliasCall(call) and
    exists(int innerCall, int argFn, int callee, int outerSym, int innerSym |
        useStateSetterAliasCallV2(innerCall) and
        CallArg(call, 0, argFn) and
        Function(argFn, _, _, _, _, _) and
        functionContainsStar(argFn, innerCall) and
        Call(innerCall, _, _) and
        Call(call, callee, _) and
        Node(callee, _, _, line, _, _, _) and
        CallCalleeSym(call, outerSym) and
        CallCalleeSym(innerCall, innerSym) and
        outerSym != innerSym
    )
}

predicate setStateUpdaterCallsOtherSetStateThroughContext_innerCtx(int call, int line) {
    useStateSetterAliasCallV2(call) and
    exists(int innerCall, int argFn, int callee, int outerSym, int innerSym |
        useStateSetterAliasCallV2(innerCall) and
        useStateSetterContextAliasCall(innerCall) and
        CallArg(call, 0, argFn) and
        Function(argFn, _, _, _, _, _) and
        functionContainsStar(argFn, innerCall) and
        Call(innerCall, _, _) and
        Call(call, callee, _) and
        Node(callee, _, _, line, _, _, _) and
        CallCalleeSym(call, outerSym) and
        CallCalleeSym(innerCall, innerSym) and
        outerSym != innerSym
    )
}

predicate setStateUpdaterCallsOtherSetStateThroughContext(int call, int line) {
    setStateUpdaterCallsOtherSetStateThroughContext_outerCtx(call, line)
    or
    setStateUpdaterCallsOtherSetStateThroughContext_innerCtx(call, line)
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
