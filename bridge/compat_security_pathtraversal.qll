/**
 * CodeQL-compatible path traversal security query library.
 * Clean-room implementation providing pre-configured source/sink
 * patterns for path traversal detection.
 *
 * This file is NOT derived from CodeQL source code.
 * API surface documented from public CodeQL documentation.
 *
 * Note: path_traversal sink kind is a placeholder — extraction does
 * not yet populate it. This library is provided for API compatibility.
 */

module PathTraversal {
    /**
     * A source of user-controlled input that may reach a path traversal sink.
     * Backed by tsq's TaintSource relation with kind "http_input".
     */
    class PathTraversalSource extends @symbol {
        PathTraversalSource() { TaintSource(this, "http_input") }
        string toString() { result = "PathTraversalSource" }
    }

    /**
     * A sink where user-controlled input would cause path traversal.
     * Backed by tsq's TaintSink relation with kind "path_traversal".
     */
    class PathTraversalSink extends @symbol {
        PathTraversalSink() { TaintSink(this, "path_traversal") }
        string toString() { result = "PathTraversalSink" }
    }

    /**
     * Holds if there is a path traversal taint flow from `source` to `sink`.
     * Backed by tsq's TaintAlert relation filtered to path_traversal sinks.
     */
    predicate hasPathTraversalFlow(int source, int sink) {
        TaintAlert(source, sink, _, "path_traversal")
    }
}
