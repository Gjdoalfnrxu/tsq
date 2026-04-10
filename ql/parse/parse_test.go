package parse_test

import (
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/ast"
	"github.com/Gjdoalfnrxu/tsq/ql/parse"
)

func mustParse(t *testing.T, src string) *ast.Module {
	t.Helper()
	p := parse.NewParser(src, "test.ql")
	mod, err := p.Parse()
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	return mod
}

func mustFail(t *testing.T, src string) {
	t.Helper()
	p := parse.NewParser(src, "test.ql")
	_, err := p.Parse()
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
}

// --- Lexer tests ---

func TestLexerBasicTokens(t *testing.T) {
	l := parse.NewLexer("( ) { } , ; . | @ :: = != < <= > >= + - * / %", "test.ql")
	expected := []parse.TokenType{
		parse.TokLParen, parse.TokRParen, parse.TokLBrace, parse.TokRBrace,
		parse.TokComma, parse.TokSemi, parse.TokDot, parse.TokPipe, parse.TokAt,
		parse.TokColCol, parse.TokEq, parse.TokNeq, parse.TokLt, parse.TokLte,
		parse.TokGt, parse.TokGte, parse.TokPlus, parse.TokMinus, parse.TokStar,
		parse.TokSlash, parse.TokPct, parse.TokEOF,
	}
	for i, exp := range expected {
		tok := l.Next()
		if tok.Type != exp {
			t.Errorf("token %d: expected type %d, got %d (lit=%q)", i, exp, tok.Type, tok.Lit)
		}
	}
}

func TestLexerKeywords(t *testing.T) {
	l := parse.NewLexer("import as class extends predicate from where select exists forall not and or instanceof result this none any true false override abstract count min max sum avg", "test.ql")
	expected := []parse.TokenType{
		parse.TokKwImport, parse.TokKwAs, parse.TokKwClass, parse.TokKwExtends,
		parse.TokKwPredicate, parse.TokKwFrom, parse.TokKwWhere, parse.TokKwSelect,
		parse.TokKwExists, parse.TokKwForall, parse.TokKwNot, parse.TokKwAnd,
		parse.TokKwOr, parse.TokKwInstanceof, parse.TokKwResult, parse.TokKwThis,
		parse.TokKwNone, parse.TokKwAny, parse.TokKwTrue, parse.TokKwFalse,
		parse.TokKwOverride, parse.TokKwAbstract, parse.TokKwCount, parse.TokKwMin,
		parse.TokKwMax, parse.TokKwSum, parse.TokKwAvg, parse.TokEOF,
	}
	for i, exp := range expected {
		tok := l.Next()
		if tok.Type != exp {
			t.Errorf("token %d: expected type %d, got %d (lit=%q)", i, exp, tok.Type, tok.Lit)
		}
	}
}

func TestLexerIdentifiers(t *testing.T) {
	l := parse.NewLexer("foo _bar baz123", "test.ql")
	for _, exp := range []string{"foo", "_bar", "baz123"} {
		tok := l.Next()
		if tok.Type != parse.TokIdent || tok.Lit != exp {
			t.Errorf("expected ident %q, got type=%d lit=%q", exp, tok.Type, tok.Lit)
		}
	}
}

func TestLexerIntegers(t *testing.T) {
	l := parse.NewLexer("0 42 9999", "test.ql")
	for _, exp := range []string{"0", "42", "9999"} {
		tok := l.Next()
		if tok.Type != parse.TokInt || tok.Lit != exp {
			t.Errorf("expected int %q, got type=%d lit=%q", exp, tok.Type, tok.Lit)
		}
	}
}

func TestLexerStrings(t *testing.T) {
	l := parse.NewLexer(`"hello" "with \"escape\"" "tab\there" "newline\n"`, "test.ql")
	expected := []string{"hello", `with "escape"`, "tab\there", "newline\n"}
	for _, exp := range expected {
		tok := l.Next()
		if tok.Type != parse.TokString || tok.Lit != exp {
			t.Errorf("expected string %q, got type=%d lit=%q", exp, tok.Type, tok.Lit)
		}
	}
}

