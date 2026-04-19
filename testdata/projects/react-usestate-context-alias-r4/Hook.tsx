// Cross-module hook indirection re-export of useContext for the FactoryCtx.
// Mirrors round-2's Hook.tsx so we exercise the existing
// `useContextCallSiteResolvesContext` path for the consumer destructure.
import { useContext } from 'react';
import { FactoryCtx } from './Actions';

export function useFactoryActionsCtx() {
  return useContext(FactoryCtx);
}
