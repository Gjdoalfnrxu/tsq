# 04 — Replace tsgo JSON-RPC client with direct Go API

## Scope

The original compat plan (section 3a) said tsgo would be imported as a
Go module and called in-process. The shipped implementation (PR #33)
instead spawns tsgo as a subprocess and talks to it over JSON-RPC 2.0
with LSP framing. This works but re-introduces the subprocess/IPC
overhead the plan explicitly wanted to avoid.

This PR replaces the JSON-RPC transport with a direct Go import of
tsgo's checker packages, preserving the existing public API of
`extract/typecheck` so callers don't change.

This PR does NOT change the fact schema, does NOT add new type
relations, and does NOT touch the bridge `.qll` files. It is a
pure transport swap behind a stable interface.

## Dependencies

None among the impl plans. Depends on tsgo upstream exposing enough
public API to perform program construction, file checking, and
type/symbol lookup. See risks.

## Files to change

- `/tmp/tsq/go.mod` — add tsgo (or `typescript-go`) dependency; run
  `go mod tidy`.
- `/tmp/tsq/extract/typecheck/client.go` — replace the subprocess +
  JSON-RPC implementation with direct calls. Preserve the exported
  method set: `NewClient`, `CheckFile`, `GetTypeAt`, `GetSymbolAt`,
  `Close`.
- `/tmp/tsq/extract/typecheck/jsonrpc.go` — delete (or retain for
  fallback, gated by build tag `//go:build tsgo_jsonrpc`).
- `/tmp/tsq/extract/typecheck/detect.go` — replace binary detection
  with a no-op; direct import means no external binary.
- `/tmp/tsq/extract/typecheck/client_test.go` — update tests that
  assumed the subprocess model; add a pure-in-process smoke test.
- `/tmp/tsq/extract/typecheck/enricher.go` — read only, verify it
  calls the preserved API surface.
- `/tmp/tsq/HANDOVER-phase-3a.md` — update the "Transport" section to
  reflect the change.

## Implementation steps

1. Probe tsgo upstream (`github.com/microsoft/typescript-go`) and
   identify the public packages that expose program creation, file
   checking, and type/symbol access. Record findings in the PR
   description.
2. If required API is available: add dependency, write a thin wrapper
   in `client.go` that matches the existing interface.
3. If required API is NOT fully available: either (a) open an upstream
   issue and pause this PR, or (b) use `internal` packages via a
   `replace` directive and document the coupling.
4. Delete the JSON-RPC framing code or gate it behind a build tag.
5. Update `detect.go` so `NewClient` no longer requires a tsgo binary
   on PATH.
6. Run the enricher tests (`go test ./extract/typecheck/...`); fix
   anything that assumed subprocess semantics (stderr routing,
   timeouts, etc.).
7. Update HANDOVER doc.

## Test strategy

- `extract/typecheck/client_test.go::TestDirectCheckerBasic` — create
  a checker, check a one-file TS program, assert a known type at a
  known position.
- `extract/typecheck/enricher_test.go` — existing tests must still
  pass unchanged (the interface is stable).
- `integration_test.go` — existing extraction tests that pull in type
  facts should not regress.

## Acceptance criteria

- [ ] `go.mod` has a direct tsgo dependency; `go mod tidy` clean.
- [ ] `extract/typecheck/client.go` has no `os/exec`, no JSON-RPC.
- [ ] Exported interface of `extract/typecheck` unchanged.
- [ ] All existing enricher tests pass.
- [ ] Smoke test for the new transport passes.
- [ ] `go test ./...` green.

## Risks and open questions

- **tsgo API maturity.** The package may still be experimental with
  no stable Go-facing checker API. The compat plan explicitly flags
  three fallbacks (upstream contribution, internal packages, Node.js
  bridge). If none is viable, this PR cannot land — note that
  outcome in the PR and close without merging.
- **Build weight.** Importing the TS checker pulls a lot of code into
  tsq's binary. Confirm binary size stays acceptable.
- **CGO / platform issues.** Depending on the upstream, Windows or
  musl-libc builds might regress.
- **Test flakiness.** The JSON-RPC client had subprocess startup
  timing logic; in-process removes that but may introduce global
  state (checker caches) that tests must reset between cases.
- **Licence.** tsgo is MIT — still confirm no additional copyleft
  transitive deps appear in `go.mod`.

## Out of scope

- Type fact schema changes.
- Any new tsgo features beyond what the JSON-RPC client already
  consumed.
- Performance benchmarking the transport swap (a separate follow-up
  PR can measure it).
