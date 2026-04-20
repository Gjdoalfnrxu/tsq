/**
 * Bridge library for intra-procedural dataflow relations (v2 Phase C1).
 * Maps LocalFlow and LocalFlowStar derived from system Datalog rules
 * over assignment, VarDecl, return, field, and destructuring facts.
 *
 * Value-flow Phase D PR1 (additive): re-exports the recursive
 * `mayResolveTo` closure from `tsq_valueflow.qll` on the dataflow
 * surface, for consumers that want value-flow-backed local resolution
 * without importing the valueflow bridge directly. Two surfaces:
 *   - `predicate mayResolveTo(int valueExpr, int sourceExpr)`
 *     — predicate-style, parallels `LocalFlow`'s sibling predicate use.
 *   - `class MayResolveTo extends @may_resolve_to`
 *     — class-style, indexed by `valueExpr`, with `getSource()` as the
 *       multi-valued getter for resolved sources.
 * Both are thin wrappers over the system `MayResolveTo` relation
 * populated by `extract/rules/mayresolveto.go` (Phase C PR4). No new
 * recursion is introduced at the QL layer; the planner's recursive-IDB
 * estimator and magic-set rewrite (Phase B PR3/PR4) handle sizing.
 *
 * This PR is purely additive. No existing predicate or class is
 * modified. Consumers of `mayResolveToRec` in `tsq_valueflow.qll`
 * continue to work unchanged — the Phase D PR1 surface is a secondary
 * view, not a replacement. See `docs/design/valueflow-phase-d-plan.md`
 * §2 PR2 for the migration sequencing.
 */

// Forward declaration of the system relation populated by
// extract/rules/mayresolveto.go (Phase C PR4). The relation has the
// same shape as the one consumed by `mayResolveToRec` in
// tsq_valueflow.qll; this .qll re-exports it on the dataflow surface
// without re-declaring its body.

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
 * Value-flow Phase D PR1 — predicate re-export of `mayResolveToRec`.
 *
 * Thin wrapper over the system `MayResolveTo(v, s)` relation — the
 * transitive closure of `FlowStep` starting from `ExprValueSource`,
 * populated by `extract/rules/mayresolveto.go` (Phase C PR4). Exposes
 * the closure on the `tsq::dataflow` surface so consumers that already
 * import `tsq::dataflow` for `LocalFlow` / `LocalFlowStar` can reach
 * value-flow-backed resolution without importing `tsq::valueflow`
 * directly.
 *
 * Semantically identical to `mayResolveToRec(v, s)` in
 * `tsq_valueflow.qll`; the same underlying relation backs both. No
 * additional recursion is introduced.
 *
 * Non-recursive at the QL layer: the predicate body is a single
 * literal call into the system IDB head `MayResolveTo`. Phase B's
 * recursive-IDB estimator sizes the closure at the system rule, not
 * here.
 */
predicate mayResolveTo(int valueExpr, int sourceExpr) {
    MayResolveTo(valueExpr, sourceExpr)
}

/**
 * Value-flow Phase D PR1 — class wrapper for `mayResolveTo`.
 *
 * `MayResolveTo` classifies an expression node `v` that has at least
 * one resolved source in the Phase C recursive closure. Indexed by
 * `valueExpr`: `this` is the value-expression whose resolution is
 * being asked about. `getSource()` is the multi-valued getter that
 * returns each resolved source expression; use it in a `from`-clause
 * existential to iterate resolutions.
 *
 * Example (consumer usage):
 *   from MayResolveTo v, ASTNode s
 *   where s = v.getSource()
 *   select v, s
 *
 * Class surface parallels `LocalFlow` / `LocalFlowStar` in this file.
 * Predicate surface is `mayResolveTo` (above) — pick whichever fits
 * the consumer better. Both are the same relation.
 */
class MayResolveTo extends @may_resolve_to {
    MayResolveTo() { MayResolveTo(this, _) }

    /** Gets a resolved source expression for this value expression. */
    int getSource() { MayResolveTo(this, result) }

    /** Gets a textual representation. */
    string toString() { result = "MayResolveTo" }
}
