# tsq — Project Instructions

## Development Workflow

All changes via PR. Never push directly to main.

### Agent Workflow

1. **Implementation agent** works on a feature branch (`feat/phase-N-*`), writes tests first (TDD), commits, pushes, opens PR.
2. **Adversarial review agent** reviews the PR for: panics, silent failures, logic bugs, injection risks, thread safety, spec compliance.
3. **Fix agent** patches any blocking findings and **adds regression tests for every bug found** — not just code fixes.
4. Merge only after CI passes and review is clean.

### Testing Rules

- Run `go test ./... -count=1` before committing. All tests must pass.
- Every adversarial review finding that gets fixed MUST have an accompanying regression test that would have caught the original bug.
- Property-based tests use `pgregory.net/rapid`.
- CGO_ENABLED=1 required for tree-sitter tests.

### Pre-commit Hook

Active via `make setup` (sets `core.hooksPath` to `.githooks/`). Requires Go + golangci-lint installed.

### Go Toolchain

```
export PATH=$PATH:/usr/local/go/bin:$HOME/go/bin
```

## Architecture

TypeScript code analysis tool — lightweight open-source CodeQL alternative.

**Pipeline:** Extract (tree-sitter AST → fact DB) → Query (QL parse → resolve → desugar → plan → evaluate → format output)

**Key packages:**
- `extract/` — tree-sitter backend, AST walker, fact emission
- `extract/schema/` — fact relation definitions (source of truth for relation names/arities)
- `extract/db/` — binary columnar fact DB reader/writer
- `ql/parse/` — QL lexer + recursive descent parser
- `ql/resolve/` — name resolution
- `ql/desugar/` — QL → Datalog IR lowering
- `ql/plan/` — stratification + join ordering
- `ql/eval/` — semi-naive bottom-up Datalog evaluator
- `bridge/` — .qll files mapping fact schema to QL classes
- `output/` — SARIF/JSON/CSV formatters
- `cmd/tsq/` — CLI entry point

## Known Conventions

- Schema relation names are PascalCase (e.g., `CallArg`, `JsxElement`) — .qll bridge files must match exactly.
- Zero external CLI dependencies — raw `flag` package, not cobra.
- Minimal runtime deps — only tree-sitter cgo bindings.
