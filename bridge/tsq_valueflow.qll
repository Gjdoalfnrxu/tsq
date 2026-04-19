/**
 * Bridge library for the value-flow layer (Phase A).
 *
 * Provides a non-recursive `mayResolveTo(valueExpr, sourceExpr)` predicate
 * built from six named branches, unioned at the top level via an
 * `or`-of-calls. Each branch consumes only EDB / extractor-grounded
 * relations — no branch body references `mayResolveTo` itself, so the
 * planner's existing non-recursive sizing path (trivial-IDB pre-pass + P2b
 * sampling estimator) handles cardinality without recursive-IDB work.
 *
 * The `or`-of-named-calls top-level shape is the known-good workaround for
 * disjunction-poisoning bug #166: each branch is its own predicate and the
 * union evaluates as a disjunction of literals each of which is a call to a
 * separate IDB head. That keeps the planner's magic-set rewrite from
 * collapsing the binding-loss case the inline `(A or B)` form triggers.
 *
 * Six branches per docs/design/valueflow-phase-a-plan.md §2:
 *   2.1 mayResolveToBase         — identity on ExprValueSource
 *   2.2 mayResolveToVarInit      — sym whose VarDecl init is a value-source
 *   2.3 mayResolveToAssign       — sym whose Assign rhs is a value-source
 *   2.4 mayResolveToParamBind    — sym is a parameter; arg at call site is value-source
 *   2.5 mayResolveToFieldRead    — FieldRead of a field whose any FieldWrite is value-source
 *   2.6 mayResolveToObjectField  — FieldRead through a VarDecl-bound object literal
 *
 * What Phase A explicitly does NOT cover (plan §6): no recursion, no
 * two-hop var indirection, no spread, no JSX-wrapper unwrap, no call-return
 * composition, no destructure-source, no cross-module, no method/inherit,
 * no type-driven, no effect/Proxy, no await, no `as` cast, no depth bound.
 *
 * The `resolvesToFunctionDirect` derived helper exposes the
 * "callee resolves to a function expression" question the bridge will ask
 * once Phase A PR3 collapses the easy `resolveToObjectExpr*` branches in
 * `tsq_react.qll`. Phase C will replace it with the recursive-aware form.
 */

/**
 * Branch 2.1 — Identity on value-source expressions.
 *
 * Wraps `ExprValueSource` so the union in `mayResolveTo` is a disjunction
 * of named-predicate calls (not raw EDB literals). Keeps the disj-#166
 * workaround clean: every union arm is the same shape (an IDB head call).
 */
predicate mayResolveToBase(int valueExpr, int sourceExpr) {
    ExprValueSource(valueExpr, sourceExpr)
}

/**
 * Branch 2.2 — Var-init step.
 *
 * `valueExpr` references a sym whose `VarDecl` initialiser is itself a
 * value-source. Non-recursive: the inner relation is `ExprValueSource`,
 * not `mayResolveTo`. Two-hop var indirection (`const a = b; const b =
 * {...};`) is intentionally out of scope; needs Phase C recursion.
 */
predicate mayResolveToVarInit(int valueExpr, int sourceExpr) {
    exists(int sym, int initExpr, int varDecl |
        ExprMayRef(valueExpr, sym) and
        VarDecl(varDecl, sym, initExpr, _) and
        ExprValueSource(initExpr, sourceExpr)
    )
}

/**
 * Branch 2.3 — Assign step.
 *
 * `valueExpr` references a sym that has been (re-)assigned a value-source
 * RHS. Uses the `AssignExpr(lhsSym, rhsExpr)` projection added in PR1, not
 * the 3-arity `Assign(lhsNode, rhsExpr, lhsSym)`, so the planner can key
 * the join directly on `lhsSym` without dragging the unused `lhsNode`
 * column through binding inference.
 *
 * No last-write-wins enforcement: every `AssignExpr` row whose RHS is a
 * value-source contributes. Multi-write situations are over-approximated
 * (consistent with the v1 mutation/flow-sensitivity dial in the parent
 * design doc §5).
 */
predicate mayResolveToAssign(int valueExpr, int sourceExpr) {
    exists(int sym, int rhsExpr |
        ExprMayRef(valueExpr, sym) and
        AssignExpr(sym, rhsExpr) and
        ExprValueSource(rhsExpr, sourceExpr)
    )
}

