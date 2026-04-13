# Plan 19 — Command Injection Sink Extraction

**Status:** not started
**Phase:** Extends taint sink coverage
**Dependencies:** Plan 18 (FunctionSymbol for imports — needed for destructured imports)

## Problem

`command_injection` sinks are defined in `extract/rules/frameworks.go` but
require `FunctionSymbol` resolution through destructured imports like
`const { exec } = require('child_process')`. Since FunctionSymbol is not
emitted for import bindings (Plan 18), these sinks never fire.

## Scope

Once Plan 18 lands, verify that command injection sinks work for:

1. `const { exec } = require('child_process'); exec(tainted)` → sink
2. `const { execSync } = require('child_process'); execSync(tainted)` → sink
3. `const { spawn } = require('child_process'); spawn(tainted)` → sink
4. `const cp = require('child_process'); cp.exec(tainted)` → sink (method call pattern)

Pattern 4 may require additional work: `MethodCall` + `ImportBinding` join
rather than `FunctionSymbol`.

## Files to modify

- `extract/rules/frameworks.go` — verify/extend command injection rules
- `testdata/compat/projects/multi-vuln/` — update golden if cmd injection
  sinks start firing

## Acceptance criteria

- Multi-vuln golden includes `command_injection` sink kind for exec calls
- Add a targeted compat test: `find_cmdi.ql` with a fixture containing
  child_process usage patterns