func TestLexerComments(t *testing.T) {
	src := `foo // line comment
bar /* block comment */ baz
/* multi
   line */ qux`
	l := parse.NewLexer(src, "test.ql")
	expected := []string{"foo", "bar", "baz", "qux"}
	for _, exp := range expected {
		tok := l.Next()
		if tok.Type != parse.TokIdent || tok.Lit != exp {
			t.Errorf("expected ident %q, got type=%d lit=%q", exp, tok.Type, tok.Lit)
		}
	}
}

func TestLexerLineTracking(t *testing.T) {
	l := parse.NewLexer("a\nb\nc", "test.ql")
	tok := l.Next()
	if tok.Line != 1 {
		t.Errorf("expected line 1, got %d", tok.Line)
	}
	tok = l.Next()
	if tok.Line != 2 {
		t.Errorf("expected line 2, got %d", tok.Line)
	}
	tok = l.Next()
	if tok.Line != 3 {
		t.Errorf("expected line 3, got %d", tok.Line)
	}
}

func TestLexerUnterminatedString(t *testing.T) {
	l := parse.NewLexer(`"unterminated`, "test.ql")
	tok := l.Next()
	if tok.Type != parse.TokError {
		t.Errorf("expected TokError for unterminated string, got type=%d", tok.Type)
	}
}

// --- Parser tests ---

func TestEmptyModule(t *testing.T) {
	mod := mustParse(t, "")
	if len(mod.Imports) != 0 || len(mod.Classes) != 0 || len(mod.Predicates) != 0 || mod.Select != nil {
		t.Error("expected empty module")
	}
}

func TestImportSimple(t *testing.T) {
	mod := mustParse(t, "import javascript")
	if len(mod.Imports) != 1 {
		t.Fatalf("expected 1 import, got %d", len(mod.Imports))
	}
	if mod.Imports[0].Path != "javascript" {
		t.Errorf("expected path 'javascript', got %q", mod.Imports[0].Path)
	}
	if mod.Imports[0].Alias != "" {
		t.Errorf("expected no alias, got %q", mod.Imports[0].Alias)
	}
}

func TestImportQualified(t *testing.T) {
	mod := mustParse(t, "import DataFlow::PathGraph")
	if len(mod.Imports) != 1 {
		t.Fatalf("expected 1 import, got %d", len(mod.Imports))
	}
	if mod.Imports[0].Path != "DataFlow::PathGraph" {
		t.Errorf("expected path 'DataFlow::PathGraph', got %q", mod.Imports[0].Path)
	}
}

func TestImportWithAlias(t *testing.T) {
	mod := mustParse(t, "import javascript as JS")
	if len(mod.Imports) != 1 {
		t.Fatalf("expected 1 import, got %d", len(mod.Imports))
	}
	if mod.Imports[0].Alias != "JS" {
		t.Errorf("expected alias 'JS', got %q", mod.Imports[0].Alias)
	}
}

func TestSimplePredicate(t *testing.T) {
	mod := mustParse(t, `predicate foo(int x) { x > 0 }`)
	if len(mod.Predicates) != 1 {
		t.Fatalf("expected 1 predicate, got %d", len(mod.Predicates))
	}
	pred := mod.Predicates[0]
	if pred.Name != "foo" {
		t.Errorf("expected name 'foo', got %q", pred.Name)
	}
	if len(pred.Params) != 1 {
		t.Fatalf("expected 1 param, got %d", len(pred.Params))
	}
	if pred.Params[0].Name != "x" {
		t.Errorf("expected param name 'x', got %q", pred.Params[0].Name)
	}
	if pred.Params[0].Type.String() != "int" {
		t.Errorf("expected param type 'int', got %q", pred.Params[0].Type.String())
	}
	if pred.Body == nil {
		t.Fatal("expected body, got nil")
	}
	comp, ok := (*pred.Body).(*ast.Comparison)
	if !ok {
		t.Fatalf("expected Comparison, got %T", *pred.Body)
	}
	if comp.Op != ">" {
		t.Errorf("expected op '>', got %q", comp.Op)
	}
}

