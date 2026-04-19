// Round-4 case B: factory hook returning an ObjectLiteral DIRECTLY (no
// intermediate VarDecl):
//
//   function useDirectActions() {
//     const [a, setDA] = useState(0);
//     const [b, setDB] = useState(0);
//     return { setDA, setDB };          // <-- return IS the ObjectLiteral
//   }
//
// Exercises `resolveToObjectExprHookReturnDirectSame` (the simpler shape
// of the round-4 fix). Same-module hook resolution.

import { useState, useContext, createContext, ReactNode } from 'react';

interface DirectActions {
  setDA: (updater: (prev: number) => number) => void;
  setDB: (updater: (prev: number) => number) => void;
}

const DirectCtx = createContext<DirectActions | null>(null);

function useDirectActions(): DirectActions {
  const [a, setDA] = useState(0);
  const [b, setDB] = useState(0);
  void a;
  void b;
  return { setDA, setDB };
}

export function DirectFactoryProvider({ children }: { children: ReactNode }) {
  const actions = useDirectActions();
  return <DirectCtx.Provider value={actions}>{children}</DirectCtx.Provider>;
}

export function DirectFactoryConsumer() {
  const { setDA, setDB } = useContext(DirectCtx)!;
  return (
    <button
      onClick={() => {
        setDA(prev => {
          setDB(p => p + 1);
          return prev + 1;
        });
      }}
    >
      direct
    </button>
  );
}
