// Round-4 case A: factory hook returning an ObjectLiteral.
//
//   function useActions() {
//     const [conf, setConf] = useState(initial);
//     const actions = { setConf, setOther };
//     return actions;                       // <-- hook returns variable
//   }                                       //     bound to ObjectLiteral
//
//   function Provider({ children }) {
//     const actions = useActions();         // <-- VarDecl initialiser is a CALL
//     return <Ctx.Provider value={actions}>{children}</Ctx.Provider>;
//   }
//
// Round-3's `resolveToObjectExpr` only resolved through VarDecl initialisers
// that were themselves ObjectLiterals (or chains of plain Identifier
// indirections). When the initialiser is a CallExpression (factory hook),
// the chain dies. Round-4 must follow the call into the hook's return
// statement and resolve to the returned ObjectLiteral.

import { useState, createContext, ReactNode } from 'react';

interface FactoryActions {
  setFA: (updater: (prev: number) => number) => void;
  setFB: (updater: (prev: number) => number) => void;
}

export const FactoryCtx = createContext<FactoryActions | null>(null);

// Factory hook: holds the useState pair locally and returns an actions object.
// Two return shapes need to be supported:
//   1. `return { setFA, setFB };` — return expression IS the ObjectLiteral.
//   2. `return actions;` where `const actions = { ... };` was bound earlier.
// We exercise shape (2) here (the harder one).
export function useFactoryActions(): FactoryActions {
  const [a, setFA] = useState(0);
  const [b, setFB] = useState(0);
  void a;
  void b;
  const actions = { setFA, setFB };
  return actions;
}

export function FactoryProvider({ children }: { children: ReactNode }) {
  // The Provider's value attribute resolves to the hook call's returned
  // ObjectLiteral. Round-3's chain died here because `actions` is bound to
  // a CallExpression, not an ObjectLiteral.
  const actions = useFactoryActions();
  return (
    <FactoryCtx.Provider value={actions}>{children}</FactoryCtx.Provider>
  );
}