func TestTypedPredicate(t *testing.T) {
	mod := mustParse(t, `string getName() { result = "hi" }`)
	if len(mod.Predicates) != 1 {
		t.Fatalf("expected 1 predicate, got %d", len(mod.Predicates))
	}
	pred := mod.Predicates[0]
	if pred.Name != "getName" {
		t.Errorf("expected name 'getName', got %q", pred.Name)
	}
	if pred.ReturnType == nil {
		t.Fatal("expected return type, got nil")
	}
	if pred.ReturnType.String() != "string" {
		t.Errorf("expected return type 'string', got %q", pred.ReturnType.String())
	}
}

func TestClassWithMethod(t *testing.T) {
	src := `class Foo extends Bar {
		Foo() { this.x() }
		string getX() { result = "hi" }
	}`
	mod := mustParse(t, src)
	if len(mod.Classes) != 1 {
		t.Fatalf("expected 1 class, got %d", len(mod.Classes))
	}
	cls := mod.Classes[0]
	if cls.Name != "Foo" {
		t.Errorf("expected name 'Foo', got %q", cls.Name)
	}
	if len(cls.SuperTypes) != 1 || cls.SuperTypes[0].String() != "Bar" {
		t.Errorf("expected extends Bar, got %v", cls.SuperTypes)
	}
	if cls.CharPred == nil {
		t.Error("expected characteristic predicate, got nil")
	}
	if len(cls.Members) != 1 {
		t.Fatalf("expected 1 member, got %d", len(cls.Members))
	}
	mem := cls.Members[0]
	if mem.Name != "getX" {
		t.Errorf("expected member name 'getX', got %q", mem.Name)
	}
	if mem.ReturnType == nil || mem.ReturnType.String() != "string" {
		t.Errorf("expected return type 'string'")
	}
}

func TestClassOverrideMethod(t *testing.T) {
	src := `class Foo extends Bar {
		Foo() { this.x() }
		override string getX() { result = "hi" }
	}`
	mod := mustParse(t, src)
	cls := mod.Classes[0]
	if len(cls.Members) != 1 {
		t.Fatalf("expected 1 member, got %d", len(cls.Members))
	}
	if !cls.Members[0].Override {
		t.Error("expected override=true")
	}
}

func TestSelectClause(t *testing.T) {
	mod := mustParse(t, `from int x where x > 0 select x`)
	if mod.Select == nil {
		t.Fatal("expected select clause, got nil")
	}
	sel := mod.Select
	if len(sel.Decls) != 1 {
		t.Fatalf("expected 1 decl, got %d", len(sel.Decls))
	}
	if sel.Decls[0].Name != "x" {
		t.Errorf("expected decl name 'x', got %q", sel.Decls[0].Name)
	}
	if sel.Where == nil {
		t.Error("expected where clause, got nil")
	}
	if len(sel.Select) != 1 {
		t.Fatalf("expected 1 select expr, got %d", len(sel.Select))
	}
}

func TestSelectWithLabels(t *testing.T) {
	mod := mustParse(t, `from int x where x > 0 select x as "value"`)
	if mod.Select == nil {
		t.Fatal("expected select clause")
	}
	if len(mod.Select.Labels) != 1 || mod.Select.Labels[0] != "value" {
		t.Errorf("expected label 'value', got %v", mod.Select.Labels)
	}
}

func TestSelectOnly(t *testing.T) {
	mod := mustParse(t, `select 42`)
	if mod.Select == nil {
		t.Fatal("expected select clause")
	}
	if len(mod.Select.Select) != 1 {
		t.Fatalf("expected 1 select expr, got %d", len(mod.Select.Select))
	}
}

func TestExists(t *testing.T) {
	mod := mustParse(t, `predicate foo() { exists(int y | y > 0 and y < 10) }`)
	pred := mod.Predicates[0]
	body := *pred.Body
	pc, ok := body.(*ast.Exists)
	if !ok {
		t.Fatalf("expected Exists, got %T", body)
	}
	if len(pc.Decls) != 1 || pc.Decls[0].Name != "y" {
		t.Error("expected decl y")
	}
	conj, ok := pc.Body.(*ast.Conjunction)
	if !ok {
		t.Fatalf("expected Conjunction in body, got %T", pc.Body)
	}
	_ = conj
}

