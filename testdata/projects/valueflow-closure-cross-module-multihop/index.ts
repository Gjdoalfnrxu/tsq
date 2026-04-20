// Whole-closure integration fixture — Phase C PR7 §7/§8.
//
// SHAPE: multi-hop cross-module import. A value-source in module A is
// re-exported from module B and consumed in the entry module. The
// closure must walk ifsImportExport twice (A → B → index).
//
// Hand-computed expected reachability set:
//
//   sourceExpr in module_a.ts line 4 (arrow `() => 1`) reaches:
//     - module_a.ts line 4 itself            (base)
//     - module_a.ts line 5 `svc` VarDecl init (lfsVarInit forward)
//     - module_b.ts line 2 re-exported symbol (ifsImportExport hop 1)
//     - index.ts   line 2 imported `svc`      (ifsImportExport hop 2)
//     - index.ts   line 4 callee of svc()     (lfsVarInit forward)

import { svc } from './module_b';

export function run(): number {
  return svc();
}
