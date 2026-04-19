// Branch 2.5 — FieldRead matching FieldWrite of same (baseSym, fld).
// `o.cb` is read at the call site; `o.cb = () => 1` is the write whose rhs
// is a value-source (arrow expression). Field-name + base-sym match only.
const o: { cb: () => number } = { cb: () => 0 };
o.cb = () => 1;
const r = o.cb();
