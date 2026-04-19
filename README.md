# tsq

`tsq` is a clean-room, CodeQL-compatible static analysis engine for TypeScript and TSX, written in Go. It uses tree-sitter to extract facts from source, stores them in a datalog-style fact database, and evaluates QL queries against that database — so you can write CodeQL-shaped queries against TypeScript projects without depending on the CodeQL toolchain.

## Status

Early but useful. The v1 evaluator is feature-complete enough to run real queries against React fixtures end-to-end (extract → fact DB → QL → results), including the useState updater-pattern queries under `testdata/queries/v2/`. The native bridge library (`bridge/tsq_*.qll`) is the supported query surface today. A CodeQL compatibility layer (`bridge/compat_*.qll`, `import javascript`, `TaintTracking`, `DataFlow::PathGraph`) is in progress — some queries written against the upstream CodeQL JS libraries will run, many will not. See `CODEQL-COMPAT-PLAN.md` for the current state.

Optional `tsgo` type enrichment will run automatically if a `tsgo` binary is on `PATH`; without it, type-aware relations are simply empty and the rest of the pipeline still works.

## Install

Requires Go 1.23+ and a C toolchain (the tree-sitter backend uses cgo).

```
git clone https://github.com/Gjdoalfnrxu/tsq.git
cd tsq
CGO_ENABLED=1 go build -o bin/tsq ./cmd/tsq
```

The binary lands at `bin/tsq`. There is also a `make build` target that does the same thing. `make setup` installs the repo's git hooks (formatters and linters).

Verify the build:

```
$ bin/tsq version
tsq version 0.1.0
```

## Quick start

Extract a tiny TSX project to a fact database, then run a query against it. The repo ships a React useState fixture you can use directly.

```
mkdir -p /tmp/tsq-demo && cd /tmp/tsq-demo
cp /path/to/tsq/testdata/projects/react-usestate/Counter.tsx .

# 1. Extract facts from the current directory into counter.db
#    (--tsgo=off skips type enrichment so this works without tsgo on PATH)
tsq extract --dir . --tsgo=off --output counter.db

# 2. Run a v2 query against the fact DB
tsq query --db counter.db --format json \
  /path/to/tsq/testdata/queries/v2/find_setstate_updater_calls_fn.ql
```

Expected output (extract logs go to stderr, query results to stdout):

```
extracting from . (requires CGO_ENABLED=1 for tree-sitter)...
wrote counter.db
{"col0":-840152295,"col1":13}
{"col0":701633112,"col1":18}
{"col0":-227156425,"col1":37}
```

The three result rows correspond to the three `setCount(prev => …)` calls in `Counter.tsx` whose updater body invokes another function (lines 13, 18, and 37). `col0` is the call's entity ID; `col1` is the source line.

You can also ask for SARIF or CSV with `--format sarif` / `--format csv`, and run `tsq check QUERY_FILE.ql` to type-check a query without evaluating it.

### Markdown reports

For human review, `--format markdown` (alias `md`) renders each result as a `## path:line` section with a fenced code snippet of the offending source plus ±5 lines of context, headed by the query's `@name` / `@description` / `@id` and footed with the result count and wall time.

```bash
tsq query --db counter.db --format markdown \
  --source-root . \
  /path/to/tsq/testdata/queries/v2/find_setstate_updater_calls_fn.ql \
  > report.md
```

Flags:

- `--source-root DIR` — base directory for resolving relative file paths in result rows. Required for snippet rendering when paths are relative; also enables a path-traversal guard that skips rows whose paths escape the root.
- `--md-file-col N` / `--md-line-col N` — column indices (0-based) for the file path and line number when the planner can't infer them from the column names. Defaults to auto-detect (`-1`); set explicitly when your `select` aliases are dropped or when columns are positional.

If a row has no resolvable file/line, it falls through to a bullet list of the raw column values so nothing is silently dropped.

## How it works

1. **Extract.** `extract/` walks a project, parses each `.ts` / `.tsx` file with tree-sitter, and writes typed tuples (functions, calls, variables, JSX elements, imports, …) into an in-memory fact database (`extract/db`).
2. **Type enrichment (optional).** If a `tsgo` binary is reachable, `extract/typecheck` queries it for resolved types at variable and parameter positions and populates `ResolvedType` / `ExprType` / `SymbolType` relations. Without it, those relations are empty and downstream queries that depend on them simply produce no rows.
3. **QL pipeline.** `ql/parse` → `ql/resolve` → `ql/desugar` → `ql/plan` → `ql/eval` parses a `.ql` source, resolves imports against the embedded bridge library, lowers it to a datalog program, plans it, and evaluates it semi-naively over the fact DB.
4. **Bridge.** `bridge/` is the QL-side surface: `tsq_*.qll` defines the native classes (`Call`, `Function`, `VarDecl`, `JSXElement`, …) that map onto the extracted relations. `compat_*.qll` is the in-progress CodeQL compatibility layer.

