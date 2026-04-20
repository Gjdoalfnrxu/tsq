// Arrow-assigned component fixture for #202 Gap A (PR #203).
//
// SHAPE: like DirectProp but the receiving component is an arrow
// function assigned to a const, rather than a function declaration.
// Idiomatic React style — `const Arrow = ({value}) => value`.
//
// If `FunctionSymbol` emission for arrow-assigned components is
// populated by the walker, lfsJsxPropBind should fire just the same
// and produce `(line 17 [value use] → line 20 [cfg literal])` via
// mayResolveToRec.
// If not, there is a silent walker-level miss worth filing as a
// follow-up (don't fix here — that's a separate walker change).

import { ReactNode } from 'react';

const ArrowInner = ({ value }: { value: unknown }): ReactNode =>
  value as ReactNode;

export function ArrowHost(): ReactNode {
  const cfg = { tag: 'src' };
  return <ArrowInner value={cfg} />;
}
