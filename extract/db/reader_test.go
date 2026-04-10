package db

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	db := NewDB()
	r := db.Relation("Node")
	err := r.AddTuple(db, int32(1), int32(1), "CallExpression", int32(10), int32(5), int32(10), int32(30))
	if err != nil {
		t.Fatalf("AddTuple: %v", err)
	}
	err = r.AddTuple(db, int32(2), int32(1), "Identifier", int32(10), int32(5), int32(10), int32(15))
	if err != nil {
		t.Fatalf("AddTuple: %v", err)
	}

	// Also add a File relation
	fr := db.Relation("File")
	err = fr.AddTuple(db, int32(1), "/src/main.ts", "sha256:abc")
	if err != nil {
		t.Fatalf("AddTuple File: %v", err)
	}

	// Write
	var buf bytes.Buffer
	if err := db.Encode(&buf); err != nil {
		t.Fatalf("Encode: %v", err)
	}

	// Read back
	data := buf.Bytes()
	reader := bytes.NewReader(data)
	db2, err := ReadDB(reader, int64(len(data)))
	if err != nil {
		t.Fatalf("ReadDB: %v", err)
	}

	// Verify Node relation
	node := db2.relations["Node"]
	if node == nil {
		t.Fatal("Node relation not found after round-trip")
	}
	if node.Tuples() != 2 {
		t.Fatalf("expected 2 tuples, got %d", node.Tuples())
	}

	// Check first tuple
	id, err := node.GetInt(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if id != 1 {
		t.Fatalf("expected id=1, got %d", id)
	}

	kind, err := node.GetString(db2, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if kind != "CallExpression" {
		t.Fatalf("expected kind=CallExpression, got %q", kind)
	}

	startLine, err := node.GetInt(0, 3)
	if err != nil {
		t.Fatal(err)
	}
	if startLine != 10 {
		t.Fatalf("expected startLine=10, got %d", startLine)
	}

	// Check second tuple
	kind2, err := node.GetString(db2, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if kind2 != "Identifier" {
		t.Fatalf("expected kind=Identifier, got %q", kind2)
	}

	// Verify File relation
	file := db2.relations["File"]
	if file == nil {
		t.Fatal("File relation not found after round-trip")
	}
	if file.Tuples() != 1 {
		t.Fatalf("expected 1 tuple, got %d", file.Tuples())
	}
	path, err := file.GetString(db2, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if path != "/src/main.ts" {
		t.Fatalf("expected path=/src/main.ts, got %q", path)
	}
}

func TestRoundTrip_MultipleRelations(t *testing.T) {
	db := NewDB()

	// Add Contains tuples
	c := db.Relation("Contains")
	if err := c.AddTuple(db, int32(1), int32(2)); err != nil {
		t.Fatal(err)
	}
	if err := c.AddTuple(db, int32(1), int32(3)); err != nil {
		t.Fatal(err)
	}

	// Add Symbol tuples
	s := db.Relation("Symbol")
	if err := s.AddTuple(db, int32(100), "myFunc", int32(5), int32(1)); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := db.Encode(&buf); err != nil {
		t.Fatal(err)
	}

	data := buf.Bytes()
	db2, err := ReadDB(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}

	// Verify Contains
	cr := db2.relations["Contains"]
	if cr == nil || cr.Tuples() != 2 {
		t.Fatalf("expected 2 Contains tuples, got %v", cr)
	}
	v, _ := cr.GetInt(1, 1)
	if v != 3 {
		t.Fatalf("expected child=3, got %d", v)
	}

	// Verify Symbol
	sr := db2.relations["Symbol"]
	if sr == nil || sr.Tuples() != 1 {
		t.Fatal("expected 1 Symbol tuple")
	}
	name, _ := sr.GetString(db2, 0, 1)
	if name != "myFunc" {
		t.Fatalf("expected myFunc, got %q", name)
	}
}

func TestReadDB_BadMagic(t *testing.T) {
	data := make([]byte, 16)
	copy(data[0:4], "NOPE")
	_, err := ReadDB(bytes.NewReader(data), 16)
	if err == nil {
		t.Fatal("expected error for bad magic")
	}
}

func TestReadDB_SchemaVersionMismatch(t *testing.T) {
	data := make([]byte, 16)
	copy(data[0:4], Magic)
	binary.LittleEndian.PutUint32(data[4:8], 99) // wrong version
	binary.LittleEndian.PutUint32(data[8:12], 0)
	binary.LittleEndian.PutUint32(data[12:16], 0)
	_, err := ReadDB(bytes.NewReader(data), 16)
	if err == nil {
		t.Fatal("expected error for schema version mismatch")
	}
}

func TestReadDB_EmptyDB(t *testing.T) {
	db := NewDB()
	var buf bytes.Buffer
	if err := db.Encode(&buf); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	db2, err := ReadDB(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("ReadDB empty: %v", err)
	}
	if len(db2.relations) != 0 {
		t.Fatalf("expected 0 relations, got %d", len(db2.relations))
	}
}

func TestReadDB_TruncatedHeader(t *testing.T) {
	data := []byte("TSQ")
	_, err := ReadDB(bytes.NewReader(data), int64(len(data)))
	if err == nil {
		t.Fatal("expected error for truncated header")
	}
}
