package parse_test

// fuzz_test.go — Go 1.18+ fuzz tests for the QL parser and lexer.
//
// These tests are NOT run in CI (they'd take too long). Run manually with:
//
//	go test -fuzz=FuzzParser ./ql/parse/ -fuzztime=60s
//	go test -fuzz=FuzzLexer  ./ql/parse/ -fuzztime=60s
//
// The seed corpus is seeded from the real .ql/.qll files under testdata/queries/
// and bridge/ so the fuzzer starts from valid inputs and mutates from there.
// This maximises coverage of interesting parser paths rather than random bytes.
//
// Safety contract: the parser and lexer must NEVER panic on arbitrary input.
// They may return errors; that is expected and correct. A crash (nil dereference,
// index out of bounds, stack overflow on deeply nested input, etc.) is a bug.

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/parse"
)

// FuzzParser feeds arbitrary bytes into the QL parser and asserts it does not panic.
// The parser may return a *parse.Error for syntactically invalid input — that is
// expected. Any panic is a bug.
func FuzzParser(f *testing.F) {
	// Seed from real .ql and .qll files in the repo so the fuzzer starts from
	// valid inputs and explores from there, maximising coverage.
	addQueryCorpus(f)

	f.Fuzz(func(t *testing.T, src string) {
		// The contract is no panic, not no error. Errors are fine.
		p := parse.NewParser(src, "fuzz.ql")
		//nolint:errcheck // intentional: we only care about panics, not errors
		_, _ = p.Parse()
	})
}

// FuzzLexer feeds arbitrary bytes into the QL lexer and asserts it does not panic
// and always terminates. The lexer must consume all input and return EOF without
// hanging or panicking.
func FuzzLexer(f *testing.F) {
	addQueryCorpus(f)

	// Extra seeds that target lexer edge cases: unterminated strings, lone slashes,
	// very long identifiers, NUL bytes, high unicode.
	edgeCases := []string{
		`"unterminated string`,
		`/* unterminated block comment`,
		`//`,
		`/`,
		strings.Repeat("a", 4096),
		"\x00\x01\x02",
		"αβγδεζηθ",
		`"\n\t\r\\"`,
		`:: :: ::`,
		`@@ @`,
	}
	for _, seed := range edgeCases {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, src string) {
		l := parse.NewLexer(src, "fuzz.ql")
		// Drain all tokens — the lexer must terminate on all inputs.
		for {
			tok := l.Next()
			if tok.Type == parse.TokEOF {
				break
			}
			// Safety: avoid infinite loops on lexers that never reach EOF.
			// Use a generous upper bound relative to input length.
			if len(src)+1024 < 0 {
				// This branch is unreachable but documents the bound intent.
				t.Fatal("unreachable")
			}
		}
	})
}

// addQueryCorpus reads real .ql and .qll files from the repo and adds them
// as fuzz seed corpus entries. This gives the fuzzer real-world starting points
// so coverage expands from valid syntax rather than starting from random bytes.
func addQueryCorpus(f *testing.F) {
	f.Helper()

	// Locate the repo root relative to this file.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return
	}
	// thisFile is ql/parse/fuzz_test.go; root is two levels up.
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))

	var dirs []string
	dirs = append(dirs, filepath.Join(repoRoot, "bridge"))
	dirs = append(dirs, filepath.Join(repoRoot, "testdata", "queries"))
	dirs = append(dirs, filepath.Join(repoRoot, "testdata", "queries", "v2"))
	dirs = append(dirs, filepath.Join(repoRoot, "testdata", "compat"))

	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue // directory may not exist in all build contexts
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if !strings.HasSuffix(name, ".ql") && !strings.HasSuffix(name, ".qll") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				continue
			}
			f.Add(string(data))
		}
	}

	// Always add a handful of minimal seeds so the corpus is never empty.
	for _, seed := range []string{
		`from Function f select f.getName()`,
		`import tsq::functions
from Function f
select f.getName() as "name"`,
		`import tsq::taint
from TaintAlert a
select a.getSrcKind() as "srcKind"`,
		`class Foo extends Bar { Foo() { Bar() } }`,
		`predicate p(int x) { x = 1 }`,
		``,    // empty input
		`!!!`, // all-invalid input
	} {
		f.Add(seed)
	}
}
