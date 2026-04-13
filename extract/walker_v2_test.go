package extract

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Gjdoalfnrxu/tsq/extract/db"
	_ "github.com/Gjdoalfnrxu/tsq/extract/schema" // ensure relations are registered
)

// v2WalkerTestDB runs the TypeAwareWalker over a single inline TypeScript source string
// and returns the resulting DB.
func v2WalkerTestDB(t *testing.T, src string) *db.DB {
	t.Helper()
	dir := t.TempDir()
	f := filepath.Join(dir, "test.ts")
	if err := os.WriteFile(f, []byte(src), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return v2WalkerTestDBDir(t, dir)
}

// v2WalkerTestDBDir runs the TypeAwareWalker over all TS files in dir.
func v2WalkerTestDBDir(t *testing.T, dir string) *db.DB {
	t.Helper()
	database := db.NewDB()
	walker := NewTypeAwareWalker(database)
	backend := &TreeSitterBackend{}
	ctx := context.Background()
	cfg := ProjectConfig{RootDir: dir}
	if err := walker.Run(ctx, backend, cfg); err != nil {
		t.Fatalf("Run: %v", err)
	}
	backend.Close()
	return database
}

// TestV2ClassDecl verifies ClassDecl tuples are emitted for class declarations.
func TestV2ClassDecl(t *testing.T) {
	src := `
class Animal {
  speak(): string { return ""; }
}
class Dog extends Animal {
  bark(): void {}
}
`
	database := v2WalkerTestDB(t, src)
	r := rel(t, database, "ClassDecl")
	if r.Tuples() < 2 {
		t.Fatalf("ClassDecl: expected >= 2 tuples, got %d", r.Tuples())
	}
	if !hasString(t, database, r, 1, "Animal") {
		t.Error("ClassDecl: expected name='Animal'")
	}
	if !hasString(t, database, r, 1, "Dog") {
		t.Error("ClassDecl: expected name='Dog'")
	}
}

// TestV2InterfaceDecl verifies InterfaceDecl tuples.
func TestV2InterfaceDecl(t *testing.T) {
	src := `
interface Serializable {
  serialize(): string;
}
interface Loggable {
  log(msg: string): void;
}
`
	database := v2WalkerTestDB(t, src)
	r := rel(t, database, "InterfaceDecl")
	if r.Tuples() < 2 {
		t.Fatalf("InterfaceDecl: expected >= 2 tuples, got %d", r.Tuples())
	}
	if !hasString(t, database, r, 1, "Serializable") {
		t.Error("InterfaceDecl: expected name='Serializable'")
	}
	if !hasString(t, database, r, 1, "Loggable") {
		t.Error("InterfaceDecl: expected name='Loggable'")
	}
}

// entityIDByName scans a relation (e.g. ClassDecl, InterfaceDecl) whose
// column 0 is an entity id and column 1 is a string name, and returns the
// id for the first row whose name matches. Fails the test if not found.
func entityIDByName(t *testing.T, database *db.DB, relName, name string) int32 {
	t.Helper()
	r := rel(t, database, relName)
	for i := 0; i < r.Tuples(); i++ {
		s, err := r.GetString(database, i, 1)
		if err != nil {
			continue
		}
		if s == name {
			id, err := r.GetInt(i, 0)
			if err != nil {
				t.Fatalf("%s[%d].id: %v", relName, i, err)
			}
			return id
		}
	}
	t.Fatalf("%s: no entity named %q", relName, name)
	return 0
}

// hasIntPair returns true if relation r has a tuple with (col0=a, col1=b).
func hasIntPair(r *db.Relation, a, b int32) bool {
	for i := 0; i < r.Tuples(); i++ {
		x, errA := r.GetInt(i, 0)
		y, errB := r.GetInt(i, 1)
		if errA == nil && errB == nil && x == a && y == b {
			return true
		}
	}
	return false
}

// hasChildID returns true if relation r has a tuple whose column 0 equals id.
func hasChildID(r *db.Relation, id int32) bool {
	for i := 0; i < r.Tuples(); i++ {
		if got, err := r.GetInt(i, 0); err == nil && got == id {
			return true
		}
	}
	return false
}

// TestV2Extends verifies Extends tuples for class inheritance.
// Pins (child=Dog's ClassDecl id, parent=some non-Dog id) so that a walker bug
// emitting the reversed Extends(Animal, Dog) or a self-loop Extends(Dog, Dog)
// would fail the test.
//
// Note: the walker currently emits Extends with the parent column referring to
// a *type reference* entity id — not the parent ClassDecl's own id — so we
// cannot assert parent == Animal's ClassDecl id here. The child column IS the
// Dog ClassDecl id, which is what we pin.
func TestV2Extends(t *testing.T) {
	src := `
class Animal {}
class Dog extends Animal {}
`
	database := v2WalkerTestDB(t, src)
	dogID := entityIDByName(t, database, "ClassDecl", "Dog")
	animalID := entityIDByName(t, database, "ClassDecl", "Animal")
	r := rel(t, database, "Extends")
	if r.Tuples() == 0 {
		t.Fatal("Extends: expected tuples for Dog extends Animal")
	}
	if !hasChildID(r, dogID) {
		t.Errorf("Extends: no row with child=Dog's ClassDecl id (%d); rows are for a different child", dogID)
	}
	if hasChildID(r, animalID) {
		t.Errorf("Extends: found row with child=Animal's ClassDecl id (%d) — child/parent columns appear reversed", animalID)
	}
	// Self-loop check: (Dog, Dog) would indicate the walker emitted the same id twice.
	if hasIntPair(r, dogID, dogID) {
		t.Errorf("Extends: found self-loop row (Dog, Dog)")
	}
}

// TestV2Implements verifies Implements tuples. Same caveat as TestV2Extends:
// the interface column references a type-use id, not the InterfaceDecl id, so
// we pin only the class column (= Dog's ClassDecl id).
func TestV2Implements(t *testing.T) {
	src := `
interface Serializable { serialize(): string; }
class Dog implements Serializable {
  serialize(): string { return ""; }
}
`
	database := v2WalkerTestDB(t, src)
	dogID := entityIDByName(t, database, "ClassDecl", "Dog")
	ifaceID := entityIDByName(t, database, "InterfaceDecl", "Serializable")
	r := rel(t, database, "Implements")
	if r.Tuples() == 0 {
		t.Fatal("Implements: expected tuples for Dog implements Serializable")
	}
	if !hasChildID(r, dogID) {
		t.Errorf("Implements: no row with class=Dog's ClassDecl id (%d)", dogID)
	}
	if hasChildID(r, ifaceID) {
		t.Errorf("Implements: found row with class=Serializable's InterfaceDecl id (%d) — class/iface columns appear reversed", ifaceID)
	}
	if hasIntPair(r, dogID, dogID) {
		t.Errorf("Implements: found self-loop row (Dog, Dog)")
	}
}

// TestV2MethodDecl verifies MethodDecl tuples inside classes.
func TestV2MethodDecl(t *testing.T) {
	src := `
class Dog {
  speak(): string { return "woof"; }
  fetch(item: string): void {}
}
`
	database := v2WalkerTestDB(t, src)
	r := rel(t, database, "MethodDecl")
	if r.Tuples() < 2 {
		t.Fatalf("MethodDecl: expected >= 2 tuples, got %d", r.Tuples())
	}
	if !hasString(t, database, r, 1, "speak") {
		t.Error("MethodDecl: expected name='speak'")
	}
	if !hasString(t, database, r, 1, "fetch") {
		t.Error("MethodDecl: expected name='fetch'")
	}
}

// TestV2NewExpr verifies NewExpr tuples.
func TestV2NewExpr(t *testing.T) {
	src := `
class Dog { constructor(name: string) {} }
const d = new Dog("Rex");
`
	database := v2WalkerTestDB(t, src)
	r := rel(t, database, "NewExpr")
	if r.Tuples() == 0 {
		t.Fatal("NewExpr: expected tuples for new Dog()")
	}
}

// TestV2MethodCall verifies MethodCall tuples for member call expressions.
func TestV2MethodCall(t *testing.T) {
	src := `
const obj = { greet: () => "hi" };
obj.greet();
console.log("test");
`
	database := v2WalkerTestDB(t, src)
	r := rel(t, database, "MethodCall")
	if r.Tuples() < 2 {
		t.Fatalf("MethodCall: expected >= 2 tuples (obj.greet, console.log), got %d", r.Tuples())
	}
	if !hasString(t, database, r, 2, "greet") {
		t.Error("MethodCall: expected methodName='greet'")
	}
	if !hasString(t, database, r, 2, "log") {
		t.Error("MethodCall: expected methodName='log'")
	}
}

// TestV2ReturnStmt verifies ReturnStmt tuples.
func TestV2ReturnStmt(t *testing.T) {
	src := `
function add(a: number, b: number): number {
  return a + b;
}
`
	database := v2WalkerTestDB(t, src)
	r := rel(t, database, "ReturnStmt")
	if r.Tuples() == 0 {
		t.Fatal("ReturnStmt: expected tuples")
	}
}

// TestV2FunctionContains verifies FunctionContains tuples.
func TestV2FunctionContains(t *testing.T) {
	src := `
function foo() {
  const x = 1;
  const y = x + 2;
  return y;
}
`
	database := v2WalkerTestDB(t, src)
	r := rel(t, database, "FunctionContains")
	if r.Tuples() == 0 {
		t.Fatal("FunctionContains: expected tuples for nodes inside foo()")
	}
	// Should contain multiple nodes (VarDecl, return, identifiers, etc.)
	if r.Tuples() < 3 {
		t.Errorf("FunctionContains: expected >= 3 tuples, got %d", r.Tuples())
	}
}

// TestV2TypeDecl verifies TypeDecl tuples for type alias declarations.
func TestV2TypeDecl(t *testing.T) {
	src := `
type StringOrNumber = string | number;
type Callback = (x: number) => void;
`
	database := v2WalkerTestDB(t, src)
	r := rel(t, database, "TypeDecl")
	if r.Tuples() < 2 {
		t.Fatalf("TypeDecl: expected >= 2 tuples, got %d", r.Tuples())
	}
	if !hasString(t, database, r, 1, "StringOrNumber") {
		t.Error("TypeDecl: expected name='StringOrNumber'")
	}
	if !hasString(t, database, r, 1, "Callback") {
		t.Error("TypeDecl: expected name='Callback'")
	}
}

// TestV2Symbol verifies Symbol tuples are populated for variable and function declarations.
func TestV2Symbol(t *testing.T) {
	src := `
const greeting = "hello";
function sayHi(name: string): string {
  return greeting + " " + name;
}
`
	database := v2WalkerTestDB(t, src)
	r := rel(t, database, "Symbol")
	if r.Tuples() == 0 {
		t.Fatal("Symbol: expected tuples")
	}
	if !hasString(t, database, r, 1, "greeting") {
		t.Error("Symbol: expected name='greeting'")
	}
	if !hasString(t, database, r, 1, "sayHi") {
		t.Error("Symbol: expected name='sayHi'")
	}
}

// TestV2FunctionSymbol verifies FunctionSymbol tuples.
func TestV2FunctionSymbol(t *testing.T) {
	src := `
function namedFn() {}
const arrowFn = () => {};
`
	database := v2WalkerTestDB(t, src)
	r := rel(t, database, "FunctionSymbol")
	if r.Tuples() == 0 {
		t.Fatal("FunctionSymbol: expected tuples for named function and arrow")
	}
}

// TestV2SymInFunction verifies SymInFunction tuples.
func TestV2SymInFunction(t *testing.T) {
	src := `
const outer = 42;
function foo() {
  const x = outer;
}
`
	database := v2WalkerTestDB(t, src)
	r := rel(t, database, "SymInFunction")
	if r.Tuples() == 0 {
		t.Fatal("SymInFunction: expected tuples")
	}
}

// TestV2TypeFactsPopulated verifies that structural type-fact relations are populated
// from AST patterns without requiring tsgo. ExprType/SymbolType remain empty (tsgo-dependent)
// but TypeInfo, UnionMember, IntersectionMember, TypeAlias, TypeParameter, and
// GenericInstantiation are populated structurally.
func TestV2TypeFactsPopulated(t *testing.T) {
	src := `
type StringOrNumber = string | number;
type Named = { name: string } & { age: number };
type Identity<T> = T;
interface Box<T> { value: T; }
class Container<U extends object> { item: U; }
function identity<V>(x: V): V { return x; }
const x: Box<string> = { value: "hi" };
`
	database := v2WalkerTestDB(t, src)

	// TypeInfo should be populated for union, intersection, alias, and generic types
	typeInfoR := rel(t, database, "TypeInfo")
	if typeInfoR.Tuples() == 0 {
		t.Error("TypeInfo: expected non-zero tuples from structural type emission")
	}

	// UnionMember should be populated for `string | number`
	unionR := rel(t, database, "UnionMember")
	if unionR.Tuples() == 0 {
		t.Error("UnionMember: expected non-zero tuples for union type")
	}

	// IntersectionMember should be populated for `{ name: string } & { age: number }`
	interR := rel(t, database, "IntersectionMember")
	if interR.Tuples() == 0 {
		t.Error("IntersectionMember: expected non-zero tuples for intersection type")
	}

	// TypeAlias should be populated for type alias declarations
	aliasR := rel(t, database, "TypeAlias")
	if aliasR.Tuples() == 0 {
		t.Error("TypeAlias: expected non-zero tuples for type alias declarations")
	}

	// GenericInstantiation should be populated for Box<string>
	genR := rel(t, database, "GenericInstantiation")
	if genR.Tuples() == 0 {
		t.Error("GenericInstantiation: expected non-zero tuples for generic type references")
	}

	// TypeParameter should be populated for generic declarations (class, interface, function, type alias)
	// The test source has 4 generic declarations: Identity<T>, Box<T>, Container<U>, identity<V>
	tpR := rel(t, database, "TypeParameter")
	if tpR.Tuples() < 4 {
		t.Errorf("TypeParameter: expected at least 4 tuples (one per generic decl), got %d", tpR.Tuples())
	}

	// ExprType and SymbolType remain empty without tsgo
	exprR := rel(t, database, "ExprType")
	if exprR.Tuples() != 0 {
		t.Errorf("ExprType: expected 0 tuples without tsgo, got %d", exprR.Tuples())
	}
	symR := rel(t, database, "SymbolType")
	if symR.Tuples() != 0 {
		t.Errorf("SymbolType: expected 0 tuples without tsgo, got %d", symR.Tuples())
	}
}

// TestV2FixtureDir runs the TypeAwareWalker on the v2 fixture directory.
func TestV2FixtureDir(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	dir := filepath.Join(filepath.Dir(thisFile), "..", "testdata", "ts", "v2")

	database := v2WalkerTestDBDir(t, dir)

	checks := []struct {
		rel     string
		minTups int
	}{
		{"ClassDecl", 3},        // Animal, Dog, Puppy + generic classes
		{"InterfaceDecl", 2},    // Serializable, Loggable, Printable, etc.
		{"Extends", 2},          // Dog extends Animal, Puppy extends Dog, etc.
		{"Implements", 1},       // Dog implements Serializable+Loggable
		{"MethodDecl", 3},       // speak, serialize, log, bark, etc.
		{"NewExpr", 2},          // new Dog, new Puppy, etc.
		{"MethodCall", 2},       // dog.speak(), dog.serialize(), etc.
		{"ReturnStmt", 1},       // return statements
		{"FunctionContains", 5}, // nodes inside functions
		{"TypeDecl", 1},         // DogFactory type alias
		{"Symbol", 3},           // variables, functions, classes
		{"FunctionSymbol", 1},   // createDog, getName
	}

	for _, ch := range checks {
		n := tupleCount(t, database, ch.rel)
		if n < ch.minTups {
			t.Errorf("%s: expected >= %d tuples, got %d", ch.rel, ch.minTups, n)
		}
	}
}

// TestV2BackwardsCompatibility verifies that the TypeAwareWalker still emits all v1 facts.
func TestV2BackwardsCompatibility(t *testing.T) {
	src := `
function foo(a: number, b: number): number { return a + b; }
const result = foo(1, 2);
`
	database := v2WalkerTestDB(t, src)

	v1Relations := []string{
		"SchemaVersion", "File", "Node", "Contains",
		"Function", "Parameter", "Call", "CallArg",
		"VarDecl", "ExprIsCall",
	}
	for _, name := range v1Relations {
		r := rel(t, database, name)
		if r.Tuples() == 0 {
			t.Errorf("v1 relation %s: expected tuples, got 0", name)
		}
	}
}

// TestV2MultipleImplements verifies multiple implements interfaces.
func TestV2MultipleImplements(t *testing.T) {
	src := `
interface A { a(): void; }
interface B { b(): void; }
class C implements A, B {
  a(): void {}
  b(): void {}
}
`
	database := v2WalkerTestDB(t, src)
	r := rel(t, database, "Implements")
	if r.Tuples() < 2 {
		t.Fatalf("Implements: expected >= 2 tuples for C implements A, B; got %d", r.Tuples())
	}
}

// TestV2ExprInFunction verifies ExprInFunction tuples are emitted for expressions inside functions.
func TestV2ExprInFunction(t *testing.T) {
	src := `
function foo() {
  const x = 1;
  const y = x + 2;
  return y;
}
`
	database := v2WalkerTestDB(t, src)
	r := rel(t, database, "ExprInFunction")
	if r.Tuples() == 0 {
		t.Fatal("ExprInFunction: expected tuples for expressions inside foo()")
	}
	// Should contain multiple expression nodes (identifiers, binary expressions, numbers)
	if r.Tuples() < 3 {
		t.Errorf("ExprInFunction: expected >= 3 tuples, got %d", r.Tuples())
	}
}

// TestV2EnumDecl verifies EnumDecl and EnumMember tuples.
func TestV2EnumDecl(t *testing.T) {
	src := `
enum Direction {
  Up,
  Down,
  Left,
  Right
}
`
	database := v2WalkerTestDB(t, src)
	r := rel(t, database, "EnumDecl")
	if r.Tuples() == 0 {
		t.Fatal("EnumDecl: expected tuples for Direction enum")
	}
	if !hasString(t, database, r, 1, "Direction") {
		t.Error("EnumDecl: expected name='Direction'")
	}

	mr := rel(t, database, "EnumMember")
	if mr.Tuples() < 4 {
		t.Errorf("EnumMember: expected >= 4 tuples (Up, Down, Left, Right), got %d", mr.Tuples())
	}
}

// TestV2InterfaceExtends verifies interface extends interface.
func TestV2InterfaceExtends(t *testing.T) {
	src := `
interface Base { foo(): void; }
interface Child extends Base { bar(): void; }
`
	database := v2WalkerTestDB(t, src)
	r := rel(t, database, "Extends")
	if r.Tuples() == 0 {
		t.Fatal("Extends: expected tuples for Child extends Base")
	}
}
