# HANDOVER: v2 Phase A1 — Vendor typescript-go

## Status: Complete (PR #14)

## What was done

### 1. typescript-go research findings

**typescript-go cannot be used as a Go library.** Three blockers:

1. **All Go packages are under `internal/`** — Go's visibility rules prevent any external module from importing them. The parser, checker, AST types, binder — everything is internal.
2. **Requires Go 1.26+** — Our toolchain is Go 1.23.8. The module's `go.mod` specifies `go 1.26`.
3. **API explicitly "not ready"** — The project README's status table lists "API" as "not ready".

**What tsgo does expose:** A CLI binary (`tsgo`) with three modes:
- Default: compiler (like `tsc`)
- `--lsp`: Language Server Protocol
- `--api --async`: JSON-RPC over stdin/stdout for programmatic access

### 2. Implementation approach

Given the library blockers, VendoredBackend uses a **subprocess architecture**:

- **AST walking:** Delegates to TreeSitterBackend (reused, not duplicated). tsgo's parser is internal-only.
- **Semantic analysis:** Spawns `tsgo --api --async --cwd <root>` as a subprocess. Communicates via JSON-RPC 2.0 over stdin/stdout.
- **Graceful degradation:** When tsgo binary is not found, Open succeeds, WalkAST works, semantic methods (ResolveSymbol, ResolveType, CrossFileRefs) return `ErrUnsupported`.

### 3. Files added/modified

| File | Purpose |
|------|---------|
| `extract/backend_vendored.go` | VendoredBackend implementing ExtractorBackend |
| `extract/tsgonode.go` | tsgoNode adapter: maps tsgo kinds to tsq canonical names |
| `extract/backend_vendored_test.go` | 17 tests for VendoredBackend |
| `extract/tsgonode_test.go` | Tests for kind normalisation, positions, children |
| `cmd/tsq/main.go` | `--backend vendored|treesitter` flag on extract command |
| `testdata/ts/vendored/*.ts` | Test fixtures (simple.ts, arrow.ts, types.ts) |

### 4. CLI flag

```
tsq extract --backend vendored --dir ./myproject
tsq extract --backend treesitter --dir ./myproject  # default
```

## What's NOT done (for future phases)

1. **tsgo is not installed** — No npm install of `@typescript/native-preview` is performed. The backend gracefully degrades.
2. **JSON-RPC protocol details** — The exact tsgo API methods (`getDefinition`, `getQuickInfo`, `getReferences`) are placeholder names based on LSP conventions. The actual tsgo `--api` protocol needs documentation/testing when tsgo is available.
3. **Type-enriched facts** — The FactWalker doesn't yet use ResolveType/ResolveSymbol even when available. That's Phase A2+ work.

## Assumptions to verify in next phase

- tsgo `--api --async` mode uses JSON-RPC 2.0 with Content-Length framing (LSP-style). Needs verification.
- Method names in the RPC protocol need to match tsgo's actual API surface.
- Performance of subprocess approach vs. potential Go 1.26 upgrade path.

## How to test with tsgo

```bash
npm install -g @typescript/native-preview
# Ensure tsgo is in PATH
tsq extract --backend vendored --dir ./some-ts-project
```
