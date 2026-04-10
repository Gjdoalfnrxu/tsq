/**
 * Bridge library for call-related relations.
 * Maps Call, CallArg, CallArgSpread.
 */

/** A function call or method invocation. */
class Call extends @call {
    Call() { call(this, _, _) }

    /** Gets the callee expression node. */
    ASTNode getCalleeNode() { call(this, result, _) }

    /** Gets the number of arguments. */
    int getArity() { call(this, _, result) }

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

/** An argument passed to a call. */
class CallArg extends @call_arg {
    CallArg() { call_arg(this, _, _) }

    /** Gets the call this argument belongs to. */
    Call getCall() { call_arg(result, _, _) and call_arg(this, _, _) }

    /** Gets the 0-based index of this argument. */
    int getIndex() { call_arg(_, result, _) and call_arg(this, _, _) }

    /** Gets the argument expression node. */
    ASTNode getArgNode() { call_arg(_, _, result) and call_arg(this, _, _) }

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

/** Marks a call argument as a spread argument (...args). */
class CallArgSpread extends @call_arg_spread {
    CallArgSpread() { call_arg_spread(this, _) }

    /** Gets the call. */
    Call getCall() { call_arg_spread(result, _) and call_arg_spread(this, _) }

    /** Gets the argument index. */
    int getIndex() { call_arg_spread(_, result) and call_arg_spread(this, _) }
}
