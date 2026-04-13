# Plan 18 — FunctionSymbol for Import Bindings (Sanitizer Fix)

**Status:** not started
**Phase:** Fixes sanitizer pipeline (blocking E2E sanitizer-bypass test)
**Dependencies:** none

## Problem

`Sanitizer(fn, kind)` is derived from `ImportBinding(localSym, module, name)` +
`FunctionSymbol(localSym, fn)`. However, `FunctionSymbol` is only emitted for
locally-defined functions (arrow functions and function expressions assigned to
variables). Import bindings like `const escapeHtml = require('escape-html')` or
`import escapeHtml from 'escape-html'` never produce `FunctionSymbol` facts.

Result: the `Sanitizer` relation is always empty, `SanitizedEdge` never fires,
and all taint flows through sanitizer calls unchecked.

## Scope

Extend `FunctionSymbol` emission in the extraction pipeline so that:

1. CJS default imports: `const x = require('mod')` → `FunctionSymbol(sym(x), fn_id)`
2. CJS named imports: `const { escape } = require('sqlstring')` → `FunctionSymbol(sym(escape), fn_id)`
3. ESM default imports: `import x from 'mod'` → `FunctionSymbol(sym(x), fn_id)`
4. ESM named imports: `import { escape } from 'mod'` → `FunctionSymbol(sym(escape), fn_id)`

The `fn_id` for imported functions should be a synthetic ID derived from the
import binding, since there is no local function definition to reference.

## Files to modify

- `extract/walker.go` or `extract/type_aware_walker.go` — add FunctionSymbol
  emission for import bindings during extraction
- `extract/rules/frameworks.go` — verify sanitizer rules work once FunctionSymbol
  is populated (may need no changes)

## Acceptance criteria

- `go test -run TestCompat/sanitizer_bypass -update` produces a golden with
  fewer rows (routes 2 and 5 should no longer alert)
- Existing tests still pass
- Add a unit test in `extract/` that verifies FunctionSymbol is emitted for
  each import pattern

## Verification

After implementing, regenerate the sanitizer-bypass golden:
```bash
go test -run TestCompat/sanitizer_bypass -update -count=1 ./...
git diff testdata/compat/expected/find_sanitizer_bypass.csv
```

The diff should show routes with correct-kind sanitizers disappearing from the golden.
