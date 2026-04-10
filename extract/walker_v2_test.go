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

// TestV2Extends verifies Extends tuples for class/interface inheritance.
func TestV2Extends(t *testing.T) {
	src := `
class Animal {}
class Dog extends Animal {}
`
	database := v2WalkerTestDB(t, src)
	r := rel(t, database, "Extends")
	if r.Tuples() == 0 {
		t.Fatal("Extends: expected tuples for Dog extends Animal")
	}
}

// TestV2Implements verifies Implements tuples.
func TestV2Implements(t *testing.T) {
	src := `
interface Serializable { serialize(): string; }
class Dog implements Serializable {
  serialize(): string { return ""; }
}
`
	database := v2WalkerTestDB(t, src)
	r := rel(t, database, "Implements")
	if r.Tuples() == 0 {
		t.Fatal("Implements: expected tuples for Dog implements Serializable")
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

// TestV2ExprTypeEmpty verifies ExprType is registered but empty without tsgo.
func TestV2ExprTypeEmpty(t *testing.T) {
	src := `const x = 42;`
	database := v2WalkerTestDB(t, src)
	r := rel(t, database, "ExprType")
	if r.Tuples() != 0 {
		t.Errorf("ExprType: expected 0 tuples without tsgo, got %d", r.Tuples())
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
