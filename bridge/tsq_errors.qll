/**
 * Bridge library for diagnostic relations.
 * Maps ExtractError.
 */

/**
 * An error encountered during extraction.
 *
 * NOTE: `this` binds to col 0 (file), which is not a unique identifier.
 * Multiple errors in the same file share the same col-0 value, causing
 * entity collapse.  Known v1 limitation — resolving it requires adding
 * a composite key or synthetic id column.
 */
class ExtractError extends @extract_error {
    ExtractError() { ExtractError(this, _, _, _) }

    /** Gets the file where the error occurred. */
    File getFile() { result = this }

    /** Gets the line number where the error occurred. */
    int getNodeStartLine() { ExtractError(this, result, _, _) }

    /** Gets the extraction phase that produced this error. */
    string getPhase() { ExtractError(this, _, result, _) }

    /** Gets the error message. */
    string getMessage() { ExtractError(this, _, _, result) }

    /** Gets a textual representation. */
    string toString() { result = this.getPhase() + ": " + this.getMessage() }
}
