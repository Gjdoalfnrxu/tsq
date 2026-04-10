/**
 * CodeQL-compatible JavaScript/TypeScript standard library.
 * Clean-room implementation providing CodeQL API surface
 * backed by tsq's fact relations.
 *
 * This file is NOT derived from CodeQL source code.
 * API surface documented from public CodeQL documentation at
 * https://codeql.github.com/docs/ and public query examples.
 */

// ---- Core AST ----

/** An AST node in the source code. */
class ASTNode extends @node {
    ASTNode() { Node(this, _, _, _, _, _, _) }

    /** Gets the file containing this node. */
    File getFile() { Node(this, result, _, _, _, _, _) }

    /** Gets the syntactic kind of this node (e.g. "CallExpression"). */
    string getKind() { Node(this, _, result, _, _, _, _) }

    /** Gets the start line (1-based). */
    int getStartLine() { Node(this, _, _, result, _, _, _) }

    /** Gets the start column (0-based). */
    int getStartCol() { Node(this, _, _, _, result, _, _) }

    /** Gets the end line (1-based). */
    int getEndLine() { Node(this, _, _, _, _, result, _) }

    /** Gets the end column (0-based). */
    int getEndCol() { Node(this, _, _, _, _, _, result) }

    /** Gets a textual representation of this node. */
    string toString() { result = this.getKind() }
}

/** A source file in the extraction database. */
class File extends @file {
    File() { File(this, _, _) }

    /** Gets the file path. */
    string getPath() { File(this, result, _) }

    /** Gets the content hash. */
    string getContentHash() { File(this, _, result) }

    /** Gets a textual representation of this file. */
    string toString() { result = this.getPath() }
}

// ---- Functions ----

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

    /** Gets a textual representation of this function. */
    string toString() { result = this.getName() }
}

// ---- Calls (CodeQL-compatible names) ----

/** A function call expression. */
class CallExpr extends @call {
    CallExpr() { Call(this, _, _) }

    /** Gets the callee expression node. */
    ASTNode getCallee() { Call(this, result, _) }

    /** Gets the number of arguments. */
    int getNumArgument() { Call(this, _, result) }

    /** Gets the i-th argument expression node. */
    ASTNode getArgument(int i) { CallArg(this, i, result) }

    /** Gets an argument expression node. */
    ASTNode getAnArgument() { CallArg(this, _, result) }

    /** Gets a textual representation. */
    string toString() { result = "call" }
}

/** A method call expression (e.g., `obj.method(args)`). */
class MethodCallExpr extends @method_call {
    MethodCallExpr() { MethodCall(this, _, _) }

    /** Gets the method name. */
    string getMethodName() { MethodCall(this, _, result) }

    /** Gets the receiver expression. */
    ASTNode getReceiver() { MethodCall(this, result, _) }

    /** Gets a textual representation. */
    string toString() { result = this.getMethodName() }
}

/** A `new` expression. */
class NewExpr extends @new_expr {
    NewExpr() { NewExpr(this, _) }

    /** Gets the class being instantiated. */
    int getCallee() { NewExpr(this, result) }

    /** Gets a textual representation. */
    string toString() { result = "new" }
}

// ---- Variables ----

/** A variable declaration. */
class VarDef extends @var_decl {
    VarDef() { VarDecl(this, _, _, _) }

    /** Gets the symbol for this variable. */
    int getSym() { VarDecl(this, result, _, _) }

    /** Gets the initializer expression, if any. */
    ASTNode getInit() { VarDecl(this, _, result, _) }

    /** Holds if this is a const declaration. */
    predicate isConst() { VarDecl(this, _, _, 1) }

    /** Gets a textual representation. */
    string toString() { result = "var_decl" }
}

/** A variable access expression (an expression that may reference a symbol). */
class VarAccess extends @expr_may_ref {
    VarAccess() { ExprMayRef(this, _) }

    /** Gets the symbol this expression may reference. */
    int getSym() { ExprMayRef(this, result) }

    /** Gets a textual representation. */
    string toString() { result = "var_access" }
}

/** An assignment expression. */
class AssignExpr extends @assign {
    AssignExpr() { Assign(this, _, _) }

    /** Gets the left-hand side node. */
    ASTNode getLhs() { result = this }

    /** Gets the right-hand side expression. */
    ASTNode getRhs() { Assign(this, result, _) }

    /** Gets the symbol being assigned to. */
    int getLhsSym() { Assign(this, _, result) }

    /** Gets a textual representation. */
    string toString() { result = "assign" }
}

// ---- Expressions ----

/** A property access expression (e.g., `obj.prop`). */
class PropAccess extends @field_read {
    PropAccess() { FieldRead(this, _, _) }

    /** Gets the base symbol. */
    int getBaseSym() { FieldRead(this, result, _) }

    /** Gets the property name. */
    string getPropertyName() { FieldRead(this, _, result) }

    /** Gets a textual representation. */
    string toString() { result = "." + this.getPropertyName() }
}

/** A field write expression (e.g., `obj.field = value`). */
class PropWrite extends @field_write {
    PropWrite() { FieldWrite(this, _, _, _) }

    /** Gets the base symbol. */
    int getBaseSym() { FieldWrite(this, result, _, _) }

