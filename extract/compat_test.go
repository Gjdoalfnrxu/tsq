package extract

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Gjdoalfnrxu/tsq/extract/db"
	"github.com/Gjdoalfnrxu/tsq/extract/schema"
)

// TestBackendCompatibility extracts the same TypeScript projects with both
// TreeSitterBackend and VendoredBackend and verifies that they produce
// identical fact databases. Since VendoredBackend delegates AST walking to
// TreeSitterBackend, structural parity should be automatic.
func TestBackendCompatibility(t *testing.T) {
	projects := []string{
		filepath.Join("..", "testdata", "projects", "simple"),
		filepath.Join("..", "testdata", "projects", "react-component"),
		filepath.Join("..", "testdata", "projects", "async-patterns"),
		filepath.Join("..", "testdata", "projects", "destructuring"),
		filepath.Join("..", "testdata", "projects", "imports"),
	}

	for _, dir := range projects {
		absDir, err := filepath.Abs(dir)
		if err != nil {
			t.Fatalf("abs path: %v", err)
		}
		name := filepath.Base(absDir)
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			// Extract with TreeSitterBackend
			tsDB := db.NewDB()
			tsWalker := NewFactWalker(tsDB)
			tsBackend := &TreeSitterBackend{}
			if err := tsWalker.Run(ctx, tsBackend, ProjectConfig{RootDir: absDir}); err != nil {
				t.Fatalf("TreeSitter extraction: %v", err)
			}
			tsBackend.Close()

			// Extract with VendoredBackend
			vDB := db.NewDB()
			vWalker := NewFactWalker(vDB)
			vBackend := &VendoredBackend{}
			if err := vWalker.Run(ctx, vBackend, ProjectConfig{RootDir: absDir}); err != nil {
				t.Fatalf("Vendored extraction: %v", err)
			}
			vBackend.Close()

			// Compare all relations
			compareDBs(t, tsDB, vDB)
		})
	}
}

