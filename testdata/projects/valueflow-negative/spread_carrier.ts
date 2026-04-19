// Negative — object-literal spread.
// `{ ...base }` is unmodelled in Phase A. The FieldRead `o.k` must NOT
// resolve to the inner literal `1` through the spread. Phase C will
// handle this when recursive `mayResolveTo` ships.
const base = { k: 1 };
const o = { ...base };
const r = o.k;
