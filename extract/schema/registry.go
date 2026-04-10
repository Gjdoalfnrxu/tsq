// Package schema defines the tsq fact relation registry and column types.
package schema

import "fmt"

// ColumnType identifies the type of a relation column.
type ColumnType uint8

const (
	TypeInt32     ColumnType = iota // signed 32-bit integer
	TypeEntityRef                   // unsigned 32-bit entity ID (alias for Int32 in storage)
	TypeString                      // interned string (stored as uint32 index into string table)
)

// ColumnDef describes one column of a relation.
type ColumnDef struct {
	Name string
	Type ColumnType
}

// RelationDef describes a fact relation in the schema.
type RelationDef struct {
	Name    string
	Columns []ColumnDef
	Version int // schema version when this relation was introduced
}

// Arity returns the number of columns in the relation.
func (r RelationDef) Arity() int { return len(r.Columns) }

// Validate returns an error if the RelationDef is malformed.
func (r RelationDef) Validate() error {
	if r.Name == "" {
		return fmt.Errorf("relation name must not be empty")
	}
	if len(r.Columns) == 0 {
		return fmt.Errorf("relation %q must have at least one column", r.Name)
	}
	if r.Version < 1 {
		return fmt.Errorf("relation %q version must be >= 1", r.Name)
	}
	seen := make(map[string]bool, len(r.Columns))
	for i, col := range r.Columns {
		if col.Name == "" {
			return fmt.Errorf("relation %q column %d has empty name", r.Name, i)
		}
		if seen[col.Name] {
			return fmt.Errorf("relation %q has duplicate column name %q", r.Name, col.Name)
		}
		seen[col.Name] = true
	}
	return nil
}

// Registry is the global relation registry. All relations must be registered here.
var Registry []RelationDef

// RegisterRelation adds a relation to the global registry. Panics on duplicates.
// Called from relations.go init().
func RegisterRelation(def RelationDef) {
	for _, existing := range Registry {
		if existing.Name == def.Name {
			panic(fmt.Sprintf("duplicate relation registration: %q", def.Name))
		}
	}
	Registry = append(Registry, def)
}

// Lookup returns the RelationDef for a given name, or false if not found.
func Lookup(name string) (RelationDef, bool) {
	for _, r := range Registry {
		if r.Name == name {
			return r, true
		}
	}
	return RelationDef{}, false
}
