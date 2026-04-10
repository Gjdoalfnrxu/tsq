package extract

import (
	"context"
)

// VendoredScopeAdapter wraps symbol resolution for the VendoredBackend.
// When tsgo is available, it delegates to the tsgo subprocess for richer
// cross-file resolution. When tsgo is absent, it falls back to the
// existing in-file ScopeAnalyzer (same behaviour as TreeSitterBackend).
//
// This adapter implements the same resolution interface that the FactWalker
// uses via ScopeAnalyzer.Resolve(), so the walker does not need to know
// which backend is providing scope information.
type VendoredScopeAdapter struct {
	// backend is the VendoredBackend that owns the tsgo subprocess.
	backend *VendoredBackend

	// fallback is the in-file ScopeAnalyzer used when tsgo is unavailable.
	fallback *ScopeAnalyzer
}

// NewVendoredScopeAdapter creates a scope adapter. The fallback ScopeAnalyzer
// is used whenever tsgo is not available or when the tsgo RPC call fails.
func NewVendoredScopeAdapter(backend *VendoredBackend, fallback *ScopeAnalyzer) *VendoredScopeAdapter {
	return &VendoredScopeAdapter{
		backend:  backend,
		fallback: fallback,
	}
}

// Resolve attempts to resolve a symbol name at the given AST node position.
// If tsgo is available, it tries tsgo first (which can resolve cross-file
// symbols). On failure or unavailability, it falls back to the in-file
// ScopeAnalyzer.
func (a *VendoredScopeAdapter) Resolve(name string, atNode ASTNode) (*Declaration, bool) {
	// If tsgo is available, try it first for potentially richer resolution.
	if a.backend != nil && a.backend.TsgoAvailable() {
		decl, ok := a.resolveTsgo(name, atNode)
		if ok {
			return decl, true
		}
		// Fall through to local scope on tsgo failure.
	}

	// Fall back to in-file scope analysis (same as TreeSitterBackend).
	if a.fallback != nil {
		return a.fallback.Resolve(name, atNode)
	}
	return nil, false
}

// resolveTsgo attempts symbol resolution via the tsgo subprocess.
func (a *VendoredScopeAdapter) resolveTsgo(name string, atNode ASTNode) (*Declaration, bool) {
	if atNode == nil {
		return nil, false
	}

	ctx, cancel := context.WithTimeout(context.Background(), DefaultRPCTimeout)
	defer cancel()

	ref := SymbolRef{
		FilePath:  a.fallback.filePath,
		StartByte: a.fallback.nodeStartByte(atNode),
		Name:      name,
	}

	decl, err := a.backend.ResolveSymbol(ctx, ref)
	if err != nil {
		// Any error (including ErrUnsupported) means we fall back.
		return nil, false
	}

	return &Declaration{
		Name:      decl.Name,
		FilePath:  decl.FilePath,
		StartByte: decl.StartByte,
		// StartLine and StartCol are not available from tsgo's response
		// in the current placeholder protocol. When tsgo is actually
		// wired up, these would be populated from the response.
		StartLine: 0,
		StartCol:  0,
	}, true
}

// Build delegates to the fallback ScopeAnalyzer to build the in-file scope
// tree. This is always needed because even with tsgo, the FactWalker uses
// scope information synchronously during the AST walk.
func (a *VendoredScopeAdapter) Build(root ASTNode) *Scope {
	if a.fallback != nil {
		return a.fallback.Build(root)
	}
	return nil
}
