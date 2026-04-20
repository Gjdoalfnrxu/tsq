// Adversarial fixture — Phase C PR7 §7/§8.4.
//
// SHAPE: name-colliding cross-module imports. Two modules export the
// same name `action` with different implementations. The consumer
// imports from one of them; the closure's ifsImportExport rule does
// NOT key on the module specifier (documented over-bridging from plan
// §3.2 / §4.1), so the closure will resolve `action` to BOTH
// implementations.
//
// This fixture pins that over-approximation behaviour as documented —
// a future "fix" that silently tightens the rule would regress it.

export const action = () => 'alpha';
