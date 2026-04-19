package output

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Gjdoalfnrxu/tsq/ql/eval"
)

// MarkdownOptions controls markdown report output.
type MarkdownOptions struct {
	QueryName        string        // displayed in the title (e.g. from @name or filename)
	QueryDescription string        // displayed as a blockquote
	QueryID          string        // displayed as a code-formatted line (e.g. from @id)
	ToolVersion      string        // tool version string (footer)
	SourceRoot       string        // base dir for resolving relative file paths in snippets
	WallTime         time.Duration // total query wall time (footer)
	ContextLines     int           // lines of context above/below the match (default 5)
	// FileColumn and LineColumn override the column-name heuristic when set
	// (>= 0). Useful for queries whose select clauses end up with auto-named
	// columns (col0, col1, ...) — the user can point at the right indices
	// directly. Negative values fall back to the heuristic.
	FileColumn int
	LineColumn int
}

// WriteMarkdown writes the ResultSet as a self-contained markdown report to w.
//
// Each result row that has a resolvable file+line gets its own section with a
// header, and a fenced code block containing the matched line plus
// opts.ContextLines lines of surrounding context. The matched line is marked
// with a trailing comment in the snippet's gutter prefix.
//
// File contents are read once per file and cached for the lifetime of the call.
// Files that cannot be opened are reported with a note in place of the snippet
// rather than failing the whole report.
func WriteMarkdown(w io.Writer, rs *eval.ResultSet, opts MarkdownOptions) error {
	if opts.QueryName == "" {
		opts.QueryName = "tsq-query"
	}
	if opts.ToolVersion == "" {
		opts.ToolVersion = "0.0.1-dev"
	}
	if opts.ContextLines <= 0 {
		opts.ContextLines = 5
	}
	// Column-index overrides: callers must set negative values to opt out
	// (i.e. fall back to the column-name heuristic). Struct-literal
	// callers using zero-values get treated as "not set" — both-zero is
	// disambiguated by negative defaulting in the constructor below.
	fileColOverride := opts.FileColumn
	lineColOverride := opts.LineColumn
	// A zero-value MarkdownOptions (no fields set) means "auto" — flip the
	// 0/0 sentinel into the explicit -1/-1 disabled-override state.
	if fileColOverride == 0 && lineColOverride == 0 {
		fileColOverride = -1
		lineColOverride = -1
	}

	bw := bufio.NewWriter(w)

	// Header.
	fmt.Fprintf(bw, "# %s\n\n", opts.QueryName)
	if strings.TrimSpace(opts.QueryDescription) != "" {
		for _, line := range strings.Split(strings.TrimSpace(opts.QueryDescription), "\n") {
			fmt.Fprintf(bw, "> %s\n", line)
		}
		fmt.Fprintln(bw)
	}
	if opts.QueryID != "" {
		fmt.Fprintf(bw, "`%s`\n\n", opts.QueryID)
	}

	// Index columns once.
	colIdx := make(map[string]int, len(rs.Columns))
	for i, c := range rs.Columns {
		colIdx[c] = i
	}

	cache := newFileCache(opts.SourceRoot)

	rendered := 0
	for _, row := range rs.Rows {
		file, line, ok := tryResolveFileLine(colIdx, row, fileColOverride, lineColOverride)
		if !ok {
			// No location — render as a plain bullet so the data isn't lost.
			fmt.Fprintf(bw, "- %s\n", buildRowMessage(rs.Columns, row))
			rendered++
			continue
		}

		fmt.Fprintf(bw, "## %s:%d\n\n", file, line)

		// Per-row message line. Suppress when the first column is a bare
		// integer node ID (common when queries select an `int` AST handle)
		// or when it duplicates the file path — in both cases the line adds
		// nothing for a human reader.
		msg := buildRowMessage(rs.Columns, row)
		if msg != "" && msg != file && !looksLikeBareInt(msg) {
			fmt.Fprintf(bw, "%s\n\n", msg)
		}

		lang := languageForFile(file)
		snippet, err := cache.snippet(file, line, opts.ContextLines)
		if err != nil {
			fmt.Fprintf(bw, "_could not read source: %v_\n\n", err)
			rendered++
			continue
		}
		fmt.Fprintf(bw, "```%s\n", lang)
		if _, werr := bw.WriteString(snippet); werr != nil {
			return werr
		}
		if !strings.HasSuffix(snippet, "\n") {
			if werr := bw.WriteByte('\n'); werr != nil {
				return werr
			}
		}
		fmt.Fprintln(bw, "```")
		fmt.Fprintln(bw)
		rendered++
	}

	// Footer.
	fmt.Fprintln(bw, "---")
	fmt.Fprintf(bw, "_%d result(s) in %s — tsq %s_\n", len(rs.Rows), formatDuration(opts.WallTime), opts.ToolVersion)
	_ = rendered
	return bw.Flush()
}

// tryResolveFileLine extracts (file, line) from a row using the same column
// heuristics as SARIF location extraction. Explicit column-index overrides
// (>= 0) take precedence over the name-based heuristic.
func tryResolveFileLine(colIdx map[string]int, row []eval.Value, fileColOverride, lineColOverride int) (string, int, bool) {
	fileCol := -1
	if fileColOverride >= 0 {
		fileCol = fileColOverride
	} else {
		for _, name := range []string{"file", "path", "filepath", "uri"} {
			if idx, ok := colIdx[name]; ok {
				fileCol = idx
				break
			}
		}
	}
	if fileCol < 0 || fileCol >= len(row) {
		return "", 0, false
	}
	file := eval.ValueToString(row[fileCol])
	if file == "" {
		return "", 0, false
	}

	lineCol := -1
	if lineColOverride >= 0 {
		lineCol = lineColOverride
	} else {
		for _, name := range []string{"line", "startLine", "start_line"} {
			if idx, ok := colIdx[name]; ok {
				lineCol = idx
				break
			}
		}
	}
	if lineCol < 0 || lineCol >= len(row) {
		return file, 0, false
	}
	iv, ok := row[lineCol].(eval.IntVal)
	if !ok {
		return file, 0, false
	}
	return file, int(iv.V), true
}

