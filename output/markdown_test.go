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
	if err := WriteMarkdown(&buf, rs, MarkdownOptions{QueryName: "demo"}); err != nil {
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
	if err := WriteMarkdown(&buf, rs, MarkdownOptions{SourceRoot: dir}); err != nil {
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
	if !strings.Contains(desc, "Detects variables") || !strings.Contains(desc, "never read") {
		t.Errorf("description = %q", desc)
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