func TestForall(t *testing.T) {
	mod := mustParse(t, `predicate foo() { forall(int y | y > 0 | y < 100) }`)
	pred := mod.Predicates[0]
	body := *pred.Body
	fa, ok := body.(*ast.Forall)
	if !ok {
		t.Fatalf("expected Forall, got %T", body)
	}
	if len(fa.Decls) != 1 || fa.Decls[0].Name != "y" {
		t.Error("expected decl y")
	}
	if fa.Guard == nil {
		t.Error("expected guard, got nil")
	}
	if fa.Body == nil {
		t.Error("expected body, got nil")
	}
}

func TestQualifiedType(t *testing.T) {
	mod := mustParse(t, `predicate foo(DataFlow::Node n) { n.toString() }`)
	if mod.Predicates[0].Params[0].Type.String() != "DataFlow::Node" {
		t.Errorf("expected DataFlow::Node, got %q", mod.Predicates[0].Params[0].Type.String())
	}
}

func TestMethodChain(t *testing.T) {
	mod := mustParse(t, `predicate foo() { this.getCallee().getTarget() }`)
	pred := mod.Predicates[0]
	body := *pred.Body
	// The body should be a PredicateCall (converted from MethodCall)
	pc, ok := body.(*ast.PredicateCall)
	if !ok {
		t.Fatalf("expected PredicateCall, got %T", body)
	}
	if pc.Name != "getTarget" {
		t.Errorf("expected method name 'getTarget', got %q", pc.Name)
	}
	// The receiver should be a MethodCall for getCallee
	mc, ok := pc.Recv.(*ast.MethodCall)
	if !ok {
		t.Fatalf("expected MethodCall receiver, got %T", pc.Recv)
	}
	if mc.Method != "getCallee" {
		t.Errorf("expected method name 'getCallee', got %q", mc.Method)
	}
	// The receiver of getCallee should be "this"
	v, ok := mc.Recv.(*ast.Variable)
	if !ok {
		t.Fatalf("expected Variable, got %T", mc.Recv)
	}
	if v.Name != "this" {
		t.Errorf("expected 'this', got %q", v.Name)
	}
}

func TestCast(t *testing.T) {
	mod := mustParse(t, `predicate foo() { this.(SubType).bar() }`)
	pred := mod.Predicates[0]
	body := *pred.Body
	pc, ok := body.(*ast.PredicateCall)
	if !ok {
		t.Fatalf("expected PredicateCall, got %T", body)
	}
	if pc.Name != "bar" {
		t.Errorf("expected method name 'bar', got %q", pc.Name)
	}
	cast, ok := pc.Recv.(*ast.Cast)
	if !ok {
		t.Fatalf("expected Cast receiver, got %T", pc.Recv)
	}
	if cast.Type.String() != "SubType" {
		t.Errorf("expected cast type 'SubType', got %q", cast.Type.String())
	}
}

func TestAggregate(t *testing.T) {
	mod := mustParse(t, `predicate foo() { count(int x | x > 0) > 5 }`)
	pred := mod.Predicates[0]
	body := *pred.Body
	comp, ok := body.(*ast.Comparison)
	if !ok {
		t.Fatalf("expected Comparison, got %T", body)
	}
	agg, ok := comp.Left.(*ast.Aggregate)
	if !ok {
		t.Fatalf("expected Aggregate, got %T", comp.Left)
	}
	if agg.Op != "count" {
		t.Errorf("expected op 'count', got %q", agg.Op)
	}
	if len(agg.Decls) != 1 {
		t.Fatalf("expected 1 decl, got %d", len(agg.Decls))
	}
}

