// Package main is the entry point for the tsq CLI.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Gjdoalfnrxu/tsq/bridge"
	"github.com/Gjdoalfnrxu/tsq/extract"
	"github.com/Gjdoalfnrxu/tsq/extract/db"
	"github.com/Gjdoalfnrxu/tsq/extract/typecheck"
	"github.com/Gjdoalfnrxu/tsq/output"
	"github.com/Gjdoalfnrxu/tsq/ql/ast"
	"github.com/Gjdoalfnrxu/tsq/ql/desugar"
	"github.com/Gjdoalfnrxu/tsq/ql/eval"
	"github.com/Gjdoalfnrxu/tsq/ql/parse"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
	"github.com/Gjdoalfnrxu/tsq/ql/resolve"
)

const version = "0.1.0"

// nonTaintablePrimitives is the set of TypeScript primitive type display names
// whose values cannot carry string-shaped taint. A value whose resolved type
// is one of these has typically been parsed or converted (e.g., parseInt),
// breaking the taint chain. See Phase 3d in CODEQL-COMPAT-PLAN.md.
var nonTaintablePrimitives = map[string]bool{
	"number":    true,
	"boolean":   true,
	"bigint":    true,
	"null":      true,
	"undefined": true,
	"void":      true,
	"never":     true,
}

// run executes the CLI with the given args, writing to stdout/stderr.
// Returns the exit code.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprintln(stderr, "usage: tsq <command> [flags]")
		fmt.Fprintln(stderr, "commands: extract, query, check, version")
		return 2
	}

	// Parse global flags that appear before the subcommand.
	var verbose, quiet bool
	var timeout time.Duration

	// Find the subcommand: skip global flags.
	subcmdIdx := -1
	for i, arg := range args {
		if arg == "--verbose" || arg == "-verbose" {
			verbose = true
			continue
		}
		if arg == "--quiet" || arg == "-quiet" {
			quiet = true
			continue
		}
		if strings.HasPrefix(arg, "--timeout=") || strings.HasPrefix(arg, "-timeout=") {
			parts := strings.SplitN(arg, "=", 2)
			d, err := time.ParseDuration(parts[1])
			if err != nil {
				fmt.Fprintf(stderr, "error: invalid --timeout value: %v\n", err)
				return 1
			}
			timeout = d
			continue
		}
		subcmdIdx = i
		break
	}

	if subcmdIdx < 0 {
		fmt.Fprintln(stderr, "usage: tsq <command> [flags]")
		fmt.Fprintln(stderr, "commands: extract, query, check, version")
		return 2
	}

	subcmd := args[subcmdIdx]
	subargs := args[subcmdIdx+1:]

	// Set up context with signal handling and timeout.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		select {
		case <-sigCh:
			cancel()
		case <-ctx.Done():
		}
	}()

	if timeout > 0 {
		var tcancel context.CancelFunc
		ctx, tcancel = context.WithTimeout(ctx, timeout)
		defer tcancel()
	}

	_ = verbose // available for future use
	_ = quiet

	switch subcmd {
	case "version":
		fmt.Fprintf(stdout, "tsq version %s\n", version)
		return 0
	case "extract":
		return cmdExtract(ctx, subargs, stdout, stderr)
	case "query":
		return cmdQuery(ctx, subargs, stdout, stderr)
	case "check":
		return cmdCheck(subargs, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "error: unknown command %q\n", subcmd)
		fmt.Fprintln(stderr, "commands: extract, query, check, version")
		return 2
	}
}