// TestBackendCompatibility_Roundtrip verifies parity is preserved through
// encode/decode roundtrip.
func TestBackendCompatibility_Roundtrip(t *testing.T) {
	dir, err := filepath.Abs(filepath.Join("..", "testdata", "projects", "simple"))
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Extract with VendoredBackend
	vDB := db.NewDB()
	vWalker := NewFactWalker(vDB)
	vBackend := &VendoredBackend{}
	if err := vWalker.Run(ctx, vBackend, ProjectConfig{RootDir: dir}); err != nil {
		t.Fatalf("Vendored extraction: %v", err)
	}
	vBackend.Close()

	// Encode and decode
	var buf bytes.Buffer
	if err := vDB.Encode(&buf); err != nil {
		t.Fatalf("encode: %v", err)
	}
	data := buf.Bytes()
	decoded, err := db.ReadDB(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Extract with TreeSitterBackend and compare against decoded vendored DB
	tsDB := db.NewDB()
	tsWalker := NewFactWalker(tsDB)
	tsBackend := &TreeSitterBackend{}
	if err := tsWalker.Run(ctx, tsBackend, ProjectConfig{RootDir: dir}); err != nil {
		t.Fatalf("TreeSitter extraction: %v", err)
	}
	tsBackend.Close()

	compareDBs(t, tsDB, decoded)
}

// TestBackendCompatibility_EmptyProject verifies both backends produce the
// same result for an empty project directory.
func TestBackendCompatibility_EmptyProject(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tsDB := db.NewDB()
	tsWalker := NewFactWalker(tsDB)
	tsBackend := &TreeSitterBackend{}
	if err := tsWalker.Run(ctx, tsBackend, ProjectConfig{RootDir: dir}); err != nil {
		t.Fatalf("TreeSitter: %v", err)
	}
	tsBackend.Close()

	vDB := db.NewDB()
	vWalker := NewFactWalker(vDB)
	vBackend := &VendoredBackend{}
	if err := vWalker.Run(ctx, vBackend, ProjectConfig{RootDir: dir}); err != nil {
		t.Fatalf("Vendored: %v", err)
	}
	vBackend.Close()

	compareDBs(t, tsDB, vDB)
}

// TestBackendCompatibility_SingleFile verifies parity on a single-file project
// to make debugging easier if parity breaks.
func TestBackendCompatibility_SingleFile(t *testing.T) {
	// Create a temp dir with a single TS file
	dir := t.TempDir()
	src := `
function greet(name: string): string {
  return "hello " + name;
}

const add = (a: number, b: number) => a + b;

greet("world");
add(1, 2);
`
	if err := os.WriteFile(filepath.Join(dir, "test.ts"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tsDB := db.NewDB()
	tsWalker := NewFactWalker(tsDB)
	tsBackend := &TreeSitterBackend{}
	if err := tsWalker.Run(ctx, tsBackend, ProjectConfig{RootDir: dir}); err != nil {
		t.Fatalf("TreeSitter: %v", err)
	}
	tsBackend.Close()

	vDB := db.NewDB()
	vWalker := NewFactWalker(vDB)
	vBackend := &VendoredBackend{}
	if err := vWalker.Run(ctx, vBackend, ProjectConfig{RootDir: dir}); err != nil {
		t.Fatalf("Vendored: %v", err)
	}
	vBackend.Close()

	compareDBs(t, tsDB, vDB)
}

// compareDBs compares all relations in two DBs tuple-by-tuple.
// It serialises each relation's tuples to sorted string representations
// and asserts equality.
func compareDBs(t *testing.T, expected, actual *db.DB) {
	t.Helper()

	for _, def := range schema.Registry {
		name := def.Name
		expRel := expected.Relation(name)
		actRel := actual.Relation(name)

		expTuples := serializeTuples(t, expected, expRel, def)
		actTuples := serializeTuples(t, actual, actRel, def)

		sort.Strings(expTuples)
		sort.Strings(actTuples)

		expJoined := strings.Join(expTuples, "\n")
		actJoined := strings.Join(actTuples, "\n")

		if expJoined != actJoined {
			t.Errorf("relation %s: mismatch\n  TreeSitter (%d tuples):\n    %s\n  Vendored (%d tuples):\n    %s",
				name,
				len(expTuples), indentLines(expTuples),
				len(actTuples), indentLines(actTuples))
		}
	}
}

// serializeTuples converts all tuples in a relation to sorted string slices
// for comparison. Each tuple is rendered as "col0|col1|col2|...".
func serializeTuples(t *testing.T, database *db.DB, rel *db.Relation, def schema.RelationDef) []string {
	t.Helper()
	n := rel.Tuples()
	var result []string
	for i := 0; i < n; i++ {
		var parts []string
		for j, colDef := range def.Columns {
			switch colDef.Type {
			case schema.TypeInt32, schema.TypeEntityRef:
				v, err := rel.GetInt(i, j)
				if err != nil {
					t.Fatalf("GetInt(%d, %d) for %s: %v", i, j, def.Name, err)
				}
				parts = append(parts, intToStr(v))
			case schema.TypeString:
				v, err := rel.GetString(database, i, j)
				if err != nil {
					t.Fatalf("GetString(%d, %d) for %s: %v", i, j, def.Name, err)
				}
				parts = append(parts, v)
			}
		}
		result = append(result, strings.Join(parts, "|"))
	}
	return result
}

func intToStr(v int32) string {
	return strconv.FormatInt(int64(v), 10)
}

func indentLines(lines []string) string {
	if len(lines) == 0 {
		return "(empty)"
	}
	if len(lines) > 20 {
		shown := append(lines[:10], "... ("+intToStr(int32(len(lines)-20))+" more)", "")
		shown = append(shown, lines[len(lines)-10:]...)
		lines = shown
	}
	return strings.Join(lines, "\n    ")
}
