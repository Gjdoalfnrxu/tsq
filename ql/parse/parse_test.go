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

func TestLex_UnterminatedBlockComment(t *testing.T) {
	l := parse.NewLexer("foo /* unterminated", "test.ql")
	// First token should be the identifier "foo"
	tok := l.Next()
	if tok.Type != parse.TokIdent || tok.Lit != "foo" {
		t.Errorf("expected ident 'foo', got type=%d lit=%q", tok.Type, tok.Lit)
	}
	// Second token must be TokError, not TokEOF
	tok = l.Next()
	if tok.Type != parse.TokError {
		t.Errorf("expected TokError for unterminated block comment, got type=%d lit=%q", tok.Type, tok.Lit)
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
	pe, ok := err.(*parse.Error)
	if !ok {
		t.Fatalf("expected *parse.Error, got %T", err)
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

// TestAtTypeWithKeywordName regression: @extends should parse as a type reference
// even though "extends" is a keyword. This was broken before the parser fix.
func TestAtTypeWithKeywordName(t *testing.T) {
	src := `class Extends extends @extends {
    Extends() { Extends(this, _) }
    int getChild() { result = this }
}`
	mod := mustParse(t, src)
	if len(mod.Classes) != 1 {
		t.Fatalf("expected 1 class, got %d", len(mod.Classes))
	}
	cls := mod.Classes[0]
	if cls.Name != "Extends" {
		t.Errorf("expected class name 'Extends', got %q", cls.Name)
	}
	if len(cls.SuperTypes) != 1 {
		t.Fatalf("expected 1 super type, got %d", len(cls.SuperTypes))
	}
	if cls.SuperTypes[0].String() != "@extends" {
		t.Errorf("expected super type '@extends', got %q", cls.SuperTypes[0].String())
	}
}

// TestAtTypeWithOtherKeywords verifies @from, @select, @class also work.
func TestAtTypeWithOtherKeywords(t *testing.T) {
	keywords := []string{"from", "select", "class", "where", "as", "import", "override"}
	for _, kw := range keywords {
		t.Run(kw, func(t *testing.T) {
			src := `class Foo extends @` + kw + ` {
    Foo() { Foo(this) }
}`
			mod := mustParse(t, src)
			if len(mod.Classes) != 1 {
				t.Fatalf("expected 1 class, got %d", len(mod.Classes))
			}
			if mod.Classes[0].SuperTypes[0].String() != "@"+kw {
				t.Errorf("expected @%s, got %s", kw, mod.Classes[0].SuperTypes[0].String())
			}
		})
	}
}

// --- Phase 1a: Module declarations ---

func TestModuleDeclaration(t *testing.T) {
	src := `module DataFlow {
		class Node extends @node {
			Node() { any() }
		}
		predicate isSource(Node n) { any() }
	}`
	mod := mustParse(t, src)
	if len(mod.Modules) != 1 {
		t.Fatalf("expected 1 module, got %d", len(mod.Modules))
	}
	m := mod.Modules[0]
	if m.Name != "DataFlow" {
		t.Errorf("expected module name 'DataFlow', got %q", m.Name)
	}
	if len(m.Classes) != 1 {
		t.Fatalf("expected 1 class in module, got %d", len(m.Classes))
	}
	if m.Classes[0].Name != "Node" {
		t.Errorf("expected class name 'Node', got %q", m.Classes[0].Name)
	}
	if len(m.Predicates) != 1 {
		t.Fatalf("expected 1 predicate in module, got %d", len(m.Predicates))
	}
	if m.Predicates[0].Name != "isSource" {
		t.Errorf("expected predicate name 'isSource', got %q", m.Predicates[0].Name)
	}
}

func TestNestedModuleDeclaration(t *testing.T) {
	src := `module Outer {
		module Inner {
			class Foo extends @foo { Foo() { any() } }
		}
	}`
	mod := mustParse(t, src)
	if len(mod.Modules) != 1 {
		t.Fatalf("expected 1 module, got %d", len(mod.Modules))
	}
	outer := mod.Modules[0]
	if len(outer.Modules) != 1 {
		t.Fatalf("expected 1 nested module, got %d", len(outer.Modules))
	}
	inner := outer.Modules[0]
	if inner.Name != "Inner" {
		t.Errorf("expected nested module name 'Inner', got %q", inner.Name)
	}
	if len(inner.Classes) != 1 {
		t.Fatalf("expected 1 class in nested module, got %d", len(inner.Classes))
	}
}

func TestQualifiedAccessInQuery(t *testing.T) {
	src := `module DataFlow {
		class Node extends @node { Node() { any() } }
	}
	from DataFlow::Node n
	select n`
	mod := mustParse(t, src)
	if mod.Select == nil {
		t.Fatal("expected select clause")
	}
	if len(mod.Select.Decls) != 1 {
		t.Fatalf("expected 1 decl, got %d", len(mod.Select.Decls))
	}
	if mod.Select.Decls[0].Type.String() != "DataFlow::Node" {
		t.Errorf("expected type DataFlow::Node, got %q", mod.Select.Decls[0].Type.String())
	}
}

// --- Phase 1d: Abstract classes ---

func TestAbstractClass(t *testing.T) {
	src := `abstract class Foo extends Bar { Foo() { any() } }`
	mod := mustParse(t, src)
	if len(mod.Classes) != 1 {
		t.Fatalf("expected 1 class, got %d", len(mod.Classes))
	}
	cls := mod.Classes[0]
	if !cls.IsAbstract {
		t.Error("expected IsAbstract=true")
	}
	if cls.Name != "Foo" {
		t.Errorf("expected name 'Foo', got %q", cls.Name)
	}
}

func TestAbstractClassInModule(t *testing.T) {
	src := `module M {
		abstract class Base extends @base { Base() { any() } }
		class Concrete extends Base { Concrete() { any() } }
	}`
	mod := mustParse(t, src)
	if len(mod.Modules) != 1 {
		t.Fatalf("expected 1 module, got %d", len(mod.Modules))
	}
	m := mod.Modules[0]
	if len(m.Classes) != 2 {
		t.Fatalf("expected 2 classes in module, got %d", len(m.Classes))
	}
	if !m.Classes[0].IsAbstract {
		t.Error("expected first class to be abstract")
	}
	if m.Classes[1].IsAbstract {
		t.Error("expected second class to NOT be abstract")
	}
}

// --- Lexer: module and private keywords ---

func TestLexerModulePrivateKeywords(t *testing.T) {
	l := parse.NewLexer("module private", "test.ql")
	tok := l.Next()
	if tok.Type != parse.TokKwModule {
		t.Errorf("expected TokKwModule, got %d (lit=%q)", tok.Type, tok.Lit)
	}
	tok = l.Next()
	if tok.Type != parse.TokKwPrivate {
		t.Errorf("expected TokKwPrivate, got %d (lit=%q)", tok.Type, tok.Lit)
	}
}

// --- Phase 1e-1g tests ---

func TestLexerIfThenElseKeywords(t *testing.T) {
	l := parse.NewLexer("if then else", "test.ql")
	expected := []parse.TokenType{parse.TokKwIf, parse.TokKwThen, parse.TokKwElse, parse.TokEOF}
	for i, exp := range expected {
		tok := l.Next()
		if tok.Type != exp {
			t.Errorf("token %d: expected type %d, got %d (lit=%q)", i, exp, tok.Type, tok.Lit)
		}
	}
}

func TestIfThenElse(t *testing.T) {
	mod := mustParse(t, `predicate foo() { if a() then b() else c() }`)
	pred := mod.Predicates[0]
	body := *pred.Body
	ite, ok := body.(*ast.IfThenElse)
	if !ok {
		t.Fatalf("expected IfThenElse, got %T", body)
	}
	condPC, ok := ite.Cond.(*ast.PredicateCall)
	if !ok {
		t.Fatalf("expected PredicateCall for cond, got %T", ite.Cond)
	}
	if condPC.Name != "a" {
		t.Errorf("expected cond name 'a', got %q", condPC.Name)
	}
	thenPC, ok := ite.Then.(*ast.PredicateCall)
	if !ok {
		t.Fatalf("expected PredicateCall for then, got %T", ite.Then)
	}
	if thenPC.Name != "b" {
		t.Errorf("expected then name 'b', got %q", thenPC.Name)
	}
	elsePC, ok := ite.Else.(*ast.PredicateCall)
	if !ok {
		t.Fatalf("expected PredicateCall for else, got %T", ite.Else)
	}
	if elsePC.Name != "c" {
		t.Errorf("expected else name 'c', got %q", elsePC.Name)
	}
}

func TestIfThenElseComplex(t *testing.T) {
	mod := mustParse(t, `predicate foo() { if a() and b() then c() else d() }`)
	pred := mod.Predicates[0]
	body := *pred.Body
	ite, ok := body.(*ast.IfThenElse)
	if !ok {
		t.Fatalf("expected IfThenElse, got %T", body)
	}
	_, ok = ite.Cond.(*ast.Conjunction)
	if !ok {
		t.Fatalf("expected Conjunction for cond, got %T", ite.Cond)
	}
}

func TestClosureCallPlus(t *testing.T) {
	mod := mustParse(t, `predicate foo() { reaches+(x, y) }`)
	pred := mod.Predicates[0]
	body := *pred.Body
	cc, ok := body.(*ast.ClosureCall)
	if !ok {
		t.Fatalf("expected ClosureCall, got %T", body)
	}
	if cc.Name != "reaches" {
		t.Errorf("expected name 'reaches', got %q", cc.Name)
	}
	if !cc.Plus {
		t.Error("expected Plus=true")
	}
	if len(cc.Args) != 2 {
		t.Fatalf("expected 2 args, got %d", len(cc.Args))
	}
}

func TestClosureCallStar(t *testing.T) {
	mod := mustParse(t, `predicate foo() { reaches*(x, y) }`)
	pred := mod.Predicates[0]
	body := *pred.Body
	cc, ok := body.(*ast.ClosureCall)
	if !ok {
		t.Fatalf("expected ClosureCall, got %T", body)
	}
	if cc.Name != "reaches" {
		t.Errorf("expected name 'reaches', got %q", cc.Name)
	}
	if cc.Plus {
		t.Error("expected Plus=false for star closure")
	}
}

func TestClosureCallInConjunction(t *testing.T) {
	mod := mustParse(t, `predicate foo() { reaches+(x, y) and y = 42 }`)
	pred := mod.Predicates[0]
	body := *pred.Body
	conj, ok := body.(*ast.Conjunction)
	if !ok {
		t.Fatalf("expected Conjunction, got %T", body)
	}
	_, ok = conj.Left.(*ast.ClosureCall)
	if !ok {
		t.Fatalf("expected ClosureCall on left, got %T", conj.Left)
	}
}

// --- Phase 1h: Additional aggregates ---

func TestParseConcat(t *testing.T) {
	mod := mustParse(t, `
		from string s
		where s = concat(", " | string v | v = "a" | v)
		select s
	`)
	if mod.Select == nil {
		t.Fatal("expected select clause")
	}
}

func TestParseConcatNoSep(t *testing.T) {
	mod := mustParse(t, `
		from string s
		where s = concat(string v | v = "a" | v)
		select s
	`)
	if mod.Select == nil {
		t.Fatal("expected select clause")
	}
}

func TestParseStrictcount(t *testing.T) {
	mod := mustParse(t, `
		predicate foo(int n) {
			n = strictcount(int v | v = 1)
		}
	`)
	pred := mod.Predicates[0]
	body := *pred.Body
	cmp, ok := body.(*ast.Comparison)
	if !ok {
		t.Fatalf("expected Comparison, got %T", body)
	}
	agg, ok := cmp.Right.(*ast.Aggregate)
	if !ok {
		t.Fatalf("expected Aggregate, got %T", cmp.Right)
	}
	if agg.Op != "strictcount" {
		t.Errorf("expected op 'strictcount', got %q", agg.Op)
	}
}

func TestParseStrictsum(t *testing.T) {
	mod := mustParse(t, `
		predicate foo(int n) {
			n = strictsum(int v | v = 1 | v)
		}
	`)
	pred := mod.Predicates[0]
	body := *pred.Body
	cmp, ok := body.(*ast.Comparison)
	if !ok {
		t.Fatalf("expected Comparison, got %T", body)
	}
	agg, ok := cmp.Right.(*ast.Aggregate)
	if !ok {
		t.Fatalf("expected Aggregate, got %T", cmp.Right)
	}
	if agg.Op != "strictsum" {
		t.Errorf("expected op 'strictsum', got %q", agg.Op)
	}
}

func TestParseRank(t *testing.T) {
	mod := mustParse(t, `
		predicate foo(int n) {
			n = rank(int v | v = 1 | v)
		}
	`)
	pred := mod.Predicates[0]
	body := *pred.Body
	cmp, ok := body.(*ast.Comparison)
	if !ok {
		t.Fatalf("expected Comparison, got %T", body)
	}
	agg, ok := cmp.Right.(*ast.Aggregate)
	if !ok {
		t.Fatalf("expected Aggregate, got %T", cmp.Right)
	}
	if agg.Op != "rank" {
		t.Errorf("expected op 'rank', got %q", agg.Op)
	}
}

// --- Phase 1i: forex ---

func TestParseForex(t *testing.T) {
	mod := mustParse(t, `
		predicate allPositive() {
			forex(int x | x > 0 | x < 100)
		}
	`)
	pred := mod.Predicates[0]
	body := *pred.Body
	fx, ok := body.(*ast.Forex)
	if !ok {
		t.Fatalf("expected Forex, got %T", body)
	}
	if len(fx.Decls) != 1 {
		t.Errorf("expected 1 decl, got %d", len(fx.Decls))
	}
	if fx.Decls[0].Name != "x" {
		t.Errorf("expected decl name 'x', got %q", fx.Decls[0].Name)
	}
}

// --- Phase 1j: super ---

func TestParseSuperMethodCall(t *testing.T) {
	mod := mustParse(t, `
		class A extends Base {
			override int getValue() {
				result = super.getValue()
			}
		}
	`)
	cls := mod.Classes[0]
	if len(cls.Members) != 1 {
		t.Fatalf("expected 1 member, got %d", len(cls.Members))
	}
	body := *cls.Members[0].Body
	cmp, ok := body.(*ast.Comparison)
	if !ok {
		t.Fatalf("expected Comparison, got %T", body)
	}
	mc, ok := cmp.Right.(*ast.MethodCall)
	if !ok {
		t.Fatalf("expected MethodCall, got %T", cmp.Right)
	}
	recv, ok := mc.Recv.(*ast.Variable)
	if !ok {
		t.Fatalf("expected Variable recv, got %T", mc.Recv)
	}
	if recv.Name != "super" {
		t.Errorf("expected recv name 'super', got %q", recv.Name)
	}
	if mc.Method != "getValue" {
		t.Errorf("expected method 'getValue', got %q", mc.Method)
	}
}

// --- Phase 1k: Multiple inheritance (parser already supports comma-separated extends) ---

func TestParseMultipleInheritance(t *testing.T) {
	mod := mustParse(t, `
		class C extends A, B {
			C() { this instanceof A }
		}
	`)
	cls := mod.Classes[0]
	if len(cls.SuperTypes) != 2 {
		t.Fatalf("expected 2 supertypes, got %d", len(cls.SuperTypes))
	}
	if cls.SuperTypes[0].String() != "A" || cls.SuperTypes[1].String() != "B" {
		t.Errorf("expected supertypes [A, B], got [%s, %s]", cls.SuperTypes[0], cls.SuperTypes[1])
	}
}

// --- Phase 1l: Annotations ---

func TestParsePrivateAnnotation(t *testing.T) {
	mod := mustParse(t, `
		private predicate helper(int x) { x = 1 }
	`)
	pred := mod.Predicates[0]
	if len(pred.Annotations) != 1 {
		t.Fatalf("expected 1 annotation, got %d", len(pred.Annotations))
	}
	if pred.Annotations[0].Name != "private" {
		t.Errorf("expected annotation 'private', got %q", pred.Annotations[0].Name)
	}
}

func TestParseDeprecatedAnnotation(t *testing.T) {
	mod := mustParse(t, `
		deprecated predicate old(int x) { x = 1 }
	`)
	pred := mod.Predicates[0]
	if len(pred.Annotations) != 1 {
		t.Fatalf("expected 1 annotation, got %d", len(pred.Annotations))
	}
	if pred.Annotations[0].Name != "deprecated" {
		t.Errorf("expected annotation 'deprecated', got %q", pred.Annotations[0].Name)
	}
}

func TestParsePragmaAnnotation(t *testing.T) {
	mod := mustParse(t, `
		pragma[inline]
		predicate helper(int x) { x = 1 }
	`)
	pred := mod.Predicates[0]
	if len(pred.Annotations) != 1 {
		t.Fatalf("expected 1 annotation, got %d", len(pred.Annotations))
	}
	ann := pred.Annotations[0]
	if ann.Name != "pragma" {
		t.Errorf("expected annotation 'pragma', got %q", ann.Name)
	}
	if len(ann.Args) != 1 || ann.Args[0] != "inline" {
		t.Errorf("expected args [inline], got %v", ann.Args)
	}
}

func TestParseBindingsetAnnotation(t *testing.T) {
	mod := mustParse(t, `
		bindingset[x, y]
		predicate helper(int x, int y) { x = y }
	`)
	pred := mod.Predicates[0]
	if len(pred.Annotations) != 1 {
		t.Fatalf("expected 1 annotation, got %d", len(pred.Annotations))
	}
	ann := pred.Annotations[0]
	if ann.Name != "bindingset" {
		t.Errorf("expected annotation 'bindingset', got %q", ann.Name)
	}
	if len(ann.Args) != 2 || ann.Args[0] != "x" || ann.Args[1] != "y" {
		t.Errorf("expected args [x, y], got %v", ann.Args)
	}
}

func TestParseLanguageAnnotation(t *testing.T) {
	mod := mustParse(t, `
		language[monotonicAggregates]
		predicate helper(int x) { x = 1 }
	`)
	pred := mod.Predicates[0]
	if len(pred.Annotations) != 1 {
		t.Fatalf("expected 1 annotation, got %d", len(pred.Annotations))
	}
	ann := pred.Annotations[0]
	if ann.Name != "language" {
		t.Errorf("expected annotation 'language', got %q", ann.Name)
	}
	if len(ann.Args) != 1 || ann.Args[0] != "monotonicAggregates" {
		t.Errorf("expected args [monotonicAggregates], got %v", ann.Args)
	}
}

func TestParseMultipleAnnotations(t *testing.T) {
	mod := mustParse(t, `
		private deprecated
		predicate helper(int x) { x = 1 }
	`)
	pred := mod.Predicates[0]
	if len(pred.Annotations) != 2 {
		t.Fatalf("expected 2 annotations, got %d", len(pred.Annotations))
	}
	if pred.Annotations[0].Name != "private" {
		t.Errorf("expected first annotation 'private', got %q", pred.Annotations[0].Name)
	}
	if pred.Annotations[1].Name != "deprecated" {
		t.Errorf("expected second annotation 'deprecated', got %q", pred.Annotations[1].Name)
	}
}

func TestParseMemberAnnotation(t *testing.T) {
	mod := mustParse(t, `
		class Foo extends Bar {
			private predicate helper() { none() }
		}
	`)
	cls := mod.Classes[0]
	if len(cls.Members) != 1 {
		t.Fatalf("expected 1 member, got %d", len(cls.Members))
	}
	member := cls.Members[0]
	if len(member.Annotations) != 1 {
		t.Fatalf("expected 1 annotation on member, got %d", len(member.Annotations))
	}
	if member.Annotations[0].Name != "private" {
		t.Errorf("expected annotation 'private', got %q", member.Annotations[0].Name)
	}
}

func TestLexerBrackets(t *testing.T) {
	l := parse.NewLexer("[ ]", "test.ql")
	tok := l.Next()
	if tok.Type != parse.TokLBrack {
		t.Errorf("expected TokLBrack, got %d", tok.Type)
	}
	tok = l.Next()
	if tok.Type != parse.TokRBrack {
		t.Errorf("expected TokRBrack, got %d", tok.Type)
	}
}

// TestParseDeprecatedClassAnnotation verifies that deprecated annotations
// on class declarations are preserved through parsing (regression test:
// parser previously dropped annotations for class declarations).
func TestParseDeprecatedClassAnnotation(t *testing.T) {
	src := `deprecated class OldThing extends @node { }`
	p := parse.NewParser(src, "test.ql")
	mod, err := p.Parse()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(mod.Classes) != 1 {
		t.Fatalf("expected 1 class, got %d", len(mod.Classes))
	}
	cls := mod.Classes[0]
	if len(cls.Annotations) == 0 {
		t.Fatal("expected deprecated annotation on class, got none")
	}
	found := false
	for _, a := range cls.Annotations {
		if a.Name == "deprecated" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'deprecated' annotation, got %v", cls.Annotations)
	}
}
