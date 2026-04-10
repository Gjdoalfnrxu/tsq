/**
 * Bridge library for JSX-related relations.
 * Maps JsxElement, JsxAttribute.
 */

/** A JSX element (<Component ... />). */
class JsxElement extends @jsx_element {
    JsxElement() { JsxElement(this, _, _) }

    /** Gets the tag expression node. */
    ASTNode getTagNode() { JsxElement(this, result, _) }

    /** Gets the tag symbol. */
    int getTagSym() { JsxElement(this, _, result) }

    /** Gets an attribute of this element. */
    JsxAttribute getAnAttribute() { result.getElement() = this }

    /** Gets a textual representation. */
    string toString() { result = "jsx_element" }
}

/**
 * An attribute on a JSX element (<Foo bar={expr} />).
 *
 * NOTE: `this` binds to col 0 (element), which is not a unique identifier.
 * Multiple attributes on the same element share the same col-0 value,
 * causing entity collapse.  Known v1 limitation.
 */
class JsxAttribute extends @jsx_attribute {
    JsxAttribute() { JsxAttribute(this, _, _) }

    /** Gets the element this attribute belongs to. */
    JsxElement getElement() { result = this }

    /** Gets the attribute name. */
    string getName() { JsxAttribute(this, result, _) }

    /** Gets the value expression node. */
    ASTNode getValueExpr() { JsxAttribute(this, _, result) }

    /** Gets a textual representation. */
    string toString() { result = this.getName() }
}
