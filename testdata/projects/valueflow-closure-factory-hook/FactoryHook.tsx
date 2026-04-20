// Whole-closure integration fixture — Phase C PR7 §7/§8.
//
// SHAPE: factory hook return (R4 analogue). A custom hook constructs an
// object literal and returns it; the consumer destructures the returned
// object. Closure composition: lfsReturnToCallSite + lfsVarInit (on the
// destructure) + object-field read.
//
// Observed reachability set (mayResolveToRec; see
// mayResolveTo.expected.csv):
//
//   FactoryHook.tsx:19 → :19   (useFactory function declaration base)
//   FactoryHook.tsx:20 → :20   (object literal `{ doIt: () => 42 }` base)
//   FactoryHook.tsx:21 → :20   (return expr reaches literal — lfsReturnExpr)
//   FactoryHook.tsx:24 → :24   (Consumer function base)
//   FactoryHook.tsx:25 → :20   (destructure `const { doIt }` reaches literal
//                                — lfsReturnToCallSite ∘ lfsVarInit,
//                                load-bearing R4 composition)

export function useFactory(): { doIt: () => number } {
  const api = { doIt: () => 42 };
  return api;
}

export function Consumer(): number {
  const { doIt } = useFactory();
  return doIt();
}
