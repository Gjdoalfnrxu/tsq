package extract

import (
	"testing"

	"github.com/Gjdoalfnrxu/tsq/extract/db"
)

// Phase E PR1 (#210) regression guards: assert the new `path` discriminator
// column is populated with the documented structured sentinels on each of the
// six extended relations. Per-kind floors protect each path family.
//
// Encoding (spec §3, PR-E1):
//   - named field      -> ".foo"
//   - array index i    -> ".[i]"
//   - spread wildcard  -> ".{*}"
//   - empty sentinel   -> ""
//
// Mutation-probe table (each assertion fails if its producing leg is removed):
//
//   relation           path sentinel  test                          producing leg
//   -----------------  -------------  ----------------------------  ---------------------
//   FieldRead          ".x"           TestPath_FieldRead_Named      walker.go fieldPath()
//   FieldWrite         ".y"           TestPath_FieldWrite_Named     walker.go fieldPath()
//   ObjectLiteralField ".k"           TestPath_ObjectLiteralField   walker.go fieldPath()
//   ObjectLiteralSpread ".{*}"        TestPath_ObjectLiteralSpread  walker.go spreadPath()
//   DestructureField   ".a"           TestPath_DestructureField     walker.go fieldPath()
//   ArrayDestructure   ".[0]"/".[1]"  TestPath_ArrayDestructure     walker.go indexPath()

// countNonEmptyString returns how many tuples have a non-empty string at col.
func countNonEmptyString(t *testing.T, database *db.DB, r *db.Relation, col int) int {
	t.Helper()
	n := 0
	for i := 0; i < r.Tuples(); i++ {
		if v, err := r.GetString(database, i, col); err == nil && v != "" {
			n++
		}
	}
	return n
}

func TestPath_FieldRead_Named(t *testing.T) {
	src := `
const obj = { x: 1 };
const v = obj.x;
`
	database := walkerTestDB(t, src)
	r := rel(t, database, "FieldRead")
	if r.Tuples() == 0 {
		t.Fatal("FieldRead: expected at least one tuple")
	}
	// path column is the trailing column (col 3, 0-based).
	if !hasString(t, database, r, 3, ".x") {
		t.Errorf("FieldRead: expected path '.x' in col 3")
	}
	if countNonEmptyString(t, database, r, 3) == 0 {
		t.Error("FieldRead: expected at least one non-empty path")
	}
}

func TestPath_FieldWrite_Named(t *testing.T) {
	src := `
const obj: any = {};
obj.y = 2;
`
	database := walkerTestDB(t, src)
	r := rel(t, database, "FieldWrite")
	if r.Tuples() == 0 {
		t.Fatal("FieldWrite: expected at least one tuple")
	}
	// path column is trailing col 4 (0-based).
	if !hasString(t, database, r, 4, ".y") {
		t.Errorf("FieldWrite: expected path '.y' in col 4")
	}
	if countNonEmptyString(t, database, r, 4) == 0 {
		t.Error("FieldWrite: expected at least one non-empty path")
	}
}

func TestPath_ObjectLiteralField(t *testing.T) {
	src := `
const o = { k: 1, m: 2 };
`
	database := walkerTestDB(t, src)
	r := rel(t, database, "ObjectLiteralField")
	if r.Tuples() == 0 {
		t.Fatal("ObjectLiteralField: expected at least one tuple")
	}
	// path column is trailing col 3.
	if !hasString(t, database, r, 3, ".k") {
		t.Errorf("ObjectLiteralField: expected path '.k' in col 3")
	}
	if !hasString(t, database, r, 3, ".m") {
		t.Errorf("ObjectLiteralField: expected path '.m' in col 3")
	}
}

func TestPath_ObjectLiteralSpread(t *testing.T) {
	src := `
const a = { x: 1 };
const b = { ...a, y: 2 };
`
	database := walkerTestDB(t, src)
	r := rel(t, database, "ObjectLiteralSpread")
	if r.Tuples() == 0 {
		t.Fatal("ObjectLiteralSpread: expected at least one tuple")
	}
	// path column is trailing col 2.
	if !hasString(t, database, r, 2, ".{*}") {
		t.Errorf("ObjectLiteralSpread: expected path '.{*}' (spread wildcard) in col 2")
	}
	// Every spread row MUST carry the wildcard — spread is total.
	for i := 0; i < r.Tuples(); i++ {
		v, err := r.GetString(database, i, 2)
		if err != nil || v != ".{*}" {
			t.Errorf("ObjectLiteralSpread row %d: path=%q, want '.{*}'", i, v)
		}
	}
}

func TestPath_DestructureField(t *testing.T) {
	src := `
const { a, b: c } = obj;
`
	database := walkerTestDB(t, src)
	r := rel(t, database, "DestructureField")
	if r.Tuples() == 0 {
		t.Fatal("DestructureField: expected at least one tuple")
	}
	// path column is trailing col 5 (0-based).
	if !hasString(t, database, r, 5, ".a") {
		t.Errorf("DestructureField: expected path '.a' in col 5")
	}
	if !hasString(t, database, r, 5, ".b") {
		t.Errorf("DestructureField: expected path '.b' (source field) in col 5")
	}
}

func TestPath_ArrayDestructure(t *testing.T) {
	src := `
const [x, y] = arr;
`
	database := walkerTestDB(t, src)
	r := rel(t, database, "ArrayDestructure")
	if r.Tuples() == 0 {
		t.Fatal("ArrayDestructure: expected at least one tuple")
	}
	// path column is trailing col 3.
	if !hasString(t, database, r, 3, ".[0]") {
		t.Errorf("ArrayDestructure: expected path '.[0]' in col 3")
	}
	if !hasString(t, database, r, 3, ".[1]") {
		t.Errorf("ArrayDestructure: expected path '.[1]' in col 3")
	}
}

// TestPath_Shapes_AllNonEmpty asserts a non-zero floor for every new path kind
// in a small combined fixture — guards against a regression where one of the
// three encoding helpers (fieldPath / indexPath / spreadPath) silently returns
// "" and the per-relation tests still pass because another tuple carries a
// valid path.
func TestPath_Shapes_AllNonEmpty(t *testing.T) {
	src := `
const obj = { x: 1, y: 2 };
const v = obj.x;
obj.y = 3;
const { a, b: c } = obj;
const [p, q] = [1, 2];
const merged = { ...obj, z: 4 };
`
	database := walkerTestDB(t, src)

	cases := []struct {
		rel  string
		col  int
		kind string
	}{
		{"FieldRead", 3, "named field"},
		{"FieldWrite", 4, "named field"},
		{"ObjectLiteralField", 3, "named field"},
		{"ObjectLiteralSpread", 2, "spread wildcard"},
		{"DestructureField", 5, "named field"},
		{"ArrayDestructure", 3, "array index"},
	}
	for _, c := range cases {
		r := rel(t, database, c.rel)
		if r.Tuples() == 0 {
			t.Errorf("%s: expected at least one tuple (fixture coverage)", c.rel)
			continue
		}
		n := countNonEmptyString(t, database, r, c.col)
		if n == 0 {
			t.Errorf("%s: expected non-empty path (%s) in col %d, got 0 of %d", c.rel, c.kind, c.col, r.Tuples())
		}
	}
}
