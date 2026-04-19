// Multi-hop value-flow fixture (Phase A PR4 — integration test).
//
// Phase A's `mayResolveTo` is non-recursive by construction (plan §6 #1):
// no rule body references `mayResolveTo` itself. So "multi-hop" here does
// NOT mean recursive chaining. It means: a single fixture in which every
// distinct branch of `mayResolveTo` MUST fire, so downstream consumers
// (the bridge through-context query, the `resolvesToFunctionDirect`
// helper) observe the full vocabulary in a single extraction without one
// branch silently masking another.
//
// This fixture exercises ALL 7 branches that compose `mayResolveTo`:
//
//   1. base         (§2.1) — identity: a literal value-source whose
//                            valueExpr == sourceExpr.
//   2. var_init     (§2.2) — sym whose VarDecl init is a value-source.
//   3. assign       (§2.3) — sym (re-)assigned a value-source rhs.
//   4. param_bind   (§2.4) — parameter sym whose ParamBinding arg is a
//                            value-source.
//   5. field_read   (§2.5) — FieldRead matched to a FieldWrite of the
//                            same (baseSym, fld) whose rhs is a
//                            value-source.
//   6. object_field (§2.6) — FieldRead through a VarDecl-bound object
//                            literal whose field value is a value-source.
//   7. jsx_wrap     (§2.7) — JsxExpression wrapper around an identifier
//                            whose unwrapped form resolves through the
//                            core branches.
//
// Without all seven branches contributing, the chain breaks silently —
// that's the regression this fixture guards.
//
// Used by: TestValueflow_MultiHopFixture in valueflow_phase_a_pr4_test.go.

import { ReactNode } from 'react';

const Theme = { current: { color: 'red' } } as any;

// Param sink: lets us call sink(<value-source>) so that the literal in
// argument position is itself a value-source — the base branch fires on
// the literal expression with valueExpr == sourceExpr.
function sink(_: any): void {}

// --- Branch 1: base (identity on a literal) ----------------------------
// `42` is a Number literal — a value-source. Passing it inline at a sink
// site exercises mayResolveToBase(42-expr, 42-expr).
sink(42);

// --- Branch 2: var_init ------------------------------------------------
const vi = { kind: "var_init" };
sink(vi);

// --- Branch 3: assign --------------------------------------------------
let asg: { kind: string };
asg = { kind: "assign" };
sink(asg);

// --- Branch 4: param_bind ----------------------------------------------
function pb(cb: () => number): number {
  return cb();
}
const r_pb = pb(() => 1);

// --- Branch 5: field_read ----------------------------------------------
// `o.cb` read at the call site; `o.cb = () => 2` is the FieldWrite whose
// rhs is a value-source. Field-name + base-sym match only.
const o: { cb: () => number } = { cb: () => 0 };
o.cb = () => 2;
const r_fr = o.cb();

// --- Branch 6: object_field --------------------------------------------
// `const ob = { f: () => 3 }; ob.f` — FieldRead through a VarDecl-bound
// object literal; the obj-field branch resolves it to the field value.
const ob = { f: () => 3, k: 7 };
const r_of = ob.f();
const r_of_k = ob.k;

// --- Branch 7: jsx_wrap ------------------------------------------------
// `<Theme.Provider value={theme}>` — JsxAttribute valueExpr is the
// JsxExpression `{theme}` wrapper, NOT the inner Identifier `theme`. The
// JsxWrapped branch unwraps a single JsxExpression layer and re-runs the
// core union on the inner expression.
export function ThemeProvider({ children }: { children: ReactNode }) {
  const theme = { color: 'red' };
  return (
    <Theme.Provider value={theme}>{children}</Theme.Provider>
  );
}
