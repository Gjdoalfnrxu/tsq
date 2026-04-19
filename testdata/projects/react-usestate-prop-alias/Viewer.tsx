import { useState } from 'react';

interface ZoomConfig {
  zoom: number;
  pan: number;
}

interface ZoomControlProps {
  onConfigChange: (updater: (prev: ZoomConfig) => ZoomConfig) => void;
}

// Child component receives a setState alias via the `onConfigChange` prop
// and invokes it with an updater function. The updater's body calls
// ANOTHER setState (here aliased through `onLog`), demonstrating the
// outer + inner alias case.
function ZoomControl({ onConfigChange }: ZoomControlProps) {
  return (
    <button
      onClick={() => {
        onConfigChange(prev => {
          // Inner setState: calls the alias `onLog` provided as a prop.
          // For the simple variant we still want to catch the case where
          // ONLY the outer call is aliased; this fixture covers both.
          return { ...prev, zoom: prev.zoom + 1 };
        });
      }}
    >
      Zoom in
    </button>
  );
}

interface LoggerProps {
  onLog: (msg: string) => void;
}

function Logger({ onLog }: LoggerProps) {
  return <button onClick={() => onLog('hi')}>log</button>;
}

// Outer component holds both pieces of state and passes their setters down.
export function Viewer() {
  const [zoomConfig, setZoomConfig] = useState<ZoomConfig>({ zoom: 1, pan: 0 });
  const [log, setLog] = useState<string>('');

  // Direct case (no aliasing) — outer setter is setZoomConfig itself,
  // inner setter is also a direct setter via setLog.
  const onResetDirect = () => {
    setZoomConfig(prev => {
      setLog('reset');
      return { ...prev, zoom: 1 };
    });
  };

  // Alias case — `setZoomConfig` is passed through `onConfigChange`,
  // and inside ZoomControl the updater body indirectly triggers `setLog`
  // via Logger's `onLog` prop alias. We pass `setLog` directly to Logger
  // here as a sibling prop alias to exercise the predicate; the cross-
  // component "updater calls aliased setter" pattern requires the inner
  // call to live inside the outer updater scope, which we cover with the
  // intra-component case below.
  return (
    <div onClick={onResetDirect}>
      <ZoomControl onConfigChange={setZoomConfig} />
      <Logger onLog={setLog} />
      <Mixed onConfigChange={setZoomConfig} onLog={setLog} />
    </div>
  );
}

interface MixedProps {
  onConfigChange: (updater: (prev: ZoomConfig) => ZoomConfig) => void;
  onLog: (msg: string) => void;
}

// Intra-component prop-alias outer + inner case: both aliases live inside
// the same component body, and the outer alias's updater body invokes the
// inner alias. This is the canonical positive case for
// setStateUpdaterCallsOtherSetStateThroughProps.
function Mixed({ onConfigChange, onLog }: MixedProps) {
  return (
    <button
      onClick={() => {
        onConfigChange(prev => {
          onLog('zooming');
          return { ...prev, zoom: prev.zoom + 1 };
        });
      }}
    >
      Zoom + log
    </button>
  );
}
