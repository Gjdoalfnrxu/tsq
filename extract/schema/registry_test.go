package schema

import (
	"testing"
)

func TestValidate_EmptyName(t *testing.T) {
	r := RelationDef{Name: "", Version: 1, Columns: []ColumnDef{{Name: "a", Type: TypeInt32}}}
	if err := r.Validate(); err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestValidate_NoColumns(t *testing.T) {
	r := RelationDef{Name: "Foo", Version: 1, Columns: nil}
	if err := r.Validate(); err == nil {
		t.Fatal("expected error for no columns")
	}
}

func TestValidate_ZeroVersion(t *testing.T) {
	r := RelationDef{Name: "Foo", Version: 0, Columns: []ColumnDef{{Name: "a", Type: TypeInt32}}}
	if err := r.Validate(); err == nil {
		t.Fatal("expected error for version 0")
	}
}

func TestValidate_EmptyColumnName(t *testing.T) {
	r := RelationDef{Name: "Foo", Version: 1, Columns: []ColumnDef{{Name: "", Type: TypeInt32}}}
	if err := r.Validate(); err == nil {
		t.Fatal("expected error for empty column name")
	}
}

func TestValidate_DuplicateColumnName(t *testing.T) {
	r := RelationDef{Name: "Foo", Version: 1, Columns: []ColumnDef{
		{Name: "a", Type: TypeInt32},
		{Name: "a", Type: TypeString},
	}}
	if err := r.Validate(); err == nil {
		t.Fatal("expected error for duplicate column name")
	}
}

func TestValidate_OK(t *testing.T) {
	r := RelationDef{Name: "Foo", Version: 1, Columns: []ColumnDef{
		{Name: "a", Type: TypeInt32},
		{Name: "b", Type: TypeString},
	}}
	if err := r.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestArity(t *testing.T) {
	r := RelationDef{Name: "X", Version: 1, Columns: []ColumnDef{
		{Name: "a", Type: TypeInt32},
		{Name: "b", Type: TypeInt32},
		{Name: "c", Type: TypeString},
	}}
	if r.Arity() != 3 {
		t.Fatalf("expected arity 3, got %d", r.Arity())
	}
}

func TestLookup_Found(t *testing.T) {
	// "Node" is registered in init()
	def, ok := Lookup("Node")
	if !ok {
		t.Fatal("expected Node to be in registry")
	}
	if def.Name != "Node" {
		t.Fatalf("expected name Node, got %q", def.Name)
	}
}

func TestLookup_NotFound(t *testing.T) {
	_, ok := Lookup("NoSuchRelation")
	if ok {
		t.Fatal("expected not found")
	}
}

func TestRegisterRelation_DuplicatePanics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on duplicate registration")
		}
	}()
	// "File" is already registered
	RegisterRelation(RelationDef{Name: "File", Version: 1, Columns: []ColumnDef{{Name: "x", Type: TypeInt32}}})
}