func TestNegation(t *testing.T) {
	mod := mustParse(t, `predicate foo() { not this.bar() }`)
	pred := mod.Predicates[0]
	body := *pred.Body
	neg, ok := body.(*ast.Negation)
	if !ok {
		t.Fatalf("expected Negation, got %T", body)
	}
	pc, ok := neg.Formula.(*ast.PredicateCall)
	if !ok {
		t.Fatalf("expected PredicateCall inside negation, got %T", neg.Formula)
	}
	if pc.Name != "bar" {
		t.Errorf("expected method name 'bar', got %q", pc.Name)
	}
}

func TestConjunctionDisjunctionPrecedence(t *testing.T) {
	// "a or b and c" should parse as "a or (b and c)"
	mod := mustParse(t, `predicate foo() { a() or b() and c() }`)
	pred := mod.Predicates[0]
	body := *pred.Body
	disj, ok := body.(*ast.Disjunction)
	if !ok {
		t.Fatalf("expected Disjunction at top, got %T", body)
	}
	_, ok = disj.Left.(*ast.PredicateCall)
	if !ok {
		t.Fatalf("expected PredicateCall on left of or, got %T", disj.Left)
	}
	conj, ok := disj.Right.(*ast.Conjunction)
	if !ok {
		t.Fatalf("expected Conjunction on right of or, got %T", disj.Right)
	}
	_ = conj
}

func TestNoneFormula(t *testing.T) {
	mod := mustParse(t, `predicate foo() { none() }`)
	pred := mod.Predicates[0]
	body := *pred.Body
	_, ok := body.(*ast.None)
	if !ok {
		t.Fatalf("expected None, got %T", body)
	}
}

func TestAnyFormula(t *testing.T) {
	mod := mustParse(t, `predicate foo() { any() }`)
	pred := mod.Predicates[0]
	body := *pred.Body
	_, ok := body.(*ast.Any)
	if !ok {
		t.Fatalf("expected Any, got %T", body)
	}
}

func TestInstanceOf(t *testing.T) {
	mod := mustParse(t, `predicate foo() { x instanceof Foo }`)
	pred := mod.Predicates[0]
	body := *pred.Body
	io, ok := body.(*ast.InstanceOf)
	if !ok {
		t.Fatalf("expected InstanceOf, got %T", body)
	}
	if io.Type.String() != "Foo" {
		t.Errorf("expected type Foo, got %q", io.Type.String())
	}
}

func TestBoolLiterals(t *testing.T) {
	mod := mustParse(t, `select true, false`)
	if mod.Select == nil {
		t.Fatal("expected select")
	}
	if len(mod.Select.Select) != 2 {
		t.Fatalf("expected 2 select exprs, got %d", len(mod.Select.Select))
	}
	b1, ok := mod.Select.Select[0].(*ast.BoolLiteral)
	if !ok || !b1.Value {
		t.Error("expected true")
	}
	b2, ok := mod.Select.Select[1].(*ast.BoolLiteral)
	if !ok || b2.Value {
		t.Error("expected false")
	}
}

func TestArithmeticPrecedence(t *testing.T) {
	// "1 + 2 * 3" should parse as "1 + (2 * 3)"
	mod := mustParse(t, `select 1 + 2 * 3`)
	expr := mod.Select.Select[0]
	add, ok := expr.(*ast.BinaryExpr)
	if !ok {
		t.Fatalf("expected BinaryExpr, got %T", expr)
	}
	if add.Op != "+" {
		t.Errorf("expected op '+', got %q", add.Op)
	}
	mul, ok := add.Right.(*ast.BinaryExpr)
	if !ok {
		t.Fatalf("expected BinaryExpr on right, got %T", add.Right)
	}
	if mul.Op != "*" {
		t.Errorf("expected op '*', got %q", mul.Op)
	}
}

func TestStringLiteralExpr(t *testing.T) {
	mod := mustParse(t, `select "hello world"`)
	expr := mod.Select.Select[0]
	s, ok := expr.(*ast.StringLiteral)
	if !ok {
		t.Fatalf("expected StringLiteral, got %T", expr)
	}
	if s.Value != "hello world" {
		t.Errorf("expected 'hello world', got %q", s.Value)
	}
}

