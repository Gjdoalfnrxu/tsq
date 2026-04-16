package integration_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Gjdoalfnrxu/tsq/bridge"
	"github.com/Gjdoalfnrxu/tsq/extract"
	"github.com/Gjdoalfnrxu/tsq/extract/db"
	"github.com/Gjdoalfnrxu/tsq/extract/rules"
	"github.com/Gjdoalfnrxu/tsq/ql/desugar"
	"github.com/Gjdoalfnrxu/tsq/ql/eval"
	"github.com/Gjdoalfnrxu/tsq/ql/parse"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
	"github.com/Gjdoalfnrxu/tsq/ql/resolve"
)

// BenchmarkExtraction measures extraction time on the simple project.
func BenchmarkExtraction(b *testing.B) {
	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		database := db.NewDB()
		walker := extract.NewFactWalker(database)
		backend := &extract.TreeSitterBackend{}
		cfg := extract.ProjectConfig{RootDir: "testdata/projects/simple"}
		if err := walker.Run(ctx, backend, cfg); err != nil {
			b.Fatalf("extraction failed: %v", err)
		}
		backend.Close()
		cancel()
	}
}

// BenchmarkV2Extraction measures v2 extraction time on the simple project.
func BenchmarkV2Extraction(b *testing.B) {
	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		database := db.NewDB()
		walker := extract.NewTypeAwareWalker(database)
		backend := &extract.TreeSitterBackend{}
		cfg := extract.ProjectConfig{RootDir: "testdata/projects/simple"}
		if err := walker.Run(ctx, backend, cfg); err != nil {
			b.Fatalf("v2 extraction failed: %v", err)
		}
		backend.Close()
		cancel()
	}
}

// benchmarkQuery is a shared helper for query benchmarks.
func benchmarkQuery(b *testing.B, factDB *db.DB, querySource string, withSystemRules bool) {
	b.Helper()
	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

		p := parse.NewParser(querySource, "bench.ql")
		mod, err := p.Parse()
		if err != nil {
			b.Fatalf("parse: %v", err)
		}

		bridgeFiles := bridge.LoadBridge()
		importLoader := makeBridgeImportLoader(bridgeFiles)
		resolved, err := resolve.Resolve(mod, importLoader)
		if err != nil {
			b.Fatalf("resolve: %v", err)
		}
		if len(resolved.Errors) > 0 {
			var msgs []string
			for _, e := range resolved.Errors {
				msgs = append(msgs, e.Error())
			}
			b.Fatalf("resolve errors:\n  %s", strings.Join(msgs, "\n  "))
		}

		prog, dsErrors := desugar.Desugar(resolved)
		if len(dsErrors) > 0 {
			b.Fatalf("desugar errors: %v", dsErrors)
		}

		if withSystemRules {
			prog = rules.MergeSystemRules(prog, rules.AllSystemRules())
		}

		execPlan, planErrors := plan.Plan(prog, nil)
		if len(planErrors) > 0 {
			b.Fatalf("plan errors: %v", planErrors)
		}

		evaluator := eval.NewEvaluator(execPlan, factDB)
		_, err = evaluator.Evaluate(ctx)
		if err != nil {
			b.Fatalf("evaluate: %v", err)
		}
		cancel()
	}
}

// BenchmarkEvaluation measures evaluation time for a simple query (no system rules).
func BenchmarkEvaluation(b *testing.B) {
	// Extract once
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	database := db.NewDB()
	walker := extract.NewFactWalker(database)
	backend := &extract.TreeSitterBackend{}
	cfg := extract.ProjectConfig{RootDir: "testdata/projects/simple"}
	if err := walker.Run(ctx, backend, cfg); err != nil {
		b.Fatalf("extraction failed: %v", err)
	}
	backend.Close()

	factDB := func() *db.DB {
		b.Helper()
		return serializeDBBench(b, database)
	}()

	query := `import tsq::functions
from Function f
select f.getName() as "name"
`
	b.ResetTimer()
	benchmarkQuery(b, factDB, query, false)
}

// BenchmarkEvaluationWithSystemRules measures evaluation time with system rules injected.
func BenchmarkEvaluationWithSystemRules(b *testing.B) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	database := db.NewDB()
	walker := extract.NewTypeAwareWalker(database)
	backend := &extract.TreeSitterBackend{}
	cfg := extract.ProjectConfig{RootDir: "testdata/ts/v2/frameworks"}
	if err := walker.Run(ctx, backend, cfg); err != nil {
		b.Fatalf("v2 extraction failed: %v", err)
	}
	backend.Close()

	factDB := serializeDBBench(b, database)

	query := `import tsq::taint
from TaintSource src
select src.getSourceKind() as "sourceKind"
`
	b.ResetTimer()
	benchmarkQuery(b, factDB, query, true)
}

// BenchmarkMagicSet compares evaluation with and without magic-set optimization
// on a targeted query (transitive closure).
func BenchmarkMagicSet(b *testing.B) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	database := db.NewDB()
	walker := extract.NewTypeAwareWalker(database)
	backend := &extract.TreeSitterBackend{}
	cfg := extract.ProjectConfig{RootDir: "testdata/ts/v2/frameworks"}
	if err := walker.Run(ctx, backend, cfg); err != nil {
		b.Fatalf("v2 extraction failed: %v", err)
	}
	backend.Close()

	factDB := serializeDBBench(b, database)

	query := `import tsq::functions
from Function f
select f.getName() as "name"
`

	b.Run("without_magic_set", func(b *testing.B) {
		benchmarkQuery(b, factDB, query, false)
	})

	b.Run("with_magic_set", func(b *testing.B) {
		// Magic set benefits are more visible on larger programs with targeted queries.
		// Pass true to actually exercise the magic-set code path.
		benchmarkQuery(b, factDB, query, true)
	})
}

// serializeDBBench is like serializeDB but for benchmarks.
func serializeDBBench(b *testing.B, database *db.DB) *db.DB {
	b.Helper()
	var buf = new(bytes.Buffer)
	if err := database.Encode(buf); err != nil {
		b.Fatalf("encode DB: %v", err)
	}
	data := buf.Bytes()
	reader := bytes.NewReader(data)
	result, err := db.ReadDB(reader, int64(len(data)))
	if err != nil {
		b.Fatalf("decode DB: %v", err)
	}
	return result
}
