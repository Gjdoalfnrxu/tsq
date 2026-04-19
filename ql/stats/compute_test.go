package stats

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/Gjdoalfnrxu/tsq/extract/db"
)

// End-to-end: populate an EDB with a known shape, run Compute, persist,
// reload, check that NDV/RowCount and at least one TopK match a hand
// computation. Plan §7.1 gate: "tsq stats inspect output matches a
// hand-computed gold for a 3-relation fixture."
func TestCompute_HandComputedFixture(t *testing.T) {
	database := db.NewDB()

	// File: 3 rows, all distinct ids. Triggers the schema's File rel.
	files := database.Relation("File")
	for i := int32(1); i <= 3; i++ {
		if err := files.AddTuple(database, i, "/tmp/f"+itoa(int(i))+".ts", "hash"+itoa(int(i))); err != nil {
			t.Fatalf("File row %d: %v", i, err)
		}
	}

	// Node: 100 rows, file column heavily skewed to file id 1 (90 rows).
	nodes := database.Relation("Node")
	for i := int32(1); i <= 90; i++ {
		nodes.AddTuple(database, i, int32(1), "Identifier", int32(1), int32(1), int32(1), int32(2))
	}
	for i := int32(91); i <= 100; i++ {
		nodes.AddTuple(database, i, int32(2), "Identifier", int32(1), int32(1), int32(1), int32(2))
	}

	// Encode to disk so we have an EDB to hash.
	dir := t.TempDir()
	edbPath := filepath.Join(dir, "fixture.db")
	f, _ := os.Create(edbPath)
	if err := database.Encode(f); err != nil {
		t.Fatal(err)
	}
	f.Close()

	hash, err := HashFile(edbPath)
	if err != nil {
		t.Fatal(err)
	}
	s, err := Compute(database, hash)
	if err != nil {
		t.Fatal(err)
	}

	fileStats := s.Lookup("File")
	if fileStats == nil {
		t.Fatal("File stats missing")
	}
	if fileStats.RowCount != 3 {
		t.Errorf("File RowCount = %d, want 3", fileStats.RowCount)
	}
	if fileStats.Cols[0].NDV < 3 {
		t.Errorf("File.id NDV = %d, want ≥3", fileStats.Cols[0].NDV)
	}

	nodeStats := s.Lookup("Node")
	if nodeStats == nil {
		t.Fatal("Node stats missing")
	}
	if nodeStats.RowCount != 100 {
		t.Errorf("Node RowCount = %d, want 100", nodeStats.RowCount)
	}
	// Node.file (col 1) should have a heavy hitter at value 1 with count 90.
	fileColTopK := nodeStats.Cols[1].TopK
	if len(fileColTopK) < 2 {
		t.Fatalf("Node.file TopK len=%d, want ≥2: %+v", len(fileColTopK), fileColTopK)
	}
	if fileColTopK[0].Value != 1 || fileColTopK[0].Count != 90 {
		t.Errorf("Node.file top-1 = (%d, %d), want (1, 90)", fileColTopK[0].Value, fileColTopK[0].Count)
	}

	// Persist + reload + hash-validate.
	if err := Save(edbPath, s); err != nil {
		t.Fatal(err)
	}
	var warn bytes.Buffer
	loaded, err := Load(edbPath, &warn)
	if err != nil {
		t.Fatalf("load: %v (warn=%s)", err, warn.String())
	}
	if loaded.Lookup("Node").Cols[1].TopK[0].Value != 1 {
		t.Fatal("round-trip lost TopK")
	}

	// Inspect smoke test: write to /dev/null equivalent.
	var inspectBuf bytes.Buffer
	Inspect(&inspectBuf, loaded, "Node")
	if inspectBuf.Len() == 0 {
		t.Fatal("Inspect produced empty output")
	}
}

// Empty schema: Compute on an empty DB should still produce a valid
// schema with all relations at row=0.
func TestCompute_EmptyDB(t *testing.T) {
	database := db.NewDB()
	dir := t.TempDir()
	edb := filepath.Join(dir, "empty.db")
	f, _ := os.Create(edb)
	database.Encode(f)
	f.Close()

	hash, _ := HashFile(edb)
	s, err := Compute(database, hash)
	if err != nil {
		t.Fatal(err)
	}
	for n, r := range s.Rels {
		if r.RowCount != 0 {
			t.Errorf("rel %s row=%d, want 0", n, r.RowCount)
		}
	}
}

// Default-stats fallback: when there's no sidecar, planner-side code
// will see nil. Lookup on nil schema must not panic.
func TestSchema_NilLookup(t *testing.T) {
	var s *Schema
	if got := s.Lookup("Anything"); got != nil {
		t.Fatalf("nil.Lookup should return nil, got %+v", got)
	}
}
