/**
 * Bridge library for intra-procedural dataflow relations (v2 Phase C1).
 * Maps LocalFlow and LocalFlowStar derived from system Datalog rules
 * over assignment, VarDecl, return, field, and destructuring facts.
 */

/**
 * A local (intra-procedural) data-flow edge within a single function.
 * Holds when `srcSym` flows to `dstSym` in one step inside function `fnId`.
 */
class LocalFlow extends @local_flow {
    LocalFlow() { LocalFlow(this, _, _) }

    /** Gets the enclosing function. */
    int getFunction() { result = this }

    /** Gets the source symbol. */
    int getSource() { LocalFlow(this, result, _) }

    /** Gets the destination symbol. */
    int getDestination() { LocalFlow(this, _, result) }

    /** Gets a textual representation. */
    string toString() { result = "LocalFlow" }
}

/**
 * Transitive closure of local data-flow edges.
 * Holds when there exists a chain of LocalFlow edges from `srcSym` to `dstSym`
 * within the same function `fnId`.
 */
class LocalFlowStar extends @local_flow_star {
    LocalFlowStar() { LocalFlowStar(this, _, _) }

    /** Gets the enclosing function. */
    int getFunction() { result = this }

    /** Gets the source symbol. */
    int getSource() { LocalFlowStar(this, result, _) }

    /** Gets the destination symbol. */
    int getDestination() { LocalFlowStar(this, _, result) }

    /** Gets a textual representation. */
    string toString() { result = "LocalFlowStar" }
}
