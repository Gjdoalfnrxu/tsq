// Whole-closure integration fixture — Phase C PR7 §7/§8.
//
// SHAPE: higher-order function. Parent doc §3.3's canonical case:
//
//   function makeIncrementer(step) { return x => x + step; }
//   const inc5 = makeIncrementer(5);
//   const r = inc5(10);
//
// The closure must follow the returned arrow function back to its
// definition site (lfsReturnToCallSite) and then through the VarDecl
// init (lfsVarInit) to the use-site callee reference.
//
// Hand-computed expected reachability set:
//
//   sourceExpr line 17 (arrow `(x) => x + step`) reaches:
//     - line 17 itself              (base)
//     - line 21 `inc5` VarDecl init (via lfsReturnToCallSite)
//     - line 22 callee in `inc5(10)` (via lfsVarInit forward)

export function makeIncrementer(step: number): (x: number) => number {
  return (x: number) => x + step;
}

export function useIt(): number {
  const inc5 = makeIncrementer(5);
  return inc5(10);
}
