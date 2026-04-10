/**
 * CodeQL-compatible command injection security query library.
 * Clean-room implementation providing pre-configured source/sink
 * patterns for OS command injection detection.
 *
 * This file is NOT derived from CodeQL source code.
 * API surface documented from public CodeQL documentation.
 */

module CommandInjection {
    /**
     * A source of user-controlled input that may reach a command injection sink.
     * Backed by tsq's TaintSource relation with kind "http_input".
     */
    class CommandInjectionSource extends @symbol {
        CommandInjectionSource() { TaintSource(this, "http_input") }
        string toString() { result = "CommandInjectionSource" }
    }

    /**
     * A sink where user-controlled input would cause command injection.
     * Backed by tsq's TaintSink relation with kind "command_injection".
     */
    class CommandInjectionSink extends @symbol {
        CommandInjectionSink() { TaintSink(this, "command_injection") }
        string toString() { result = "CommandInjectionSink" }
    }

    /**
     * Holds if there is a command injection taint flow from `source` to `sink`.
     * Backed by tsq's TaintAlert relation filtered to command_injection sinks.
     */
    predicate hasCommandInjectionFlow(int source, int sink) {
        TaintAlert(source, sink, _, "command_injection")
    }
}
