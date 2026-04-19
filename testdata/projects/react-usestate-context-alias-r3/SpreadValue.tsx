// Round-3 case B: spread + own field in Provider value.
//
//   const base = { setSB };
//   <Ctx.Provider value={{ ...base, setSA }}>...</Ctx.Provider>
//
// Both `setSA` (own) and `setSB` (from spread of `base`) must be recognised
// as fields of the Provider value.

import { useState, useContext, createContext, ReactNode } from 'react';

interface SpreadActions {
  setSA: (updater: (prev: number) => number) => void;
  setSB: (updater: (prev: number) => number) => void;
}

export const SpreadCtx = createContext<SpreadActions | null>(null);

export function SpreadProvider({ children }: { children: ReactNode }) {
  const [a, setSA] = useState(0);
  const [b, setSB] = useState(0);
  void a; void b;
  // `base` carries the second setter via spread.
  const base = { setSB };
  return (
    <SpreadCtx.Provider value={{ ...base, setSA }}>{children}</SpreadCtx.Provider>
  );
}

export function useSpreadActions() {
  return useContext(SpreadCtx);
}

export function SpreadConsumer() {
  const { setSA, setSB } = useSpreadActions()!;
  return (
    <button
      onClick={() => {
        setSA(prev => {
          setSB(p => p + 1);
          return prev + 1;
        });
      }}
    >
      spread
    </button>
  );
}
