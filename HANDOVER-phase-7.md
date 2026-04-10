# Phase 7: CLI and Output Formatters — Handover

## What was built

### CLI (`cmd/tsq/main.go`)

Subcommand-based CLI using raw `flag` (zero external deps):

| Command | Purpose |
|---------|---------|
| `tsq extract [--dir DIR] [--output FILE]` | Run extraction, write fact DB |
| `tsq query [--db FILE] [--format sarif\|json\|csv] QUERY_FILE` | Evaluate a QL query against a fact DB |
| `tsq check QUERY_FILE` | Parse, resolve, and plan a QL file; report errors and capability warnings |
| `tsq version` | Print version info |

**Global flags:** `--verbose`, `--quiet`, `--timeout DURATION` (parsed before the subcommand).

**Signal handling:** SIGINT/SIGTERM trigger context cancellation for graceful shutdown.

**Error handling:** User-friendly error messages on stderr, non-zero exit codes on failure.

**Exit codes:** 0 = success, 1 = error.

### Output formatters (`output/` package)

| File | Format | Description |
|------|--------|-------------|
| `output/sarif.go` | SARIF 2.1.0 | Full SARIF JSON with location heuristics (columns named `file`/`path` → URI, `line`/`startLine` → line, `col`/`column` → column) |
| `output/json.go` | JSON Lines | One JSON object per result row; IntVal → number, StrVal → string |
| `output/csv.go` | CSV (RFC 4180) | Header row + data rows; handles commas, quotes, and newlines in values |

### Test files

| File | Tests |
|------|-------|
| `output/sarif_test.go` | Empty result set, single result with location, multiple results, SARIF structure validity |
| `output/json_test.go` | Empty, single row, special characters, integer values, multiple rows |
| `output/csv_test.go` | Empty, single row, commas in values, newlines in values, quotes in values |
| `cmd/tsq/main_test.go` | Version prints, unknown subcommand errors, missing args, bad format, nonexistent file |

## Pipeline wiring

The full pipeline from CLI perspective:

```
[.ql source] → parse.NewParser().Parse()
             → resolve.Resolve(mod, bridgeImportLoader)
             → desugar.Desugar(resolved)
             → plan.Plan(prog, sizeHints)
             → eval.NewEvaluator(execPlan, factDB)
             → evaluator.Evaluate(ctx)
             → ResultSet

[fact DB file] → os.Open → db.ReadDB(f, size) → *db.DB (passed to NewEvaluator)
```

The bridge import loader (`makeBridgeImportLoader`) maps `tsq::*` import paths to embedded `.qll` files, parses them with `parse.NewParser`, and returns `*ast.Module` for the resolver.

## Design decisions

1. **`run()` function for testability** — `main()` delegates to `run(args, stdout, stderr) int`, making the CLI fully testable without process execution.

2. **Global flags before subcommand** — parsed manually before dispatching to subcommand-specific `flag.FlagSet`. This avoids cobra while keeping clean flag handling.

3. **SARIF location heuristics** — columns are matched by well-known names (`file`, `path`, `line`, `col`, etc.) rather than requiring explicit location metadata in queries. This works naturally with typical tsq query patterns.

4. **JSON Lines not JSON array** — one object per line is streamable and works with `jq`, `grep`, etc. No array wrapping.

5. **Extract requires CGO_ENABLED=1** — tree-sitter binding is CGO. Documented in extract help text. Query/check commands work without CGO.

## CGO note

The `extract` command imports `extract.TreeSitterBackend` which requires CGO for tree-sitter. The `query` and `check` commands do not use tree-sitter at runtime, but because they share the binary, CGO_ENABLED=1 is required at build time.

## What's NOT in this phase

- Interactive REPL
- Watch mode / incremental rebuilding
- Configuration file
- Plugin system
- Size hints from DB for planner optimization (could be added by iterating schema.Registry and calling `factDB.Relation(name).Tuples()`)
