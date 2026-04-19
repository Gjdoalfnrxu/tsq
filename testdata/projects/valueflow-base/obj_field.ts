// Branch 2.6 — ObjectField projection through a single VarDecl.
// `const o = { f: () => 1 }; o.f` — the FieldRead of `f` on baseSym `o`
// resolves through the VarDecl init (object-literal) to the field's value
// expression (arrow), which is itself a value-source.
const o = { f: () => 1 };
const r = o.f();
