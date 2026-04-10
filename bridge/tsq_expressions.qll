/**
 * Bridge library for expression-related relations.
 * Maps ExprMayRef, ExprIsCall, FieldRead, FieldWrite, Await, Cast,
 * DestructureField, ArrayDestructure, DestructureRest.
 */

/** An expression that may reference a symbol. */
class ExprMayRef extends @expr_may_ref {
    ExprMayRef() { expr_may_ref(this, _) }

    /** Gets the expression node. */
    ASTNode getExpr() { result = this }

    /** Gets the symbol this expression may reference. */
    int getSym() { expr_may_ref(_, result) and expr_may_ref(this, _) }

    /** Gets a textual representation. */
    string toString() { result = "expr_may_ref" }
}

/** An expression that is also a call. */
class ExprIsCall extends @expr_is_call {
    ExprIsCall() { expr_is_call(this, _) }

    /** Gets the expression node. */
    ASTNode getExpr() { result = this }

    /** Gets the corresponding call. */
    Call getCall() { expr_is_call(_, result) and expr_is_call(this, _) }

    /** Gets a textual representation. */
    string toString() { result = "expr_is_call" }
}

/** A field read access (e.g. obj.field). */
class FieldRead extends @field_read {
    FieldRead() { field_read(this, _, _) }

    /** Gets the expression node. */
    ASTNode getExpr() { result = this }

    /** Gets the base symbol. */
    int getBaseSym() { field_read(_, result, _) and field_read(this, _, _) }

    /** Gets the field name. */
    string getFieldName() { field_read(_, _, result) and field_read(this, _, _) }

    /** Gets a textual representation. */
    string toString() { result = "." + this.getFieldName() }
}

/** A field write access (e.g. obj.field = value). */
class FieldWrite extends @field_write {
    FieldWrite() { field_write(this, _, _, _) }

    /** Gets the assignment node. */
    ASTNode getAssignNode() { result = this }

    /** Gets the base symbol. */
    int getBaseSym() { field_write(_, result, _, _) and field_write(this, _, _, _) }

    /** Gets the field name. */
    string getFieldName() { field_write(_, _, result, _) and field_write(this, _, _, _) }

    /** Gets the right-hand side expression. */
    ASTNode getRhsExpr() { field_write(_, _, _, result) and field_write(this, _, _, _) }

    /** Gets a textual representation. */
    string toString() { result = "." + this.getFieldName() + " =" }
}

/** An await expression. */
class Await extends @await {
    Await() { await(this, _) }

    /** Gets the outer expression node. */
    ASTNode getExpr() { result = this }

    /** Gets the inner (awaited) expression. */
    ASTNode getInnerExpr() { await(_, result) and await(this, _) }

    /** Gets a textual representation. */
    string toString() { result = "await" }
}

/** A type cast expression (as / satisfies). */
class Cast extends @cast {
    Cast() { cast(this, _) }

    /** Gets the outer expression node. */
    ASTNode getExpr() { result = this }

    /** Gets the inner expression. */
    ASTNode getInnerExpr() { cast(_, result) and cast(this, _) }

    /** Gets a textual representation. */
    string toString() { result = "cast" }
}

/** A field in a destructuring pattern ({ key: binding }). */
class DestructureField extends @destructure_field {
    DestructureField() { destructure_field(this, _, _, _, _) }

    /** Gets the parent pattern node. */
    ASTNode getParent() { result = this }

    /** Gets the source field name. */
    string getSourceField() { destructure_field(_, result, _, _, _) and destructure_field(this, _, _, _, _) }

    /** Gets the binding name. */
    string getBindName() { destructure_field(_, _, result, _, _) and destructure_field(this, _, _, _, _) }

    /** Gets the binding symbol. */
    int getBindSym() { destructure_field(_, _, _, result, _) and destructure_field(this, _, _, _, _) }

    /** Gets the field index. */
    int getIndex() { destructure_field(_, _, _, _, result) and destructure_field(this, _, _, _, _) }

    /** Gets a textual representation. */
    string toString() { result = this.getSourceField() + ": " + this.getBindName() }
}

/** An element in an array destructuring pattern ([a, b]). */
class ArrayDestructure extends @array_destructure {
    ArrayDestructure() { array_destructure(this, _, _) }

    /** Gets the parent pattern node. */
    ASTNode getParent() { result = this }

    /** Gets the element index. */
    int getIndex() { array_destructure(_, result, _) and array_destructure(this, _, _) }

    /** Gets the binding symbol. */
    int getBindSym() { array_destructure(_, _, result) and array_destructure(this, _, _) }

    /** Gets a textual representation. */
    string toString() { result = "array_destructure" }
}

/** A rest element in a destructuring pattern (...rest). */
class DestructureRest extends @destructure_rest {
    DestructureRest() { destructure_rest(this, _) }

    /** Gets the parent pattern node. */
    ASTNode getParent() { result = this }

    /** Gets the binding symbol. */
    int getBindSym() { destructure_rest(_, result) and destructure_rest(this, _) }

    /** Gets a textual representation. */
    string toString() { result = "...rest" }
}
