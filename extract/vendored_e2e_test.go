package extract

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Gjdoalfnrxu/tsq/extract/db"
)

// TestVendoredBackend_E2E_ExtractAndSerialize verifies the full end-to-end
// pipeline: extract with vendored backend, serialise, deserialise, and confirm
// the DB contains expected relations. This exercises the same code path as
// `tsq extract --backend vendored`.
func TestVendoredBackend_E2E_ExtractAndSerialize(t *testing.T) {
	projects := []struct {
		name     string
		dir      string
		wantRels []string // relations that must have at least one tuple
	}{
		{
			name: "simple",
			dir:  filepath.Join("..", "testdata", "projects", "simple"),
			wantRels: []string{
				"SchemaVersion", "File", "Node", "Contains",
				"Function", "Call",
			},
		},
		{
			name: "react-component",
			dir:  filepath.Join("..", "testdata", "projects", "react-component"),
			wantRels: []string{
				"SchemaVersion", "File", "Node", "Contains",
				"JsxElement",
			},
		},
		{
			name: "async-patterns",
			dir:  filepath.Join("..", "testdata", "projects", "async-patterns"),
			wantRels: []string{
				"SchemaVersion", "File", "Node", "Contains",
				"Function", "Await",
			},
		},
		{
			name: "imports",
			dir:  filepath.Join("..", "testdata", "projects", "imports"),
			wantRels: []string{
				"SchemaVersion", "File", "Node", "Contains",
				"ImportBinding",
			},
		},
		{
			name: "destructuring",
			dir:  filepath.Join("..", "testdata", "projects", "destructuring"),
			wantRels: []string{
				"SchemaVersion", "File", "Node", "Contains",
				"DestructureField",
			},
		},
	}

	for _, tc := range projects {
		t.Run(tc.name, func(t *testing.T) {
			absDir, err := filepath.Abs(tc.dir)
			if err != nil {
				t.Fatal(err)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			// Step 1: Extract with VendoredBackend
			database := db.NewDB()
			walker := NewFactWalker(database)
			backend := &VendoredBackend{}
			defer backend.Close()

			if err := walker.Run(ctx, backend, ProjectConfig{RootDir: absDir}); err != nil {
				t.Fatalf("extraction: %v", err)
			}

			// Step 2: Serialise to bytes (same as writing to tsq.db)
			var buf bytes.Buffer
			if err := database.Encode(&buf); err != nil {
				t.Fatalf("encode: %v", err)
			}

			// Step 3: Deserialise (same as `tsq query --db`)
			data := buf.Bytes()
			decoded, err := db.ReadDB(bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("decode: %v", err)
			}

			// Step 4: Verify expected relations have data
			for _, relName := range tc.wantRels {
				rel := decoded.Relation(relName)
				if rel.Tuples() == 0 {
					t.Errorf("relation %s: expected at least one tuple, got 0", relName)
				}
			}
		})
	}
}

// TestVendoredBackend_E2E_FileOutput verifies that the vendored backend can
// write a valid .db file to disk, mimicking `tsq extract --backend vendored --output`.
func TestVendoredBackend_E2E_FileOutput(t *testing.T) {
	absDir, err := filepath.Abs(filepath.Join("..", "testdata", "projects", "simple"))
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	database := db.NewDB()
	walker := NewFactWalker(database)
	backend := &VendoredBackend{}
	defer backend.Close()

	if err := walker.Run(ctx, backend, ProjectConfig{RootDir: absDir}); err != nil {
		t.Fatalf("extraction: %v", err)
	}

	// Write to temp file
	outPath := filepath.Join(t.TempDir(), "test.db")
	f, err := os.Create(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Encode(f); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()

	// Read back
	f2, err := os.Open(outPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f2.Close()

	fi, err := f2.Stat()
	if err != nil {
		t.Fatal(err)
	}

	decoded, err := db.ReadDB(f2, fi.Size())
	if err != nil {
		t.Fatalf("ReadDB from file: %v", err)
	}

	// Basic sanity: SchemaVersion and Node must exist
	if decoded.Relation("SchemaVersion").Tuples() != 1 {
		t.Error("expected exactly 1 SchemaVersion tuple")
	}
	if decoded.Relation("Node").Tuples() == 0 {
		t.Error("expected Node tuples")
	}
}

// TestVendoredBackend_E2E_DegradedMode verifies that the vendored backend
// produces valid output in degraded mode (no tsgo binary).
func TestVendoredBackend_E2E_DegradedMode(t *testing.T) {
	absDir, err := filepath.Abs(filepath.Join("..", "testdata", "projects", "simple"))
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	database := db.NewDB()
	walker := NewFactWalker(database)
	backend := &VendoredBackend{}
	defer backend.Close()

	if err := walker.Run(ctx, backend, ProjectConfig{RootDir: absDir}); err != nil {
		t.Fatalf("extraction: %v", err)
	}

	// In degraded mode, tsgo should not be available
	if backend.TsgoAvailable() {
		t.Skip("tsgo is available; skipping degraded mode test")
	}

	// But the DB should still contain all structural facts
	rels := []string{"SchemaVersion", "File", "Node", "Contains", "Function", "Call"}
	for _, name := range rels {
		rel := database.Relation(name)
		if rel.Tuples() == 0 {
			t.Errorf("degraded mode: relation %s has 0 tuples, expected >0", name)
		}
	}
}
