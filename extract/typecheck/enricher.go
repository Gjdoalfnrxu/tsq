package typecheck

import (
	"fmt"
	"os"
	"sync"
	"unicode/utf8"
)

// TypeFact represents a resolved type for a source position.
type TypeFact struct {
	Line        int
	Col         int
	TypeDisplay string // human-readable type string from tsgo
	TypeHandle  string // tsgo type handle for further queries
}

// EnrichStats summarises a single EnrichFile run. It exists so callers can
// distinguish "no symbols at the queried positions" (genuinely empty result)
// from "every RPC failed" (broken pipeline that previously hid behind a
// silent-skip on every error). All counters are best-effort.
type EnrichStats struct {
	SymbolQueries int // total getSymbolAtPosition calls attempted
	SymbolErrors  int // RPC errors from getSymbolAtPosition
	SymbolEmpty   int // calls that returned an empty handle (no symbol at offset)
	TypeQueries   int // total getTypeOfSymbol calls attempted
	TypeErrors    int // RPC errors from getTypeOfSymbol
	TypeEmpty     int // calls that returned an empty type handle
	TypeToString  int // typeToString calls attempted
	TypeToStrErr  int // typeToString failures
	OffsetErrors  int // positions that could not be mapped to a byte offset
	FactsEmitted  int // len(returned facts)
}

// Enricher uses a tsgo Client to enrich an extraction database with type information.
type Enricher struct {
	client       *Client
	rootDir      string
	tsconfigPath string // optional absolute path to a tsconfig.json

	registerMu sync.Mutex
	registered []string // file paths to advertise via fileChanges.created on OpenProject

	projectOnce sync.Once
	project     string
	projectErr  error

	cacheMu     sync.Mutex
	offsetCache map[string][]int // filePath → cumulative UTF-16 code-unit offset per line start
}

// NewEnricher creates an enricher. It initializes tsgo and opens the project.
//
// Deprecated: use NewEnricherWithConfig to pass an explicit tsconfig.json
// location. Without a tsconfig the tsgo session has no project loaded and
// downstream queries will fail.
func NewEnricher(client *Client, rootDir string) (*Enricher, error) {
	return NewEnricherWithConfig(client, rootDir, "")
}

// NewEnricherWithConfig creates an enricher and prepares it to load the given
// tsconfig.json. The actual updateSnapshot call is deferred until the first
// EnrichFile so callers can RegisterFiles in between for fileChanges.created.
func NewEnricherWithConfig(client *Client, rootDir, tsconfigPath string) (*Enricher, error) {
	if _, err := client.Initialize(); err != nil {
		return nil, fmt.Errorf("enricher: initialize tsgo: %w", err)
	}
	return &Enricher{
		client:       client,
		rootDir:      rootDir,
		tsconfigPath: tsconfigPath,
		offsetCache:  make(map[string][]int),
	}, nil
}

// RegisterFiles tells the enricher about source files that should be
// advertised to tsgo via UpdateSnapshotParams.FileChanges.Created when the
// project is opened. Empirically the live tsgo binary refuses to resolve
// position queries for files reachable only via tsconfig `include` globs
// unless they have been declared this way first.
//
// Must be called before the first EnrichFile (which triggers OpenProject).
// IMPORTANT: only files registered before that first EnrichFile call are
// seeded into the snapshot — getProject uses sync.Once to open the project
// exactly once and reads the registered slice at that moment. Subsequent
// RegisterFiles calls append to the slice but have no effect on tsgo's view
// of the project, and EnrichFile's `appendUnique(files, filePath)` fallback
// only covers the single file being queried right now. Callers that discover
// files lazily must register them all up-front, or the late arrivals will
// fail to resolve position queries.
func (e *Enricher) RegisterFiles(paths []string) {
	e.registerMu.Lock()
	defer e.registerMu.Unlock()
	e.registered = append(e.registered, paths...)
}

func (e *Enricher) getProject(filePath string) (string, error) {
	e.projectOnce.Do(func() {
		if e.tsconfigPath != "" {
			e.registerMu.Lock()
			files := append([]string(nil), e.registered...)
			e.registerMu.Unlock()
			// Always include the file we are about to query, in case the
			// caller forgot to RegisterFiles.
			files = appendUnique(files, filePath)
			e.project, e.projectErr = e.client.OpenProjectWithFiles(e.tsconfigPath, files)
			return
		}
		e.project, e.projectErr = e.client.GetProjectForFile(filePath)
	})
	return e.project, e.projectErr
}

func appendUnique(xs []string, x string) []string {
	for _, v := range xs {
		if v == x {
			return xs
		}
	}
	return append(xs, x)
}

