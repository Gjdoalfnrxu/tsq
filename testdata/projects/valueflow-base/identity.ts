// Branch 2.1 — Identity (base case).
// Any value-source expression resolves to itself via ExprValueSource.
// This file produces multiple value-source rows: arrow, object literal,
// number literal, string literal.
const arrow = () => 1;
const obj = { k: 1 };
const num = 42;
const str = "hi";