func cmdExtract(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("extract", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", ".", "project root directory")
	outputFile := fs.String("output", "tsq.db", "output fact database file")
	backendFlag := fs.String("backend", "treesitter", "extraction backend: treesitter or vendored")
	tsgoFlag := fs.String("tsgo", "", "tsgo binary path (empty=auto-detect, \"off\"=disabled)")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}

	fmt.Fprintf(stderr, "extracting from %s (requires CGO_ENABLED=1 for tree-sitter)...\n", *dir)

	database := db.NewDB()
	walker := extract.NewTypeAwareWalker(database)

	var backend extract.ExtractorBackend
	switch *backendFlag {
	case "treesitter":
		backend = &extract.TreeSitterBackend{}
	case "vendored":
		backend = &extract.VendoredBackend{}
	default:
		fmt.Fprintf(stderr, "error: unknown backend %q (must be treesitter or vendored)\n", *backendFlag)
		return 1
	}
	defer func() {
		if err := backend.Close(); err != nil {
			fmt.Fprintf(stderr, "warning: close backend: %v\n", err)
		}
	}()

	cfg := extract.ProjectConfig{RootDir: *dir}
	if err := walker.Run(ctx, backend, cfg); err != nil {
		fmt.Fprintf(stderr, "error: extraction failed: %v\n", err)
		return 1
	}

	// tsgo type enrichment phase
	tsgoPath := resolveTsgo(*tsgoFlag)
	if tsgoPath != "" {
		if err := enrichWithTsgo(ctx, database, tsgoPath, *dir, stderr); err != nil {
			fmt.Fprintf(stderr, "warning: tsgo enrichment failed: %v\n", err)
			// Continue without type info — graceful degradation
		}
	}

	// Write to a temp file first, rename on success to avoid partial output.
	outDir := filepath.Dir(*outputFile)
	tmpFile, err := os.CreateTemp(outDir, ".tsq-*.db.tmp")
	if err != nil {
		fmt.Fprintf(stderr, "error: create temp file: %v\n", err)
		return 1
	}
	tmpPath := tmpFile.Name()
	success := false
	defer func() {
		if !success {
			os.Remove(tmpPath)
		}
	}()

	if err := database.Encode(tmpFile); err != nil {
		tmpFile.Close()
		fmt.Fprintf(stderr, "error: write database: %v\n", err)
		return 1
	}
	if err := tmpFile.Close(); err != nil {
		fmt.Fprintf(stderr, "error: close temp file: %v\n", err)
		return 1
	}

	if err := os.Rename(tmpPath, *outputFile); err != nil {
		fmt.Fprintf(stderr, "error: rename output file: %v\n", err)
		return 1
	}
	success = true

	fmt.Fprintf(stderr, "wrote %s\n", *outputFile)
	return 0
}

// resolveTsgo determines the tsgo binary path from the flag value.
// Returns empty string if tsgo is disabled or not found.
func resolveTsgo(flag string) string {
	if flag == "off" {
		return ""
	}
	if flag != "" {
		return flag // explicit path
	}
	// Auto-detect
	return typecheck.DetectTsgo()
}

// enrichWithTsgo runs tsgo type enrichment over extracted files in the database.
// It queries tsgo for types at variable declaration and parameter positions,
// then populates ResolvedType, SymbolType, and ExprType relations.
func enrichWithTsgo(_ context.Context, database *db.DB, tsgoPath, rootDir string, stderr io.Writer) error {
	client, err := typecheck.NewClient(tsgoPath, rootDir)
	if err != nil {
		return fmt.Errorf("start tsgo: %w", err)
	}

	enricher, err := typecheck.NewEnricher(client, rootDir)
	if err != nil {
		client.Close()
		return fmt.Errorf("init enricher: %w", err)
	}
	defer enricher.Close()

	// Collect extracted file paths from the File relation
	fileRel := database.Relation("File")
	numFiles := fileRel.Tuples()
	for i := 0; i < numFiles; i++ {
		filePath, err := fileRel.GetString(database, i, 1) // col 1 = path
		if err != nil {
			continue
		}

		// Collect positions: variable declarations and parameters
		positions := collectEnrichmentPositions(database, filePath)
		if len(positions) == 0 {
			continue
		}

		facts, err := enricher.EnrichFile(filePath, positions)
		if err != nil {
			fmt.Fprintf(stderr, "warning: tsgo enrich %s: %v\n", filePath, err)
			continue
		}

		// Populate ResolvedType and SymbolType/ExprType relations
		for _, fact := range facts {
			typeID := extract.TypeEntityID(fact.TypeHandle)
			if err := database.Relation("ResolvedType").AddTuple(database, typeID, fact.TypeDisplay); err != nil {
				fmt.Fprintf(stderr, "warning: add ResolvedType: %v\n", err)
				continue
			}

			// Phase 3d: mark non-taintable primitive types for type-based sanitization.
			if nonTaintablePrimitives[fact.TypeDisplay] {
				if err := database.Relation("NonTaintableType").AddTuple(database, typeID); err != nil {
					fmt.Fprintf(stderr, "warning: add NonTaintableType: %v\n", err)
				}
			}

			// Map position back to a node ID for ExprType
			nodeID := extract.PositionNodeID(filePath, fact.Line, fact.Col)
			if err := database.Relation("ExprType").AddTuple(database, nodeID, typeID); err != nil {
				fmt.Fprintf(stderr, "warning: add ExprType: %v\n", err)
			}

			// If we can resolve a symbol at this position, populate SymbolType
			symID := extract.SymID(filePath, "", fact.Line, fact.Col)
			if err := database.Relation("SymbolType").AddTuple(database, symID, typeID); err != nil {
				fmt.Fprintf(stderr, "warning: add SymbolType: %v\n", err)
			}
		}
	}

	fmt.Fprintf(stderr, "tsgo type enrichment complete\n")
	return nil
}

