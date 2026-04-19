// Multi-hop value-flow fixture (Phase A PR4 — integration test).
//
// Phase A's `mayResolveTo` is non-recursive by construction (plan §6 #1):
// no rule body references `mayResolveTo` itself. So "multi-hop" here does
// NOT mean recursive chaining. It means: a single fixture in which several
// distinct branches of `mayResolveTo` MUST fire together for a downstream
// consumer (the bridge through-context query, or the `resolvesToFunctionDirect`
// helper) to observe the right resolution.
//
// Concretely this file exercises three branches simultaneously:
//
//   1. var-init   (§2.2): `const obj = { handler: () => 1 }` — the var-init
//      branch resolves a sym reference back to the object literal value-source.
//   2. param-bind (§2.4): `function call(cb) { cb(); }` called with a value-source
//      arrow expression. ParamBinding pairs the param sym with the arg expr;
//      the param-bind branch resolves the use-site `cb` to the arrow value-source.
//   3. obj-field  (§2.6): `obj.handler` — FieldRead through a VarDecl-bound
//      object literal; the obj-field branch resolves it to the field value.
//
// All three must contribute rows to the union for the multi-hop integration
// test to assert the joint property: the SAME use-site expression (e.g. `cb`
// in `cb()`) participates in mayResolveTo via ParamBinding, AND the SAME
// arrow-fn that is the resolved sourceExpr also functions as a Function whose
// reference is reachable from another path through obj.handler. Without all
// three branches the chain breaks silently — that's the regression this
// fixture guards.
//
// Used by: TestValueflow_MultiHopFixture in valueflow_phase_a_pr4_test.go.

const arrow = () => 1;

const obj = { handler: arrow, k: 42 };

function call(cb: () => number): number {
  return cb();
}

// Use-site exercises var-init (`obj` resolves to the object literal),
// obj-field (`obj.handler` resolves to the arrow fn), and param-bind (the
// arrow passed inline resolves through `cb`'s ParamBinding).
const r1 = call(arrow);
const r2 = call(() => 2);
const r3 = obj.handler();
const r4 = obj.k;