    /** Gets the property name. */
    string getPropertyName() { FieldWrite(this, _, result, _) }

    /** Gets the right-hand side expression. */
    ASTNode getRhs() { FieldWrite(this, _, _, result) }

    /** Gets a textual representation. */
    string toString() { result = "." + this.getPropertyName() + " =" }
}

/** An await expression. */
class AwaitExpr extends @await {
    AwaitExpr() { Await(this, _) }

    /** Gets the inner (awaited) expression. */
    ASTNode getOperand() { Await(this, result) }

    /** Gets a textual representation. */
    string toString() { result = "await" }
}

// ---- Types ----

/** A class definition. */
class ClassDefinition extends @class_decl {
    ClassDefinition() { ClassDecl(this, _, _) }

    /** Gets the class name. */
    string getName() { ClassDecl(this, result, _) }

    /** Gets the file containing this class. */
    File getFile() { ClassDecl(this, _, result) }

    /** Gets a textual representation. */
    string toString() { result = this.getName() }
}

/** An interface declaration. */
class InterfaceDefinition extends @interface_decl {
    InterfaceDefinition() { InterfaceDecl(this, _, _) }

    /** Gets the interface name. */
    string getName() { InterfaceDecl(this, result, _) }

    /** Gets the file containing this interface. */
    File getFile() { InterfaceDecl(this, _, result) }

    /** Gets a textual representation. */
    string toString() { result = this.getName() }
}

/** A type declaration (type alias, enum, etc.). */
class TypeDefinition extends @type_decl {
    TypeDefinition() { TypeDecl(this, _, _, _) }

    /** Gets the type name. */
    string getName() { TypeDecl(this, result, _, _) }

    /** Gets the kind of type (e.g. "alias"). */
    string getTypeKind() { TypeDecl(this, _, result, _) }

    /** Gets the file containing this type. */
    File getFile() { TypeDecl(this, _, _, result) }

    /** Gets a textual representation. */
    string toString() { result = this.getName() }
}

// ---- Imports ----

/** An import declaration. */
class ImportDeclaration extends @import_binding {
    ImportDeclaration() { ImportBinding(this, _, _) }

    /** Gets the import path/source. */
    string getImportPath() { ImportBinding(this, result, _) }

    /** Gets the imported name (or "default" / "*"). */
    string getImportedName() { ImportBinding(this, _, result) }

    /** Gets a textual representation. */
    string toString() { result = this.getImportedName() + " from " + this.getImportPath() }
}

/** An export declaration. */
class ExportDeclaration extends @export_binding {
    ExportDeclaration() { ExportBinding(this, _, _) }

    /** Gets the exported name. */
    string getExportedName() { result = this }

    /** Gets the file containing this export. */
    File getFile() { ExportBinding(this, _, result) }

    /** Gets a textual representation. */
    string toString() { result = this.getExportedName() }
}

// ---- Parameters ----

/** A parameter of a function. */
class Parameter extends @parameter {
    Parameter() { Parameter(this, _, _, _, _, _) }

    /** Gets the function this parameter belongs to. */
    Function getFunction() { result = this }

    /** Gets the parameter index (0-based). */
    int getIndex() { Parameter(this, result, _, _, _, _) }

    /** Gets the parameter name. */
    string getName() { Parameter(this, _, result, _, _, _) }

    /** Gets the parameter node. */
    ASTNode getNode() { Parameter(this, _, _, result, _, _) }

    /** Gets the type annotation text. */
    string getTypeText() { Parameter(this, _, _, _, _, result) }

    /** Gets a textual representation. */
    string toString() { result = this.getName() }
}

// ---- Symbols ----

/** A symbol declaration. */
class Symbol extends @symbol {
    Symbol() { Symbol(this, _, _, _) }

    /** Gets the symbol name. */
    string getName() { Symbol(this, result, _, _) }

    /** Gets the declaration node. */
    ASTNode getDeclNode() { Symbol(this, _, result, _) }

    /** Gets the file containing the declaration. */
    File getDeclFile() { Symbol(this, _, _, result) }

    /** Gets a textual representation. */
    string toString() { result = this.getName() }
}

// ---- Taint ----

/** A taint source expression. */
class TaintSource extends @taint_source {
    TaintSource() { TaintSource(this, _) }

    /** Gets the source kind. */
    string getSourceKind() { TaintSource(this, result) }

    /** Gets a textual representation. */
    string toString() { result = "TaintSource" }
}

/** A taint sink expression. */
class TaintSink extends @taint_sink {
    TaintSink() { TaintSink(this, _) }

    /** Gets the sink kind. */
    string getSinkKind() { TaintSink(this, result) }

    /** Gets a textual representation. */
    string toString() { result = "TaintSink" }
}

/** A taint alert — tainted data from source reaches sink. */
class TaintAlert extends @taint_alert {
    TaintAlert() { TaintAlert(this, _, _, _) }

    /** Gets the sink expression. */
    int getSinkExpr() { TaintAlert(this, result, _, _) }

    /** Gets the source taint kind. */
    string getSrcKind() { TaintAlert(this, _, result, _) }

    /** Gets the sink kind. */
    string getSinkKind() { TaintAlert(this, _, _, result) }

    /** Gets a textual representation. */
    string toString() { result = "TaintAlert" }
}
