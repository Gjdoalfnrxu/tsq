/**
 * Bridge library for function-related relations.
 * Maps Function, Parameter, ParameterRest, ParameterOptional, ParamIsFunctionType.
 */

/** A function declaration or expression. */
class Function extends @function {
    Function() { function(this, _, _, _, _, _) }

    /** Gets the function name (may be empty for anonymous functions). */
    string getName() { function(this, result, _, _, _, _) }

    /** Holds if this is an arrow function. */
    predicate isArrow() { function(this, _, 1, _, _, _) }

    /** Holds if this is an async function. */
    predicate isAsync() { function(this, _, _, 1, _, _) }

    /** Holds if this is a generator function. */
    predicate isGenerator() { function(this, _, _, _, 1, _) }

    /** Holds if this is a method. */
    predicate isMethod() { function(this, _, _, _, _, 1) }

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

/** A function parameter. */
class Parameter extends @parameter {
    Parameter() { parameter(this, _, _, _, _, _) }

    /** Gets the function this parameter belongs to. */
    Function getFunction() { parameter(result, _, _, _, _, _) and parameter(this, _, _, _, _, _) }

    /** Gets the 0-based index of this parameter. */
    int getIndex() { parameter(_, result, _, _, _, _) and parameter(this, _, _, _, _, _) }

    /** Gets the parameter name. */
    string getName() { parameter(_, _, result, _, _, _) and parameter(this, _, _, _, _, _) }

    /** Gets the parameter node. */
    ASTNode getNode() { parameter(_, _, _, result, _, _) and parameter(this, _, _, _, _, _) }

    /** Gets the symbol for this parameter. */
    int getSym() { parameter(_, _, _, _, result, _) and parameter(this, _, _, _, _, _) }

    /** Gets the type annotation text. */
    string getTypeText() { parameter(_, _, _, _, _, result) and parameter(this, _, _, _, _, _) }

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

/** Marks a parameter as a rest parameter (...args). */
class ParameterRest extends @parameter_rest {
    ParameterRest() { parameter_rest(this, _) }

    /** Gets the function. */
    Function getFunction() { parameter_rest(result, _) and parameter_rest(this, _) }

    /** Gets the parameter index. */
    int getIndex() { parameter_rest(_, result) and parameter_rest(this, _) }
}

/** Marks a parameter as optional (arg?). */
class ParameterOptional extends @parameter_optional {
    ParameterOptional() { parameter_optional(this, _) }

    /** Gets the function. */
    Function getFunction() { parameter_optional(result, _) and parameter_optional(this, _) }

    /** Gets the parameter index. */
    int getIndex() { parameter_optional(_, result) and parameter_optional(this, _) }
}

/** Marks a parameter type as a function type. */
class ParamIsFunctionType extends @param_is_function_type {
    ParamIsFunctionType() { param_is_function_type(this, _) }

    /** Gets the function. */
    Function getFunction() { param_is_function_type(result, _) and param_is_function_type(this, _) }

    /** Gets the parameter index. */
    int getIndex() { param_is_function_type(_, result) and param_is_function_type(this, _) }
}
