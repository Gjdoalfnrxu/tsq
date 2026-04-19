// Negative fixtures for round-4: hook-return resolution must NOT match these.

import { useState, useContext, createContext, ReactNode } from 'react';

interface NegActions {
  setNA: (updater: (prev: number) => number) => void;
}

const NegCtx = createContext<NegActions | null>(null);

// (N1) Hook returns a NON-object (a number). resolveToObjectExprHookReturn
// must not surface anything for the consumer below.
function useNegNumber(): number {
  const [n, setNA] = useState(0);
  void setNA;
  return n;
}

export function NegNumberProvider({ children }: { children: ReactNode }) {
  // value is a primitive. ObjectLiteral resolution must not trigger.
  const value = useNegNumber();
  void value;
  return <NegCtx.Provider value={null}>{children}</NegCtx.Provider>;
}

// (N2) Hook with conditional return — one branch is an ObjectLiteral, the
// other is null. Single-branch over-approximation is acceptable (it's an
// over-approximation matching round-1/2/3 conventions), but the consumer
// here doesn't even hit the chain because the Provider value is null.
function useMaybeActions(): NegActions | null {
  const [n, setNA] = useState(0);
  void n;
  if (Math.random() > 0.5) {
    return { setNA };
  }
  return null;
}

export function MaybeProvider({ children }: { children: ReactNode }) {
  const actions = useMaybeActions();
  void actions;
  // Deliberately bind value to literal null so the consumer chain dies
  // upstream of resolveToObjectExpr.
  return <NegCtx.Provider value={null}>{children}</NegCtx.Provider>;
}

export function NegConsumer() {
  const v = useContext(NegCtx);
  void v;
  // No setter calls — purely a non-match consumer, ensures useState setters
  // wired up in the providers above don't get spuriously surfaced through
  // unrelated chains.
  return null;
}
