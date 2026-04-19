// Round-3 case A: variable-indirect Provider value.
//
//   const actions = { setIA, setIB };
//   <Ctx.Provider value={actions}>...</Ctx.Provider>
//
// Round-2 only matched the literal `value={{ setIA }}` shape. Round-3 must
// also recognise the indirect form via VarDecl-to-ObjectExpression resolution.

import { useState, useContext, createContext, ReactNode } from 'react';

interface IndirectActions {
  setIA: (updater: (prev: number) => number) => void;
  setIB: (updater: (prev: number) => number) => void;
}

export const IndirectCtx = createContext<IndirectActions | null>(null);

export function IndirectProvider({ children }: { children: ReactNode }) {
  const [a, setIA] = useState(0);
  const [b, setIB] = useState(0);
  void a; void b;
  // Variable-indirect: `actions` binds an ObjectLiteral; the Provider's
  // value attribute is `actions`, not the literal itself.
  const actions = { setIA, setIB };
  return (
    <IndirectCtx.Provider value={actions}>{children}</IndirectCtx.Provider>
  );
}

export function useIndirectActions() {
  return useContext(IndirectCtx);
}

// Consumer: outer `setIA(...)` updater body invokes the inner `setIB(...)`.
// Both setters arrive through the variable-indirect Provider value.
export function IndirectConsumer() {
  const { setIA, setIB } = useIndirectActions()!;
  return (
    <button
      onClick={() => {
        setIA(prev => {
          setIB(p => p + 1);
          return prev + 1;
        });
      }}
    >
      indirect
    </button>
  );
}
