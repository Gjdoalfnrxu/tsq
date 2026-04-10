/**
 * Bridge library for React framework models (v2 Phase F).
 * Provides QL classes for React component XSS detection via dangerouslySetInnerHTML.
 */

/**
 * A React XSS sink via dangerouslySetInnerHTML. These are TaintSink facts
 * with kind "xss" derived from JsxAttribute facts matching the attribute name
 * "dangerouslySetInnerHTML".
 */
class DangerouslySetInnerHTML extends TaintSink {
    DangerouslySetInnerHTML() {
        this.getSinkKind() = "xss" and
        exists(int elem | JsxAttribute(elem, "dangerouslySetInnerHTML", this))
    }

    /** Gets a textual representation. */
    override string toString() { result = "DangerouslySetInnerHTML" }
}