// collectEnrichmentPositions collects positions of variable declarations and
// function parameters from the database for a given file.
func collectEnrichmentPositions(database *db.DB, filePath string) []typecheck.Position {
	fileID := extract.FileID(filePath)
	var positions []typecheck.Position

	// Collect variable declaration positions from VarDecl -> Symbol -> Node
	// We use the Node relation directly: find nodes in this file that are
	// VariableDeclarator or Parameter kinds.
	nodeRel := database.Relation("Node")
	numNodes := nodeRel.Tuples()
	for i := 0; i < numNodes; i++ {
		nodeFile, err := nodeRel.GetInt(i, 1) // col 1 = file
		if err != nil || uint32(nodeFile) != fileID {
			continue
		}
		kind, err := nodeRel.GetString(database, i, 2) // col 2 = kind
		if err != nil {
			continue
		}
		switch kind {
		case "VariableDeclarator", "RequiredParameter", "OptionalParameter":
			line, err := nodeRel.GetInt(i, 3) // col 3 = startLine
			if err != nil {
				continue
			}
			col, err := nodeRel.GetInt(i, 4) // col 4 = startCol
			if err != nil {
				continue
			}
			positions = append(positions, typecheck.Position{
				Line: int(line),
				Col:  int(col),
			})
		}
	}

	return positions
}

func cmdQuery(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("query", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dbFile := fs.String("db", "tsq.db", "fact database file")
	format := fs.String("format", "json", "output format: sarif, json, csv")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}

	if fs.NArg() < 1 {
		fmt.Fprintln(stderr, "error: query requires a QUERY_FILE argument")
		fmt.Fprintln(stderr, "usage: tsq query [--db FILE] [--format sarif|json|csv] QUERY_FILE")
		return 2
	}
	queryFile := fs.Arg(0)

	// Validate format.
	switch *format {
	case "json", "sarif", "csv":
	default:
		fmt.Fprintf(stderr, "error: unknown format %q (must be json, sarif, or csv)\n", *format)
		return 1
	}

	// Read and compile the query.
	rs, err := compileAndEval(ctx, queryFile, *dbFile)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	// Format output.
	switch *format {
	case "json":
		if err := output.WriteJSONLines(stdout, rs); err != nil {
			fmt.Fprintf(stderr, "error: write JSON output: %v\n", err)
			return 1
		}
	case "sarif":
		opts := output.SARIFOptions{
			QueryName:   strings.TrimSuffix(queryFile, ".ql"),
			ToolVersion: version,
		}
		if err := output.WriteSARIF(stdout, rs, opts); err != nil {
			fmt.Fprintf(stderr, "error: write SARIF output: %v\n", err)
			return 1
		}
	case "csv":
		if err := output.WriteCSV(stdout, rs); err != nil {
			fmt.Fprintf(stderr, "error: write CSV output: %v\n", err)
			return 1
		}
	}
	return 0
}

func cmdCheck(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(stderr)

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}

	if fs.NArg() < 1 {
		fmt.Fprintln(stderr, "error: check requires a QUERY_FILE argument")
		fmt.Fprintln(stderr, "usage: tsq check QUERY_FILE")
		return 2
	}
	queryFile := fs.Arg(0)

	src, err := os.ReadFile(queryFile)
	if err != nil {
		fmt.Fprintf(stderr, "error: read query file: %v\n", err)
		return 1
	}

	// Parse.
	p := parse.NewParser(string(src), queryFile)
	mod, err := p.Parse()
	if err != nil {
		fmt.Fprintf(stderr, "parse error: %v\n", err)
		return 1
	}

	// Resolve with bridge loader.
	bridgeFiles := bridge.LoadBridge()
	importLoader := makeBridgeImportLoader(bridgeFiles)
	resolved, err := resolve.Resolve(mod, importLoader)
	if err != nil {
		fmt.Fprintf(stderr, "resolve error: %v\n", err)
		return 1
	}

	hasErrors := false
	if len(resolved.Errors) > 0 {
		for _, e := range resolved.Errors {
			fmt.Fprintf(stderr, "  %s\n", e.Error())
		}
		hasErrors = true
	}

	// Desugar.
	prog, dsErrors := desugar.Desugar(resolved)
	if len(dsErrors) > 0 {
		for _, e := range dsErrors {
			fmt.Fprintf(stderr, "  desugar: %v\n", e)
		}
		hasErrors = true
	}

	// Plan.
	_, planErrors := plan.Plan(prog, nil)
	if len(planErrors) > 0 {
		for _, e := range planErrors {
			fmt.Fprintf(stderr, "  plan: %v\n", e)
		}
		hasErrors = true
	}

	// Capability warnings.
	manifest := bridge.V1Manifest()
	var imports []string
	for _, imp := range mod.Imports {
		imports = append(imports, imp.Path)
	}
	warnings := manifest.CheckQuery(imports)
	for _, w := range warnings {
		fmt.Fprintf(stdout, "warning: import %q uses unavailable feature (%s, expected %s)\n",
			w.Import, w.Reason, w.VersionTarget)
	}

	if hasErrors {
		fmt.Fprintln(stderr, "check: errors found")
		return 1
	}

	fmt.Fprintln(stdout, "check: ok")
	return 0
}

