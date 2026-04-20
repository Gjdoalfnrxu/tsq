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
// Observed reachability set (mayResolveToRec; see
// mayResolveTo.expected.csv):
//
//   Higher.ts:25 → :25   (makeIncrementer function base)
//   Higher.ts:26 → :26   (returned arrow `(x) => x + step` base)
//   Higher.ts:26 → :30   (back-edge from VarDecl site into arrow)
//   Higher.ts:29 → :29   (useIt function base)
//   Higher.ts:30 → :26   (inc5 VarDecl init reaches arrow — lfsReturnToCallSite)
//   Higher.ts:30 → :30   (VarDecl base)
//   Higher.ts:31 → :26   (callee `inc5` reaches arrow — HOF composition under test)
//   Higher.ts:31 → :31   (call-site base)

export function makeIncrementer(step: number): (x: number) => number {
  return (x: number) => x + step;
}

export function useIt(): number {
  const inc5 = makeIncrementer(5);
  return inc5(10);
}
