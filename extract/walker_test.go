package extract

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Gjdoalfnrxu/tsq/extract/db"
	_ "github.com/Gjdoalfnrxu/tsq/extract/schema" // ensure relations are registered
)

// ---- test helpers ----

// walkerTestDB runs the FactWalker over a single inline TypeScript source string
// and returns the resulting DB. The source is written to a temp file.
func walkerTestDB(t *testing.T, src string) *db.DB {
	t.Helper()
	dir := t.TempDir()
	f := filepath.Join(dir, "test.ts")
	if err := os.WriteFile(f, []byte(src), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return walkerTestDBDir(t, dir)
}

// walkerTestDBDir runs the FactWalker over all TS files in dir.
func walkerTestDBDir(t *testing.T, dir string) *db.DB {
	t.Helper()
	database := db.NewDB()
	walker := NewFactWalker(database)
	backend := &TreeSitterBackend{}
	ctx := context.Background()
	cfg := ProjectConfig{RootDir: dir}
	if err := walker.Run(ctx, backend, cfg); err != nil {
		t.Fatalf("Run: %v", err)
	}
	backend.Close()
	return database
}

// rel returns the named relation from the DB.
func rel(t *testing.T, database *db.DB, name string) *db.Relation {
	t.Helper()
	r := database.Relation(name)
	if r == nil {
		t.Fatalf("relation %q not found", name)
	}
	return r
}

// tupleCount returns the number of tuples in the named relation.
func tupleCount(t *testing.T, database *db.DB, name string) int {
	t.Helper()
	return rel(t, database, name).Tuples()
}

// getInt gets an int32 column value at (tuple, col).
func getInt(t *testing.T, r *db.Relation, tuple, col int) int32 {
	t.Helper()
	v, err := r.GetInt(tuple, col)
	if err != nil {
		t.Fatalf("GetInt(%d,%d): %v", tuple, col, err)
	}
	return v
}

// hasString returns true if any tuple in the relation has s in the given column.
func hasString(t *testing.T, database *db.DB, r *db.Relation, col int, s string) bool {
	t.Helper()
	for i := 0; i < r.Tuples(); i++ {
		if v, err := r.GetString(database, i, col); err == nil && v == s {
			return true
		}
	}
	return false
}

// hasIntInCol returns true if any tuple in the relation has v in the given int column.
func hasIntInCol(r *db.Relation, col int, v int32) bool {
	for i := 0; i < r.Tuples(); i++ {
		if got, err := r.GetInt(i, col); err == nil && got == v {
			return true
		}
	}
	return false
}

// encodeDecode round-trips the DB through Encode/Decode to verify serialisation.
func encodeDecode(t *testing.T, database *db.DB) *db.DB {
	t.Helper()
	var buf bytes.Buffer
	if err := database.Encode(&buf); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	data := buf.Bytes()
	decoded, err := db.ReadDB(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("ReadDB: %v", err)
	}
	return decoded
}

// ---- tests ----

// TestEmitSchemaVersion verifies that exactly one SchemaVersion tuple is emitted.
func TestEmitSchemaVersion(t *testing.T) {
	database := walkerTestDB(t, `const x = 1;`)
	r := rel(t, database, "SchemaVersion")
	if r.Tuples() != 1 {
		t.Errorf("SchemaVersion: expected 1 tuple, got %d", r.Tuples())
	}
	v := getInt(t, r, 0, 0)
	if v != int32(db.SchemaVersion) {
		t.Errorf("SchemaVersion: expected version=%d, got %d", db.SchemaVersion, v)
	}
}

// TestEmitFile verifies File tuples: id, path, contentHash.
func TestEmitFile(t *testing.T) {
	src := `const x = 42;`
	database := walkerTestDB(t, src)
	r := rel(t, database, "File")
	if r.Tuples() == 0 {
		t.Fatal("File: expected at least one tuple")
	}
	// Path column (col 1) should contain the file path (ends with test.ts)
	found := false
	for i := 0; i < r.Tuples(); i++ {
		path, _ := r.GetString(database, i, 1)
		if len(path) > 0 {
			found = true
		}
	}
	if !found {
		t.Error("File: no non-empty path found")
	}
	// Content hash column (col 2) should be a 64-char hex string
	for i := 0; i < r.Tuples(); i++ {
		hash, _ := r.GetString(database, i, 2)
		if len(hash) != 64 {
			t.Errorf("File tuple %d: expected 64-char hash, got %q (len=%d)", i, hash, len(hash))
		}
	}
}

// TestEmitNode verifies Node tuples are emitted with correct columns.
func TestEmitNode(t *testing.T) {
	src := `function foo() {}`
	database := walkerTestDB(t, src)
	r := rel(t, database, "Node")
	if r.Tuples() == 0 {
		t.Fatal("Node: expected tuples, got none")
	}
	// At least one node should have kind="FunctionDeclaration"
	if !hasString(t, database, r, 2, "FunctionDeclaration") {
		t.Error("Node: expected FunctionDeclaration kind")
	}
}

// TestEmitContains verifies parent/child relationships.
func TestEmitContains(t *testing.T) {
	src := `function foo() { return 1; }`
	database := walkerTestDB(t, src)
	r := rel(t, database, "Contains")
	if r.Tuples() == 0 {
		t.Fatal("Contains: expected tuples, got none")
	}
	// Parent IDs should not equal child IDs
	for i := 0; i < r.Tuples(); i++ {
		parent := getInt(t, r, i, 0)
		child := getInt(t, r, i, 1)
		if parent == child {
			t.Errorf("Contains tuple %d: parent==child==%d", i, parent)
		}
	}
}

// TestEmitFunction verifies Function tuples for various function kinds.
func TestEmitFunction(t *testing.T) {
	src := `
function namedFn(x: number) {}
const arrow = async (y: string) => y;
class C {
  method() {}
}
`
	database := walkerTestDB(t, src)
	r := rel(t, database, "Function")

	if r.Tuples() == 0 {
		t.Fatal("Function: expected tuples")
	}

	// "namedFn" should appear in name column (col 1)
	if !hasString(t, database, r, 1, "namedFn") {
		t.Error("Function: expected namedFn")
	}
	// Arrow function: isArrow=1 in col 2
	if !hasIntInCol(r, 2, 1) {
		t.Error("Function: expected isArrow=1")
	}
	// Async: isAsync=1 in col 3
	if !hasIntInCol(r, 3, 1) {
		t.Error("Function: expected isAsync=1")
	}
}

// TestEmitParameter verifies Parameter tuples.
func TestEmitParameter(t *testing.T) {
	src := `function f(a: string, b: number, ...rest: any[]) {}`
	database := walkerTestDB(t, src)
	rp := rel(t, database, "Parameter")
	if rp.Tuples() == 0 {
		t.Fatal("Parameter: expected tuples")
	}
	// Check that "a" appears in name column (col 2)
	if !hasString(t, database, rp, 2, "a") {
		t.Error("Parameter: expected 'a' in name column")
	}
	// ParameterRest should have a tuple
	rr := rel(t, database, "ParameterRest")
	if rr.Tuples() == 0 {
		t.Error("ParameterRest: expected at least one tuple for ...rest")
	}
}

// TestEmitCall verifies Call, CallArg, ExprIsCall tuples.
func TestEmitCall(t *testing.T) {
	src := `
function foo(a, b) {}
foo(1, 2);
`
	database := walkerTestDB(t, src)
	rc := rel(t, database, "Call")
	if rc.Tuples() == 0 {
		t.Fatal("Call: expected tuples")
	}
	// Arity should be 2 for foo(1,2)
	if !hasIntInCol(rc, 2, 2) {
		t.Error("Call: expected arity=2")
	}
	// CallArg
	ra := rel(t, database, "CallArg")
	if ra.Tuples() == 0 {
		t.Fatal("CallArg: expected tuples")
	}
	// ExprIsCall
	ri := rel(t, database, "ExprIsCall")
	if ri.Tuples() == 0 {
		t.Fatal("ExprIsCall: expected tuples")
	}
}

// TestEmitCallArgSpread verifies spread arguments.
func TestEmitCallArgSpread(t *testing.T) {
	src := `function f(...args) {} f(...[1,2,3]);`
	database := walkerTestDB(t, src)
	r := rel(t, database, "CallArgSpread")
	if r.Tuples() == 0 {
		t.Error("CallArgSpread: expected at least one tuple for spread arg")
	}
}

// TestEmitVarDecl verifies VarDecl tuples and isConst.
func TestEmitVarDecl(t *testing.T) {
	src := `
const x = 42;
let y = "hello";
var z = true;
`
	database := walkerTestDB(t, src)
	r := rel(t, database, "VarDecl")
	if r.Tuples() < 3 {
		t.Fatalf("VarDecl: expected at least 3 tuples, got %d", r.Tuples())
	}
	// isConst column (col 3): at least one tuple should have isConst=1
	if !hasIntInCol(r, 3, 1) {
		t.Error("VarDecl: expected isConst=1 for const declaration")
	}
	// At least one with isConst=0
	if !hasIntInCol(r, 3, 0) {
		t.Error("VarDecl: expected isConst=0 for let/var declaration")
	}
}

// TestEmitAssign verifies Assign tuples.
func TestEmitAssign(t *testing.T) {
	src := `
let x = 1;
x = 2;
`
	database := walkerTestDB(t, src)
	r := rel(t, database, "Assign")
	if r.Tuples() == 0 {
		t.Fatal("Assign: expected tuples")
	}
}

// TestEmitExprMayRef verifies ExprMayRef for in-scope identifiers.
func TestEmitExprMayRef(t *testing.T) {
	src := `
const foo = 1;
const bar = foo + 1;
`
	database := walkerTestDB(t, src)
	r := rel(t, database, "ExprMayRef")
	if r.Tuples() == 0 {
		t.Fatal("ExprMayRef: expected tuples for resolved identifier 'foo'")
	}
}

// TestEmitFieldReadWrite verifies FieldRead and FieldWrite tuples.
func TestEmitFieldReadWrite(t *testing.T) {
	src := `
const obj = { x: 1 };
const v = obj.x;
obj.y = 2;
`
	database := walkerTestDB(t, src)
	rfr := rel(t, database, "FieldRead")
	if rfr.Tuples() == 0 {
		t.Fatal("FieldRead: expected tuples")
	}
	// fieldName column (col 2) should contain "x"
	if !hasString(t, database, rfr, 2, "x") {
		t.Error("FieldRead: expected fieldName='x'")
	}
	rfw := rel(t, database, "FieldWrite")
	if rfw.Tuples() == 0 {
		t.Fatal("FieldWrite: expected tuples")
	}
	if !hasString(t, database, rfw, 2, "y") {
		t.Error("FieldWrite: expected fieldName='y'")
	}
}

// TestEmitAwaitCast verifies Await and Cast tuples.
func TestEmitAwaitCast(t *testing.T) {
	src := `
async function f() {
  const x = await fetch("url");
  const y = x as string;
}
`
	database := walkerTestDB(t, src)
	ra := rel(t, database, "Await")
	if ra.Tuples() == 0 {
		t.Fatal("Await: expected tuples")
	}
	rc := rel(t, database, "Cast")
	if rc.Tuples() == 0 {
		t.Fatal("Cast: expected tuples for 'as' expression")
	}
}

// TestEmitDestructure verifies destructuring relations.
func TestEmitDestructure(t *testing.T) {
	src := `
const { a, b: c } = obj;
const [x, y] = arr;
const { ...rest } = other;
`
	database := walkerTestDB(t, src)

	rdf := rel(t, database, "DestructureField")
	if rdf.Tuples() == 0 {
		t.Fatal("DestructureField: expected tuples")
	}
	// "a" should appear as both sourceField (col 1) and bindName (col 2)
	if !hasString(t, database, rdf, 1, "a") {
		t.Error("DestructureField: expected sourceField='a'")
	}

	rad := rel(t, database, "ArrayDestructure")
	if rad.Tuples() == 0 {
		t.Fatal("ArrayDestructure: expected tuples")
	}

	rdr := rel(t, database, "DestructureRest")
	if rdr.Tuples() == 0 {
		t.Fatal("DestructureRest: expected tuples for ...rest")
	}
}

// TestEmitImportExport verifies ImportBinding and ExportBinding.
func TestEmitImportExport(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	importsFile := filepath.Join(filepath.Dir(thisFile), "..", "testdata", "ts", "imports.ts")
	dir := filepath.Dir(importsFile)

	database := walkerTestDBDir(t, dir)

	ri := rel(t, database, "ImportBinding")
	if ri.Tuples() == 0 {
		t.Fatal("ImportBinding: expected tuples")
	}
	// moduleSpec column (col 1) should contain "./module"
	if !hasString(t, database, ri, 1, "./module") {
		t.Error("ImportBinding: expected moduleSpec './module'")
	}

	re := rel(t, database, "ExportBinding")
	if re.Tuples() == 0 {
		t.Fatal("ExportBinding: expected tuples")
	}
	// exportedName column (col 0) should contain "exportedFn"
	if !hasString(t, database, re, 0, "exportedFn") {
		t.Error("ExportBinding: expected exportedName='exportedFn'")
	}
}

// TestEmitJsx verifies JsxElement and JsxAttribute tuples.
func TestEmitJsx(t *testing.T) {
	dir := t.TempDir()
	src := `
import React from "react";
function App() {
  return <div className="container"><span id="s" /></div>;
}
`
	f := filepath.Join(dir, "app.tsx")
	if err := os.WriteFile(f, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	database := walkerTestDBDir(t, dir)

	rj := rel(t, database, "JsxElement")
	if rj.Tuples() == 0 {
		t.Fatal("JsxElement: expected tuples")
	}
	rja := rel(t, database, "JsxAttribute")
	if rja.Tuples() == 0 {
		t.Fatal("JsxAttribute: expected tuples")
	}
	// Attribute name "className" should appear
	if !hasString(t, database, rja, 1, "className") {
		t.Error("JsxAttribute: expected name='className'")
	}
}

// TestEmitExtractError verifies that parse errors emit ExtractError tuples.
func TestEmitExtractError(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	syntaxErrorFile := filepath.Join(filepath.Dir(thisFile), "..", "testdata", "ts", "syntax_error.ts")
	dir := filepath.Dir(syntaxErrorFile)

	database := walkerTestDBDir(t, dir)
	r := rel(t, database, "ExtractError")
	// Note: tree-sitter may partially recover, so we can't guarantee errors.
	// But if there are any, they should have valid columns.
	for i := 0; i < r.Tuples(); i++ {
		phase, _ := r.GetString(database, i, 2)
		msg, _ := r.GetString(database, i, 3)
		if phase == "" {
			t.Errorf("ExtractError tuple %d: empty phase", i)
		}
		if msg == "" {
			t.Errorf("ExtractError tuple %d: empty message", i)
		}
	}
}

// TestEmitCallCalleeSym verifies that in-scope callee identifiers get a sym.
func TestEmitCallCalleeSym(t *testing.T) {
	src := `
function greet(name) { return name; }
greet("world");
`
	database := walkerTestDB(t, src)
	r := rel(t, database, "CallCalleeSym")
	if r.Tuples() == 0 {
		t.Fatal("CallCalleeSym: expected tuples for resolved 'greet'")
	}
}

// TestNodeIDDeterminism verifies that NodeID is deterministic.
func TestNodeIDDeterminism(t *testing.T) {
	id1 := NodeID("foo.ts", 1, 0, 1, 10, "Identifier")
	id2 := NodeID("foo.ts", 1, 0, 1, 10, "Identifier")
	if id1 != id2 {
		t.Errorf("NodeID not deterministic: %d vs %d", id1, id2)
	}
	// Different inputs should (almost certainly) differ
	id3 := NodeID("foo.ts", 2, 0, 2, 10, "Identifier")
	if id1 == id3 {
		t.Error("NodeID: different positions produced same ID")
	}
}

// TestSymIDDeterminism verifies that SymID is deterministic.
func TestSymIDDeterminism(t *testing.T) {
	id1 := SymID("foo.ts", "myVar", 1, 0)
	id2 := SymID("foo.ts", "myVar", 1, 0)
	if id1 != id2 {
		t.Errorf("SymID not deterministic: %d vs %d", id1, id2)
	}
	id3 := SymID("foo.ts", "otherVar", 1, 0)
	if id1 == id3 {
		t.Error("SymID: different names produced same ID")
	}
}

// TestWalkerRoundTrip verifies the DB survives an encode/decode round-trip.
func TestWalkerRoundTrip(t *testing.T) {
	src := `
function add(a: number, b: number): number {
  return a + b;
}
const result = add(1, 2);
`
	database := walkerTestDB(t, src)
	decoded := encodeDecode(t, database)

	// Check that key relations survive round-trip
	for _, relName := range []string{"File", "Node", "Function", "Call", "VarDecl", "SchemaVersion"} {
		orig := database.Relation(relName).Tuples()
		got := decoded.Relation(relName).Tuples()
		if orig != got {
			t.Errorf("round-trip %s: original %d tuples, decoded %d", relName, orig, got)
		}
	}
}

// TestWalkerFullFile runs the walker on simple_function.ts and checks all
// major relations are populated — a golden integration test.
func TestWalkerFullFile(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	dir := filepath.Join(filepath.Dir(thisFile), "..", "testdata", "ts")

	database := walkerTestDBDir(t, dir)

	checks := []struct {
		rel     string
		minTups int
	}{
		{"SchemaVersion", 1},
		{"File", 1},
		{"Node", 10},
		{"Contains", 5},
		{"Function", 1},
		{"Parameter", 1},
		{"Call", 1},
		{"VarDecl", 1},
	}

	for _, ch := range checks {
		n := tupleCount(t, database, ch.rel)
		if n < ch.minTups {
			t.Errorf("%s: expected >= %d tuples, got %d", ch.rel, ch.minTups, n)
		}
	}
}

// TestWalkerNoFile verifies that an empty directory produces at least a SchemaVersion.
func TestWalkerNoFile(t *testing.T) {
	dir := t.TempDir()
	database := walkerTestDBDir(t, dir)
	r := rel(t, database, "SchemaVersion")
	if r.Tuples() != 1 {
		t.Errorf("expected SchemaVersion=1 even with no files, got %d", r.Tuples())
	}
	// No File tuples expected
	if tupleCount(t, database, "File") != 0 {
		t.Errorf("File: expected 0 tuples for empty dir")
	}
}

// TestParameterOptional verifies optional parameters are flagged.
func TestParameterOptional(t *testing.T) {
	src := `function f(x: string, y?: number) {}`
	database := walkerTestDB(t, src)
	r := rel(t, database, "ParameterOptional")
	if r.Tuples() == 0 {
		t.Fatal("ParameterOptional: expected tuples for y?")
	}
}

// TestParamIsFunctionType verifies callback-typed parameters are flagged.
func TestParamIsFunctionType(t *testing.T) {
	src := `function f(cb: (x: number) => void) {}`
	database := walkerTestDB(t, src)
	r := rel(t, database, "ParamIsFunctionType")
	if r.Tuples() == 0 {
		t.Fatal("ParamIsFunctionType: expected tuples for function-typed param")
	}
}

// TestGeneratorFunction verifies isGenerator=1 is set.
func TestGeneratorFunction(t *testing.T) {
	src := `function* gen() { yield 1; }`
	database := walkerTestDB(t, src)
	r := rel(t, database, "Function")
	if !hasIntInCol(r, 4, 1) {
		t.Error("Function: expected isGenerator=1")
	}
}

// ---- Value-flow Phase A PR1: ExprValueSource + AssignExpr emission ----

// TestExprValueSource_ObjectLiteral verifies an object literal emits an
// identity row in ExprValueSource (expr == sourceExpr).
func TestExprValueSource_ObjectLiteral(t *testing.T) {
	src := `const o = { x: 5 };`
	database := walkerTestDB(t, src)
	r := rel(t, database, "ExprValueSource")
	if r.Tuples() < 2 {
		t.Fatalf("ExprValueSource: expected at least 2 tuples (obj literal + number), got %d", r.Tuples())
	}
	// Each row must be (id, id) — identity.
	for i := 0; i < r.Tuples(); i++ {
		a := getInt(t, r, i, 0)
		b := getInt(t, r, i, 1)
		if a != b {
			t.Errorf("ExprValueSource row %d: expected identity, got (%d, %d)", i, a, b)
		}
	}
}

// TestExprValueSource_ArrowFunction verifies arrow functions are value-source.
func TestExprValueSource_ArrowFunction(t *testing.T) {
	src := `const f = () => 1;`
	database := walkerTestDB(t, src)
	r := rel(t, database, "ExprValueSource")
	// Expect at least: arrow function + numeric literal.
	if r.Tuples() < 2 {
		t.Fatalf("ExprValueSource: expected >=2 tuples for arrow + literal, got %d", r.Tuples())
	}
}

// TestExprValueSource_NotForCalls confirms calls and identifiers are NOT in ExprValueSource.
func TestExprValueSource_NotForCalls(t *testing.T) {
	src := `function g() { return 1; } const x = g(); const y = x;`
	database := walkerTestDB(t, src)
	rValueSrc := rel(t, database, "ExprValueSource")
	rNode := rel(t, database, "Node")

	// Build a kind lookup: NodeID -> kind.
	kindOf := make(map[int32]string)
	for i := 0; i < rNode.Tuples(); i++ {
		id := getInt(t, rNode, i, 0)
		k, err := rNode.GetString(database, i, 2)
		if err != nil {
			t.Fatalf("GetString kind: %v", err)
		}
		kindOf[id] = k
	}

	for i := 0; i < rValueSrc.Tuples(); i++ {
		id := getInt(t, rValueSrc, i, 0)
		k := kindOf[id]
		switch k {
		case "Identifier", "CallExpression", "MemberExpression",
			"BinaryExpression", "AwaitExpression", "AsExpression",
			"NonNullExpression", "ParenthesizedExpression":
			t.Errorf("ExprValueSource row for %s (id=%d) — should NOT be a value-source", k, id)
		}
	}
}

// TestExprValueSource_TemplateLiteralWithSubstitution verifies that template
// literals containing ${...} are NOT emitted as value-sources, while bare
// template literals are.
func TestExprValueSource_TemplateLiteralWithSubstitution(t *testing.T) {
	src := "const a = `bare`; const b = `hi ${a}`;"
	database := walkerTestDB(t, src)
	rValueSrc := rel(t, database, "ExprValueSource")
	rNode := rel(t, database, "Node")

	// Find both template node ids.
	var bareID, substID int32 = 0, 0
	for i := 0; i < rNode.Tuples(); i++ {
		k, _ := rNode.GetString(database, i, 2)
		if k != "TemplateString" {
			continue
		}
		startLine := getInt(t, rNode, i, 3)
		id := getInt(t, rNode, i, 0)
		// `bare` is on line 1 col 10ish; `hi ${a}` is on line 1 too.
		// Discriminate by start col.
		startCol := getInt(t, rNode, i, 4)
		_ = startLine
		if startCol < 25 {
			bareID = id
		} else {
			substID = id
		}
	}
	if bareID == 0 || substID == 0 {
		t.Fatalf("expected to find both template nodes; bare=%d subst=%d", bareID, substID)
	}
	hasID := func(target int32) bool {
		for i := 0; i < rValueSrc.Tuples(); i++ {
			if getInt(t, rValueSrc, i, 0) == target {
				return true
			}
		}
		return false
	}
	if !hasID(bareID) {
		t.Error("bare template literal should be in ExprValueSource")
	}
	if hasID(substID) {
		t.Error("template literal with substitution should NOT be in ExprValueSource")
	}
}

// TestAssignExpr_Basic verifies AssignExpr is emitted for a simple identifier
// reassignment.
func TestAssignExpr_Basic(t *testing.T) {
	src := `let x; x = 42;`
	database := walkerTestDB(t, src)
	r := rel(t, database, "AssignExpr")
	if r.Tuples() != 1 {
		t.Fatalf("AssignExpr: expected 1 tuple, got %d", r.Tuples())
	}
	lhsSym := getInt(t, r, 0, 0)
	rhs := getInt(t, r, 0, 1)
	if lhsSym == 0 {
		t.Error("AssignExpr: lhsSym should be non-zero (resolved symbol)")
	}
	if rhs == 0 {
		t.Error("AssignExpr: rhsExpr should be non-zero")
	}
}

// TestAssignExpr_NotForFieldWrite confirms that an assignment to a member
// expression (`o.x = 1`) does NOT emit AssignExpr (those go through
// FieldWrite).
func TestAssignExpr_NotForFieldWrite(t *testing.T) {
	src := `const o: any = {}; o.x = 1;`
	database := walkerTestDB(t, src)
	r := rel(t, database, "AssignExpr")
	// `o = ...` style assignments don't appear here; only `o.x = 1` which
	// has a member-expression LHS — must NOT emit AssignExpr.
	if r.Tuples() != 0 {
		t.Errorf("AssignExpr: expected 0 tuples for member-LHS assignment, got %d", r.Tuples())
	}
}

// TestAssignExpr_MultipleAssigns verifies multiple assignments to the same
// symbol each emit a row (last-write-wins is NOT enforced).
func TestAssignExpr_MultipleAssigns(t *testing.T) {
	src := `let x; x = 1; x = 2; x = 3;`
	database := walkerTestDB(t, src)
	r := rel(t, database, "AssignExpr")
	if r.Tuples() != 3 {
		t.Errorf("AssignExpr: expected 3 tuples for 3 assignments, got %d", r.Tuples())
	}
}
