/**
 * Bridge library for backward-dataflow tracking via Configuration-class
 * surface (issue #121, Phase A).
 *
 * This library introduces a user-facing query surface that declares the
 * sink first, then propagates backward through `flowsTo` via the magic-set
 * transform. The intent is to give selectivity to queries that today are
 * forward-enumerated and OOM on large corpora — see
 * `bridge/tsq_react.qll::setStateUpdaterCallsFn` and the Mastodon
 * regression in issue #130.
 *
 * Two primitives are exposed:
 *
 *   - `flowsTo(int srcSym, int sinkSym)` — a named binary predicate over
 *     `FlowStar`. Lifting it into a top-level predicate (rather than
 *     inlining the raw atom in every Configuration body) is what gives
 *     the planner a clean rewrite target: magic-set rules are keyed by
 *     predicate name, so a named `flowsTo` can be magic-set-rewritten,
 *     whereas an inlined `FlowStar(...)` literal cannot.
 *
 *   - `BackwardTracker` — an abstract base class that subclasses extend
 *     to declare a backward-dataflow query. Subclasses override
 *     `isSink`, optionally `isSource` and `isBarrier`. `hasFlowTo` is
 *     defined once on the base class with the body written sink-first
 *     so that the planner's rule-body binding inference (added in
 *     `ql/plan/magicset_infer.go::InferRuleBindings`) can propagate
 *     binding from the (typically tiny) `isSink` IDB backward through
 *     `flowsTo` and into the `isSource` candidates.
 *
 * Phase A keeps the surface deliberately minimal:
 *   - `int sym` not `Node` (Phase B introduces `DataFlow::Node`).
 *   - barrier-as-node only; barrier-as-edge / `isAdditionalFlowStep`
 *     deferred to Phase B.
 *   - no `PathGraph`, no `localFlowsTo`, no `TypeBackTracker` — those
 *     are Phase B and Phase C respectively.
 *
 * The full design plan and rationale live at
 * `Documents/ObsidianVault/Wiki/Tech/tsq-issue-121-plan.md`.
 */

/**
 * Holds if `srcSym` reaches `sinkSym` via the system `FlowStar` relation
 * (transitive closure of LocalFlow + InterFlow). This is a thin named
 * wrapper that exists so the planner can magic-set-rewrite it; user
 * queries should prefer `BackwardTracker.hasFlowTo` which seeds the
 * binding from the sink side.
 */
predicate flowsTo(int srcSym, int sinkSym) {
    FlowStar(srcSym, sinkSym)
}

/**
 * A backward-dataflow query configuration.
 *
 * Subclass this and override `isSink` (and optionally `isSource`,
 * `isBarrier`, `step`) to declare a custom backward-tracking query.
 * The `hasFlowTo(srcSym, sinkSym)` predicate then holds for
 * source/sink pairs of `int` symbols connected via `step`, filtered
 * by the configuration's sink/source choices and excluding sources
 * marked as barriers.
 *
 * Body shape — sink-first by construction so that magic-set inference
 * binds the second argument of `step` (the sink) and propagates the
 * binding back into `isSource`:
 *
 *   hasFlowTo(s, t) :-
 *       isSink(t), isSource(s), step(s, t), not isBarrier(s).
 *
 * The default `step(s, t)` body is `flowsTo(s, t)` (i.e., the system
 * `FlowStar` relation), which preserves Phase A's dataflow-tracking
 * behaviour exactly. Subclasses may override `step` with any binary
 * relation — for example, `functionContainsStar(s, t) and Call(t, _, _)`
 * for a structural containment-walk Configuration (see
 * `bridge/tsq_react.qll::SetStateUpdaterTracker`). The magic-set
 * inference works for *any* binary `step` relation: the body is shaped
 * so the planner's rule-body binding inference (issue #121 Phase A.1,
 * `ql/plan/magicset_infer.go::InferRuleBindings`) propagates the
 * binding from the small `isSink_<Subclass>` IDB into the second
 * argument of the (subclass-specialised) `step` predicate, regardless
 * of what relation that step expands to. The contract is that `step`
 * is some derived binary IDB; the planner does not care whether its
 * body recurses through `FlowStar`, `FunctionContains`, or anything
 * else.
 *
 * The base class extends `@symbol` purely to satisfy the desugarer's
 * requirement that abstract classes have an entity-typed root for
 * grounding `this` in the per-subclass dispatch rules. The actual
 * Configuration identity is the subclass; `this` is the symbol-typed
 * instance the abstract class uses internally and is not exposed in
 * the public predicate signatures.
 */
abstract class BackwardTracker extends @symbol {
    /** Holds if `sinkSym` is a sink in this configuration. Override this. */
    predicate isSink(int sinkSym) { none() }

    /**
     * Holds if `srcSym` is a candidate source. Default is "any symbol",
     * which lets backward propagation drive source discovery directly
     * from `isSink` via `step`. Override to constrain the source set.
     */
    predicate isSource(int srcSym) {
        // any() over int isn't representable directly; over-approximate
        // by recognising any symbol that participates in flow either as
        // a source or destination. This keeps the default cheap (it is
        // a projection of the FlowStar relation) and avoids enumerating
        // the entire symbol universe.
        FlowStar(srcSym, _) or FlowStar(_, srcSym)
    }

    /** Holds if `sym` is a barrier (sanitizer) blocking backward flow. */
    predicate isBarrier(int sym) { none() }

    /**
     * Holds if there is a step from `srcSym` to `sinkSym` under this
     * configuration's notion of "reaches". Default body is `flowsTo`
     * (the system `FlowStar` relation). Override to use a different
     * binary relation — e.g. `functionContainsStar(srcSym, sinkSym)`
     * for a structural containment-walk Configuration. The relation
     * may be transitive in its own body (recommended) or single-step;
     * `hasFlowTo` does not wrap `step` in a transitive closure,
     * leaving that choice to the override.
     */
    predicate step(int srcSym, int sinkSym) {
        flowsTo(srcSym, sinkSym)
    }

    /**
     * Holds if data flows from `srcSym` to `sinkSym` in this
     * configuration. Body is sink-first so that the planner's
     * rule-body binding inference (`InferRuleBindings` in
     * ql/plan/magicset_infer.go) propagates the binding from the
     * (typically tiny) `isSink` IDB backward through `step` into
     * `isSource`. Forward-written equivalent OOMs on Mastodon —
     * see issue #121 for the load-bearing rationale.
     */
    predicate hasFlowTo(int srcSym, int sinkSym) {
        this.isSink(sinkSym) and
        this.isSource(srcSym) and
        this.step(srcSym, sinkSym) and
        not this.isBarrier(srcSym)
    }
}
