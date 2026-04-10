/**
 * Bridge library for inter-procedural flow composition relations (v2 Phase C3).
 * Maps composition relations derived from system Datalog rules that combine
 * function-level summaries with call graph edges to propagate dataflow
 * across function boundaries.
 */

/**
 * Holds when there is inter-procedural dataflow from `srcSym` to `dstSym`
 * across a call site — either argument-to-return or argument-to-inner-argument.
 */
class InterFlow extends @inter_flow {
    InterFlow() { InterFlow(this, _) }

    /** Gets the source symbol. */
    int getSrc() { result = this }

    /** Gets the destination symbol. */
    int getDst() { InterFlow(this, result) }

    /** Gets a textual representation. */
    string toString() { result = "InterFlow" }
}

/**
 * Holds when there is whole-program transitive dataflow from `srcSym`
 * to `dstSym`, composing local intra-procedural flow and inter-procedural
 * flow across call sites.
 */
class FlowStar extends @flow_star {
    FlowStar() { FlowStar(this, _) }

    /** Gets the source symbol. */
    int getSrc() { result = this }

    /** Gets the destination symbol. */
    int getDst() { FlowStar(this, result) }

    /** Gets a textual representation. */
    string toString() { result = "FlowStar" }
}
