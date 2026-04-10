package extract

import (
	"hash/fnv"
	"strconv"
)

// NodeID returns a deterministic 64-bit ID for an AST node, truncated to 32
// bits for storage as int32. Uses FNV-1a of (filePath, startLine, startCol,
// endLine, endCol, kind).
func NodeID(filePath string, startLine, startCol, endLine, endCol int, kind string) uint32 {
	h := fnv.New64a()
	h.Write([]byte(filePath))
	h.Write([]byte{0})
	h.Write([]byte(strconv.Itoa(startLine)))
	h.Write([]byte{0})
	h.Write([]byte(strconv.Itoa(startCol)))
	h.Write([]byte{0})
	h.Write([]byte(strconv.Itoa(endLine)))
	h.Write([]byte{0})
	h.Write([]byte(strconv.Itoa(endCol)))
	h.Write([]byte{0})
	h.Write([]byte(kind))
	return uint32(h.Sum64())
}

// SymID returns a deterministic 64-bit ID for a symbol, truncated to 32 bits.
// Uses FNV-1a of (filePath, name, startLine, startCol).
func SymID(filePath, name string, startLine, startCol int) uint32 {
	h := fnv.New64a()
	h.Write([]byte(filePath))
	h.Write([]byte{0})
	h.Write([]byte(name))
	h.Write([]byte{0})
	h.Write([]byte(strconv.Itoa(startLine)))
	h.Write([]byte{0})
	h.Write([]byte(strconv.Itoa(startCol)))
	return uint32(h.Sum64())
}

// ReturnSymID returns a deterministic 32-bit ID for the synthetic return symbol
// of a function. Uses FNV-1a of (filePath, "$return", fnStartLine, fnStartCol).
func ReturnSymID(filePath string, fnStartLine, fnStartCol int) uint32 {
	return SymID(filePath, "$return", fnStartLine, fnStartCol)
}

// FileID returns a deterministic 32-bit ID for a file path.
func FileID(filePath string) uint32 {
	h := fnv.New64a()
	h.Write([]byte("file:"))
	h.Write([]byte(filePath))
	return uint32(h.Sum64())
}
