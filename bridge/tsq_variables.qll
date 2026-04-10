/**
 * Bridge library for variable-related relations.
 * Maps VarDecl, Assign.
 */

/** A variable declaration (let, const, var). */
class VarDecl extends @var_decl {
    VarDecl() { var_decl(this, _, _, _) }

    /** Gets the symbol for this variable. */
    int getSym() { var_decl(this, result, _, _) }

    /** Gets the initializer expression, if any. */
    ASTNode getInitExpr() { var_decl(this, _, result, _) }

    /** Holds if this is a const declaration. */
    predicate isConst() { var_decl(this, _, _, 1) }

    /** Gets a textual representation of this declaration. */
    string toString() { result = "var_decl" }
}

/** An assignment expression. */
class Assign extends @assign {
    Assign() { assign(this, _, _) }

    /** Gets the left-hand side node. */
    ASTNode getLhsNode() { result = this }

    /** Gets the right-hand side expression. */
    ASTNode getRhsExpr() { assign(_, result, _) and assign(this, _, _) }

    /** Gets the symbol being assigned to. */
    int getLhsSym() { assign(_, _, result) and assign(this, _, _) }

    /** Gets a textual representation of this assignment. */
    string toString() { result = "assign" }
}
