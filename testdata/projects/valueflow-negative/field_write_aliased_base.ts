// Negative — field write through aliased base.
// `o2.cb = ...` writes via the alias `o2`, but the read site reads via `o`.
// Phase A's FieldRead branch keys on baseSym match — under v1's
// "no shape" / no-alias-tracking posture this should not resolve.
// (Whether `o` and `o2` collapse to the same baseSym depends on the
// extractor's `ExprMayRef`/sym resolution; if they share a sym this
// "negative" resolves and the test must surface that as a known
// over-approximation rather than a leak.)
const o: { cb: () => number } = { cb: () => 0 };
const o2 = o;
o2.cb = () => 1;
const r = o.cb();
