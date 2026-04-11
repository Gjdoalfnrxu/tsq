package parse_test

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/ast"
	"github.com/Gjdoalfnrxu/tsq/ql/parse"
	"pgregory.net/rapid"
)

// TestPropertyParseWhitespaceInvariance is a property test over the real QL
// corpus in bridge/*.qll: for any such source, injecting extra whitespace at
// lexer-neutral positions (between existing tokens) must not change the parsed
// AST once source-location Span fields are ignored.
//
// Why this is a real property (not a tautology): the oracle is the AST from
// the original, unmodified source. The mutated source goes through the
// lexer and parser independently and must produce an AST that is
// structurally identical. If the lexer accidentally captured leading
// whitespace as part of a token, or the parser's whitespace handling leaked
// between tokens, the two ASTs would diverge.
//
// Real bug class caught: lexer regressions that mishandle whitespace or
// comments (e.g., treating a comment as a token, gluing identifiers across
// whitespace, or losing a token after an injected space); parser regressions
// that consume whitespace positions to disambiguate grammar.
func TestPropertyParseWhitespaceInvariance(t *testing.T) {
	corpus := loadBridgeCorpus(t)
	if len(corpus) == 0 {
		t.Fatal("bridge corpus is empty — the whitespace-invariance property needs real .qll files to mutate")
	}

	rapid.Check(t, func(t *rapid.T) {
		// Pick a source file from the corpus.
		idx := rapid.IntRange(0, len(corpus)-1).Draw(t, "corpusIdx")
		entry := corpus[idx]

		// Parse the original. The bridge corpus is real and must always parse
		// cleanly — a failure here is a parser regression, not a generator
		// rejection. Failing hard catches "parser broke so nothing parses" bugs
		// that would otherwise show up as skips.
		origMod, err := parse.NewParser(entry.src, entry.path).Parse()
		if err != nil {
			t.Fatalf("bridge corpus file %s failed to parse: %v", entry.path, err)
		}

		// Mutate: inject extra whitespace (and optionally a comment) at token
		// boundaries. We only insert characters that the lexer treats as
		// whitespace or comment — so if the parser is correct, the mutated
		// source must parse to an equivalent AST.
		mutated := injectWhitespace(t, entry.src)
		if mutated == entry.src {
			t.Skip("mutation produced identical source")
		}

		mutMod, err := parse.NewParser(mutated, entry.path).Parse()
		if err != nil {
			t.Fatalf("mutated source failed to parse but original succeeded:\nerror: %v\noriginal bytes: %d\nmutated bytes:  %d",
				err, len(entry.src), len(mutated))
		}

		// Compare the two ASTs with Span fields zeroed. Span is source-location
		// metadata — it legitimately differs after injecting whitespace, so
		// must be excluded from the oracle.
		zeroSpans(reflect.ValueOf(origMod).Elem())
		zeroSpans(reflect.ValueOf(mutMod).Elem())

		if !reflect.DeepEqual(origMod, mutMod) {
			// Produce a readable diff hint.
			t.Fatalf("whitespace-invariance violated for %s:\n  original parse differs from mutated parse after zeroing spans", entry.path)
		}
	})
}

type corpusEntry struct {
	path string
	src  string
}

func loadBridgeCorpus(t *testing.T) []corpusEntry {
	t.Helper()
	// Start from the test's working directory (ql/parse) and walk up to repo root.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	// repo root is two levels up from ql/parse
	root := filepath.Dir(filepath.Dir(wd))
	bridgeDir := filepath.Join(root, "bridge")
	entries, err := os.ReadDir(bridgeDir)
	if err != nil {
		t.Fatalf("read bridge dir %s: %v", bridgeDir, err)
	}
	var out []corpusEntry
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".qll") && !strings.HasSuffix(name, ".ql") {
			continue
		}
		full := filepath.Join(bridgeDir, name)
		data, err := os.ReadFile(full)
		if err != nil {
			t.Fatalf("read %s: %v", full, err)
		}
		out = append(out, corpusEntry{path: name, src: string(data)})
	}
	return out
}

