/**
 * Bridge library for Node.js framework models (v2 Phase F).
 * Provides QL classes for Node.js security-sensitive operations
 * such as child_process.exec (command injection sinks).
 */

/**
 * A command injection sink via child_process.exec or similar.
 * These are TaintSink facts with kind "command_injection" derived
 * from calls to functions named "exec".
 */
class CommandInjectionSink extends TaintSink {
    CommandInjectionSink() { this.getSinkKind() = "command_injection" }

    /** Gets a textual representation. */
    override string toString() { result = "CommandInjectionSink" }
}
