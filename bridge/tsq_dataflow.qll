/**
 * Bridge library for intra-procedural dataflow relations.
 * Maps LocalFlow / LocalFlowStar (v2 Phase C1) and re-exports the
 * recursive `mayResolveTo` closure (Phase D PR1, additive) on the
 * dataflow surface — predicate + class shapes over the same system
 * relation that backs `mayResolveToRec` in `tsq_valueflow.qll`.
 * Rule (c) overlap is intentional: two consumer-facing views of one
 * relation. See wiki Valueflow/phase-d-pr1 for design narrative.
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

/**
 * Holds when value-expression `valueExpr` may resolve to source
 * expression `sourceExpr` via the Phase C recursive `FlowStep`
 * closure (`extract/rules/mayresolveto.go`). Thin wrapper over the
 * system `MayResolveTo` relation; no QL-layer recursion.
 *
 * Example:
 *   from ASTNode v, ASTNode s
 *   where mayResolveTo(v, s)
 *   select v, s
 */
predicate mayResolveTo(int valueExpr, int sourceExpr) {
    MayResolveTo(valueExpr, sourceExpr)
}

/**
 * Class surface for the `MayResolveTo` closure — indexed by the
 * value expression, with `getSource()` returning each resolved
 * source. Same underlying relation as the `mayResolveTo` predicate;
 * pick whichever fits the consumer. Sibling of the `mayResolveToRec`
 * predicate in `tsq_valueflow.qll` (rule (c) overlap — intentional).
 *
 * Example:
 *   from MayResolveTo v, ASTNode s
 *   where s = v.getSource()
 *   select v, s
 */
class MayResolveTo extends @may_resolve_to {
    MayResolveTo() { MayResolveTo(this, _) }

    /** Gets a resolved source expression for this value expression. */
    int getSource() { MayResolveTo(this, result) }

    /** Gets a textual representation. */
    string toString() { result = "MayResolveTo" }
}
