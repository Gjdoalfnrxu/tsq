// Whole-closure integration fixture — Phase C PR7 §7/§8.
//
// SHAPE: direct prop pass (R1 analogue). Designed to exercise
// "lfsVarInit + lfsParamBind composed" — value-source through a
// JSX prop into a parameter binding.
//
// PR7 note: under the current PR6 closure the advertised
// lfsVarInit+lfsParamBind composition does NOT fire — no row lands
// on Inner's `value` parameter (line 23). See follow-up issue #202.
// The pins in valueflow_closure_integration_test.go assert only
// identity-observed reachability until that gap is closed.
//
// Observed reachability set (mayResolveToRec; see
// mayResolveTo.expected.csv for the machine-readable form):
//
//   DirectProp.tsx:21 → DirectProp.tsx:21   (import stmt base)
//   DirectProp.tsx:28 → DirectProp.tsx:28   (cfg object literal base)
//   DirectProp.tsx:29 → DirectProp.tsx:28   (JSX expr → cfg literal)
//   DirectProp.tsx:29 → DirectProp.tsx:29   (JSX expr base)

import { ReactNode } from 'react';

function Inner({ value }: { value: unknown }): ReactNode {
  return value as ReactNode;
}

export function DirectProp(): ReactNode {
  const cfg = { tag: 'src' };
  return <Inner value={cfg} />;
}
