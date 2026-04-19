import { useState, createContext, ReactNode } from 'react';

interface ZoomConfig {
  zoom: number;
  pan: number;
}

interface ViewerActions {
  setZoom: (updater: (prev: ZoomConfig) => ZoomConfig) => void;
  setPan: (updater: (prev: ZoomConfig) => ZoomConfig) => void;
}

// The context symbol — `ViewerStateActions` is the VarDecl bound to the
// createContext call result. Its `.Provider` JSX surface is the
// "context provider" pattern the round-2 alias closure needs to recognise.
export const ViewerStateActions = createContext<ViewerActions | null>(null);

// Provider component holds the useState pair locally and exposes the setter
// through the Context Provider's `value` prop using a shorthand object literal.
// Both setZoom and setPan are useState setters by the round-1 base case — the
// new round-2 hop is "make these also useStateSetterAlias when reached through
// useContext + destructure".
export function ViewerProvider({ children }: { children: ReactNode }) {
  const [zoom, setZoom] = useState<ZoomConfig>({ zoom: 1, pan: 0 });
  const [pan, setPan] = useState<ZoomConfig>({ zoom: 1, pan: 0 });
  // suppress unused-var noise from the type-side
  void zoom;
  void pan;
  return (
    <ViewerStateActions.Provider value={{ setZoom, setPan }}>
      {children}
    </ViewerStateActions.Provider>
  );
}
