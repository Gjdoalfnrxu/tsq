// Whole-closure integration fixture — Phase C PR7 §7/§8.
//
// SHAPE: Context provider with own-fields (R2 analogue). Designed to
// exercise "inc FieldRead reaches Provider arrow" via lfsVarInit +
// object-field read.
//
// PR7 note: under the current PR6 closure this composition does NOT
// fire — no row on line 29 (`inc()` call) reaches the default-literal
// arrow (line 21) or the Provider's inline value literal (line 24).
// Observed rows are identity-only. See follow-up issue #202.
//
// Observed reachability set (mayResolveToRec; see
// mayResolveTo.expected.csv):
//
//   ContextOwn.tsx:19 → ContextOwn.tsx:19   (import stmt base)
//   ContextOwn.tsx:21 → ContextOwn.tsx:21   (Ctx createContext default literal)
//   ContextOwn.tsx:24 → ContextOwn.tsx:24   (Provider value literal base)

import { createContext, useContext, ReactNode } from 'react';

const Ctx = createContext<{ inc: () => void }>({ inc: () => {} });

export function Provider({ children }: { children: ReactNode }): ReactNode {
  return <Ctx.Provider value={{ inc: () => {} }}>{children}</Ctx.Provider>;
}

export function useInc(): void {
  const { inc } = useContext(Ctx);
  inc();
}
