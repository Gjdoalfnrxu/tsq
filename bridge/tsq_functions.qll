/**
 * Bridge library for function-related relations.
 * Maps Function, Parameter, ParameterRest, ParameterOptional, ParamIsFunctionType.
 */

/** A function declaration or expression. */
class Function extends @function {
    Function() { Function(this, _, _, _, _, _) }

    /** Gets the function name (may be empty for anonymous functions). */
    string getName() { Function(this, result, _, _, _, _) }

    /** Holds if this is an arrow function. */
    predicate isArrow() { Function(this, _, 1, _, _, _) }

    /** Holds if this is an async function. */
    predicate isAsync() { Function(this, _, _, 1, _, _) }

    /** Holds if this is a generator function. */
    predicate isGenerator() { Function(this, _, _, _, 1, _) }

    /** Holds if this is a method. */
    predicate isMethod() { Function(this, _, _, _, _, 1) }

    /** Gets a parameter of this function. */
    Parameter getAParameter() { result.getFunction() = this }

    /** Gets the parameter at the given index. */
    Parameter getParameter(int idx) {
        result.getFunction() = this and
        result.getIndex() = idx
    }

    /** Gets a textual representation of this function. */
    string toString() { result = this.getName() }
}

/**
 * A function parameter.
 *
 * NOTE: `this` binds to col 0 (fn), which is not a unique identifier.
 * Multiple parameters of the same function share the same col-0 value,
 * causing entity collapse.  Known v1 limitation.
 */
class Parameter extends @parameter {
    Parameter() { Parameter(this, _, _, _, _, _) }

    /** Gets the function this parameter belongs to. */
    Function getFunction() { result = this }

    /** Gets the 0-based index of this parameter. */
    int getIndex() { Parameter(this, result, _, _, _, _) }

    /** Gets the parameter name. */
    string getName() { Parameter(this, _, result, _, _, _) }

    /** Gets the parameter node. */
    ASTNode getNode() { Parameter(this, _, _, result, _, _) }

    /** Gets the symbol for this parameter. */
    int getSym() { Parameter(this, _, _, _, result, _) }

    /** Gets the type annotation text. */
    string getTypeText() { Parameter(this, _, _, _, _, result) }

    /** Holds if this is a rest parameter. */
    predicate isRest() {
        exists(ParameterRest pr |
            pr.getFunction() = this.getFunction() and
            pr.getIndex() = this.getIndex()
        )
    }

    /** Holds if this is an optional parameter. */
    predicate isOptional() {
        exists(ParameterOptional po |
            po.getFunction() = this.getFunction() and
            po.getIndex() = this.getIndex()
        )
    }

    /** Gets a textual representation of this parameter. */
    string toString() { result = this.getName() }
}

/**
 * Marks a parameter as a rest parameter (...args).
 *
 * NOTE: `this` binds to col 0 (fn), not a unique id.
 * Same entity-collapse caveat as Parameter.
 */
class ParameterRest extends @parameter_rest {
    ParameterRest() { ParameterRest(this, _) }

    /** Gets the function. */
    Function getFunction() { result = this }

    /** Gets the parameter index. */
    int getIndex() { ParameterRest(this, result) }
}

/**
 * Marks a parameter as optional (arg?).
 *
 * NOTE: `this` binds to col 0 (fn), not a unique id.
 * Same entity-collapse caveat as Parameter.
 */
class ParameterOptional extends @parameter_optional {
    ParameterOptional() { ParameterOptional(this, _) }

    /** Gets the function. */
    Function getFunction() { result = this }

    /** Gets the parameter index. */
    int getIndex() { ParameterOptional(this, result) }
}

/**
 * Marks a parameter type as a function type.
 *
 * NOTE: `this` binds to col 0 (fn), not a unique id.
 * Same entity-collapse caveat as Parameter.
 */
class ParamIsFunctionType extends @param_is_function_type {
    ParamIsFunctionType() { ParamIsFunctionType(this, _) }

    /** Gets the function. */
    Function getFunction() { result = this }

    /** Gets the parameter index. */
    int getIndex() { ParamIsFunctionType(this, result) }
}
