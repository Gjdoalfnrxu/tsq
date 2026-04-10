/**
 * Bridge library for type-aware relations (v2).
 * Maps ClassDecl, InterfaceDecl, Implements, Extends, MethodDecl,
 * MethodCall, NewExpr, ExprType, TypeDecl.
 */

/** A class declaration. */
class ClassDecl extends @class_decl {
    ClassDecl() { ClassDecl(this, _, _) }

    /** Gets the class name. */
    string getName() { ClassDecl(this, result, _) }

    /** Gets the file containing this class. */
    File getFile() { ClassDecl(this, _, result) }

    /** Gets a method declared in this class. */
    MethodDecl getAMethod() { result.getClassOrInterface() = this }

    /** Holds if this class extends another class. */
    predicate hasParent() { Extends(this, _) }

    /** Gets the parent class (if any). */
    int getParent() { Extends(this, result) }

    /** Gets a textual representation. */
    string toString() { result = this.getName() }
}

/** An interface declaration. */
class InterfaceDecl extends @interface_decl {
    InterfaceDecl() { InterfaceDecl(this, _, _) }

    /** Gets the interface name. */
    string getName() { InterfaceDecl(this, result, _) }

    /** Gets the file containing this interface. */
    File getFile() { InterfaceDecl(this, _, result) }

    /** Gets a method declared in this interface. */
    MethodDecl getAMethod() { result.getClassOrInterface() = this }

    /** Gets a textual representation. */
    string toString() { result = this.getName() }
}

/** An implements relationship: classId implements interfaceId. */
class Implements extends @implements {
    Implements() { Implements(this, _) }

    /** Gets the class. */
    ClassDecl getClass() { result = this }

    /** Gets the implemented interface. */
    int getInterface() { Implements(this, result) }
}

/** An extends relationship: childId extends parentId. */
class Extends extends @extends {
    Extends() { Extends(this, _) }

    /** Gets the child class or interface. */
    int getChild() { result = this }

    /** Gets the parent class or interface. */
    int getParent() { Extends(this, result) }
}

/** A method declaration inside a class or interface. */
class MethodDecl extends @method_decl {
    MethodDecl() { MethodDecl(this, _, _) }

    /** Gets the containing class or interface. */
    int getClassOrInterface() { result = this }

    /** Gets the method name. */
    string getName() { MethodDecl(this, result, _) }

    /** Gets the function node for this method. */
    Function getFunction() { MethodDecl(this, _, result) }

    /** Gets a textual representation. */
    string toString() { result = this.getName() }
}

/** A method call expression (obj.method()). */
class MethodCall extends @method_call {
    MethodCall() { MethodCall(this, _, _) }

    /** Gets the receiver expression. */
    ASTNode getReceiver() { MethodCall(this, result, _) }

    /** Gets the method name. */
    string getMethodName() { MethodCall(this, _, result) }

    /** Gets a textual representation. */
    string toString() { result = this.getMethodName() }
}

/** A new expression (new ClassName()). */
class NewExpr extends @new_expr {
    NewExpr() { NewExpr(this, _) }

    /** Gets the class being instantiated. */
    int getClass() { NewExpr(this, result) }

    /** Gets a textual representation. */
    string toString() { result = "new" }
}

/**
 * An expression type assignment (requires tsgo).
 * Empty when tsgo is unavailable — structural emission only provides this
 * as a placeholder for future semantic enrichment.
 */
class ExprType extends @expr_type {
    ExprType() { ExprType(this, _) }

    /** Gets the expression. */
    ASTNode getExpr() { result = this }

    /** Gets the type. */
    int getType() { ExprType(this, result) }
}

/** A type declaration (type alias, enum, etc.). */
class TypeDecl extends @type_decl {
    TypeDecl() { TypeDecl(this, _, _, _) }

    /** Gets the type name. */
    string getName() { TypeDecl(this, result, _, _) }

    /** Gets the kind of type (e.g. "alias"). */
    string getTypeKind() { TypeDecl(this, _, result, _) }

    /** Gets the file containing this type. */
    File getFile() { TypeDecl(this, _, _, result) }

    /** Gets a textual representation. */
    string toString() { result = this.getName() }
}
