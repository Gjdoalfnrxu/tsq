package typecheck

// InitializeResponse is returned by the initialize method.
type InitializeResponse struct {
	UseCaseSensitiveFileNames bool   `json:"useCaseSensitiveFileNames"`
	CurrentDirectory          string `json:"currentDirectory"`
}

// TypeInfo describes a resolved type.
type TypeInfo struct {
	Handle      string `json:"handle"`
	DisplayName string `json:"displayName"`
	Flags       int    `json:"flags"`
}

// SymbolInfo describes a resolved symbol.
type SymbolInfo struct {
	Handle string `json:"handle"`
	Name   string `json:"name"`
	Flags  int    `json:"flags"`
}

// MemberInfo describes a member of a symbol or type.
type MemberInfo struct {
	Handle string `json:"handle"`
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
