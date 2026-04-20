// Adversarial fixture — Phase C PR7 §7/§8.4.
//
// SHAPE: deep-spread chain. Four nested spread hops from a base
// value-source. Exercises the depth-cap behaviour documented at plan
// §5.2 (DefaultMaxIterations = 100 in ql/eval/seminaive.go governs the
// fixpoint; path sensitivity is PR5's problem).
//
// Four-level nesting:
//   L1 base literal  line 14  ({ v: () => 1 })
//   L2 spread into   line 15
//   L3 spread into   line 16
//   L4 spread into   line 17
//   use site         line 19  (`x.v` field read)
//
// The closure must NOT loop. With PR4's MaxIterations=100 and
// (v, s) tuple finiteness, termination is structural.

export function chain(): () => number {
  const l1 = { v: () => 1 };
  const l2 = { ...l1 };
  const l3 = { ...l2 };
  const l4 = { ...l3 };

  return l4.v;
}
