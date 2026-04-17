package typecheck

// InitializeResponse is returned by the initialize method.
type InitializeResponse struct {
	UseCaseSensitiveFileNames bool   `json:"useCaseSensitiveFileNames"`
	CurrentDirectory          string `json:"currentDirectory"`
}

// TypeInfo describes a resolved type.
//
// Field tags match upstream TypeResponse (microsoft/typescript-go/internal/api
// proto.go:489-503): the handle is the `id` field, not `handle`. Display name
// is NOT part of TypeResponse — call typeToString separately to obtain a
// human-readable string. The DisplayName field on this struct is populated by
// the client/enricher after a follow-up typeToString round-trip.
type TypeInfo struct {
	Handle      string `json:"id"`
	DisplayName string `json:"-"`
	Flags       int    `json:"flags"`
}

// SymbolInfo describes a resolved symbol.
//
// Field tags match upstream SymbolResponse (proto.go:444-451): the handle is
// the `id` field, not `handle`.
type SymbolInfo struct {
	Handle string `json:"id"`
	Name   string `json:"name"`
	Flags  int    `json:"flags"`
}

// MemberInfo describes a member of a symbol or type.
type MemberInfo struct {
	Handle string `json:"id"`
	Name   string `json:"name"`
}

// Diagnostic represents a type error or warning from the checker.
type Diagnostic struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Col     int    `json:"col"`
	Message string `json:"message"`
}

// Location is a source position for batch queries.
type Location struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Col  int    `json:"col"`
}
