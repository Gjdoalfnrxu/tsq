/**
 * CodeQL-compatible cryptographic operation and cleartext logging stubs.
 * Clean-room implementation providing detection of crypto API usage
 * and logging of potentially sensitive data.
 *
 * This file is NOT derived from CodeQL source code.
 * API surface documented from public CodeQL documentation.
 */

/**
 * A call to a cryptographic operation from the Node.js crypto module
 * or similar libraries. Matches calls to createHash, createCipher,
 * createCipheriv, createDecipher, createDecipheriv, createHmac, createSign,
 * createVerify, etc.
 */
class CryptographicOperation extends @method_call {
    CryptographicOperation() {
        MethodCall(this, _, "createHash") or
        MethodCall(this, _, "createCipher") or
        MethodCall(this, _, "createCipheriv") or
        MethodCall(this, _, "createDecipher") or
        MethodCall(this, _, "createDecipheriv") or
        MethodCall(this, _, "createHmac") or
        MethodCall(this, _, "createSign") or
        MethodCall(this, _, "createVerify")
    }

    /** Gets the method name of this cryptographic operation. */
    string getMethodName() { MethodCall(this, _, result) }

    /** Gets the receiver expression. */
    int getReceiver() { MethodCall(this, result, _) }

    /** Gets a textual representation. */
    string toString() { result = "CryptographicOperation" }
}

/**
 * A call to a logging function that may expose cleartext sensitive data.
 * Matches console.log, console.error, console.warn, console.info,
 * and common logging library methods (winston, pino, bunyan).
 */
class CleartextLogging extends @method_call {
    CleartextLogging() {
        MethodCall(this, _, "log") or
        MethodCall(this, _, "error") or
        MethodCall(this, _, "warn") or
        MethodCall(this, _, "info") or
        MethodCall(this, _, "debug") or
        MethodCall(this, _, "trace")
    }

    /** Gets the method name. */
    string getMethodName() { MethodCall(this, _, result) }

    /** Gets the receiver expression. */
    int getReceiver() { MethodCall(this, result, _) }

    /** Gets a textual representation. */
    string toString() { result = "CleartextLogging" }
}

/**
 * An abstract class representing an expression that contains sensitive data.
 * Users can extend this class to mark specific expressions as sensitive.
 * This is a marker class — concrete subclasses must define the characteristic
 * predicate to identify which expressions are sensitive.
 */
class SensitiveDataExpr extends @symbol {
    /** Gets the kind of sensitive data (e.g., "password", "credential", "secret"). */
    string getKind() { result = "sensitive" }

    /** Gets a textual representation. */
    string toString() { result = "SensitiveDataExpr" }
}
