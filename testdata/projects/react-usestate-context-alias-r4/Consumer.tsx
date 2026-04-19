// Consumer for round-4. The setters arrive through a context whose
// Provider value is bound to the result of a factory hook call:
//
//   const actions = useFactoryActions();
//   <FactoryCtx.Provider value={actions}>
//
// Round-3's `resolveToObjectExpr` died at this hop because `actions` is
// bound to a CallExpression, not an ObjectLiteral. Round-4 follows the
// call into the hook's return statement (`const actions = { ... };
// return actions;` — VarDecl-bound shape).

import { useFactoryActionsCtx } from './Hook';

export function FactoryConsumer() {
  const { setFA, setFB } = useFactoryActionsCtx()!;
  return (
    <button
      onClick={() => {
        setFA(prev => {
          // Inner setter setFB arrived through the same factory hook return.
          setFB(p => p + 1);
          return prev + 1;
        });
      }}
    >
      factory
    </button>
  );
}
