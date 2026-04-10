// Package extract provides TypeScript AST extraction into a typed fact database.
package extract

import (
	"context"
	"errors"
)

// ErrUnsupported is returned by semantic methods when the backend does not
// support that operation. Callers should degrade gracefully.
var ErrUnsupported = errors.New("operation not supported by backend")

// ExtractorBackend is the whole-program extraction interface. Implementations
// may support only AST walking (returning ErrUnsupported from semantic methods)
// or may additionally support symbol resolution and cross-file queries.
type ExtractorBackend interface {
	// Open initialises the backend for a project. Must be called before any
	// other method. Resolves source files according to cfg.
	Open(ctx context.Context, cfg ProjectConfig) error

	// WalkAST calls the visitor for every AST node in every source file.
	// The backend controls file ordering. Files are visited sequentially.
	WalkAST(ctx context.Context, v ASTVisitor) error

	// ResolveSymbol returns the declaration site for a reference.
	// Returns ErrUnsupported if the backend cannot resolve symbols.
	ResolveSymbol(ctx context.Context, ref SymbolRef) (SymbolDecl, error)

	// ResolveType returns the inferred type annotation for a node as text.
	// Returns ErrUnsupported if type information is unavailable.
	ResolveType(ctx context.Context, node NodeRef) (string, error)

	// CrossFileRefs returns all source references to a symbol across the
	// project. Returns ErrUnsupported if cross-file resolution is unavailable.
	CrossFileRefs(ctx context.Context, sym SymbolRef) ([]NodeRef, error)

	// Close releases all resources held by the backend.
	Close() error
}

// ProjectConfig configures a project for extraction.
type ProjectConfig struct {
	RootDir    string // absolute path to the project root
	TSConfig   string // optional path to tsconfig.json
	SourceGlob string // optional source glob; default "**/*.{ts,tsx}"
}

// ASTVisitor receives AST nodes during WalkAST. Methods are called in
// depth-first pre/post order. Returning an error from any method aborts
// the walk and propagates the error from WalkAST.
type ASTVisitor interface {
	// EnterFile is called before visiting any node in path.
	EnterFile(path string) error

	// Enter is called when the walker descends into node.
	// If descend is false the node's children are skipped.
	Enter(node ASTNode) (descend bool, err error)

	// Leave is called after all children of node have been visited.
	Leave(node ASTNode) error

	// LeaveFile is called after the last node in the current file.
	LeaveFile(path string) error
}

// ASTNode represents a single node in the concrete syntax tree.
type ASTNode interface {
	Kind() string      // normalised PascalCase kind name
	StartLine() int    // 1-based
	StartCol() int     // 0-based byte column
	EndLine() int      // 1-based
	EndCol() int       // 0-based byte column
	Text() string      // source text of this node
	ChildCount() int   // number of direct children (named + anonymous)
	Child(i int) ASTNode
	FieldName() string // field name this node occupies in its parent, or ""
}

// SymbolRef identifies a symbol reference site.
type SymbolRef struct {
	FilePath  string
	StartByte int
	Name      string
}

// SymbolDecl identifies a symbol declaration site.
type SymbolDecl struct {
	FilePath  string
	StartByte int
	Name      string
}

// NodeRef identifies a node by byte range and kind.
type NodeRef struct {
	FilePath  string
	StartByte int
	EndByte   int
	Kind      string
}
