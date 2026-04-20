// Adversarial fixture — Phase C PR7 §7/§8.4.
//
// SHAPE: deep-spread chain. Four nested spread hops from a base
// value-source. Exercises the depth-cap behaviour documented at plan
// §5.2 (DefaultMaxIterations = 100 in ql/eval/seminaive.go governs the
// fixpoint; path sensitivity is PR5's problem).
//
// Four-level nesting (actual line numbers):
//   chain function  line 21  (wrapper)
//   L1 base literal line 22  (`{ v: () => 1 }`)
//   L2 spread into  line 23
//   L3 spread into  line 24
//   L4 spread into  line 25
//   return expr     line 27  (`return l4.v`)
//
// The closure must NOT loop. With DefaultMaxIterations=100 and
// (v, s) tuple finiteness, termination is structural. This fixture
// is only 4 levels deep — a TRUE depth-cap fixture (100+ levels) is
// tracked as follow-up issue #199. See mayResolveTo.expected.csv.

export function chain(): () => number {
  const l1 = { v: () => 1 };
  const l2 = { ...l1 };
  const l3 = { ...l2 };
  const l4 = { ...l3 };

  return l4.v;
}
