// Re-exporter. The ifsImportExport rule bridges the `svc` symbol from
// module_a into module_b's export surface; index.ts imports from here.

export { svc } from './module_a';
