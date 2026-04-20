// Whole-closure integration fixture — Phase C PR7 §7/§8.
//
// SHAPE: recursive function call — cycle termination test.
//
// `function f() { return f(); }` creates a cycle in FlowStep
// (ifsRetToCall on the recursive call composes with itself). The
// closure's (v, s) tuple space is finite, so the seminaive evaluator
// must terminate — this fixture is the real-world AST analogue of the
// synthetic TestMayResolveToCycleTerminates unit test (plan §5.4).
//
// Expected behaviour: closure terminates (no timeout). The exact row
// set is extractor-dependent (ExprValueSource may or may not seed on
// the `f()` call depending on ValueSourceKinds coverage). The test
// asserts termination and a bounded row count, not an exact row set.

export function f(): number {
  return f();
}

export function g(): number {
  return f();
}
