# Handover: Phase 0 — Repository Skeleton

## What Was Implemented
- GitHub repo created at github.com/Gjdoalfnrxu/tsq
- Go module: github.com/Gjdoalfnrxu/tsq
- All package stubs created with doc comments
- CI: GitHub Actions with Go 1.22.x and 1.23.x matrix, golangci-lint
- Makefile: build, test, lint, extract, query targets
- internal/testutil: minimal test helpers (Equal, ErrorIs, NilError, Contains, Failf)
- CONTRIBUTING.md: documents worktree/PR/adversarial-review workflow
- .golangci.yml: linter configuration

## What Was Intentionally Left Out
- All implementation logic (Phase 1a, 1b onwards)
- External dependencies (none added in this phase)
- tree-sitter bindings (Phase 2a)
- Any type definitions beyond package declarations

## Key Decisions Made
- No testify: zero test dependencies, idiomatic Go, internal/testutil sufficient
- Standard library only: no cobra, no other CLI framework — cmd/tsq/main.go stays minimal
- golangci-lint config: exhaustruct disabled (too noisy during incremental development)

## Test Coverage Summary
- Total tests: 5 (version check + testutil helpers)
- Key test cases: version non-empty, Equal, ErrorIs, NilError, Contains

## Files Changed
- go.mod
- cmd/tsq/main.go
- cmd/tsq/main_test.go
- internal/testutil/testutil.go
- internal/testutil/testutil_test.go
- extract/backend.go
- extract/walker.go
- extract/schema/registry.go
- extract/schema/relations.go
- extract/db/writer.go
- extract/db/reader.go
- ql/ast/ast.go
- ql/parse/lexer.go
- ql/parse/parser.go
- ql/parse/parse_test.go
- ql/resolve/resolve.go
- ql/resolve/resolve_test.go
- ql/desugar/desugar.go
- ql/desugar/desugar_test.go
- ql/datalog/ir.go
- ql/plan/plan.go
- ql/plan/plan_test.go
- ql/eval/eval.go
- ql/eval/eval_test.go
- bridge/manifest.go
- output/sarif.go
- output/json.go
- output/csv.go
- testdata/.gitkeep
- .github/workflows/ci.yml
- .golangci.yml
- Makefile
- CONTRIBUTING.md
- .gitignore (extended)

## Dependencies: What the Next Phase Needs From This One
- Compilable repo at github.com/Gjdoalfnrxu/tsq
- Go module at github.com/Gjdoalfnrxu/tsq
- internal/testutil package for test helpers
- All package stubs exist (phases can add to them without creating directories)

## Known Issues / Technical Debt
- None

## Next Steps
- Phase 1a: implement extract/schema/registry.go and extract/db/ (binary format)
- Phase 1b: implement ql/parse/ (lexer + parser) — can start in parallel with 1a
- Both can branch from main immediately after this PR merges
