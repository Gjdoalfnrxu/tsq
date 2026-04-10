package db

import (
	"bytes"
	"testing"
)

func TestNewDB(t *testing.T) {
	db := NewDB()
	if db == nil {
		t.Fatal("NewDB returned nil")
	}
	// Should have empty string at index 0
	if len(db.strings) != 1 || db.strings[0] != "" {
		t.Fatalf("expected strings=[\"\"]; got %v", db.strings)
	}
}

func TestRelation_UnknownPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for unknown relation")
		}
	}()
	db := NewDB()
	db.Relation("TotallyFakeRelation")
}

func TestAddTuple_OK(t *testing.T) {
	db := NewDB()
	r := db.Relation("Contains")
	err := r.AddTuple(db, int32(1), int32(2))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Tuples() != 1 {
		t.Fatalf("expected 1 tuple, got %d", r.Tuples())
	}
}

func TestAddTuple_WrongArity(t *testing.T) {
	db := NewDB()
	r := db.Relation("Contains") // 2 columns
	err := r.AddTuple(db, int32(1))
	if err == nil {
		t.Fatal("expected error for wrong arity")
	}
}

func TestAddTuple_WrongType(t *testing.T) {
	db := NewDB()
	r := db.Relation("File") // id=EntityRef, path=String, contentHash=String
	err := r.AddTuple(db, int32(1), 42, "abc")
	if err == nil {
		t.Fatal("expected error for wrong type (int instead of string)")
	}
}

func TestAddTuple_WrongTypeInt(t *testing.T) {
	db := NewDB()
	r := db.Relation("Contains") // parent=EntityRef, child=EntityRef
	err := r.AddTuple(db, "notAnInt", int32(2))
	if err == nil {
		t.Fatal("expected error for string in int column")
	}
}

func TestEncode_Empty(t *testing.T) {
	db := NewDB()
	var buf bytes.Buffer
	err := db.Encode(&buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should have valid header at minimum
	data := buf.Bytes()
	if len(data) < 16 {
		t.Fatalf("output too short: %d bytes", len(data))
	}
	if string(data[0:4]) != Magic {
		t.Fatalf("bad magic: %q", data[0:4])
	}
}

func TestGetInt(t *testing.T) {
	db := NewDB()
	r := db.Relation("Contains")
	if err := r.AddTuple(db, int32(10), int32(20)); err != nil {
		t.Fatal(err)
	}
	v, err := r.GetInt(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if v != 10 {
		t.Fatalf("expected 10, got %d", v)
	}
	v, err = r.GetInt(0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if v != 20 {
		t.Fatalf("expected 20, got %d", v)
	}
}

func TestGetString(t *testing.T) {
	db := NewDB()
	r := db.Relation("File")
	if err := r.AddTuple(db, int32(1), "/src/main.ts", "abc123"); err != nil {
		t.Fatal(err)
	}
	s, err := r.GetString(db, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if s != "/src/main.ts" {
		t.Fatalf("expected /src/main.ts, got %q", s)
	}
}

func TestGetInt_OutOfRange(t *testing.T) {
	db := NewDB()
	r := db.Relation("Contains")
	if err := r.AddTuple(db, int32(1), int32(2)); err != nil {
		t.Fatal(err)
	}
	if _, err := r.GetInt(5, 0); err == nil {
		t.Fatal("expected error for out-of-range tuple")
	}
	if _, err := r.GetInt(0, 5); err == nil {
		t.Fatal("expected error for out-of-range col")
	}
}
