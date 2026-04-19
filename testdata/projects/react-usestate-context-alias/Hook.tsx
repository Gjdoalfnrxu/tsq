import { useContext } from 'react';
import { ViewerStateActions } from './Provider';

// Hook indirection: a function whose body returns useContext(ContextSym).
// Round-2 should recognise calls to `useViewerActions()` as resolving to the
// same value that useContext(ViewerStateActions) resolves to — which is the
// Provider's `value={{ setZoom, setPan }}` object.
export function useViewerActions() {
  return useContext(ViewerStateActions);
}
