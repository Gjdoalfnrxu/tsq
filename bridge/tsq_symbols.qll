/**
 * Bridge library for symbol-related relations (v2).
 * Maps Symbol, FunctionSymbol, TypeFromLib, SymInFunction.
 *
 * These relations were registered in v1 but left empty.
 * v2 populates them structurally from tree-sitter AST patterns.
 * Full cross-file symbol resolution will require tsgo (future).
 */

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

/**
 * Associates a symbol with its function node.
 * Populated for named function declarations and variable-assigned functions.
 */
class FunctionSymbol extends @function_symbol {
    FunctionSymbol() { FunctionSymbol(this, _) }

    /** Gets the symbol. */
    Symbol getSymbol() { result = this }

    /** Gets the function. */
    Function getFunction() { FunctionSymbol(this, result) }
}

/**
 * Associates a symbol with a library type.
 * Requires tsgo for population — empty under structural-only extraction.
 */
class TypeFromLib extends @type_from_lib {
    TypeFromLib() { TypeFromLib(this, _) }

    /** Gets the symbol. */
    Symbol getSymbol() { result = this }

    /** Gets the library name. */
    string getLibName() { TypeFromLib(this, result) }
}

/** Associates a symbol reference with its containing function. */
class SymInFunction extends @sym_in_function {
    SymInFunction() { SymInFunction(this, _) }

    /** Gets the symbol. */
    Symbol getSymbol() { result = this }

    /** Gets the containing function. */
    Function getFunction() { SymInFunction(this, result) }
}
