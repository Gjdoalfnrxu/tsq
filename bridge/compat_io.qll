/**
 * CodeQL-compatible IO abstraction stubs.
 * Clean-room implementation providing database access and
 * file system access classes for IO-related security analysis.
 *
 * This file is NOT derived from CodeQL source code.
 * API surface documented from public CodeQL documentation.
 */

/**
 * A database access operation, detected via taint sinks.
 * Matches SQL and NoSQL injection sinks.
 */
class DatabaseAccess extends @taint_sink {
    DatabaseAccess() { TaintSink(this, "sql") or TaintSink(this, "nosql") }

    /** Gets the kind of database query (sql or nosql). */
    string getQueryKind() { TaintSink(this, result) and (result = "sql" or result = "nosql") }

    /** Gets a textual representation. */
    string toString() { result = "DatabaseAccess" }
}

/**
 * A file system access operation.
 * Stub — real detection would need import tracking for the "fs" module.
 */
class FileSystemAccess extends @symbol {
    FileSystemAccess() { none() }

    /** Gets a textual representation. */
    string toString() { result = "FileSystemAccess" }
}
