// Whole-closure integration fixture — Phase C PR7 §7/§8.
//
// SHAPE: factory hook return (R4 analogue). A custom hook constructs an
// object literal and returns it; the consumer destructures the returned
// object. Closure composition: lfsReturnToCallSite + lfsVarInit (on the
// destructure) + object-field read.
//
// Hand-computed expected reachability set:
//
//   sourceExpr line 16 (object literal `{ doIt: ... }`) reaches:
//     - line 16 itself                        (base)
//     - line 22 (return value of useFactory)  (lfsReturnToCallSite)
//     - line 23 `const { doIt } = ...`        (lfsVarInit on destructure
//                                              — extractor-dependent
//                                              modelling; pinned as an
//                                              observed reachability,
//                                              not a proof)

export function useFactory(): { doIt: () => number } {
  const api = { doIt: () => 42 };
  return api;
}

export function Consumer(): number {
  const { doIt } = useFactory();
  return doIt();
}
