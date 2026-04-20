// Whole-closure integration fixture — Phase C PR7 §7/§8.
//
// SHAPE: direct prop pass (R1 analogue). The canonical "value-source
// flows through a JSX-prop parameter binding into a use site" closure
// case — exercises lfsVarInit + lfsParamBind composed.
//
// Hand-computed expected reachability set (mayResolveToRec(v, s), keyed
// by file-suffix + line for both endpoints):
//
//   sourceExpr line 13 (literal object `{ tag: 'src' }`) reaches:
//     - line 13 itself              (base: ExprValueSource identity)
//     - line 18 `value` param use   (lfsParamBind into Inner)
//
// The fixture is designed so no OTHER value-source flows to those use
// sites — any additional row indicates an over-bridging regression.

import { ReactNode } from 'react';

function Inner({ value }: { value: unknown }): ReactNode {
  return value as ReactNode;
}

export function DirectProp(): ReactNode {
  const cfg = { tag: 'src' };
  return <Inner value={cfg} />;
}
