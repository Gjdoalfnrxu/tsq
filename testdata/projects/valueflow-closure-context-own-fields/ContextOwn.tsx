// Whole-closure integration fixture — Phase C PR7 §7/§8.
//
// SHAPE: Context provider with own-fields (R2 analogue). The object
// literal's own-field value flows through FieldRead in the consumer.
//
// Hand-computed expected reachability set:
//
//   sourceExpr line 17 (arrow `() => {}`) reaches:
//     - line 17 itself              (base)
//     - line 24 `inc` FieldRead     (via lfsVarInit + object-field branch
//                                    of mayResolveToRec: ctx.inc through
//                                    the Provider's value literal)
//
// The consumer destructures `{ inc }` out of the context; the closure
// must follow the field.

import { createContext, useContext, ReactNode } from 'react';

const Ctx = createContext<{ inc: () => void }>({ inc: () => {} });

export function Provider({ children }: { children: ReactNode }): ReactNode {
  return <Ctx.Provider value={{ inc: () => {} }}>{children}</Ctx.Provider>;
}

export function useInc(): void {
  const { inc } = useContext(Ctx);
  inc();
}
