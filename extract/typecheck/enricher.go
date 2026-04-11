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

	projectOnce sync.Once
	projectErr  error
	rootDir     string
}

// NewEnricher creates an enricher. It initializes tsgo and opens the project.
func NewEnricher(client *Client, rootDir string) (*Enricher, error) {
	_, err := client.Initialize()
	if err != nil {
		return nil, fmt.Errorf("enricher: initialize tsgo: %w", err)
	}
	return &Enricher{
		client:  client,
		rootDir: rootDir,
	}, nil
}

// getProject returns the cached project handle, resolving it on first call
// using the given file path.
func (e *Enricher) getProject(filePath string) (string, error) {
	e.projectOnce.Do(func() {
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

// Close releases tsgo resources.
func (e *Enricher) Close() error {
	return e.client.Close()
}

// Position represents a source position to query.
type Position struct {
	Line int
	Col  int
}
