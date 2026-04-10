/**
 * Bridge library for JSX-related relations.
 * Maps JsxElement, JsxAttribute.
 */

/** A JSX element (<Component ... />). */
class JsxElement extends @jsx_element {
    JsxElement() { jsx_element(this, _, _) }

    /** Gets the tag expression node. */
    ASTNode getTagNode() { jsx_element(this, result, _) }

    /** Gets the tag symbol. */
    int getTagSym() { jsx_element(this, _, result) }

    /** Gets an attribute of this element. */
    JsxAttribute getAnAttribute() { result.getElement() = this }

    /** Gets a textual representation. */
    string toString() { result = "jsx_element" }
}

/** An attribute on a JSX element (<Foo bar={expr} />). */
class JsxAttribute extends @jsx_attribute {
    JsxAttribute() { jsx_attribute(this, _, _) }

    /** Gets the element this attribute belongs to. */
    JsxElement getElement() { jsx_attribute(result, _, _) and jsx_attribute(this, _, _) }

    /** Gets the attribute name. */
    string getName() { jsx_attribute(_, result, _) and jsx_attribute(this, _, _) }

    /** Gets the value expression node. */
    ASTNode getValueExpr() { jsx_attribute(_, _, result) and jsx_attribute(this, _, _) }

    /** Gets a textual representation. */
    string toString() { result = this.getName() }
}
