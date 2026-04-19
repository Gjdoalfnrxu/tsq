// Round-3 case C: string-constant computed-key Provider field.
//
//   const KEY_A = "setCA";
//   const obj = { [KEY_A]: setCA, ["setCB"]: setCB };
//   <Ctx.Provider value={obj}>...</Ctx.Provider>
//
// Two computed-key shapes:
//   - identifier whose binding is `const KEY_A = "setCA"` (resolved at extraction)
//   - inline string literal `["setCB"]`
// Both must be recognised as named fields.

import { useState, useContext, createContext, ReactNode } from 'react';

interface ComputedActions {
  setCA: (updater: (prev: number) => number) => void;
  setCB: (updater: (prev: number) => number) => void;
}

export const ComputedCtx = createContext<ComputedActions | null>(null);

const KEY_A = "setCA";

export function ComputedProvider({ children }: { children: ReactNode }) {
  const [a, setCA] = useState(0);
  const [b, setCB] = useState(0);
  void a; void b;
  const obj = {
    [KEY_A]: setCA,
    ["setCB"]: setCB,
  };
  return (
    <ComputedCtx.Provider value={obj}>{children}</ComputedCtx.Provider>
  );
}

export function useComputedActions() {
  return useContext(ComputedCtx);
}

export function ComputedConsumer() {
  const { setCA, setCB } = useComputedActions()!;
  return (
    <button
      onClick={() => {
        setCA(prev => {
          setCB(p => p + 1);
          return prev + 1;
        });
      }}
    >
      computed
    </button>
  );
}
