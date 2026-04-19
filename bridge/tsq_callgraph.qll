/**
 * Bridge library for call graph derived relations (v2 Phase B).
 * Maps CallTarget (CHA), CallTargetRTA, and Instantiated derived from
 * system Datalog rules over base type-aware facts.
 */

/**
 * A resolved call target (CHA — Class Hierarchy Analysis).
 * Holds when `call` may invoke `fn` based on class hierarchy analysis,
 * including direct calls, concrete method dispatch, and interface dispatch
 * to all implementing classes.
 */
class CallTarget extends @call_target {
    CallTarget() { CallTarget(this, _) }

    /** Gets the call site. */
    int getCall() { result = this }

    /** Gets the target function. */
    int getFunction() { CallTarget(this, result) }

    /** Gets a textual representation. */
    string toString() { result = "CallTarget" }
}

/**
 * A resolved call target (RTA — Rapid Type Analysis).
 * Stricter than CHA: only includes targets where the implementing class
 * has been instantiated (via `new`) somewhere in the program.
 */
class CallTargetRTA extends @call_target_rta {
    CallTargetRTA() { CallTargetRTA(this, _) }

    /** Gets the call site. */
    int getCall() { result = this }

    /** Gets the target function. */
    int getFunction() { CallTargetRTA(this, result) }

    /** Gets a textual representation. */
    string toString() { result = "CallTargetRTA" }
}

/**
 * A cross-module-resolved call target.
 * Holds when `call`'s callee is an imported local symbol that resolves
 * through one import/export hop to function `fn` in another module.
 * Populated as a system Datalog rule in `extract/rules/valueflow.go` from
 * the join `CallCalleeSym × ImportBinding × ExportBinding × FunctionSymbol`.
 *
 * Name-keyed (the import/export join ignores module specifier), so two
 * modules that export the same name will cross-bridge — same posture as
 * the legacy `importedFunctionSymbol` predicate in `tsq_react.qll`.
 * Tightening requires a real module resolver, deferred indefinitely from
 * Phase C (see `docs/design/valueflow-phase-c-plan.md` §3.2 / §4.1).
 *
 * Phase C PR3 lands the first consumer: the `ifsRetToCall` inter-
 * procedural step rule (extract/rules/interflowstep.go) that PR4's
 * recursive `mayResolveTo` will close over.
 */
class CallTargetCrossModule extends @call_target_cross_module {
    CallTargetCrossModule() { CallTargetCrossModule(this, _) }

    /** Gets the call site. */
    int getCall() { result = this }

    /** Gets the cross-module target function. */
    int getFunction() { CallTargetCrossModule(this, result) }

    /** Gets a textual representation. */
    string toString() { result = "CallTargetCrossModule" }
}

/**
 * An instantiated class (observed via `new ClassName()`).
 * Used by RTA to prune infeasible call targets.
 */
class Instantiated extends @instantiated {
    Instantiated() { Instantiated(this) }

    /** Gets a textual representation. */
    string toString() { result = "Instantiated" }
}
