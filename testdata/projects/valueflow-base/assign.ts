// Branch 2.3 — Assign
// `x` is a let; assigned an arrow function. The use-site `x()` references
// the sym whose AssignExpr rhs is a value-source (arrow expression).
let x: () => number;
x = () => 1;
const r = x();
