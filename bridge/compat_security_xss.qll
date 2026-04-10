/**
 * CodeQL-compatible XSS security query library.
 * Clean-room implementation providing pre-configured source/sink
 * patterns for cross-site scripting detection.
 *
 * This file is NOT derived from CodeQL source code.
 * API surface documented from public CodeQL documentation.
 */

module Xss {
    /**
     * A source of user-controlled input that may reach an XSS sink.
     * Backed by tsq's TaintSource relation with kind "http_input".
     */
    class XssSource extends @symbol {
        XssSource() { TaintSource(this, "http_input") }
        string toString() { result = "XssSource" }
    }

    /**
     * A sink where user-controlled input would cause XSS.
     * Backed by tsq's TaintSink relation with kind "xss".
     */
    class XssSink extends @symbol {
        XssSink() { TaintSink(this, "xss") }
        string toString() { result = "XssSink" }
    }

    /**
     * Holds if there is an XSS taint flow from `source` to `sink`.
     * Backed by tsq's TaintAlert relation filtered to http_input -> xss.
     */
    predicate hasXssFlow(int source, int sink) {
        TaintAlert(source, sink, "http_input", "xss")
    }
}
