package output

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Gjdoalfnrxu/tsq/ql/eval"
)

// errPathOutsideRoot is returned by fileCache.load when SourceRoot is set and
// the requested path resolves outside the root (absolute path or upward
// traversal). WriteMarkdown turns this into a "path outside source root" note
// instead of a hard read error.
var errPathOutsideRoot = errors.New("path outside source root")

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
	// (i.e. fall back to the column-name heuristic). The CLI defaults
	// --md-file-col and --md-line-col to -1, so a zero here is a real
	// caller-provided index (e.g. col 0 is the file column).
	fileColOverride := opts.FileColumn
	lineColOverride := opts.LineColumn

	bw := bufio.NewWriter(w)

	// Nil result-set guard: emit a minimal header + "no results" footer.
	if rs == nil {
		fmt.Fprintf(bw, "# %s\n\n", opts.QueryName)
		fmt.Fprintln(bw, "---")
		fmt.Fprintf(bw, "_0 result(s) in %s — tsq %s_\n", formatDuration(opts.WallTime), opts.ToolVersion)
		return bw.Flush()
	}

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

	for _, row := range rs.Rows {
		file, line, ok := tryResolveFileLine(colIdx, row, fileColOverride, lineColOverride)
		if !ok {
			// No location — render as a plain bullet so the data isn't lost.
			fmt.Fprintf(bw, "- %s\n", buildRowMessage(rs.Columns, row))
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
			if errors.Is(err, errPathOutsideRoot) {
				fmt.Fprintf(bw, "_skipped: path outside source root_\n\n")
			} else {
				fmt.Fprintf(bw, "_could not read source: %v_\n\n", err)
			}
			continue
		}
		fence := fenceFor(snippet)
		fmt.Fprintf(bw, "%s%s\n", fence, lang)
		if _, werr := bw.WriteString(snippet); werr != nil {
			return werr
		}
		if !strings.HasSuffix(snippet, "\n") {
			if werr := bw.WriteByte('\n'); werr != nil {
				return werr
			}
		}
		fmt.Fprintln(bw, fence)
		fmt.Fprintln(bw)
	}

	// Footer.
	fmt.Fprintln(bw, "---")
	fmt.Fprintf(bw, "_%d result(s) in %s — tsq %s_\n", len(rs.Rows), formatDuration(opts.WallTime), opts.ToolVersion)
	return bw.Flush()
}

// fenceFor returns a backtick fence string at least 3 long, and always longer
// than the longest run of consecutive backticks inside snippet. This prevents
// a snippet containing literal triple-backticks from prematurely closing the
// outer fence.
func fenceFor(snippet string) string {
	longest := 0
	run := 0
	for i := 0; i < len(snippet); i++ {
		if snippet[i] == '`' {
			run++
			if run > longest {
				longest = run
			}
		} else {
			run = 0
		}
	}
	n := longest + 1
	if n < 3 {
		n = 3
	}
	return strings.Repeat("`", n)
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
//
// Not safe for concurrent use; intended for one-shot CLI runs where a single
// goroutine builds the report.
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
	if c.root != "" {
		// Containment: when a root is configured, reject absolute paths
		// from query results outright, and reject any relative path that
		// escapes the root via ".." traversal. When no root is set,
		// behaviour is unchanged — the caller is trusted to supply paths.
		if filepath.IsAbs(path) {
			c.errs[path] = errPathOutsideRoot
			return nil, errPathOutsideRoot
		}
		joined := filepath.Join(c.root, path)
		rel, relErr := filepath.Rel(c.root, joined)
		if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			c.errs[path] = errPathOutsideRoot
			return nil, errPathOutsideRoot
		}
		full = joined
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