For deeper notes see `CODEQL-COMPAT-PLAN.md` and the various `HANDOVER-*.md` documents.

## Writing queries

A tsq query is a `.ql` file that imports one or more bridge modules and selects rows from them. Queries follow CodeQL's QL surface syntax: `from`, `where`, `select`, classes with member predicates, etc. The native bridge lives in `bridge/tsq_*.qll` and is imported as `tsq::<module>`.

The classes you'll reach for most often are defined in those files — for example:

- `Call`, `CallArg`, `CallArgSpread` (`bridge/tsq_calls.qll`)
- `MethodCall` (`bridge/tsq_types.qll`)
- `Function`, `Parameter`, `ReturnStmt`, `FunctionContains` (`bridge/tsq_functions.qll`)
- `VarDecl`, `Assign` (`bridge/tsq_variables.qll`)
- `Symbol` (`bridge/tsq_symbols.qll`)
- `JsxElement`, `JsxAttribute` (`bridge/tsq_jsx.qll`)
- React-specific helpers including `useState` setters (`bridge/tsq_react.qll`)

Real, runnable examples live in `testdata/queries/v2/`. The smallest one is a one-liner that lists every method-call name in the database:

```ql
import tsq::types

from MethodCall mc
select mc.getMethodName() as "methodName"
```

Save it as `methods.ql` and run `tsq query --db counter.db methods.ql`.

A more substantial worked example is `testdata/queries/v2/find_setstate_updater_calls_fn.ql`, which finds React `setX(prev => helper(prev))` patterns by composing predicates from `tsq::react`, `tsq::calls`, `tsq::variables`, and `tsq::functions`:

```ql
/**
 * @name useState setter call with function-literal updater that calls a function
 * @kind problem
 * @id js/tsq/setstate-updater-calls-fn
 */

import tsq::react
import tsq::calls
import tsq::variables
import tsq::functions

from int c, int line
where setStateUpdaterCallsFn(c, line)
select c as "call", line as "line"
```

Use `tsq check path/to/query.ql` to validate a query (parse, resolve, desugar, plan, capability warnings) without running it.

## CodeQL compatibility

The compatibility layer is partial and explicitly marked as such. Roughly:

- **Works:** importing `javascript`, basic `DataFlow::PathGraph` and `TaintTracking` shapes, and a handful of `semmle.javascript.security.dataflow.*Query` modules (XSS, command injection, SQL injection, path traversal). These map onto tsq's native relations under the hood.
- **In progress / partial:** the wider `semmle.javascript.*` surface, dataflow configurations beyond the bundled query shapes, and anything that depends on CodeQL library predicates we haven't ported yet.
- **Out of scope (for now):** non-JS CodeQL languages, the CodeQL CLI, query packs, and the upstream test runner.

If you write a query against the compat layer and `tsq check` warns that an import uses an unavailable feature, that's the manifest telling you the relation isn't wired up yet. `CODEQL-COMPAT-PLAN.md` tracks what's landed and what's queued.

## Project layout

- `cmd/tsq/` — CLI entry point. Subcommands: `extract`, `query`, `check`, `version`.
- `extract/` — tree-sitter walker, fact schema, optional `tsgo` type enrichment, and the in-memory fact DB (`extract/db`).
- `ql/` — the QL pipeline: `parse`, `resolve`, `desugar`, `plan`, `eval`, plus the AST and datalog IR.
- `bridge/` — embedded `.qll` bridge libraries: `tsq_*.qll` (native v2) and `compat_*.qll` (CodeQL compat layer in progress).
- `output/` — result formatters (JSON Lines, SARIF, CSV).
- `testdata/` — fixture projects (`testdata/projects/*`) and example queries (`testdata/queries/v2/*`).

## Contributing

Run the test suite with:

```
go test ./...
```

(or `make test` for the race-enabled, no-cache variant the CI uses).

All changes go in via pull request — direct pushes to `main` are not permitted. Branch naming and worktree conventions live in `CONTRIBUTING.md`.

Every PR is subject to a read-only adversarial review pass before it can merge. The reviewer is a separate agent whose job is to look for, and call out, things like:

- tests that game the implementation (asserting on internal scaffolding rather than observable behaviour)
- min-maxing or overfitting to a benchmark or fixture rather than the real-world case the change claims to address
- vacuous assertions (`assert true`, `len(x) >= 0`, ignoring error returns)
- silent failures, swallowed errors, panics on hostile input
- thread-safety regressions and shared-state hazards

The implementing agent fixes anything real the reviewer surfaces, then the reviewer re-checks. CI must be green before merge.

Internal design notes and longer-form context live in the maintainer's wiki under `Wiki/Tech/`. If you're a maintainer and you learn something worth keeping, write it there.
