// Branch 2.2 — VarInit
// `x` is a sym whose VarDecl initialiser is an object-literal value-source.
// The use site `use(x)` references `x`; the VarDecl init is the value-source.
const x = { a: 1 };
function use(o: { a: number }): number { return o.a; }
const r = use(x);
