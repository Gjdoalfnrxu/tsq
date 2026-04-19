// Negative — two-hop var indirection.
// Phase A is depth-1 only. `const a = b; const b = {...};` requires
// recursion through `mayResolveTo` (Phase C). The use-site `use(a)`
// must NOT resolve to the object literal under Phase A.
const b = { k: 1 };
const a = b;
function use(o: { k: number }): number { return o.k; }
const r = use(a);
