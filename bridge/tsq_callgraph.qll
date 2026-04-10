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
 * An instantiated class (observed via `new ClassName()`).
 * Used by RTA to prune infeasible call targets.
 */
class Instantiated extends @instantiated {
    Instantiated() { Instantiated(this) }

    /** Gets a textual representation. */
    string toString() { result = "Instantiated" }
}
