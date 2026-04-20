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
// Hand-computed expected reachability set (keyed by line):
//
//   sourceExpr line 22 (arrow `() => { base() }`) reaches line 22, and
//   also reaches the spread-composed value literal on line 26 via
//   lfsSpreadElement, and thence to consumer FieldRead at line 31.

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
