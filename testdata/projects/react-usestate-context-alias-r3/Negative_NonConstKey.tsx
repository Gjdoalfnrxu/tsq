// Round-3 negative: non-const computed key must NOT over-match.
//
//   let dynamicKey = "setNN";  // let, not const
//   function compute() { return "setNN"; }
//   const obj = { [dynamicKey]: setNN, [compute()]: setNN };
//   <Ctx.Provider value={obj}>...</Ctx.Provider>
//
// Neither computed key is a const-string-literal binding, so the bridge must
// emit zero ObjectLiteralField rows for these fields and the consumer's
// `setNN(...)` call must NOT be recognised as a context-aliased setter on
// the strength of these computed keys.

import { useState, useContext, createContext, ReactNode } from 'react';

interface NegActions {
  setNN: (updater: (prev: number) => number) => void;
}

export const NegCtx = createContext<NegActions | null>(null);

let dynamicKey = "setNN";

function compute() {
  return "setNN";
}

export function NegProvider({ children }: { children: ReactNode }) {
  const [a, setNN] = useState(0);
  void a;
  // Both keys are non-const-string and must be skipped at extraction time.
  const obj = {
    [dynamicKey]: setNN,
    [compute()]: setNN,
  };
  return (
    <NegCtx.Provider value={obj}>{children}</NegCtx.Provider>
  );
}

export function useNegActions() {
  return useContext(NegCtx);
}

export function NegConsumer() {
  const ctx = useNegActions()!;
  // We deliberately read the field via member access (no destructure) — so
  // even if the predicate over-matched on the keys, the consumer wouldn't
  // create a destructure binding. But the assertion in the test is on the
  // bridge-level field set: contextProviderField for NegCtx must contain no
  // rows attributable to these computed keys.
  const setNN = (ctx as unknown as NegActions).setNN;
  return (
    <button
      onClick={() => {
        setNN(prev => prev + 1);
      }}
    >
      neg
    </button>
  );
}

// Suppress dynamicKey unused-mutation lint: we only read its initial value at
// the object literal site; reassignment elsewhere isn't necessary for the
// negative case but documents intent.
export function _touchDynamicKey() {
  dynamicKey = "setNN";
  return dynamicKey;
}
