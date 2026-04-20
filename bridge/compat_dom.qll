/**
 * CodeQL-compatible DOM standard library stubs.
 * Clean-room implementation providing DOM element classes,
 * innerHTML/outerHTML write sinks, document.write sinks,
 * and attribute write detection.
 *
 * This file is NOT derived from CodeQL source code.
 * API surface documented from public CodeQL documentation.
 */

module DOM {
    /**
     * A DOM element, backed by JsxElement relation.
     * Provides access to element attributes and tag name.
     */
    class Element extends @jsx_element {
        Element() { JsxElement(this, _, _) }

        /** Gets an attribute of this element. */
        int getAnAttribute() { JsxAttribute(this, _, result) }

        /** Gets the tag node for this element. */
        int getTagName() { JsxElement(this, result, _) }

        /** Gets a textual representation. */
        string toString() { result = "DOM::Element" }
    }

    /**
     * A write to innerHTML or outerHTML property.
     * This is a TaintSink("xss") — writing user-controlled data to
     * innerHTML/outerHTML enables cross-site scripting.
     */
    class InnerHtmlWrite extends @field_write {
        InnerHtmlWrite() {
            FieldWrite(this, _, "innerHTML", _, _) or
            FieldWrite(this, _, "outerHTML", _, _)
        }

        /** Gets the right-hand side expression being written. */
        int getRhs() { FieldWrite(this, _, _, result, _) }

        /** Gets a textual representation. */
        string toString() { result = "DOM::InnerHtmlWrite" }
    }

    /**
     * A call to document.write() or document.writeln().
     * This is a TaintSink("xss") — passing user-controlled data to
     * document.write enables cross-site scripting.
     */
    class DocumentWrite extends @method_call {
        DocumentWrite() {
            MethodCall(this, _, "write") or
            MethodCall(this, _, "writeln")
        }

        /** Gets the receiver expression. */
        int getReceiver() { MethodCall(this, result, _) }

        /** Gets a textual representation. */
        string toString() { result = "DOM::DocumentWrite" }
    }

    /**
     * A write to a DOM element property (e.g., element.src = ...).
     * Backed by FieldWrite relation on DOM element symbols.
     */
    class AttributeWrite extends @field_write {
        AttributeWrite() { FieldWrite(this, _, _, _, _) }

        /** Gets the base symbol. */
        int getBaseSym() { FieldWrite(this, result, _, _, _) }

        /** Gets the property name. */
        string getPropertyName() { FieldWrite(this, _, result, _, _) }

        /** Gets the right-hand side expression. */
        int getRhs() { FieldWrite(this, _, _, result, _) }

        /** Gets a textual representation. */
        string toString() { result = "DOM::AttributeWrite" }
    }
}
