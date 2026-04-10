/**
 * CodeQL-compatible SQL injection security query library.
 * Clean-room implementation providing pre-configured source/sink
 * patterns for SQL injection detection.
 *
 * This file is NOT derived from CodeQL source code.
 * API surface documented from public CodeQL documentation.
 */

module SqlInjection {
    /**
     * A source of user-controlled input that may reach a SQL injection sink.
     * Backed by tsq's TaintSource relation with kind "http_input".
     */
    class SqlInjectionSource extends @symbol {
        SqlInjectionSource() { TaintSource(this, "http_input") }
        string toString() { result = "SqlInjectionSource" }
    }

    /**
     * A sink where user-controlled input would cause SQL injection.
     * Backed by tsq's TaintSink relation with kind "sql".
     */
    class SqlInjectionSink extends @symbol {
        SqlInjectionSink() { TaintSink(this, "sql") }
        string toString() { result = "SqlInjectionSink" }
    }

    /**
     * Holds if there is a SQL injection taint flow from `source` to `sink`.
     * Backed by tsq's TaintAlert relation filtered to sql sinks.
     */
    predicate hasSqlInjectionFlow(int source, int sink) {
        TaintAlert(source, sink, _, "sql")
    }
}
