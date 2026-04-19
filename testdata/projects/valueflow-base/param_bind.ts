// Branch 2.4 — ParamBind
// `g` is a parameter sym; the call site passes an arrow function (value-source)
// at slot 0. Use-site `g()` references `g`; ParamBinding(f, 0, g, <arrow>)
// pairs the param sym with the arg expression.
function f(g: () => number): number { return g(); }
const r = f(() => 1);
