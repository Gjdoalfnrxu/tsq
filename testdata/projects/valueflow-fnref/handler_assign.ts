// resolvesToFunctionDirect fixture.
// `cb` is initialised from a function expression (an arrow). The use site
// `cb()` references `cb`; mayResolveTo's var-init branch resolves it to
// the arrow node, which FunctionSymbol maps to the same fn id.
// The integration test asserts at least one (callee, fnId) row.
const cb = () => 42;
const r = cb();