// injectWhitespace inserts extra whitespace/comments at character positions in
// src. Insertions are only placed between tokens (between non-identifier and
// identifier, or at positions where the original source already has a space),
// which is a conservative approximation that never glues two tokens together.
func injectWhitespace(t *rapid.T, src string) string {
	nInsertions := rapid.IntRange(1, 5).Draw(t, "nInsertions")

	// Candidate insert positions: every index that is outside a comment
	// or string literal AND whose previous character is whitespace or
	// token-ending punctuation. Excluding comments/strings is critical —
	// inserting a newline inside a `//` line comment would truncate it and
	// expose the tail; inserting anything inside a `/* */` block comment
	// is likewise dangerous if it could interact with the close.
	candidates := safeInsertPositions(src)
	if len(candidates) == 0 {
		return src
	}

	// Build the mutated source by inserting at randomly-chosen candidates.
	// Sort insertion positions so we can build in one pass.
	type insertion struct {
		pos int
		s   string
	}
	insertions := make([]insertion, 0, nInsertions)
	for i := 0; i < nInsertions; i++ {
		ci := rapid.IntRange(0, len(candidates)-1).Draw(t, fmt.Sprintf("cand_%d", i))
		// Pick a whitespace/comment payload. All options are lexer-neutral.
		// Only whitespace payloads. Comment payloads are unsafe because they
		// can land inside an existing block comment and close it early
		// (the lexer does not support nested comments), which would make
		// the mutation change token structure rather than preserve it.
		payload := rapid.SampledFrom([]string{
			" ", "  ", "\t", "\n", " \n ", "\n\n",
		}).Draw(t, fmt.Sprintf("payload_%d", i))
		insertions = append(insertions, insertion{pos: candidates[ci], s: payload})
	}
	// Stable sort by pos ascending.
	for i := 1; i < len(insertions); i++ {
		for j := i; j > 0 && insertions[j-1].pos > insertions[j].pos; j-- {
			insertions[j-1], insertions[j] = insertions[j], insertions[j-1]
		}
	}

	var b strings.Builder
	b.Grow(len(src) + 64)
	last := 0
	for _, ins := range insertions {
		b.WriteString(src[last:ins.pos])
		b.WriteString(ins.s)
		last = ins.pos
	}
	b.WriteString(src[last:])
	return b.String()
}

func isWSOrPunct(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\r':
		return true
	case '(', ')', '{', '}', ',', ';', '[', ']':
		return true
	}
	return false
}

// safeInsertPositions walks src tracking comment and string state, returning
// positions where an extra whitespace character may be inserted without
// changing the token stream. We only emit positions that are (a) in normal
// (non-comment, non-string) lexical state and (b) whose preceding character
// is whitespace or token-ending punctuation — so there's no chance of gluing
// or splitting an existing token.
func safeInsertPositions(src string) []int {
	var out []int
	i := 0
	for i < len(src) {
		c := src[i]
		// Line comment
		if c == '/' && i+1 < len(src) && src[i+1] == '/' {
			for i < len(src) && src[i] != '\n' {
				i++
			}
			continue
		}
		// Block comment
		if c == '/' && i+1 < len(src) && src[i+1] == '*' {
			i += 2
			for i+1 < len(src) && !(src[i] == '*' && src[i+1] == '/') {
				i++
			}
			if i+1 < len(src) {
				i += 2
			}
			continue
		}
		// String literal (QL uses " ... " with \ escapes)
		if c == '"' {
			i++
			for i < len(src) && src[i] != '"' {
				if src[i] == '\\' && i+1 < len(src) {
					i += 2
					continue
				}
				i++
			}
			if i < len(src) {
				i++
			}
			continue
		}
		// Normal state: candidate if previous char was WS or punctuation.
		if i > 0 && isWSOrPunct(src[i-1]) {
			out = append(out, i)
		}
		i++
	}
	return out
}

// zeroSpans walks a reflect.Value and zeroes every field of type ast.Span.
// This lets reflect.DeepEqual compare the structural shape of two ASTs
// without false-positive mismatches on source locations.
func zeroSpans(v reflect.Value) {
	switch v.Kind() {
	case reflect.Struct:
		if v.Type() == reflect.TypeOf(ast.Span{}) {
			v.Set(reflect.Zero(v.Type()))
			return
		}
		for i := 0; i < v.NumField(); i++ {
			f := v.Field(i)
			if f.CanSet() {
				zeroSpans(f)
			}
		}
	case reflect.Ptr:
		if !v.IsNil() {
			zeroSpans(v.Elem())
		}
	case reflect.Interface:
		if !v.IsNil() {
			// Interface values need a detour: extract the concrete value,
			// zero it, then set back. For ASTs this handles Formula and Expr
			// interface fields.
			concrete := v.Elem()
			if concrete.Kind() == reflect.Ptr && !concrete.IsNil() {
				zeroSpans(concrete.Elem())
			} else if concrete.Kind() == reflect.Struct {
				// We cannot assign through an interface without a pointer,
				// so make an addressable copy, zero it, and set back.
				copyVal := reflect.New(concrete.Type()).Elem()
				copyVal.Set(concrete)
				zeroSpans(copyVal)
				v.Set(copyVal)
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			zeroSpans(v.Index(i))
		}
	case reflect.Map:
		for _, k := range v.MapKeys() {
			ev := v.MapIndex(k)
			copyVal := reflect.New(ev.Type()).Elem()
			copyVal.Set(ev)
			zeroSpans(copyVal)
			v.SetMapIndex(k, copyVal)
		}
	}
}