// fileCache reads source files once and serves snippet requests against them.
type fileCache struct {
	root  string
	files map[string][]string // path -> lines (no trailing \n)
	errs  map[string]error
}

func newFileCache(root string) *fileCache {
	return &fileCache{
		root:  root,
		files: make(map[string][]string),
		errs:  make(map[string]error),
	}
}

func (c *fileCache) load(path string) ([]string, error) {
	if lines, ok := c.files[path]; ok {
		return lines, nil
	}
	if err, ok := c.errs[path]; ok {
		return nil, err
	}
	full := path
	if c.root != "" && !filepath.IsAbs(path) {
		full = filepath.Join(c.root, path)
	}
	data, err := os.ReadFile(full)
	if err != nil {
		c.errs[path] = err
		return nil, err
	}
	// Use Split rather than SplitAfter to keep lines clean; we add the
	// trailing newline back when emitting.
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	c.files[path] = lines
	return lines, nil
}

// snippet returns a rendered code block body containing line and ±ctx lines
// of context, with a 4-char gutter (line number + match marker).
func (c *fileCache) snippet(path string, line, ctx int) (string, error) {
	lines, err := c.load(path)
	if err != nil {
		return "", err
	}
	if line < 1 {
		return "", fmt.Errorf("invalid line number %d", line)
	}
	start := line - ctx
	if start < 1 {
		start = 1
	}
	end := line + ctx
	if end > len(lines) {
		end = len(lines)
	}
	// Trim a trailing empty element from a final \n.
	if end == len(lines) && end > 0 && lines[end-1] == "" {
		end--
	}
	if start > end {
		return "", fmt.Errorf("line %d outside file (length %d)", line, len(lines))
	}

	width := len(fmt.Sprintf("%d", end))
	var sb strings.Builder
	for i := start; i <= end; i++ {
		marker := "  "
		if i == line {
			marker = "> "
		}
		fmt.Fprintf(&sb, "%s%*d | %s\n", marker, width, i, lines[i-1])
	}
	return sb.String(), nil
}

// looksLikeBareInt reports whether s parses as a signed decimal integer with
// no other characters. Used to suppress noisy per-row messages when the
// first selected column is an AST node ID.
func looksLikeBareInt(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	start := 0
	if s[0] == '-' || s[0] == '+' {
		if len(s) == 1 {
			return false
		}
		start = 1
	}
	for i := start; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// languageForFile returns a markdown code-fence language hint based on the
// file extension. Falls back to "" (plain) when unknown.
func languageForFile(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".ts":
		return "ts"
	case ".tsx":
		return "tsx"
	case ".js", ".mjs", ".cjs":
		return "js"
	case ".jsx":
		return "jsx"
	case ".json":
		return "json"
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".rs":
		return "rust"
	case ".css":
		return "css"
	case ".html":
		return "html"
	case ".md":
		return "markdown"
	case ".yml", ".yaml":
		return "yaml"
	case ".sh", ".bash":
		return "bash"
	}
	return ""
}

// formatDuration renders a duration with sensible precision for the footer.
func formatDuration(d time.Duration) string {
	if d <= 0 {
		return "n/a"
	}
	if d < time.Microsecond {
		return d.String()
	}
	if d < time.Millisecond {
		return fmt.Sprintf("%dµs", d.Microseconds())
	}
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return d.Round(10 * time.Millisecond).String()
}

// metadataRe matches /** ... */ leading block comments in CodeQL-style query
// headers. Used to extract @name / @description / @id when present.
var metadataRe = regexp.MustCompile(`(?s)^\s*/\*\*(.*?)\*/`)

// ParseQueryMetadata extracts @name, @description, and @id from the leading
// /** ... */ block of a query source. Returns zero values when no such block
// is present. Whitespace and leading "*" gutters are stripped.
//
// Exported for use by callers that want to populate MarkdownOptions from a
// raw query file without re-implementing the parser.
func ParseQueryMetadata(src string) (name, description, id string) {
	m := metadataRe.FindStringSubmatch(src)
	if m == nil {
		return "", "", ""
	}
	body := m[1]

	// Normalise gutters: each line may start with whitespace + "*".
	var clean []string
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimLeft(line, " \t")
		line = strings.TrimPrefix(line, "*")
		line = strings.TrimLeft(line, " \t")
		clean = append(clean, line)
	}

	// Walk lines, splitting on @tag boundaries. Tag content runs until the
	// next @tag or end of comment.
	type tag struct{ name, value string }
	var tags []tag
	var cur *tag
	for _, line := range clean {
		if strings.HasPrefix(line, "@") {
			parts := strings.SplitN(line, " ", 2)
			t := tag{name: strings.TrimPrefix(parts[0], "@")}
			if len(parts) == 2 {
				t.value = parts[1]
			}
			tags = append(tags, t)
			cur = &tags[len(tags)-1]
		} else if cur != nil && strings.TrimSpace(line) != "" {
			cur.value += "\n" + line
		}
	}
	// Stable ordering by tag name doesn't matter; we only consume known ones.
	sort.SliceStable(tags, func(i, j int) bool { return false })
	for _, t := range tags {
		switch t.name {
		case "name":
			name = strings.TrimSpace(t.value)
		case "description":
			description = strings.TrimSpace(t.value)
		case "id":
			id = strings.TrimSpace(t.value)
		}
	}
	return name, description, id
}
