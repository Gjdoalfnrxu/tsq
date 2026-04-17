package typecheck

import (
	"fmt"
	"sync"
)

// TypeFact represents a resolved type for a source position.
type TypeFact struct {
	Line        int
	Col         int
	TypeDisplay string // human-readable type string from tsgo
	TypeHandle  string // tsgo type handle for further queries
}

// Enricher uses a tsgo Client to enrich an extraction database with type information.
type Enricher struct {
	client  *Client
	project string // project handle from tsgo (cached)

	projectOnce  sync.Once
	projectErr   error
	rootDir      string
	tsconfigPath string // optional absolute path to a tsconfig.json
}

// NewEnricher creates an enricher. It initializes tsgo and opens the project.
//
// Deprecated: use NewEnricherWithConfig to pass an explicit tsconfig.json
// location. Without a tsconfig the tsgo session has no project loaded and
// getDefaultProjectForFile will return an empty project for every file.
func NewEnricher(client *Client, rootDir string) (*Enricher, error) {
	return NewEnricherWithConfig(client, rootDir, "")
}

// NewEnricherWithConfig creates an enricher and, if tsconfigPath is non-empty,
// loads the tsconfig.json into the tsgo session via OpenProject. Without the
// tsconfig load, tsgo has no project context and getDefaultProjectForFile
// silently returns an empty project handle, defeating the entire enrichment
// pipeline.
func NewEnricherWithConfig(client *Client, rootDir, tsconfigPath string) (*Enricher, error) {
	_, err := client.Initialize()
	if err != nil {
		return nil, fmt.Errorf("enricher: initialize tsgo: %w", err)
	}
	return &Enricher{
		client:       client,
		rootDir:      rootDir,
		tsconfigPath: tsconfigPath,
	}, nil
}

// getProject returns the cached project handle, resolving it on first call.
// If a tsconfigPath was supplied, it loads that project explicitly via
// OpenProject. Otherwise it falls back to tsgo's default project resolution
// for the given file path (which usually fails when no project has been
// opened — this is the legacy behaviour preserved for callers that still
// use NewEnricher).
func (e *Enricher) getProject(filePath string) (string, error) {
	e.projectOnce.Do(func() {
		if e.tsconfigPath != "" {
			e.project, e.projectErr = e.client.OpenProject(e.tsconfigPath)
			return
		}
		e.project, e.projectErr = e.client.GetProjectForFile(filePath)
	})
	return e.project, e.projectErr
}

// EnrichFile queries tsgo for type information about symbols at the given positions.
// positions is a list of (line, col) pairs representing variable declarations and
// function parameters. The caller determines which positions to query.
// Returns TypeFact entries for each position where tsgo returned a valid type.
func (e *Enricher) EnrichFile(filePath string, positions []Position) ([]TypeFact, error) {
	proj, err := e.getProject(filePath)
	if err != nil {
		return nil, fmt.Errorf("enricher: get project for %s: %w", filePath, err)
	}

	var facts []TypeFact
	for _, pos := range positions {
		sym, err := e.client.GetSymbolAtPosition(proj, filePath, pos.Line, pos.Col)
		if err != nil {
			// Gracefully skip positions where tsgo can't resolve a symbol
			continue
		}
		if sym.Handle == "" {
			continue
		}

		typeInfo, err := e.client.GetTypeOfSymbol(proj, sym.Handle)
		if err != nil {
			continue
		}
		if typeInfo.Handle == "" && typeInfo.DisplayName == "" {
			continue
		}

		facts = append(facts, TypeFact{
			Line:        pos.Line,
			Col:         pos.Col,
			TypeDisplay: typeInfo.DisplayName,
			TypeHandle:  typeInfo.Handle,
		})
	}

	return facts, nil
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
	// Track which types we've already emitted ResolvedType for to avoid duplicates.
	seenTypes := make(map[string]bool)
	for _, fact := range facts {
		if fact.TypeHandle == "" {
			continue
		}
		typeID := typeEntityID(fact.TypeHandle)

		// ResolvedType: emit once per unique type handle
		if !seenTypes[fact.TypeHandle] {
			seenTypes[fact.TypeHandle] = true
			emit("ResolvedType", typeID, fact.TypeDisplay)
		}

		// ExprType: link the expression node at this position to the type
		exprID := posNodeID(filePath, fact.Line, fact.Col)
		emit("ExprType", exprID, typeID)

		// SymbolType: link the symbol at this position to the type
		sID := symID(filePath, fact.Line, fact.Col)
		emit("SymbolType", sID, typeID)
	}
}

// Close releases tsgo resources.
func (e *Enricher) Close() error {
	return e.client.Close()
}

// Position represents a source position to query.
type Position struct {
	Line int
	Col  int
}