func TestParenthesisedFormula(t *testing.T) {
	mod := mustParse(t, `predicate foo() { (a() or b()) and c() }`)
	pred := mod.Predicates[0]
	body := *pred.Body
	conj, ok := body.(*ast.Conjunction)
	if !ok {
		t.Fatalf("expected Conjunction at top, got %T", body)
	}
	_, ok = conj.Left.(*ast.Disjunction)
	if !ok {
		t.Fatalf("expected Disjunction on left of and, got %T", conj.Left)
	}
}

func TestMultipleImports(t *testing.T) {
	mod := mustParse(t, `import javascript
import DataFlow::PathGraph as DFP`)
	if len(mod.Imports) != 2 {
		t.Fatalf("expected 2 imports, got %d", len(mod.Imports))
	}
	if mod.Imports[1].Path != "DataFlow::PathGraph" {
		t.Errorf("expected DataFlow::PathGraph, got %q", mod.Imports[1].Path)
	}
	if mod.Imports[1].Alias != "DFP" {
		t.Errorf("expected alias DFP, got %q", mod.Imports[1].Alias)
	}
}

func TestPredicateEmptyBody(t *testing.T) {
	mod := mustParse(t, `predicate foo() { }`)
	if len(mod.Predicates) != 1 {
		t.Fatalf("expected 1 predicate, got %d", len(mod.Predicates))
	}
	if mod.Predicates[0].Body != nil {
		t.Error("expected nil body for empty predicate")
	}
}

func TestPredicateMultipleParams(t *testing.T) {
	mod := mustParse(t, `predicate foo(int x, string y, DataFlow::Node z) { x > 0 }`)
	params := mod.Predicates[0].Params
	if len(params) != 3 {
		t.Fatalf("expected 3 params, got %d", len(params))
	}
	if params[0].Type.String() != "int" {
		t.Errorf("param 0: expected int, got %q", params[0].Type.String())
	}
	if params[1].Type.String() != "string" {
		t.Errorf("param 1: expected string, got %q", params[1].Type.String())
	}
	if params[2].Type.String() != "DataFlow::Node" {
		t.Errorf("param 2: expected DataFlow::Node, got %q", params[2].Type.String())
	}
}

func TestFieldAccess(t *testing.T) {
	mod := mustParse(t, `select this.name`)
	expr := mod.Select.Select[0]
	fa, ok := expr.(*ast.FieldAccess)
	if !ok {
		t.Fatalf("expected FieldAccess, got %T", expr)
	}
	if fa.Field != "name" {
		t.Errorf("expected field 'name', got %q", fa.Field)
	}
}

func TestFunctionCallAsFormula(t *testing.T) {
	mod := mustParse(t, `predicate foo() { bar(1, 2) }`)
	pred := mod.Predicates[0]
	body := *pred.Body
	pc, ok := body.(*ast.PredicateCall)
	if !ok {
		t.Fatalf("expected PredicateCall, got %T", body)
	}
	if pc.Name != "bar" {
		t.Errorf("expected name 'bar', got %q", pc.Name)
	}
	if len(pc.Args) != 2 {
		t.Fatalf("expected 2 args, got %d", len(pc.Args))
	}
}

func TestResultAssignment(t *testing.T) {
	mod := mustParse(t, `string foo() { result = "hello" }`)
	pred := mod.Predicates[0]
	body := *pred.Body
	comp, ok := body.(*ast.Comparison)
	if !ok {
		t.Fatalf("expected Comparison, got %T", body)
	}
	if comp.Op != "=" {
		t.Errorf("expected op '=', got %q", comp.Op)
	}
	v, ok := comp.Left.(*ast.Variable)
	if !ok {
		t.Fatalf("expected Variable, got %T", comp.Left)
	}
	if v.Name != "result" {
		t.Errorf("expected 'result', got %q", v.Name)
	}
}

func TestMultipleSelectExprs(t *testing.T) {
	mod := mustParse(t, `from int x, int y where x > 0 select x, y`)
	if len(mod.Select.Decls) != 2 {
		t.Fatalf("expected 2 decls, got %d", len(mod.Select.Decls))
	}
	if len(mod.Select.Select) != 2 {
		t.Fatalf("expected 2 select exprs, got %d", len(mod.Select.Select))
	}
}

