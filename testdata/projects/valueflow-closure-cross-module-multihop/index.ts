// Whole-closure integration fixture — Phase C PR7 §7/§8.
//
// SHAPE: multi-hop cross-module import. A value-source in module A is
// re-exported from module B and consumed in the entry module. The
// closure must walk ifsImportExport twice (A → B → index).
//
// Observed reachability set (mayResolveToRec; see
// mayResolveTo.expected.csv):
//
//   module_a.ts:4  → module_a.ts:4    (arrow `() => 1` base)
//   module_b.ts:4  → module_b.ts:4    (re-export base)
//   index.ts:17    → index.ts:17      (import stmt base)
//   index.ts:17    → module_a.ts:4    (import reaches source — ifsImportExport chain)
//   index.ts:19    → index.ts:19      (run function base)
//   index.ts:20    → module_a.ts:4    (callee `svc()` reaches source, load-bearing multi-hop)

import { svc } from './module_b';

export function run(): number {
  return svc();
}
