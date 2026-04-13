/**
 * CodeQL-compatible regular expression stubs.
 * Clean-room implementation providing regex literal and
 * regex term classes for ReDoS and regex injection analysis.
 *
 * This file is NOT derived from CodeQL source code.
 * API surface documented from public CodeQL documentation.
 */

/**
 * A regular expression literal.
 * Stub for regex literal analysis — requires AST-level regex parsing.
 */
class RegExpLiteral extends @symbol {
    RegExpLiteral() { none() }

    /** Gets a textual representation. */
    string toString() { result = "RegExpLiteral" }
}

/**
 * A term within a regular expression.
 * Stub for regex term analysis — requires regex parse tree.
 */
class RegExpTerm extends @symbol {
    RegExpTerm() { none() }

    /** Gets a textual representation. */
    string toString() { result = "RegExpTerm" }
}
