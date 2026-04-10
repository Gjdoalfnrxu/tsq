/**
 * Bridge library for diagnostic relations.
 * Maps ExtractError.
 */

/** An error encountered during extraction. */
class ExtractError extends @extract_error {
    ExtractError() { extract_error(this, _, _, _) }

    /** Gets the file where the error occurred. */
    File getFile() { extract_error(result, _, _, _) and extract_error(this, _, _, _) }

    /** Gets the line number where the error occurred. */
    int getNodeStartLine() { extract_error(_, result, _, _) and extract_error(this, _, _, _) }

    /** Gets the extraction phase that produced this error. */
    string getPhase() { extract_error(_, _, result, _) and extract_error(this, _, _, _) }

    /** Gets the error message. */
    string getMessage() { extract_error(_, _, _, result) and extract_error(this, _, _, _) }

    /** Gets a textual representation. */
    string toString() { result = this.getPhase() + ": " + this.getMessage() }
}
