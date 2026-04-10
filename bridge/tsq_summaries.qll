/**
 * Bridge library for function-level summary relations (v2 Phase C2).
 * Maps summary relations derived from system Datalog rules that combine
 * intra-procedural LocalFlowStar with function boundary facts.
 */

/**
 * Holds when parameter at index `paramIdx` of function `fnId` flows
 * to the function's return value.
 */
class ParamToReturn extends @param_to_return {
    ParamToReturn() { ParamToReturn(this, _) }

    /** Gets the enclosing function. */
    int getFunction() { result = this }

    /** Gets the parameter index. */
    int getParamIdx() { ParamToReturn(this, result) }

    /** Gets a textual representation. */
    string toString() { result = "ParamToReturn" }
}

/**
 * Holds when parameter at index `paramIdx` of function `fnId` flows
 * to argument at index `argIdx` of a callee identified by `calleeSym`.
 */
class ParamToCallArg extends @param_to_call_arg {
    ParamToCallArg() { ParamToCallArg(this, _, _, _) }

    /** Gets the enclosing function. */
    int getFunction() { result = this }

    /** Gets the parameter index. */
    int getParamIdx() { ParamToCallArg(this, result, _, _) }

    /** Gets the callee symbol. */
    int getCalleeSym() { ParamToCallArg(this, _, result, _) }

    /** Gets the argument index. */
    int getArgIdx() { ParamToCallArg(this, _, _, result) }

    /** Gets a textual representation. */
    string toString() { result = "ParamToCallArg" }
}

/**
 * Holds when parameter at index `paramIdx` of function `fnId` flows
 * to a field write with the given `fieldName`.
 */
class ParamToFieldWrite extends @param_to_field_write {
    ParamToFieldWrite() { ParamToFieldWrite(this, _, _) }

    /** Gets the enclosing function. */
    int getFunction() { result = this }

    /** Gets the parameter index. */
    int getParamIdx() { ParamToFieldWrite(this, result, _) }

    /** Gets the field name. */
    string getFieldName() { ParamToFieldWrite(this, _, result) }

    /** Gets a textual representation. */
    string toString() { result = "ParamToFieldWrite" }
}

/**
 * Holds when parameter at index `paramIdx` of function `fnId` reaches
 * a taint sink of the given `sinkKind`.
 * Inactive until Phase D populates TaintSink.
 */
class ParamToSink extends @param_to_sink {
    ParamToSink() { ParamToSink(this, _, _) }

    /** Gets the enclosing function. */
    int getFunction() { result = this }

    /** Gets the parameter index. */
    int getParamIdx() { ParamToSink(this, result, _) }

    /** Gets the sink kind. */
    string getSinkKind() { ParamToSink(this, _, result) }

    /** Gets a textual representation. */
    string toString() { result = "ParamToSink" }
}

/**
 * Holds when a taint source of the given `sourceKind` within function `fnId`
 * flows to the function's return value.
 * Inactive until Phase D populates TaintSource.
 */
class SourceToReturn extends @source_to_return {
    SourceToReturn() { SourceToReturn(this, _) }

    /** Gets the enclosing function. */
    int getFunction() { result = this }

    /** Gets the source kind. */
    string getSourceKind() { SourceToReturn(this, result) }

    /** Gets a textual representation. */
    string toString() { result = "SourceToReturn" }
}

/**
 * Holds when the return value of a call within function `fnId` flows
 * to the function's own return value.
 */
class CallReturnToReturn extends @call_return_to_return {
    CallReturnToReturn() { CallReturnToReturn(this, _) }

    /** Gets the enclosing function. */
    int getFunction() { result = this }

    /** Gets the call node. */
    int getCall() { CallReturnToReturn(this, result) }

    /** Gets a textual representation. */
    string toString() { result = "CallReturnToReturn" }
}
