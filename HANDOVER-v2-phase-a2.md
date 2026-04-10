# HANDOVER: v2 Phase A2 — Vendored Structural Parity

## Status: Complete

## Summary

Phase A2 verified that VendoredBackend produces identical structural facts to TreeSitterBackend. Since VendoredBackend delegates all AST walking to an embedded TreeSitterBackend instance, structural parity is automatic — the same tree-sitter parser, the same FactWalker, the same NodeID/SymID generation. The primary deliverable is the verification test suite itself.

## What was done

### 1. Compatibility test suite (`extract/compat_test.go`)

Four test functions that extract the same TypeScript projects with both backends and compare fact databases tuple-by-tuple:

| Test | What it verifies |
|------|-----------------|
| `TestBackendCompatibility` | All 5 testdata projects (simple, react-component, async-patterns, destructuring, imports) produce identical facts across all 28 schema relations |
| `TestBackendCompatibility_Roundtrip` | Parity is preserved through encode/decode serialisation |
| `TestBackendCompatibility_EmptyProject` | Both backends handle empty directories identically |
| `TestBackendCompatibility_SingleFile` | Parity on a synthetic single-file project for easier debugging |

The comparison uses `compareDBs()` which iterates every relation in the schema registry, serialises all tuples to `col0|col1|...` strings, sorts them, and asserts equality. This catches any discrepancy in node IDs, file IDs, symbol IDs, or relation contents.

### 2. VendoredScopeAdapter (`extract/vendored_scope.go`)

Adapter that wraps symbol resolution for the vendored backend:

- When **tsgo is available**: tries tsgo's `getDefinition` RPC first for cross-file symbol resolution, falls back to ScopeAnalyzer on failure
- When **tsgo is absent** (current state): delegates entirely to the existing in-file ScopeAnalyzer — identical to TreeSitterBackend behaviour
- Implements `Resolve(name, atNode)` and `Build(root)` matching the ScopeAnalyzer interface the FactWalker uses

### 3. End-to-end CLI tests (`extract/vendored_e2e_test.go`)

Three test functions exercising the full `tsq extract --backend vendored` pipeline:

| Test | What it verifies |
|------|-----------------|
| `TestVendoredBackend_E2E_ExtractAndSerialize` | All 5 projects: extract → encode → decode → verify expected relations have data |
| `TestVendoredBackend_E2E_FileOutput` | Write to disk file → read back → verify SchemaVersion and Node tuples |
| `TestVendoredBackend_E2E_DegradedMode` | Confirms structural facts (Function, Call, etc.) are present even without tsgo |

### 4. Files added

| File | Purpose |
|------|---------|
| `extract/compat_test.go` | Backend compatibility test suite (4 tests) |
| `extract/vendored_scope.go` | VendoredScopeAdapter for tsgo-enhanced scope resolution |
| `extract/vendored_e2e_test.go` | End-to-end pipeline tests (3 tests) |
| `HANDOVER-v2-phase-a2.md` | This document |

## Key finding

**Structural parity is inherent, not earned.** Because VendoredBackend.WalkAST() is a one-line delegation to TreeSitterBackend.WalkAST(), and the FactWalker is backend-agnostic, there is zero divergence in structural facts. The compatibility tests confirm this across all 28 relations and all 5 test projects.

## What's NOT done (for future phases)

1. **tsgo integration testing** — tsgo is not installed; all tests run in degraded mode. The VendoredScopeAdapter's tsgo path is untested against a real subprocess.
2. **Cross-file symbol resolution** — The adapter is wired but inactive without tsgo. When tsgo becomes available, Symbol/FunctionSymbol/CallResultSym relations could be enriched.
3. **Type-enriched facts** — ResolveType is not yet used by the FactWalker. TypeFromLib remains empty.
4. **Performance comparison** — No benchmarks comparing vendored vs treesitter extraction speed (expected identical since they share the same code path).

## Test results

All 15 existing tests + 7 new tests pass. Full suite green:

```
ok  github.com/Gjdoalfnrxu/tsq/extract  2.973s
```