// EnrichFile queries tsgo for type information about symbols at the given
// positions (1-based line, 0-based byte column — matching the tree-sitter /
// extractor convention). It returns a TypeFact per position where tsgo
// resolved a non-empty type, plus an EnrichStats summarising all RPC outcomes.
//
// EnrichFile only returns a non-nil error for the failure modes that make the
// rest of the work meaningless (project resolution, source-file read).
// Per-position RPC failures are recorded in the returned stats; callers should
// inspect stats.SymbolErrors / stats.TypeErrors to detect a broken pipeline
// even when len(facts) == 0.
func (e *Enricher) EnrichFile(filePath string, positions []Position) ([]TypeFact, EnrichStats, error) {
	var stats EnrichStats
	proj, err := e.getProject(filePath)
	if err != nil {
		return nil, stats, fmt.Errorf("enricher: get project for %s: %w", filePath, err)
	}

	// Resolve byte offsets up front. If the source file can't be read, that
	// is a genuine error — bail out.
	lineStarts, err := e.lineStartsFor(filePath)
	if err != nil {
		return nil, stats, fmt.Errorf("enricher: read source for %s: %w", filePath, err)
	}

	var facts []TypeFact
	for _, pos := range positions {
		offset, ok := offsetForLineCol(lineStarts, pos.Line, pos.Col)
		if !ok {
			stats.OffsetErrors++
			continue
		}

		stats.SymbolQueries++
		sym, err := e.client.GetSymbolAtOffset(proj, filePath, offset)
		if err != nil {
			stats.SymbolErrors++
			continue
		}
		if sym.Handle == "" {
			stats.SymbolEmpty++
			continue
		}

		stats.TypeQueries++
		typeInfo, err := e.client.GetTypeOfSymbol(proj, sym.Handle)
		if err != nil {
			stats.TypeErrors++
			continue
		}
		if typeInfo.Handle == "" {
			stats.TypeEmpty++
			continue
		}

		// TypeResponse does not include a display name; resolve it via a
		// dedicated typeToString round-trip. A failure here doesn't drop
		// the fact (we can still link the type handle); we just leave the
		// display blank and count it.
		stats.TypeToString++
		display, err := e.client.TypeToString(proj, typeInfo.Handle)
		if err != nil {
			stats.TypeToStrErr++
			display = typeInfo.DisplayName // legacy mocks may still set this
		}

		facts = append(facts, TypeFact{
			Line:        pos.Line,
			Col:         pos.Col,
			TypeDisplay: display,
			TypeHandle:  typeInfo.Handle,
		})
	}

	stats.FactsEmitted = len(facts)
	return facts, stats, nil
}

// lineStartsFor returns a slice s such that s[k] is the UTF-16 code-unit
// offset of the start of line k+1 (1-based). The result is cached per file.
//
// tsgo treats `position` as a UTF-16 offset (proto.go references UTF16ToUTF8
// internally); for pure ASCII source the offset equals the byte count.
func (e *Enricher) lineStartsFor(filePath string) ([]int, error) {
	e.cacheMu.Lock()
	if cached, ok := e.offsetCache[filePath]; ok {
		e.cacheMu.Unlock()
		return cached, nil
	}
	e.cacheMu.Unlock()

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	starts := buildLineStartsUTF16(data)

	e.cacheMu.Lock()
	e.offsetCache[filePath] = starts
	e.cacheMu.Unlock()
	return starts, nil
}

// buildLineStartsUTF16 walks src as UTF-8, computing the UTF-16 code-unit
// offset of each line start (line 1 = index 0).
func buildLineStartsUTF16(src []byte) []int {
	starts := []int{0}
	utf16Off := 0
	i := 0
	for i < len(src) {
		r, sz := utf8.DecodeRune(src[i:])
		i += sz
		if r >= 0x10000 {
			utf16Off += 2 // surrogate pair
		} else {
			utf16Off++
		}
		if r == '\n' {
			starts = append(starts, utf16Off)
		}
	}
	return starts
}

// offsetForLineCol converts a (1-based line, 0-based column) pair to a UTF-16
// offset. Returns false if the line is out of range.
//
// NOTE: `col` from the tree-sitter walker is a byte column. tsgo / TS APIs
// expect UTF-16 columns. For pure-ASCII source the two are identical, so the
// happy path is correct. For source containing non-ASCII characters before
// the queried position, the byte column is larger than the UTF-16 column
// (multi-byte UTF-8 sequences count as one UTF-16 code unit for BMP chars,
// two for astral chars), so the offset we compute here will land past the
// intended node and tsgo will return the wrong type — or no type at all.
// We do NOT silently "treat byte columns as UTF-16"; this code is plainly
// wrong for non-ASCII source and the tracking issue is in the wiki. Fixing
// it requires either surfacing UTF-16 columns from the extractor or doing
// a UTF-8 -> UTF-16 reconversion here using the line text.
func offsetForLineCol(lineStarts []int, line, col int) (uint32, bool) {
	if line < 1 || line > len(lineStarts) {
		return 0, false
	}
	off := lineStarts[line-1] + col
	if off < 0 {
		return 0, false
	}
	return uint32(off), true
}

// WriteTypeFacts writes TypeFact values into the fact DB via the emit callback.
// For each TypeFact, it emits ExprType and SymbolType tuples, and a ResolvedType
// tuple for the type itself. The posNodeID callback converts (filePath, line, col)
// to an entity ID for the expression/symbol at that position. The symID callback
// converts (filePath, line, col) to a symbol entity ID. The typeEntityID callback
// converts a type handle to a type entity ID.
func WriteTypeFacts(
	emit func(relName string, cols ...interface{}),
	facts []TypeFact,
	filePath string,
	posNodeID func(filePath string, line, col int) uint32,
	symID func(filePath string, line, col int) uint32,
	typeEntityID func(typeHandle string) uint32,
) {
	seenTypes := make(map[string]bool)
	for _, fact := range facts {
		if fact.TypeHandle == "" {
			continue
		}
		typeID := typeEntityID(fact.TypeHandle)
		if !seenTypes[fact.TypeHandle] {
			seenTypes[fact.TypeHandle] = true
			emit("ResolvedType", typeID, fact.TypeDisplay)
		}
		exprID := posNodeID(filePath, fact.Line, fact.Col)
		emit("ExprType", exprID, typeID)
		sID := symID(filePath, fact.Line, fact.Col)
		emit("SymbolType", sID, typeID)
	}
}

// Close releases tsgo resources.
func (e *Enricher) Close() error {
	return e.client.Close()
}

// Position represents a source position to query.
//
// Line is 1-based. Col is a 0-based UTF-16 column (in practice a byte column
// for ASCII source; see offsetForLineCol).
type Position struct {
	Line int
	Col  int
}
