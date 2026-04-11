import { useState } from 'react';

function helper(n: number): number {
  return n + 1;
}

export function Counter() {
  const [count, setCount] = useState(0);
  const [name, setName] = useState("");

  // Case A: updater function calls another function
  const onClick = () => {
    setCount(prev => helper(prev));
  };

  // Case B: updater function calls ANOTHER setState
  const onReset = () => {
    setCount(prev => {
      setName("");
      return 0;
    });
  };

  // Plain (not an updater function) — should NOT match
  const onClear = () => setCount(0);

  // Updater with no function call in body — should NOT match Case A
  const onBump = () => setCount(prev => prev + 1);

  return <button onClick={onClick}>{count} {name}</button>;
}