func TestClassMultipleSuperTypes(t *testing.T) {
	src := `class Foo extends Bar, Baz { Foo() { } }`
	mod := mustParse(t, src)
	cls := mod.Classes[0]
	if len(cls.SuperTypes) != 2 {
		t.Fatalf("expected 2 supertypes, got %d", len(cls.SuperTypes))
	}
	if cls.SuperTypes[0].String() != "Bar" {
		t.Errorf("expected Bar, got %q", cls.SuperTypes[0].String())
	}
	if cls.SuperTypes[1].String() != "Baz" {
		t.Errorf("expected Baz, got %q", cls.SuperTypes[1].String())
	}
}

func TestExistsWithGuard(t *testing.T) {
	mod := mustParse(t, `predicate foo() { exists(int y | y > 0 | y < 10) }`)
	pred := mod.Predicates[0]
	body := *pred.Body
	ex, ok := body.(*ast.Exists)
	if !ok {
		t.Fatalf("expected Exists, got %T", body)
	}
	if ex.Guard == nil {
		t.Error("expected guard, got nil")
	}
	if ex.Body == nil {
		t.Error("expected body, got nil")
	}
}

func TestComparisonOperators(t *testing.T) {
	ops := []struct {
		src string
		op  string
	}{
		{`predicate foo() { x = 0 }`, "="},
		{`predicate foo() { x != 0 }`, "!="},
		{`predicate foo() { x < 0 }`, "<"},
		{`predicate foo() { x <= 0 }`, "<="},
		{`predicate foo() { x > 0 }`, ">"},
		{`predicate foo() { x >= 0 }`, ">="},
	}
	for _, tc := range ops {
		mod := mustParse(t, tc.src)
		body := *mod.Predicates[0].Body
		comp, ok := body.(*ast.Comparison)
		if !ok {
			t.Fatalf("op %q: expected Comparison, got %T", tc.op, body)
		}
		if comp.Op != tc.op {
			t.Errorf("expected op %q, got %q", tc.op, comp.Op)
		}
	}
}

// --- Error cases ---

func TestErrorUnexpectedToken(t *testing.T) {
	mustFail(t, `predicate { }`)
}

func TestErrorUnclosedBrace(t *testing.T) {
	mustFail(t, `predicate foo() {`)
}

func TestErrorUnclosedParen(t *testing.T) {
	mustFail(t, `predicate foo( { }`)
}

func TestErrorBadTopLevel(t *testing.T) {
	mustFail(t, `+ +`)
}

func TestErrorParseErrorLocation(t *testing.T) {
	p := parse.NewParser("predicate { }", "test.ql")
	_, err := p.Parse()
	if err == nil {
		t.Fatal("expected error")
	}
	pe, ok := err.(*parse.ParseError)
	if !ok {
		t.Fatalf("expected *ParseError, got %T", err)
	}
	if pe.File != "test.ql" {
		t.Errorf("expected file 'test.ql', got %q", pe.File)
	}
	if pe.Line != 1 {
		t.Errorf("expected line 1, got %d", pe.Line)
	}
}

func TestFullModule(t *testing.T) {
	src := `
import javascript
import DataFlow::PathGraph as DFP

class MyClass extends DataFlow::Node {
	MyClass() { this.toString() != "" }
	override predicate hasFlow() {
		exists(DataFlow::Node src | src instanceof MyClass)
	}
}

predicate isInteresting(DataFlow::Node n) {
	not n.toString() = ""
	and n instanceof MyClass
}

from DataFlow::Node n
where isInteresting(n)
select n as "node"
`
	mod := mustParse(t, src)
	if len(mod.Imports) != 2 {
		t.Errorf("expected 2 imports, got %d", len(mod.Imports))
	}
	if len(mod.Classes) != 1 {
		t.Errorf("expected 1 class, got %d", len(mod.Classes))
	}
	if len(mod.Predicates) != 1 {
		t.Errorf("expected 1 predicate, got %d", len(mod.Predicates))
	}
	if mod.Select == nil {
		t.Error("expected select clause, got nil")
	}
}
