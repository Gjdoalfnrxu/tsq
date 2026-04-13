# Plan 20 — Path Traversal Sink Extraction

**Status:** not started
**Phase:** Extends taint sink coverage
**Dependencies:** Plan 18 (FunctionSymbol for imports)

## Problem

No extraction rule exists for `path_traversal` sinks. The `frameworks.go`
file defines sanitizer rules for command injection and SQL injection but has
no `TaintSink` rules for file system operations.

## Scope

Add `TaintSink(expr, "path_traversal")` rules for:

1. `fs.readFile(path)` — first argument
2. `fs.readFileSync(path)` — first argument
3. `fs.writeFile(path, data)` — first argument
4. `fs.writeFileSync(path, data)` — first argument
5. `fs.unlink(path)` — first argument
6. `fs.access(path)` — first argument
7. `path.join(...)` when result flows to fs operations (transitive — may be
   covered by FlowStar once the direct sinks are defined)

These require matching `ImportBinding` for the `fs` module and `MethodCall`
for the specific method.

## Files to modify

- `extract/rules/frameworks.go` — add path traversal sink rules
- Add corresponding sanitizer rules for `path.normalize`, `path.resolve`

## Acceptance criteria

- Multi-vuln golden includes `path_traversal` sink kind for fs.readFile calls
- Targeted compat test with fs operations
