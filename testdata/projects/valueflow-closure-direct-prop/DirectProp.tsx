// Whole-closure integration fixture — Phase C PR7 §7/§8.
//
// SHAPE: direct prop pass (R1 analogue). Exercises lfsJsxPropBind
// composing with lfsVarInit — cfg literal at line 28 flows through
// the `value` prop into the destructured-param use at line 24.
//
// PR8 (#202 Gap A): `lfsJsxPropBind` closes the JSX-prop →
// destructured-param path. The walker also unwraps JsxExpression
// `{…}` so the prop's valueExpr anchors at the inner cfg identifier
// (line 29), letting the closure resolve back to cfg on line 28.
//
// Observed reachability set (mayResolveToRec; see
// mayResolveTo.expected.csv for the machine-readable form):
//
//   21 → 21   (import stmt base)
//   24 → 28   (Inner `value` use → cfg — lfsJsxPropBind)
//   28 → 28   (cfg object literal base)
//   29 → 28   (JSX prop → cfg literal)
//   29 → 29   (JSX element base)

import { ReactNode } from 'react';

function Inner({ value }: { value: unknown }): ReactNode {
  return value as ReactNode;
}

export function DirectProp(): ReactNode {
  const cfg = { tag: 'src' };
  return <Inner value={cfg} />;
}
