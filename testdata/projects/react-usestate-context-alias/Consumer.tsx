import { useViewerActions } from './Hook';

// The motivating positive case: outer setter (`setZoom`) is reached through
// the context-alias closure, AND its updater body invokes a DIFFERENT
// context-alias setter (`setPan`). The bridge predicate
// `setStateUpdaterCallsOtherSetStateThroughContext` should match the outer
// `setZoom(...)` call.
export function ZoomButton() {
  const { setZoom, setPan } = useViewerActions()!;
  return (
    <button
      onClick={() => {
        setZoom(prev => {
          // Inner setter: `setPan` arrived through the same context hop.
          // Both outer and inner are context-aliased setters with different
          // callee symbols.
          setPan(p => ({ ...p, pan: p.pan + 1 }));
          return { ...prev, zoom: prev.zoom + 1 };
        });
      }}
    >
      Zoom + Pan
    </button>
  );
}
