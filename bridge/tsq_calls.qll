/**
 * Bridge library for call-related relations.
 * Maps Call, CallArg, CallArgSpread.
 */

/** A function call or method invocation. */
class Call extends @call {
    Call() { Call(this, _, _) }

    /** Gets the callee expression node. */
    ASTNode getCalleeNode() { Call(this, result, _) }

    /** Gets the number of arguments. */
    int getArity() { Call(this, _, result) }

    /** Gets an argument to this call. */
    CallArg getAnArgument() { result.getCall() = this }

    /** Gets the argument at the given index. */
    CallArg getArgument(int idx) {
        result.getCall() = this and
        result.getIndex() = idx
    }

    /** Gets a textual representation of this call. */
    string toString() { result = "call" }
}

/**
 * An argument passed to a call.
 *
 * NOTE: `this` binds to col 0 (call), which is not a unique identifier.
 * Multiple arguments to the same call share the same col-0 value, so they
 * collapse into a single QL entity.  This is a known v1 limitation —
 * resolving it requires adding a composite key or synthetic id column.
 */
class CallArg extends @call_arg {
    CallArg() { CallArg(this, _, _) }

    /** Gets the call this argument belongs to. */
    Call getCall() { result = this }

    /** Gets the 0-based index of this argument. */
    int getIndex() { CallArg(this, result, _) }

    /** Gets the argument expression node. */
    ASTNode getArgNode() { CallArg(this, _, result) }

    /** Holds if this argument is a spread argument. */
    predicate isSpread() {
        exists(CallArgSpread cas |
            cas.getCall() = this.getCall() and
            cas.getIndex() = this.getIndex()
        )
    }

    /** Gets a textual representation of this argument. */
    string toString() { result = "arg" }
}

/**
 * Marks a call argument as a spread argument (...args).
 *
 * NOTE: `this` binds to col 0 (call), not a unique id.
 * Same entity-collapse caveat as CallArg.
 */
class CallArgSpread extends @call_arg_spread {
    CallArgSpread() { CallArgSpread(this, _) }

    /** Gets the call. */
    Call getCall() { result = this }

    /** Gets the argument index. */
    int getIndex() { CallArgSpread(this, result) }
}