/**
 * Branch 2.4 — Param-binding step.
 *
 * `valueExpr` references a parameter sym; some call site passes a
 * value-source expression as the actual argument at the matching slot.
 * Uses `ParamBinding(fn, paramIdx, paramSym, argExpr)` from PR1 — the
 * 4-arity composition of `CallTarget × CallArg × Parameter` materialised
 * once at extraction time. Carve-outs for spread args / rest params /
 * destructured params are encoded in the extractor-side rule, so this
 * branch does not need to repeat them.
 *
 * Cardinality budget: ParamBinding ≤ 5x CallArg (plan §7.3 budget gate
 * enforced in `extract/rules/valueflow_budget_test.go`).
 */
predicate mayResolveToParamBind(int valueExpr, int sourceExpr) {
    exists(int sym, int fn, int idx, int argExpr |
        ExprMayRef(valueExpr, sym) and
        ParamBinding(fn, idx, sym, argExpr) and
        ExprValueSource(argExpr, sourceExpr)
    )
}

/**
 * Branch 2.5 — Field-read of any field-write of the same `(baseSym, fld)`.
 *
 * Field-name + base-sym match only; no shape recursion (parent design doc
 * §5: "Field-named, no shape" is the v1 default). All writes are
 * may-occur — last-write-wins is not enforced. This is the same precision
 * posture as the existing `TaintedField` rule.
 */
predicate mayResolveToFieldRead(int valueExpr, int sourceExpr) {
    exists(int baseSym, string fld, int rhsExpr, int writeNode |
        FieldRead(valueExpr, baseSym, fld) and
        FieldWrite(writeNode, baseSym, fld, rhsExpr) and
        ExprValueSource(rhsExpr, sourceExpr)
    )
}

/**
 * Branch 2.6 — Object-literal field projection through a single VarDecl.
 *
 * `const o = { k: v }; o.k` resolves to `v`. Single VarDecl indirection,
 * own field only. **No spread, no depth-2 var indirection, no computed
 * key** — those need recursion through `mayResolveTo` (Phase C).
 *
 * This is the Phase A version of "the easy `resolveToObjectExpr` cases"
 * in `tsq_react.qll`. PR3 of the Phase A series will delete the five
 * subsumed bridge predicates listed in plan §3.1.
 */
predicate mayResolveToObjectField(int valueExpr, int sourceExpr) {
    exists(int objExpr, string fld, int fieldValExpr, int baseSym, int varDecl |
        FieldRead(valueExpr, baseSym, fld) and
        VarDecl(varDecl, baseSym, objExpr, _) and
        ObjectLiteralField(objExpr, fld, fieldValExpr) and
        ExprValueSource(fieldValExpr, sourceExpr)
    )
}

/**
 * Top-level union — `or`-of-calls.
 *
 * Each disjunct is a call to a separate named IDB head. This shape
 * sidesteps disjunction-poisoning bug #166 by construction: the planner's
 * disjunction rewrite never sees a multi-branch literal-disjunction inside
 * a single rule body, so the binding-loss case never fires. If a
 * regression appears here in the future (per-branch row count > 0 but
 * union row count = 0), that is the classic #166 signature — escalate to
 * the planner team rather than rewriting the value-flow rules. The
 * regression guard is `TestValueflow_UnionMatchesSumOfBranches` in
 * `valueflow_integration_test.go`.
 */
predicate mayResolveTo(int valueExpr, int sourceExpr) {
    mayResolveToBase(valueExpr, sourceExpr)
    or mayResolveToVarInit(valueExpr, sourceExpr)
    or mayResolveToAssign(valueExpr, sourceExpr)
    or mayResolveToParamBind(valueExpr, sourceExpr)
    or mayResolveToFieldRead(valueExpr, sourceExpr)
    or mayResolveToObjectField(valueExpr, sourceExpr)
}

/**
 * Derived helper — `resolvesToFunctionDirect(callee, fnId)`.
 *
 * Holds when the value-source `callee` may resolve to is a function
 * expression node identified by `fnId`. Phase A surface for the bridge:
 * "is this callee's resolved value-source a function expression node?"
 * Phase C will replace this with a recursive-aware version that follows
 * call-return composition, cross-module imports, and method dispatch.
 *
 * Uses `FunctionSymbol(sym, fn)` to confirm the resolved source is a
 * declared function. The existential over `sourceExpr` keeps the predicate
 * arity at 2 (callee, fnId) — the bridge cares about the function id, not
 * the syntactic source expression.
 */
predicate resolvesToFunctionDirect(int callee, int fnId) {
    exists(int sourceExpr, int sym |
        mayResolveTo(callee, sourceExpr) and
        FunctionSymbol(sym, fnId) and
        sourceExpr = fnId
    )
}
