/**
 * Bridge library for expression-related relations.
 * Maps ExprMayRef, ExprIsCall, FieldRead, FieldWrite, Await, Cast,
 * DestructureField, ArrayDestructure, DestructureRest.
 */

/** An expression that may reference a symbol. */
class ExprMayRef extends @expr_may_ref {
    ExprMayRef() { ExprMayRef(this, _) }

    /** Gets the expression node. */
    ASTNode getExpr() { result = this }

    /** Gets the symbol this expression may reference. */
    int getSym() { ExprMayRef(this, result) }

    /** Gets a textual representation. */
    string toString() { result = "expr_may_ref" }
}

/** An expression that is also a call. */
class ExprIsCall extends @expr_is_call {
    ExprIsCall() { ExprIsCall(this, _) }

    /** Gets the expression node. */
    ASTNode getExpr() { result = this }

    /** Gets the corresponding call. */
    Call getCall() { ExprIsCall(this, result) }

    /** Gets a textual representation. */
    string toString() { result = "expr_is_call" }
}

/** A field read access (e.g. obj.field). */
class FieldRead extends @field_read {
    FieldRead() { FieldRead(this, _, _) }

    /** Gets the expression node. */
    ASTNode getExpr() { result = this }

    /** Gets the base symbol. */
    int getBaseSym() { FieldRead(this, result, _) }

    /** Gets the field name. */
    string getFieldName() { FieldRead(this, _, result) }

    /** Gets a textual representation. */
    string toString() { result = "." + this.getFieldName() }
}

/** A field write access (e.g. obj.field = value). */
class FieldWrite extends @field_write {
    FieldWrite() { FieldWrite(this, _, _, _) }

    /** Gets the assignment node. */
    ASTNode getAssignNode() { result = this }

    /** Gets the base symbol. */
    int getBaseSym() { FieldWrite(this, result, _, _) }

    /** Gets the field name. */
    string getFieldName() { FieldWrite(this, _, result, _) }

    /** Gets the right-hand side expression. */
    ASTNode getRhsExpr() { FieldWrite(this, _, _, result) }

    /** Gets a textual representation. */
    string toString() { result = "." + this.getFieldName() + " =" }
}

/** An await expression. */
class Await extends @await {
    Await() { Await(this, _) }

    /** Gets the outer expression node. */
    ASTNode getExpr() { result = this }

    /** Gets the inner (awaited) expression. */
    ASTNode getInnerExpr() { Await(this, result) }

    /** Gets a textual representation. */
    string toString() { result = "await" }
}

/** A type cast expression (as / satisfies). */
class Cast extends @cast {
    Cast() { Cast(this, _) }

    /** Gets the outer expression node. */
    ASTNode getExpr() { result = this }

    /** Gets the inner expression. */
    ASTNode getInnerExpr() { Cast(this, result) }

    /** Gets a textual representation. */
    string toString() { result = "cast" }
}

/**
 * A field in an object literal expression ({ a, b: expr }).
 *
 * For shorthand `{ foo }` the fieldName equals the binding name and
 * valueExpr is the Identifier node (which has its own ExprMayRef row).
 * For `{ foo: expr }` the fieldName is the source key and valueExpr is the
 * value-position expression. Spread elements (`{ ...rest }`) and computed-key
 * properties are skipped by the extractor — v1 limitation.
 *
 * NOTE: `this` binds to col 0 (parent), which is not a unique identifier.
 * Multiple fields in the same object literal share the same col-0 value,
 * causing entity collapse. Same v1 limitation as DestructureField.
 */
class ObjectLiteralField extends @object_literal_field {
    ObjectLiteralField() { ObjectLiteralField(this, _, _) }

    /** Gets the parent object-literal node. */
    ASTNode getParent() { result = this }

    /** Gets the field name (shorthand binding name OR source key). */
    string getFieldName() { ObjectLiteralField(this, result, _) }

    /** Gets the value-position expression node. */
    ASTNode getValueExpr() { ObjectLiteralField(this, _, result) }

    /** Gets a textual representation. */
    string toString() { result = this.getFieldName() }
}

/**
 * A field in a destructuring pattern ({ key: binding }).
 *
 * NOTE: `this` binds to col 0 (parent), which is not a unique identifier.
 * Multiple fields in the same destructuring share the same col-0 value,
 * causing entity collapse.  Known v1 limitation.
 */
class DestructureField extends @destructure_field {
    DestructureField() { DestructureField(this, _, _, _, _) }

    /** Gets the parent pattern node. */
    ASTNode getParent() { result = this }

    /** Gets the source field name. */
    string getSourceField() { DestructureField(this, result, _, _, _) }

    /** Gets the binding name. */
    string getBindName() { DestructureField(this, _, result, _, _) }

    /** Gets the binding symbol. */
    int getBindSym() { DestructureField(this, _, _, result, _) }

    /** Gets the field index. */
    int getIndex() { DestructureField(this, _, _, _, result) }

    /** Gets a textual representation. */
    string toString() { result = this.getSourceField() + ": " + this.getBindName() }
}

/**
 * An element in an array destructuring pattern ([a, b]).
 *
 * NOTE: `this` binds to col 0 (parent), not a unique id.
 * Same entity-collapse caveat as DestructureField.
 */
class ArrayDestructure extends @array_destructure {
    ArrayDestructure() { ArrayDestructure(this, _, _) }

    /** Gets the parent pattern node. */
    ASTNode getParent() { result = this }

    /** Gets the element index. */
    int getIndex() { ArrayDestructure(this, result, _) }

    /** Gets the binding symbol. */
    int getBindSym() { ArrayDestructure(this, _, result) }

    /** Gets a textual representation. */
    string toString() { result = "array_destructure" }
}

/**
 * A rest element in a destructuring pattern (...rest).
 *
 * NOTE: `this` binds to col 0 (parent), not a unique id.
 * Same entity-collapse caveat as DestructureField.
 */
class DestructureRest extends @destructure_rest {
    DestructureRest() { DestructureRest(this, _) }

    /** Gets the parent pattern node. */
    ASTNode getParent() { result = this }

    /** Gets the binding symbol. */
    int getBindSym() { DestructureRest(this, result) }

    /** Gets a textual representation. */
    string toString() { result = "...rest" }
}
