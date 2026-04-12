package resolve_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/ast"
	"github.com/Gjdoalfnrxu/tsq/ql/resolve"
)

// helpers

func span() ast.Span { return ast.Span{File: "test.ql", StartLine: 1, StartCol: 1} }

func typeRef(name string) ast.TypeRef {
	return ast.TypeRef{Path: []string{name}, Span: span()}
}

func varExpr(name string) *ast.Variable {
	return &ast.Variable{BaseExpr: ast.BaseExpr{Span: span()}, Name: name}
}

func noErrors(t *testing.T, rm *resolve.ResolvedModule) {
	t.Helper()
	for _, e := range rm.Errors {
		t.Errorf("unexpected error: %s", e.Message)
	}
}

func hasError(t *testing.T, rm *resolve.ResolvedModule, substr string) {
	t.Helper()
	for _, e := range rm.Errors {
		if strings.Contains(e.Message, substr) {
			return
		}
	}
	t.Errorf("expected error containing %q; got errors: %v", substr, rm.Errors)
}

// ---- Tests ----

// Resolve module with one class → class in env.
func TestResolveOneClass(t *testing.T) {
	mod := &ast.Module{
		Classes: []ast.ClassDecl{
			{Name: "Foo", Span: span()},
		},
	}
	rm, err := resolve.Resolve(mod, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	noErrors(t, rm)
	if _, ok := rm.Env.Classes["Foo"]; !ok {
		t.Error("expected Foo in env.Classes")
	}
}

// Resolve module with predicate calling another predicate → no errors, predicate in env.
func TestResolvePredCallsPred(t *testing.T) {
	body := ast.Formula(&ast.PredicateCall{
		BaseFormula: ast.BaseFormula{Span: span()},
		Name:        "bar",
		Args:        []ast.Expr{},
	})
	mod := &ast.Module{
		Predicates: []ast.PredicateDecl{
			{Name: "bar", Body: nil, Span: span()},
			{
				Name: "foo",
				Body: &body,
				Span: span(),
			},
		},
	}
	rm, err := resolve.Resolve(mod, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	noErrors(t, rm)
}

// Class extends another class → supertype resolved, no errors.
func TestClassExtendsClass(t *testing.T) {
	mod := &ast.Module{
		Classes: []ast.ClassDecl{
			{Name: "Base", Span: span()},
			{
				Name:       "Derived",
				SuperTypes: []ast.TypeRef{typeRef("Base")},
				Span:       span(),
			},
		},
	}
	rm, err := resolve.Resolve(mod, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	noErrors(t, rm)
}

// `this` inside class body → valid.
func TestThisInsideClass(t *testing.T) {
	thisVar := varExpr("this")
	body := ast.Formula(&ast.Comparison{
		BaseFormula: ast.BaseFormula{Span: span()},
		Left:        thisVar,
		Right:       &ast.IntLiteral{BaseExpr: ast.BaseExpr{Span: span()}, Value: 1},
		Op:          "=",
	})
	mod := &ast.Module{
		Classes: []ast.ClassDecl{
			{
				Name:     "Foo",
				CharPred: &body,
				Span:     span(),
			},
		},
	}
	rm, err := resolve.Resolve(mod, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	noErrors(t, rm)
}

// `this` outside class body → ResolveError.
func TestThisOutsideClass(t *testing.T) {
	thisVar := varExpr("this")
	body := ast.Formula(&ast.Comparison{
		BaseFormula: ast.BaseFormula{Span: span()},
		Left:        thisVar,
		Right:       &ast.IntLiteral{BaseExpr: ast.BaseExpr{Span: span()}, Value: 1},
		Op:          "=",
	})
	mod := &ast.Module{
		Predicates: []ast.PredicateDecl{
			{Name: "foo", Body: &body, Span: span()},
		},
	}
	rm, _ := resolve.Resolve(mod, nil)
	hasError(t, rm, "`this` used outside a class body")
}

// `result` inside method with return type → valid.
func TestResultInsideMethodWithReturnType(t *testing.T) {
	resultVar := varExpr("result")
	body := ast.Formula(&ast.Comparison{
		BaseFormula: ast.BaseFormula{Span: span()},
		Left:        resultVar,
		Right:       &ast.IntLiteral{BaseExpr: ast.BaseExpr{Span: span()}, Value: 42},
		Op:          "=",
	})
	rt := typeRef("int")
	mod := &ast.Module{
		Classes: []ast.ClassDecl{
			{
				Name: "Foo",
				Members: []ast.MemberDecl{
					{
						Name:       "getValue",
						ReturnType: &rt,
						Body:       &body,
						Span:       span(),
					},
				},
				Span: span(),
			},
		},
	}
	rm, err := resolve.Resolve(mod, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	noErrors(t, rm)
}

// `result` in predicate without return type → ResolveError.
func TestResultInPredicateWithoutReturnType(t *testing.T) {
	resultVar := varExpr("result")
	body := ast.Formula(&ast.Comparison{
		BaseFormula: ast.BaseFormula{Span: span()},
		Left:        resultVar,
		Right:       &ast.IntLiteral{BaseExpr: ast.BaseExpr{Span: span()}, Value: 42},
		Op:          "=",
	})
	mod := &ast.Module{
		Predicates: []ast.PredicateDecl{
			{Name: "foo", ReturnType: nil, Body: &body, Span: span()},
		},
	}
	rm, _ := resolve.Resolve(mod, nil)
	hasError(t, rm, "`result` used in predicate/method without a return type")
}

// Undefined predicate name → ResolveError with position.
func TestUndefinedPredicate(t *testing.T) {
	callSpan := ast.Span{File: "test.ql", StartLine: 5, StartCol: 3}
	body := ast.Formula(&ast.PredicateCall{
		BaseFormula: ast.BaseFormula{Span: callSpan},
		Name:        "doesNotExist",
	})
	mod := &ast.Module{
		Predicates: []ast.PredicateDecl{
			{Name: "foo", Body: &body, Span: span()},
		},
	}
	rm, _ := resolve.Resolve(mod, nil)
	hasError(t, rm, "undefined predicate")
	// Check position is propagated.
	if len(rm.Errors) == 0 {
		t.Fatal("no errors")
	}
	found := false
	for _, e := range rm.Errors {
		if e.Pos.StartLine == 5 && e.Pos.StartCol == 3 {
			found = true
		}
	}
	if !found {
		t.Errorf("expected error at 5:3, got %v", rm.Errors)
	}
}

// Undefined class in extends → ResolveError.
func TestUndefinedClassInExtends(t *testing.T) {
	mod := &ast.Module{
		Classes: []ast.ClassDecl{
			{
				Name:       "Child",
				SuperTypes: []ast.TypeRef{typeRef("Ghost")},
				Span:       span(),
			},
		},
	}
	rm, _ := resolve.Resolve(mod, nil)
	hasError(t, rm, "undefined type")
}

// ---- Deprecated annotation warnings ----

// TestDeprecatedPredicateWarning: calling a deprecated predicate emits a warning.
func TestDeprecatedPredicateWarning(t *testing.T) {
	body := ast.Formula(&ast.PredicateCall{
		BaseFormula: ast.BaseFormula{Span: span()},
		Name:        "oldPred",
		Args:        []ast.Expr{},
	})
	mod := &ast.Module{
		Predicates: []ast.PredicateDecl{
			{
				Name: "oldPred",
				Annotations: []ast.Annotation{
					{Name: "deprecated"},
				},
				Span: span(),
			},
			{
				Name: "caller",
				Body: &body,
				Span: span(),
			},
		},
	}
	rm, err := resolve.Resolve(mod, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	noErrors(t, rm)
	if len(rm.Warnings) == 0 {
		t.Fatal("expected a deprecation warning, got none")
	}
	found := false
	for _, w := range rm.Warnings {
		if strings.Contains(w.Message, "oldPred") && strings.Contains(w.Message, "deprecated") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected warning mentioning oldPred and deprecated, got: %v", rm.Warnings)
	}
}

// TestDeprecatedClassWarning: referencing a deprecated class emits a warning.
func TestDeprecatedClassWarning(t *testing.T) {
	mod := &ast.Module{
		Classes: []ast.ClassDecl{
			{
				Name: "OldClass",
				Annotations: []ast.Annotation{
					{Name: "deprecated"},
				},
				Span: span(),
			},
			{
				Name:       "NewClass",
				SuperTypes: []ast.TypeRef{typeRef("OldClass")},
				Span:       span(),
			},
		},
	}
	rm, err := resolve.Resolve(mod, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	noErrors(t, rm)
	if len(rm.Warnings) == 0 {
		t.Fatal("expected a deprecation warning, got none")
	}
	found := false
	for _, w := range rm.Warnings {
		if strings.Contains(w.Message, "OldClass") && strings.Contains(w.Message, "deprecated") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected warning mentioning OldClass and deprecated, got: %v", rm.Warnings)
	}
}

// TestDeprecatedMemberWarning: calling a deprecated member emits a warning.
func TestDeprecatedMemberWarning(t *testing.T) {
	rt := typeRef("string")
	fooClass := ast.ClassDecl{
		Name: "Foo",
		Members: []ast.MemberDecl{
			{
				Name:       "oldMethod",
				ReturnType: &rt,
				Annotations: []ast.Annotation{
					{Name: "deprecated"},
				},
				Span: span(),
			},
		},
		Span: span(),
	}
	xVar := varExpr("x")
	mc := &ast.MethodCall{
		BaseExpr: ast.BaseExpr{Span: span()},
		Recv:     xVar,
		Method:   "oldMethod",
	}
	resultVar := varExpr("result")
	predBody := ast.Formula(&ast.Comparison{
		BaseFormula: ast.BaseFormula{Span: span()},
		Left:        resultVar,
		Right:       mc,
		Op:          "=",
	})
	prt := typeRef("string")
	pred := ast.PredicateDecl{
		Name:       "p",
		ReturnType: &prt,
		Params: []ast.ParamDecl{
			{Type: typeRef("Foo"), Name: "x", Span: span()},
		},
		Body: &predBody,
		Span: span(),
	}
	mod := &ast.Module{
		Classes:    []ast.ClassDecl{fooClass},
		Predicates: []ast.PredicateDecl{pred},
	}
	rm, err := resolve.Resolve(mod, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	noErrors(t, rm)
	if len(rm.Warnings) == 0 {
		t.Fatal("expected a deprecation warning, got none")
	}
	found := false
	for _, w := range rm.Warnings {
		if strings.Contains(w.Message, "oldMethod") && strings.Contains(w.Message, "deprecated") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected warning mentioning oldMethod and deprecated, got: %v", rm.Warnings)
	}
}

// TestNoWarningForUndeprecated: no deprecated annotation means no warnings.
func TestNoWarningForUndeprecated(t *testing.T) {
	body := ast.Formula(&ast.PredicateCall{
		BaseFormula: ast.BaseFormula{Span: span()},
		Name:        "normalPred",
		Args:        []ast.Expr{},
	})
	mod := &ast.Module{
		Predicates: []ast.PredicateDecl{
			{Name: "normalPred", Span: span()},
			{Name: "caller", Body: &body, Span: span()},
		},
	}
	rm, err := resolve.Resolve(mod, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	noErrors(t, rm)
	if len(rm.Warnings) != 0 {
		t.Errorf("expected no warnings, got %d: %v", len(rm.Warnings), rm.Warnings)
	}
}

// Variable not bound by exists/forall → ResolveError.
func TestUnboundVariable(t *testing.T) {
	unbound := varExpr("x")
	body := ast.Formula(&ast.Comparison{
		BaseFormula: ast.BaseFormula{Span: span()},
		Left:        unbound,
		Right:       &ast.IntLiteral{BaseExpr: ast.BaseExpr{Span: span()}, Value: 1},
		Op:          "=",
	})
	mod := &ast.Module{
		Predicates: []ast.PredicateDecl{
			{Name: "foo", Body: &body, Span: span()},
		},
	}
	rm, _ := resolve.Resolve(mod, nil)
	hasError(t, rm, "undefined variable")
}

// Import: mock importLoader returning a pre-built ast.Module → imported predicates accessible.
func TestImportLoader(t *testing.T) {
	importedMod := &ast.Module{
		Predicates: []ast.PredicateDecl{
			{Name: "importedPred", Span: span()},
		},
	}
	loader := func(path string) (*ast.Module, error) {
		if path == "mylib" {
			return importedMod, nil
		}
		return nil, errors.New("not found")
	}
	body := ast.Formula(&ast.PredicateCall{
		BaseFormula: ast.BaseFormula{Span: span()},
		Name:        "importedPred",
	})
	mod := &ast.Module{
		Imports: []ast.ImportDecl{
			{Path: "mylib", Span: span()},
		},
		Predicates: []ast.PredicateDecl{
			{Name: "callIt", Body: &body, Span: span()},
		},
	}
	rm, err := resolve.Resolve(mod, loader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	noErrors(t, rm)
	if _, ok := rm.Env.Imports["mylib"]; !ok {
		t.Error("expected mylib in env.Imports")
	}
}

// Method call x.getY() where x: Foo → resolved to Foo.getY member.
func TestMethodCallResolved(t *testing.T) {
	// Build: Foo class with method getY returning string.
	rt := typeRef("string")
	getY := ast.MemberDecl{
		Name:       "getY",
		ReturnType: &rt,
		Span:       span(),
	}
	fooClass := ast.ClassDecl{
		Name:    "Foo",
		Members: []ast.MemberDecl{getY},
		Span:    span(),
	}

	// predicate: string p(Foo x) { result = x.getY() }
	xVar := varExpr("x")
	mc := &ast.MethodCall{
		BaseExpr: ast.BaseExpr{Span: span()},
		Recv:     xVar,
		Method:   "getY",
	}
	resultVar := varExpr("result")
	predBody := ast.Formula(&ast.Comparison{
		BaseFormula: ast.BaseFormula{Span: span()},
		Left:        resultVar,
		Right:       mc,
		Op:          "=",
	})
	prt := typeRef("string")
	pred := ast.PredicateDecl{
		Name:       "p",
		ReturnType: &prt,
		Params: []ast.ParamDecl{
			{Type: typeRef("Foo"), Name: "x", Span: span()},
		},
		Body: &predBody,
		Span: span(),
	}

	mod := &ast.Module{
		Classes:    []ast.ClassDecl{fooClass},
		Predicates: []ast.PredicateDecl{pred},
	}

	rm, err := resolve.Resolve(mod, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	noErrors(t, rm)

	res, ok := rm.Annotations.ExprResolutions[mc]
	if !ok {
		t.Fatal("expected ExprResolutions entry for method call")
	}
	if res.DeclMember == nil || res.DeclMember.Name != "getY" {
		t.Errorf("expected DeclMember.Name=getY, got %v", res.DeclMember)
	}
	if res.DeclClass == nil || res.DeclClass.Name != "Foo" {
		t.Errorf("expected DeclClass.Name=Foo, got %v", res.DeclClass)
	}
}

// Inherited method: Foo extends Bar, getY on Bar → resolved to Bar.getY.
func TestInheritedMethodResolution(t *testing.T) {
	rt := typeRef("string")
	getY := ast.MemberDecl{
		Name:       "getY",
		ReturnType: &rt,
		Span:       span(),
	}
	barClass := ast.ClassDecl{
		Name:    "Bar",
		Members: []ast.MemberDecl{getY},
		Span:    span(),
	}
	fooClass := ast.ClassDecl{
		Name:       "Foo",
		SuperTypes: []ast.TypeRef{typeRef("Bar")},
		Span:       span(),
	}

	xVar := varExpr("x")
	mc := &ast.MethodCall{
		BaseExpr: ast.BaseExpr{Span: span()},
		Recv:     xVar,
		Method:   "getY",
	}
	resultVar := varExpr("result")
	predBody := ast.Formula(&ast.Comparison{
		BaseFormula: ast.BaseFormula{Span: span()},
		Left:        resultVar,
		Right:       mc,
		Op:          "=",
	})
	prt := typeRef("string")
	pred := ast.PredicateDecl{
		Name:       "p",
		ReturnType: &prt,
		Params: []ast.ParamDecl{
			{Type: typeRef("Foo"), Name: "x", Span: span()},
		},
		Body: &predBody,
		Span: span(),
	}

	mod := &ast.Module{
		Classes:    []ast.ClassDecl{barClass, fooClass},
		Predicates: []ast.PredicateDecl{pred},
	}

	rm, err := resolve.Resolve(mod, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	noErrors(t, rm)

	res, ok := rm.Annotations.ExprResolutions[mc]
	if !ok {
		t.Fatal("expected ExprResolutions entry for inherited method call")
	}
	if res.DeclClass == nil || res.DeclClass.Name != "Bar" {
		t.Errorf("expected DeclClass.Name=Bar (defining class), got %v", res.DeclClass)
	}
	if res.DeclMember == nil || res.DeclMember.Name != "getY" {
		t.Errorf("expected DeclMember.Name=getY, got %v", res.DeclMember)
	}
}

// Multiple errors: module with several issues → all errors collected.
func TestMultipleErrors(t *testing.T) {
	// Two undefined predicates in two different predicates.
	body1 := ast.Formula(&ast.PredicateCall{
		BaseFormula: ast.BaseFormula{Span: span()},
		Name:        "missing1",
	})
	body2 := ast.Formula(&ast.PredicateCall{
		BaseFormula: ast.BaseFormula{Span: span()},
		Name:        "missing2",
	})
	mod := &ast.Module{
		Predicates: []ast.PredicateDecl{
			{Name: "p1", Body: &body1, Span: span()},
			{Name: "p2", Body: &body2, Span: span()},
		},
	}
	rm, _ := resolve.Resolve(mod, nil)
	if len(rm.Errors) < 2 {
		t.Errorf("expected at least 2 errors, got %d: %v", len(rm.Errors), rm.Errors)
	}
}

// Duplicate class declarations → ResolveError.
func TestDuplicateClass(t *testing.T) {
	mod := &ast.Module{
		Classes: []ast.ClassDecl{
			{Name: "Foo", Span: span()},
			{Name: "Foo", Span: span()},
		},
	}
	rm, _ := resolve.Resolve(mod, nil)
	hasError(t, rm, "duplicate class declaration")
}

// Cyclic inheritance A extends B extends A → ResolveError (not infinite loop).
func TestCyclicInheritance(t *testing.T) {
	mod := &ast.Module{
		Classes: []ast.ClassDecl{
			{
				Name:       "A",
				SuperTypes: []ast.TypeRef{typeRef("B")},
				Span:       span(),
			},
			{
				Name:       "B",
				SuperTypes: []ast.TypeRef{typeRef("A")},
				Span:       span(),
			},
		},
	}
	rm, _ := resolve.Resolve(mod, nil)
	hasError(t, rm, "cyclic class inheritance")
}

// Complex query: class with method calls, exists → fully resolved, no errors.
func TestComplexQuery(t *testing.T) {
	// class Node { string getName() { result = "x" } }
	// predicate string findName(Node n) {
	//   exists(Node m | result = m.getName())
	// }
	rt := typeRef("string")
	getName := ast.MemberDecl{
		Name:       "getName",
		ReturnType: &rt,
		Body: func() *ast.Formula {
			resultVar := varExpr("result")
			f := ast.Formula(&ast.Comparison{
				BaseFormula: ast.BaseFormula{Span: span()},
				Left:        resultVar,
				Right:       &ast.StringLiteral{BaseExpr: ast.BaseExpr{Span: span()}, Value: "x"},
				Op:          "=",
			})
			return &f
		}(),
		Span: span(),
	}
	nodeClass := ast.ClassDecl{
		Name:    "Node",
		Members: []ast.MemberDecl{getName},
		Span:    span(),
	}

	// exists body: result = m.getName()
	mVar := varExpr("m")
	mc := &ast.MethodCall{
		BaseExpr: ast.BaseExpr{Span: span()},
		Recv:     mVar,
		Method:   "getName",
	}
	resultVar2 := varExpr("result")
	existsBody := ast.Formula(&ast.Comparison{
		BaseFormula: ast.BaseFormula{Span: span()},
		Left:        resultVar2,
		Right:       mc,
		Op:          "=",
	})
	existsFormula := ast.Formula(&ast.Exists{
		BaseFormula: ast.BaseFormula{Span: span()},
		Decls:       []ast.VarDecl{{Type: typeRef("Node"), Name: "m", Span: span()}},
		Body:        existsBody,
	})
	predRT := typeRef("string")
	pred := ast.PredicateDecl{
		Name:       "findName",
		ReturnType: &predRT,
		Params: []ast.ParamDecl{
			{Type: typeRef("Node"), Name: "n", Span: span()},
		},
		Body: &existsFormula,
		Span: span(),
	}

	mod := &ast.Module{
		Classes:    []ast.ClassDecl{nodeClass},
		Predicates: []ast.PredicateDecl{pred},
	}

	rm, err := resolve.Resolve(mod, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	noErrors(t, rm)

	// Verify method call resolved.
	res, ok := rm.Annotations.ExprResolutions[mc]
	if !ok {
		t.Fatal("expected ExprResolutions entry for m.getName() in exists")
	}
	if res.DeclMember == nil || res.DeclMember.Name != "getName" {
		t.Errorf("expected getName member, got %v", res.DeclMember)
	}
}

// VarBindings: parameter variable is recorded.
func TestVarBindingParameter(t *testing.T) {
	xVar := varExpr("x")
	body := ast.Formula(&ast.Comparison{
		BaseFormula: ast.BaseFormula{Span: span()},
		Left:        xVar,
		Right:       &ast.IntLiteral{BaseExpr: ast.BaseExpr{Span: span()}, Value: 0},
		Op:          "=",
	})
	mod := &ast.Module{
		Predicates: []ast.PredicateDecl{
			{
				Name: "pred",
				Params: []ast.ParamDecl{
					{Type: typeRef("int"), Name: "x", Span: span()},
				},
				Body: &body,
				Span: span(),
			},
		},
	}
	rm, err := resolve.Resolve(mod, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	noErrors(t, rm)
	binding, ok := rm.Annotations.VarBindings[xVar]
	if !ok {
		t.Fatal("expected VarBindings entry for x")
	}
	if binding.Param == nil || binding.Param.Name != "x" {
		t.Errorf("expected param name x, got %v", binding.Param)
	}
}

// Exists-bound variable resolved correctly.
func TestExistsBoundVariable(t *testing.T) {
	fVar := varExpr("f")
	mc := &ast.MethodCall{
		BaseExpr: ast.BaseExpr{Span: span()},
		Recv:     fVar,
		Method:   "doIt",
	}
	existsBody := ast.Formula(&ast.PredicateCall{
		BaseFormula: ast.BaseFormula{Span: span()},
		Recv:        fVar,
		Name:        "doIt",
	})
	_ = mc
	existsFormula := ast.Formula(&ast.Exists{
		BaseFormula: ast.BaseFormula{Span: span()},
		Decls:       []ast.VarDecl{{Type: typeRef("Foo"), Name: "f", Span: span()}},
		Body:        existsBody,
	})
	body := existsFormula
	mod := &ast.Module{
		Classes: []ast.ClassDecl{
			{
				Name:    "Foo",
				Members: []ast.MemberDecl{{Name: "doIt", Span: span()}},
				Span:    span(),
			},
		},
		Predicates: []ast.PredicateDecl{
			{Name: "pred", Body: &body, Span: span()},
		},
	}
	rm, err := resolve.Resolve(mod, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	noErrors(t, rm)
}

// Failed import → ResolveError for that import, resolution continues.
func TestImportLoaderFailure(t *testing.T) {
	loader := func(path string) (*ast.Module, error) {
		return nil, errors.New("module not found")
	}
	mod := &ast.Module{
		Imports: []ast.ImportDecl{
			{Path: "badlib", Span: span()},
		},
	}
	rm, _ := resolve.Resolve(mod, loader)
	hasError(t, rm, "cannot load import")
}

// Predicate with return type and result variable → no error.
func TestPredicateWithReturnTypeResult(t *testing.T) {
	resultVar := varExpr("result")
	body := ast.Formula(&ast.Comparison{
		BaseFormula: ast.BaseFormula{Span: span()},
		Left:        resultVar,
		Right:       &ast.IntLiteral{BaseExpr: ast.BaseExpr{Span: span()}, Value: 1},
		Op:          "=",
	})
	rt := typeRef("int")
	mod := &ast.Module{
		Predicates: []ast.PredicateDecl{
			{Name: "getOne", ReturnType: &rt, Body: &body, Span: span()},
		},
	}
	rm, err := resolve.Resolve(mod, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	noErrors(t, rm)
}

// Subclass accessing inherited non-private member -> should succeed.
func TestNonPrivateMemberInherited(t *testing.T) {
	greetCall := ast.Formula(&ast.PredicateCall{
		BaseFormula: ast.BaseFormula{Span: span()},
		Recv:        varExpr("this"),
		Name:        "greet",
	})
	mod := &ast.Module{
		Classes: []ast.ClassDecl{
			{
				Name: "Parent",
				Members: []ast.MemberDecl{
					{
						Name: "greet",
						Span: span(),
					},
				},
				Span: span(),
			},
			{
				Name:       "Child",
				SuperTypes: []ast.TypeRef{{Path: []string{"Parent"}, Span: span()}},
				Members: []ast.MemberDecl{
					{
						Name: "caller",
						Body: &greetCall,
						Span: span(),
					},
				},
				Span: span(),
			},
		},
	}
	rm, _ := resolve.Resolve(mod, nil)
	noErrors(t, rm)
}
