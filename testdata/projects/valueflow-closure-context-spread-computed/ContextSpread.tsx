// Whole-closure integration fixture — Phase C PR7 §7/§8.
//
// SHAPE: Context with spread + computed key (R3 analogue). The
// Provider's value literal is formed by spreading a base object and
// adding a computed key; consumer reads through that composite.
//
// Under PR6's mayResolveToRec, the forward edges `lfsObjectLiteralStore`
// + `lfsSpreadElement` admit the closure to follow the spread carrier
// and the field-value into the composed object literal. This is the
// "documented over-reach" shape flagged in the PR6 wiki §6 notes — the
// fixture pins it so a regression that silently tightens the semantics
// is caught.
//
// Observed reachability set (mayResolveToRec; see
// mayResolveTo.expected.csv for the machine-readable form). The
// load-bearing forward edge pinned by the test is the spread-element
// composition: the Provider value literal on line 38 reaches the
// base `{ ping: ... }` literal on line 33 via lfsSpreadElement.
//
//   ContextSpread.tsx:29 → :29   (import stmt base)
//   ContextSpread.tsx:31 → :31   (type alias site)
//   ContextSpread.tsx:33 → :33   (base `{ ping: () => { } }` literal)
//   ContextSpread.tsx:36 → :36   (`const key = 'derived';`)
//   ContextSpread.tsx:37 → :37   (inner createContext default literal)
//   ContextSpread.tsx:38 → :33   (Provider spread literal reaches base  ← load-bearing)
//   ContextSpread.tsx:38 → :36   (Provider spread literal reaches `key`)
//   ContextSpread.tsx:38 → :38   (Provider spread literal base)

import { createContext, useContext, ReactNode } from 'react';

type API = { [k: string]: () => void };

const base: API = { ping: () => { } };

export function SpreadProvider({ children }: { children: ReactNode }): ReactNode {
  const key = 'derived';
  const Ctx = createContext<API>({});
  return <Ctx.Provider value={{ ...base, [key]: () => { } }}>{children}</Ctx.Provider>;
}

export function useDerived(ctx: React.Context<API>): void {
  const api = useContext(ctx);
  api.derived();
}
