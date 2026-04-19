package output

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Gjdoalfnrxu/tsq/ql/eval"
)

func writeFixture(t *testing.T, dir, rel, body string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWriteMarkdown_Empty(t *testing.T) {
	rs := &eval.ResultSet{Columns: []string{"name"}, Rows: nil}
	var buf bytes.Buffer
	if err := WriteMarkdown(&buf, rs, MarkdownOptions{QueryName: "demo", FileColumn: -1, LineColumn: -1}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "# demo") {
		t.Errorf("missing title header: %q", out)
	}
	if !strings.Contains(out, "0 result(s)") {
		t.Errorf("missing result count footer: %q", out)
	}
}

func TestWriteMarkdown_RendersSnippetsForResults(t *testing.T) {
	dir := t.TempDir()

	tsBody := strings.Join([]string{
		"// line 1",
		"// line 2",
		"// line 3",
		"// line 4",
		"// line 5",
		"export function foo() {", // line 6
		"  setState(updater);",    // line 7 -- match
		"}",                       // line 8
		"// line 9",
		"// line 10",
		"// line 11",
		"// line 12",
		"",
	}, "\n")
	writeFixture(t, dir, "src/foo.tsx", tsBody)

	otherBody := strings.Join([]string{
		"package x",
		"",
		"func Bar() {}", // line 3 -- match
		"",
		"// trailing",
	}, "\n")
	writeFixture(t, dir, "pkg/bar.go", otherBody)

	rs := &eval.ResultSet{
		Columns: []string{"name", "file", "line"},
		Rows: [][]eval.Value{
			{eval.StrVal{V: "setState"}, eval.StrVal{V: "src/foo.tsx"}, eval.IntVal{V: 7}},
			{eval.StrVal{V: "Bar"}, eval.StrVal{V: "pkg/bar.go"}, eval.IntVal{V: 3}},
			{eval.StrVal{V: "no-loc"}, eval.StrVal{V: ""}, eval.IntVal{V: 0}},
		},
	}

	var buf bytes.Buffer
	err := WriteMarkdown(&buf, rs, MarkdownOptions{
		QueryName:        "find-setstate",
		QueryDescription: "Find suspicious setState callers.",
		QueryID:          "ts/find-setstate",
		SourceRoot:       dir,
		WallTime:         12 * time.Millisecond,
		ContextLines:     5,
		FileColumn:       -1,
		LineColumn:       -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	for _, want := range []string{
		"# find-setstate",
		"> Find suspicious setState callers.",
		"`ts/find-setstate`",
		"## src/foo.tsx:7",
		"## pkg/bar.go:3",
		"```tsx",
		"```go",
		"setState(updater);",
		"func Bar() {}",
		"3 result(s) in 12ms",
		">  7 |", // matched-line marker (width-padded) for tsx
		"> 3 |",  // matched-line marker for go
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\nfull output:\n%s", want, out)
		}
	}
}

func TestWriteMarkdown_MissingFileNoted(t *testing.T) {
	dir := t.TempDir()
	rs := &eval.ResultSet{
		Columns: []string{"file", "line"},
		Rows: [][]eval.Value{
			{eval.StrVal{V: "does/not/exist.ts"}, eval.IntVal{V: 1}},
		},
	}
	var buf bytes.Buffer
	if err := WriteMarkdown(&buf, rs, MarkdownOptions{SourceRoot: dir, FileColumn: -1, LineColumn: -1}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "## does/not/exist.ts:1") {
		t.Errorf("missing header for unresolved file: %q", out)
	}
	if !strings.Contains(out, "could not read source") {
		t.Errorf("missing read-error note: %q", out)
	}
}

func TestParseQueryMetadata(t *testing.T) {
	src := `/**
 * @name Find unused vars
 * @description Detects variables that are declared
 *              but never read.
 * @id js/unused-vars
 * @kind problem
 */
import javascript

from Variable v
select v`

	name, desc, id := ParseQueryMetadata(src)
	if name != "Find unused vars" {
		t.Errorf("name = %q", name)
	}
	wantDesc := "Detects variables that are declared\nbut never read."
	if desc != wantDesc {
		t.Errorf("description = %q, want %q", desc, wantDesc)
	}
	if id != "js/unused-vars" {
		t.Errorf("id = %q", id)
	}
}

func TestParseQueryMetadata_Absent(t *testing.T) {
	name, desc, id := ParseQueryMetadata("from X x select x\n")
	if name != "" || desc != "" || id != "" {
		t.Errorf("expected zero values, got name=%q desc=%q id=%q", name, desc, id)
	}
}

func TestWriteMarkdown_ColumnIndexOverride(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "x/y.ts", "a\nb\nc\nd\ne\nf\ng\n")
	rs := &eval.ResultSet{
		// Simulate auto-named columns from the planner: col0 (id), col1 (path), col2 (line).
		Columns: []string{"col0", "col1", "col2"},
		Rows: [][]eval.Value{
			{eval.IntVal{V: 999}, eval.StrVal{V: "x/y.ts"}, eval.IntVal{V: 4}},
		},
	}
	var buf bytes.Buffer
	err := WriteMarkdown(&buf, rs, MarkdownOptions{
		SourceRoot: dir,
		FileColumn: 1,
		LineColumn: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "## x/y.ts:4") {
		t.Errorf("override didn't take effect: %q", out)
	}
	if !strings.Contains(out, "> 4 |") {
		t.Errorf("snippet missing matched-line marker: %q", out)
	}
}

// M1: a caller that explicitly passes FileColumn=0, LineColumn=1 must have
// those indices respected — the old 0/0 sentinel collapse used to flip both
// to -1, which would then re-engage the column-name heuristic and miss col 0
// as the file column.
func TestWriteMarkdown_RespectsZeroFileColumn(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "z.ts", "a\nb\nc\nd\ne\n")
	rs := &eval.ResultSet{
		// No "file"/"line" name match — heuristic would fail.
		Columns: []string{"col0", "col1"},
		Rows: [][]eval.Value{
			{eval.StrVal{V: "z.ts"}, eval.IntVal{V: 2}},
		},
	}
	var buf bytes.Buffer
	err := WriteMarkdown(&buf, rs, MarkdownOptions{
		SourceRoot: dir,
		FileColumn: 0,
		LineColumn: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "## z.ts:2") {
		t.Errorf("FileColumn=0 not respected: %q", out)
	}
}

// M2: a row whose file path escapes the configured SourceRoot must be reported
// as a skipped section rather than reading anything outside the root.
func TestWriteMarkdown_PathTraversalContained(t *testing.T) {
	dir := t.TempDir()
	// Create a real file outside the root that we must NOT read.
	outside := filepath.Join(filepath.Dir(dir), "escape.txt")
	if err := os.WriteFile(outside, []byte("SECRET\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(outside)

	rs := &eval.ResultSet{
		Columns: []string{"file", "line"},
		Rows: [][]eval.Value{
			{eval.StrVal{V: "../escape.txt"}, eval.IntVal{V: 1}},
			{eval.StrVal{V: outside}, eval.IntVal{V: 1}}, // absolute path
		},
	}
	var buf bytes.Buffer
	if err := WriteMarkdown(&buf, rs, MarkdownOptions{SourceRoot: dir, FileColumn: -1, LineColumn: -1}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, "SECRET") {
		t.Fatalf("file content outside root leaked into report: %q", out)
	}
	if !strings.Contains(out, "skipped: path outside source root") {
		t.Errorf("missing skip note for traversal: %q", out)
	}
	// Both rows should be skipped (relative .. and absolute).
	if got := strings.Count(out, "skipped: path outside source root"); got != 2 {
		t.Errorf("expected 2 skip notes, got %d: %q", got, out)
	}
}

// M3: a snippet containing literal triple-backticks must be wrapped in a fence
// strictly longer than the longest backtick run, so the outer report is not
// corrupted (the closing fence is not eaten by snippet content).
func TestWriteMarkdown_FenceEscalation(t *testing.T) {
	dir := t.TempDir()
	// Snippet content with a literal ``` and a literal ```` (4 backticks).
	body := strings.Join([]string{
		"line1",
		"```still inside```",
		"````even longer````",
		"target line",
		"line5",
	}, "\n") + "\n"
	writeFixture(t, dir, "f.md", body)
	rs := &eval.ResultSet{
		Columns: []string{"file", "line"},
		Rows: [][]eval.Value{
			{eval.StrVal{V: "f.md"}, eval.IntVal{V: 4}},
		},
	}
	var buf bytes.Buffer
	if err := WriteMarkdown(&buf, rs, MarkdownOptions{
		SourceRoot:   dir,
		ContextLines: 5,
		FileColumn:   -1,
		LineColumn:   -1,
	}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	// Fence must be at least 5 backticks (one longer than the 4-run in content).
	if !strings.Contains(out, "`````") {
		t.Errorf("expected 5+ backtick fence, got: %q", out)
	}
	// The footer "_1 result(s)_" must still appear — i.e. the snippet did
	// not break out of its fence and corrupt subsequent rendering.
	if !strings.Contains(out, "1 result(s)") {
		t.Errorf("footer missing — fence likely corrupted output: %q", out)
	}
}

// M8: nil ResultSet must not panic; a minimal header + footer is emitted.
func TestWriteMarkdown_NilResultSet(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteMarkdown(&buf, nil, MarkdownOptions{QueryName: "nilcase"}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "# nilcase") {
		t.Errorf("missing title: %q", out)
	}
	if !strings.Contains(out, "0 result(s)") {
		t.Errorf("missing zero-results footer: %q", out)
	}
}

func TestLanguageForFile(t *testing.T) {
	cases := map[string]string{
		"a.ts":    "ts",
		"a.tsx":   "tsx",
		"a.js":    "js",
		"a.jsx":   "jsx",
		"a.go":    "go",
		"a.py":    "python",
		"a.weird": "",
	}
	for in, want := range cases {
		if got := languageForFile(in); got != want {
			t.Errorf("languageForFile(%q) = %q, want %q", in, got, want)
		}
	}
}