// compileAndEval reads a .ql file, compiles it, loads a fact DB, and evaluates.
func compileAndEval(ctx context.Context, queryFile, dbFile string) (*eval.ResultSet, error) {
	src, err := os.ReadFile(queryFile)
	if err != nil {
		return nil, fmt.Errorf("read query file: %w", err)
	}

	// Parse.
	p := parse.NewParser(string(src), queryFile)
	mod, err := p.Parse()
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}

	// Resolve.
	bridgeFiles := bridge.LoadBridge()
	importLoader := makeBridgeImportLoader(bridgeFiles)
	resolved, err := resolve.Resolve(mod, importLoader)
	if err != nil {
		return nil, fmt.Errorf("resolve: %w", err)
	}
	if len(resolved.Errors) > 0 {
		var msgs []string
		for _, e := range resolved.Errors {
			msgs = append(msgs, e.Error())
		}
		return nil, fmt.Errorf("resolve errors:\n  %s", strings.Join(msgs, "\n  "))
	}

	// Desugar.
	prog, dsErrors := desugar.Desugar(resolved)
	if len(dsErrors) > 0 {
		var msgs []string
		for _, e := range dsErrors {
			msgs = append(msgs, e.Error())
		}
		return nil, fmt.Errorf("desugar errors:\n  %s", strings.Join(msgs, "\n  "))
	}

	// Plan.
	execPlan, planErrors := plan.Plan(prog, nil)
	if len(planErrors) > 0 {
		var msgs []string
		for _, e := range planErrors {
			msgs = append(msgs, e.Error())
		}
		return nil, fmt.Errorf("plan errors:\n  %s", strings.Join(msgs, "\n  "))
	}

	// Load fact DB.
	f, err := os.Open(dbFile)
	if err != nil {
		return nil, fmt.Errorf("open fact DB: %w", err)
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat fact DB: %w", err)
	}

	factDB, err := db.ReadDB(f, fi.Size())
	if err != nil {
		return nil, fmt.Errorf("read fact DB: %w", err)
	}

	// Evaluate.
	evaluator := eval.NewEvaluator(execPlan, factDB)
	rs, err := evaluator.Evaluate(ctx)
	if err != nil {
		return nil, fmt.Errorf("evaluate: %w", err)
	}
	return rs, nil
}

// makeBridgeImportLoader creates an import loader that parses bridge .qll files.
func makeBridgeImportLoader(bridgeFiles map[string][]byte) func(path string) (*ast.Module, error) {
	pathToFile := map[string]string{
		"tsq::base":           "tsq_base.qll",
		"tsq::functions":      "tsq_functions.qll",
		"tsq::calls":          "tsq_calls.qll",
		"tsq::variables":      "tsq_variables.qll",
		"tsq::expressions":    "tsq_expressions.qll",
		"tsq::jsx":            "tsq_jsx.qll",
		"tsq::imports":        "tsq_imports.qll",
		"tsq::errors":         "tsq_errors.qll",
		"tsq::types":          "tsq_types.qll",
		"tsq::symbols":        "tsq_symbols.qll",
		"tsq::callgraph":      "tsq_callgraph.qll",
		"tsq::dataflow":       "tsq_dataflow.qll",
		"tsq::summaries":      "tsq_summaries.qll",
		"tsq::composition":    "tsq_composition.qll",
		"tsq::taint":          "tsq_taint.qll",
		"tsq::express":        "tsq_express.qll",
		"tsq::react":          "tsq_react.qll",
		"tsq::node":           "tsq_node.qll",
		"javascript":          "compat_javascript.qll",
		"DataFlow::PathGraph": "compat_dataflow.qll",
		"TaintTracking":       "compat_tainttracking.qll",
		"semmle.javascript.security.dataflow.XssQuery":              "compat_security_xss.qll",
		"semmle.javascript.security.dataflow.CommandInjectionQuery": "compat_security_cmdi.qll",
		"semmle.javascript.security.dataflow.SqlInjectionQuery":     "compat_security_sqli.qll",
		"semmle.javascript.security.dataflow.PathTraversalQuery":    "compat_security_pathtraversal.qll",
	}
	return func(path string) (*ast.Module, error) {
		filename, ok := pathToFile[path]
		if !ok {
			return nil, fmt.Errorf("unknown import: %s", path)
		}
		data, ok := bridgeFiles[filename]
		if !ok {
			return nil, fmt.Errorf("missing bridge file: %s", filename)
		}
		p := parse.NewParser(string(data), filename)
		return p.Parse()
	}
}

func main() {
	code := run(os.Args[1:], os.Stdout, os.Stderr)
	os.Exit(code)
}
