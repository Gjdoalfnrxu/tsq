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
	"github.com/Gjdoalfnrxu/tsq/extract/rules"
	"github.com/Gjdoalfnrxu/tsq/extract/schema"
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
	// verbose and quiet are accepted for forward compatibility but not yet wired up.
	var timeout time.Duration

	// Find the subcommand: skip global flags.
	subcmdIdx := -1
	for i, arg := range args {
		if arg == "--verbose" || arg == "-verbose" || arg == "--quiet" || arg == "-quiet" {
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
	tsconfigFlag := fs.String("tsconfig", "", "path to tsconfig.json for tsgo project context (empty=auto-discover by walking up from --dir)")

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
		tsconfigPath := resolveTSConfig(*tsconfigFlag, *dir, stderr)
		if err := enrichWithTsgo(ctx, database, tsgoPath, *dir, tsconfigPath, stderr); err != nil {
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

// resolveTSConfig determines the tsconfig.json path to hand to tsgo.
// Precedence:
//  1. Explicit --tsconfig path (validated to exist; absolutised).
//  2. Auto-discovery by walking up from the extraction --dir.
//  3. Empty string (caller proceeds without an explicit project; tsgo will
//     fall back to getDefaultProjectForFile and enrichment will likely
//     produce no facts — that's the legacy bug being papered over here).
func resolveTSConfig(flagVal, dir string, stderr io.Writer) string {
	if flagVal != "" {
		abs, err := filepath.Abs(flagVal)
		if err != nil {
			fmt.Fprintf(stderr, "warning: --tsconfig %q: %v\n", flagVal, err)
			return ""
		}
		if info, err := os.Stat(abs); err != nil || info.IsDir() {
			fmt.Fprintf(stderr, "warning: --tsconfig %q not found or not a file\n", abs)
			return ""
		}
		return abs
	}
	return typecheck.FindTSConfig(dir)
}

// enrichWithTsgo runs tsgo type enrichment over extracted files in the database.
// It queries tsgo for types at variable declaration and parameter positions,
// then populates ResolvedType, SymbolType, and ExprType relations.
func enrichWithTsgo(_ context.Context, database *db.DB, tsgoPath, rootDir, tsconfigPath string, stderr io.Writer) error {
	client, err := typecheck.NewClient(tsgoPath, rootDir)
	if err != nil {
		return fmt.Errorf("start tsgo: %w", err)
	}

	if tsconfigPath != "" {
		fmt.Fprintf(stderr, "tsgo: using project %s\n", tsconfigPath)
	} else {
		fmt.Fprintf(stderr, "warning: no tsconfig.json found under %s; tsgo enrichment will likely produce no type facts\n", rootDir)
	}

	enricher, err := typecheck.NewEnricherWithConfig(client, rootDir, tsconfigPath)
	if err != nil {
		client.Close()
		return fmt.Errorf("init enricher: %w", err)
	}
	defer enricher.Close()

	// Collect extracted file paths from the File relation. Register them
	// all up-front so the snapshot is opened with FileChanges.Created
	// covering every file we plan to query — without this, the live tsgo
	// binary returns "source file not found" for files reachable only via
	// the tsconfig include glob.
	fileRel := database.Relation("File")
	numFiles := fileRel.Tuples()
	allPaths := make([]string, 0, numFiles)
	for i := 0; i < numFiles; i++ {
		fp, err := fileRel.GetString(database, i, 1)
		if err != nil {
			continue
		}
		allPaths = append(allPaths, fp)
	}
	enricher.RegisterFiles(allPaths)

	var aggSymQ, aggSymErr, aggTypQ, aggTypErr, aggFacts int
	// Dedup ResolvedType emissions across all files: identical TypeHandles must
	// produce a single ResolvedType row. Without this guard the same primitive
	// (e.g. "string") is emitted once per occurrence, multiplying row counts
	// and breaking the downstream uniqueness expectation. Mirrors the seenTypes
	// map in extract/typecheck/enricher.go:WriteTypeFacts.
	seenTypes := make(map[string]bool)
	for _, filePath := range allPaths {
		// Collect positions: variable declarations and parameters
		positions := collectEnrichmentPositions(database, filePath)
		if len(positions) == 0 {
			continue
		}

		facts, stats, err := enricher.EnrichFile(filePath, positions)
		if err != nil {
			fmt.Fprintf(stderr, "warning: tsgo enrich %s: %v\n", filePath, err)
			continue
		}
		aggSymQ += stats.SymbolQueries
		aggSymErr += stats.SymbolErrors
		aggTypQ += stats.TypeQueries
		aggTypErr += stats.TypeErrors
		aggFacts += stats.FactsEmitted

		// Populate ResolvedType and SymbolType/ExprType relations
		for _, fact := range facts {
			if fact.TypeHandle == "" {
				continue
			}
			typeID := extract.TypeEntityID(fact.TypeHandle)
			if !seenTypes[fact.TypeHandle] {
				seenTypes[fact.TypeHandle] = true
				if err := database.Relation("ResolvedType").AddTuple(database, typeID, fact.TypeDisplay); err != nil {
					fmt.Fprintf(stderr, "warning: add ResolvedType: %v\n", err)
					continue
				}
				// Phase 3d: mark non-taintable primitive types for type-based sanitization.
				// Emitted once per type alongside the ResolvedType row.
				if nonTaintablePrimitives[fact.TypeDisplay] {
					if err := database.Relation("NonTaintableType").AddTuple(database, typeID); err != nil {
						fmt.Fprintf(stderr, "warning: add NonTaintableType: %v\n", err)
					}
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

	fmt.Fprintf(stderr,
		"tsgo type enrichment complete: facts=%d symbolQueries=%d (errors=%d) typeQueries=%d (errors=%d)\n",
		aggFacts, aggSymQ, aggSymErr, aggTypQ, aggTypErr,
	)
	if aggSymQ > 0 && aggFacts == 0 {
		fmt.Fprintf(stderr,
			"warning: tsgo answered %d symbol queries but produced zero type facts; downstream enrichment is not working — check tsgo binary and tsconfig\n",
			aggSymQ,
		)
	}
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

	bridgeFiles := bridge.LoadBridge()
	importLoader := makeBridgeImportLoader(bridgeFiles)

	// Run the same compilation pipeline used by `query` so that issues which
	// would surface during evaluation (e.g. system-rule planning failures) are
	// caught here. The fact DB is not loaded, so size hints are nil — the
	// planner uses its default heuristics.
	_, mod, resolveWarnings, buildErrs := buildProgram(string(src), queryFile, importLoader, nil)

	hasErrors := false
	if len(buildErrs) > 0 {
		for _, e := range buildErrs {
			fmt.Fprintf(stderr, "  %s\n", e.Error())
		}
		hasErrors = true
	}

	// Surface resolve-phase deprecation warnings (non-fatal). These were
	// previously emitted by cmdCheck directly; preserve that behaviour now
	// that resolution happens inside buildProgram.
	for _, w := range resolveWarnings {
		fmt.Fprintf(stderr, "  %s\n", w.String())
	}

	// Surface bridge capability warnings. buildProgram returns the parsed
	// module even on later-stage errors so we can still report these.
	if mod != nil {
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
	}

	if hasErrors {
		fmt.Fprintln(stderr, "check: errors found")
		return 1
	}

	fmt.Fprintln(stdout, "check: ok")
	return 0
}

// buildProgram runs the shared QL compilation pipeline used by both `check`
// and `query`: parse → resolve → desugar → MergeSystemRules → plan. Both
// callers must use this helper so that a query which passes `check` cannot
// later hang or OOM in `query` due to a divergent rule graph (issue #82).
//
// sizeHints may be nil; the planner will use its default heuristics in that
// case. The parsed *ast.Module is returned even when later phases produce
// errors, so callers can still surface things like capability warnings.
// Resolve-phase warnings (e.g. deprecated imports) are returned alongside
// errors so callers can surface them regardless of whether later phases ran.
func buildProgram(src, file string, importLoader func(string) (*ast.Module, error), sizeHints map[string]int) (*plan.ExecutionPlan, *ast.Module, []resolve.Warning, []error) {
	// Parse.
	p := parse.NewParser(src, file)
	mod, err := p.Parse()
	if err != nil {
		return nil, nil, nil, []error{fmt.Errorf("parse: %w", err)}
	}

	// Resolve.
	resolved, err := resolve.Resolve(mod, importLoader)
	if err != nil {
		return nil, mod, nil, []error{fmt.Errorf("resolve: %w", err)}
	}
	warnings := resolved.Warnings
	if len(resolved.Errors) > 0 {
		errs := make([]error, 0, len(resolved.Errors))
		for _, e := range resolved.Errors {
			errs = append(errs, fmt.Errorf("resolve: %w", e))
		}
		return nil, mod, warnings, errs
	}

	// Desugar.
	prog, dsErrors := desugar.Desugar(resolved)
	if len(dsErrors) > 0 {
		errs := make([]error, 0, len(dsErrors))
		for _, e := range dsErrors {
			errs = append(errs, fmt.Errorf("desugar: %w", e))
		}
		return nil, mod, warnings, errs
	}

	// Inject system rules so derived relations (CallTarget, LocalFlow,
	// TaintAlert, etc.) are present in the planned graph. This used to live
	// only in `query`, which meant `check` could green-light a program whose
	// system-rule-augmented form failed to plan or hung at eval time.
	prog = rules.MergeSystemRules(prog, rules.AllSystemRules())

	// Plan.
	execPlan, planErrors := plan.Plan(prog, sizeHints)
	if len(planErrors) > 0 {
		errs := make([]error, 0, len(planErrors))
		for _, e := range planErrors {
			errs = append(errs, fmt.Errorf("plan: %w", e))
		}
		return nil, mod, warnings, errs
	}

	return execPlan, mod, warnings, nil
}

// compileAndEval reads a .ql file, compiles it, loads a fact DB, and evaluates.
func compileAndEval(ctx context.Context, queryFile, dbFile string) (*eval.ResultSet, error) {
	src, err := os.ReadFile(queryFile)
	if err != nil {
		return nil, fmt.Errorf("read query file: %w", err)
	}

	// Load fact DB before planning so we can pass actual tuple counts as size hints.
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

	// Build size hints from actual tuple counts in the DB so the planner can
	// order joins by true relation size rather than a uniform default of 1000.
	sizeHints := buildSizeHints(factDB)

	// Compile via the shared pipeline so that `check` and `query` agree on the
	// rule graph (parse → resolve → desugar → MergeSystemRules → plan).
	bridgeFiles := bridge.LoadBridge()
	importLoader := makeBridgeImportLoader(bridgeFiles)
	execPlan, _, _, buildErrs := buildProgram(string(src), queryFile, importLoader, sizeHints)
	if len(buildErrs) > 0 {
		// Reproduce the prior multi-error formatting of compileAndEval: group
		// by phase and join with newline-indented messages so callers see one
		// error per phase boundary rather than a flat error.Join blob.
		return nil, joinPhaseErrors(buildErrs)
	}

	// Evaluate.
	evaluator := eval.NewEvaluator(execPlan, factDB)
	rs, err := evaluator.Evaluate(ctx)
	if err != nil {
		return nil, fmt.Errorf("evaluate: %w", err)
	}
	return rs, nil
}

// joinPhaseErrors reformats a slice of phase-prefixed errors back into the
// "<phase> errors:\n  <msg1>\n  <msg2>" shape that compileAndEval used to
// produce. Callers (cmdQuery via stderr) parse this for human display only.
func joinPhaseErrors(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	if len(errs) == 1 {
		return errs[0]
	}
	// Group consecutive errors that share the same prefix (e.g. "desugar:").
	// In practice buildProgram only returns errors from a single phase per
	// call, so this is just a faithful flatten with the phase header.
	prefix := ""
	if i := strings.Index(errs[0].Error(), ":"); i > 0 {
		prefix = errs[0].Error()[:i]
	}
	var msgs []string
	for _, e := range errs {
		s := e.Error()
		// Trim the "<phase>: " prefix added by buildProgram so it's not
		// repeated on every line of the joined output.
		if prefix != "" && strings.HasPrefix(s, prefix+": ") {
			s = s[len(prefix)+2:]
		}
		msgs = append(msgs, s)
	}
	if prefix == "" {
		return fmt.Errorf("errors:\n  %s", strings.Join(msgs, "\n  "))
	}
	return fmt.Errorf("%s errors:\n  %s", prefix, strings.Join(msgs, "\n  "))
}

// makeBridgeImportLoader creates an import loader that parses bridge .qll files.
// It uses bridge.ImportPathToFile as the single source of truth for the path→filename map.
func makeBridgeImportLoader(bridgeFiles map[string][]byte) func(path string) (*ast.Module, error) {
	return func(path string) (*ast.Module, error) {
		filename, ok := bridge.ImportPathToFile[path]
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

// buildSizeHints constructs a relation-name→tuple-count map from the loaded factDB.
// This gives the planner real cardinality data for join ordering instead of the
// uniform default of 1000.
func buildSizeHints(factDB *db.DB) map[string]int {
	hints := make(map[string]int, len(schema.Registry))
	for _, def := range schema.Registry {
		hints[def.Name] = factDB.Relation(def.Name).Tuples()
	}
	return hints
}

func main() {
	code := run(os.Args[1:], os.Stdout, os.Stderr)
	os.Exit(code)
}
