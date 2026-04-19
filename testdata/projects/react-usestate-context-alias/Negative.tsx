// Negative cases: setters that are NOT context-aliased should not match the
// context-through predicate. (They may still match the direct-form predicate
// — that's by design and out of scope for this fixture's negative coverage.)

import { useState } from 'react';

// 1) Plain useState updater that calls another plain useState setter — the
// DIRECT form would catch this; the context predicate must NOT.
export function Plain() {
  const [a, setA] = useState(0);
  const [b, setB] = useState(0);
  return (
    <button
      onClick={() => {
        setA(prev => {
          setB(b + 1);
          return prev + 1;
        });
      }}
    >
      {a + b}
    </button>
  );
}

// 2) An identifier called `setX` that has nothing to do with useState or
// useContext — purely a local function. Must not be flagged.
export function Bare() {
  const setNothing = (n: number) => n + 1;
  return <button onClick={() => setNothing(1)}>noop</button>;
}
