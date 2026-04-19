// Branch JsxWrapped — JsxExpression-wrapper-tolerant resolution.
//
// `<Provider value={X} />` shape: the JsxAttribute's valueExpr column points
// at the JsxExpression wrapper `{X}`, NOT the inner Identifier `X`. The
// six base branches of `mayResolveTo` require `ExprMayRef(valueExpr, sym)`
// directly on `valueExpr`, so without wrapper handling they silently miss
// every `value={X}` case (the bug that surfaced when PR3 first attempted
// to substitute mayResolveToVarInit for resolveToObjectExprVarD1).
//
// The wrapper-tolerant variant in `mayResolveTo` unwraps a single
// JsxExpression layer and re-runs the core branches against the inner
// expression. Below the inner Identifier `theme` is a sym whose VarDecl
// initialiser is the object-literal value-source — i.e. the var-init
// branch fires on the unwrapped form.

import { ReactNode } from 'react';

const Theme = { current: { color: 'red' } } as any;

export function ThemeProvider({ children }: { children: ReactNode }) {
  const theme = { color: 'red' };
  return (
    <Theme.Provider value={theme}>{children}</Theme.Provider>
  );
}
