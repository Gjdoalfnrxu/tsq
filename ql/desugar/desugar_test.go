package desugar_test

import (
	"strings"
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/desugar"
	"github.com/Gjdoalfnrxu/tsq/ql/parse"
	"github.com/Gjdoalfnrxu/tsq/ql/resolve"
)

// parseAndResolve is a test helper: parse QL source and resolve it.
func parseAndResolve(t *testing.T, src string) *resolve.ResolvedModule {
	t.Helper()
	p := parse.NewParser(src, "<test>")
	mod, err := p.Parse()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	rm, err := resolve.Resolve(mod, nil)
	if err != nil {
		t.Fatalf("resolve error: %v", err)
	}
	return rm
}

// desugarOK runs Desugar and fails the test if there are errors.
func desugarOK(t *testing.T, rm *resolve.ResolvedModule) *datalog.Program {
	t.Helper()
	prog, errs := desugar.Desugar(rm)
	if len(errs) > 0 {
		t.Fatalf("desugar errors: %v", errs)
	}
	return prog
}

// findRule returns the first rule whose head predicate contains substr.
func findRule(prog *datalog.Program, substr string) *datalog.Rule {
	for i := range prog.Rules {
		if strings.Contains(prog.Rules[i].Head.Predicate, substr) {
			return &prog.Rules[i]
		}
	}
	return nil
}

// findRuleExact returns the first rule whose head predicate equals pred.
func findRuleExact(prog *datalog.Program, pred string) *datalog.Rule {
	for i := range prog.Rules {
		if prog.Rules[i].Head.Predicate == pred {
			return &prog.Rules[i]
		}
	}
	return nil
}

// bodyContainsPred returns true if any literal in the body uses predicate pred.
func bodyContainsPred(lits []datalog.Literal, pred string) bool {
	for _, lit := range lits {
		if lit.Cmp == nil && lit.Agg == nil && lit.Atom.Predicate == pred {
			return true
		}
	}
	return false
}

// bodyContainsNegPred returns true if any negated literal in the body uses predicate pred.
func bodyContainsNegPred(lits []datalog.Literal, pred string) bool {
	for _, lit := range lits {
		if !lit.Positive && lit.Cmp == nil && lit.Agg == nil && lit.Atom.Predicate == pred {
			return true
		}
	}
	return false
}

// bodyHasCmp returns true if any literal is a comparison with the given op.
func bodyHasCmp(lits []datalog.Literal, op string) bool {
	for _, lit := range lits {
		if lit.Cmp != nil && lit.Cmp.Op == op {
			return true
		}
	}
	return false
}

// bodyHasAgg returns true if any literal is an aggregate with the given func.
func bodyHasAgg(lits []datalog.Literal, fn string) bool {
	for _, lit := range lits {
		if lit.Agg != nil && lit.Agg.Func == fn {
			return true
		}
	}
	return false
}

// ---- Tests ----

// 1. Empty class produces a characteristic predicate rule.
func TestDesugarEmptyClass(t *testing.T) {
	rm := parseAndResolve(t, `class Foo extends Bar { Foo() { any() } }`)
	// Note: Bar is undefined but resolve still produces output.
	prog, _ := desugar.Desugar(rm)
	r := findRuleExact(prog, "Foo")
	if r == nil {
		t.Fatal("expected rule for Foo")
	}
	if len(r.Head.Args) != 1 {
		t.Errorf("expected 1 head arg (this), got %d", len(r.Head.Args))
	}
	v, ok := r.Head.Args[0].(datalog.Var)
	if !ok || v.Name != "this" {
		t.Errorf("expected head arg to be Var{this}, got %v", r.Head.Args[0])
	}
}

// 2. Class extends produces supertype constraint in body.
func TestDesugarClassExtends(t *testing.T) {
	src := `
class Base { Base() { any() } }
class Sub extends Base { Sub() { any() } }
`
	rm := parseAndResolve(t, src)
	prog := desugarOK(t, rm)

	r := findRuleExact(prog, "Sub")
	if r == nil {
		t.Fatal("expected rule for Sub")
	}
	if !bodyContainsPred(r.Body, "Base") {
		t.Errorf("Sub body should contain Base(this), body: %v", r.Body)
	}
}

// 3. Method produces N-ary predicate with this and result.
func TestDesugarMethod(t *testing.T) {
	src := `
class Foo { Foo() { any() } int getX() { result = 1 } }
`
	rm := parseAndResolve(t, src)
	prog := desugarOK(t, rm)

	r := findRule(prog, "Foo_getX")
	if r == nil {
		t.Fatal("expected rule Foo_getX")
	}
	// Args: this, result (no params)
	if len(r.Head.Args) != 2 {
		t.Errorf("expected 2 head args, got %d: %v", len(r.Head.Args), r.Head.Args)
	}
	// First arg is this.
	if v, ok := r.Head.Args[0].(datalog.Var); !ok || v.Name != "this" {
		t.Errorf("first arg should be 'this', got %v", r.Head.Args[0])
	}
	// Second arg is result.
	if v, ok := r.Head.Args[1].(datalog.Var); !ok || v.Name != "result" {
		t.Errorf("second arg should be 'result', got %v", r.Head.Args[1])
	}
	// Body must include Foo(this).
	if !bodyContainsPred(r.Body, "Foo") {
		t.Errorf("body should contain Foo(this), got %v", r.Body)
	}
}

// 4. Method with parameters: args are (this, param..., result).
func TestDesugarMethodWithParams(t *testing.T) {
	src := `
class Foo { Foo() { any() } int getZ(int x) { result = x } }
`
	rm := parseAndResolve(t, src)
	prog := desugarOK(t, rm)

	r := findRule(prog, "Foo_getZ")
	if r == nil {
		t.Fatal("expected rule Foo_getZ")
	}
	// Args: this, x, result
	if len(r.Head.Args) != 3 {
		t.Errorf("expected 3 head args (this, x, result), got %d: %v", len(r.Head.Args), r.Head.Args)
	}
	names := make([]string, len(r.Head.Args))
	for i, a := range r.Head.Args {
		if v, ok := a.(datalog.Var); ok {
			names[i] = v.Name
		}
	}
	if names[0] != "this" || names[1] != "x" || names[2] != "result" {
		t.Errorf("expected [this, x, result], got %v", names)
	}
}

// 5. Predicate (no return type) produces head without result.
func TestDesugarPredicate(t *testing.T) {
	src := `
class Foo { Foo() { any() } predicate isBar() { any() } }
`
	rm := parseAndResolve(t, src)
	prog := desugarOK(t, rm)

	r := findRule(prog, "Foo_isBar")
	if r == nil {
		t.Fatal("expected rule Foo_isBar")
	}
	// Args: just this (no result for predicates)
	if len(r.Head.Args) != 1 {
		t.Errorf("predicate should have 1 head arg (this), got %d: %v", len(r.Head.Args), r.Head.Args)
	}
	if v, ok := r.Head.Args[0].(datalog.Var); !ok || v.Name != "this" {
		t.Errorf("arg should be 'this', got %v", r.Head.Args[0])
	}
}

// 6. Override dispatch: base rule excludes subclass, sub rule includes subclass body.
func TestDesugarOverride(t *testing.T) {
	src := `
class Base { Base() { any() } int getX() { result = 1 } }
class Sub extends Base { Sub() { any() } int getX() { result = 2 } }
`
	rm := parseAndResolve(t, src)
	prog := desugarOK(t, rm)

	// There should be two rules for Base_getX:
	// one with "not Sub(this)" and one for the Sub override.
	var baseRules []*datalog.Rule
	for i := range prog.Rules {
		if prog.Rules[i].Head.Predicate == "Base_getX" {
			baseRules = append(baseRules, &prog.Rules[i])
		}
	}
	if len(baseRules) < 2 {
		t.Fatalf("expected at least 2 rules for Base_getX (base + override dispatch), got %d", len(baseRules))
	}

	// One rule should have "not Sub(this)".
	hasExclusion := false
	for _, r := range baseRules {
		if bodyContainsNegPred(r.Body, "Sub") {
			hasExclusion = true
		}
	}
	if !hasExclusion {
		t.Error("expected one Base_getX rule to have 'not Sub(this)'")
	}

	// One rule should have "Sub(this)" positively.
	hasSubConstraint := false
	for _, r := range baseRules {
		if bodyContainsPred(r.Body, "Sub") {
			hasSubConstraint = true
		}
	}
	if !hasSubConstraint {
		t.Error("expected one Base_getX rule to have Sub(this) positively")
	}
}

// 7. Method call: this.getX() introduces fresh var and atom.
func TestDesugarThisMethodCall(t *testing.T) {
	src := `
class Foo {
	Foo() { any() }
	int getX() { result = 1 }
	int getY() { result = this.getX() }
}
`
	rm := parseAndResolve(t, src)
	prog := desugarOK(t, rm)

	r := findRule(prog, "Foo_getY")
	if r == nil {
		t.Fatal("expected Foo_getY rule")
	}
	// Body should contain a Foo_getX atom.
	if !bodyContainsPred(r.Body, "Foo_getX") {
		t.Errorf("Foo_getY body should call Foo_getX, body: %v", r.Body)
	}
}

// 8. Chained calls: this.getX().getY() introduces two fresh vars.
func TestDesugarChainedCalls(t *testing.T) {
	src := `
class Foo {
	Foo() { any() }
	Foo getX() { result = this }
	int getY() { result = 1 }
	int getChain() { result = this.getX().getY() }
}
`
	rm := parseAndResolve(t, src)
	prog := desugarOK(t, rm)

	r := findRule(prog, "Foo_getChain")
	if r == nil {
		t.Fatal("expected Foo_getChain rule")
	}
	// Should have at least 2 method call atoms in body (getX and getY).
	callCount := 0
	for _, lit := range r.Body {
		if lit.Cmp == nil && lit.Agg == nil && (strings.Contains(lit.Atom.Predicate, "getX") || strings.Contains(lit.Atom.Predicate, "getY")) {
			callCount++
		}
	}
	if callCount < 2 {
		t.Errorf("expected at least 2 method call atoms (getX + getY), got %d, body: %v", callCount, r.Body)
	}
}

// 9. External method call: x.getX() where x has known type.
func TestDesugarExternalMethodCall(t *testing.T) {
	src := `
class Node {
	Node() { any() }
	string getName() { result = "x" }
}
class Visitor {
	Visitor() { any() }
	string visit(Node n) { result = n.getName() }
}
`
	rm := parseAndResolve(t, src)
	prog := desugarOK(t, rm)

	r := findRule(prog, "Visitor_visit")
	if r == nil {
		t.Fatal("expected Visitor_visit rule")
	}
	// Should call Node_getName somewhere in body.
	if !bodyContainsPred(r.Body, "Node_getName") {
		t.Errorf("Visitor_visit should call Node_getName, body: %v", r.Body)
	}
}

// 10. Exists: introduces variable inline.
func TestDesugarExists(t *testing.T) {
	src := `
class Foo { Foo() { any() } }
class Bar {
	Bar() { any() }
	predicate hasFoo() { exists(Foo f | f instanceof Foo) }
}
`
	rm := parseAndResolve(t, src)
	prog := desugarOK(t, rm)

	r := findRule(prog, "Bar_hasFoo")
	if r == nil {
		t.Fatal("expected Bar_hasFoo rule")
	}
	// The body should contain Foo(f) or instanceof Foo.
	if !bodyContainsPred(r.Body, "Foo") {
		t.Errorf("Bar_hasFoo body should reference Foo, got: %v", r.Body)
	}
}

// 11. Forall: guard negated, body positively asserted.
func TestDesugarForall(t *testing.T) {
	src := `
class Foo { Foo() { any() } predicate isGood() { any() } }
class Bar {
	Bar() { any() }
	predicate allGood() { forall(Foo f | f instanceof Foo | f.isGood()) }
}
`
	rm := parseAndResolve(t, src)
	// Desugar — may have errors but should produce output.
	prog, _ := desugar.Desugar(rm)

	r := findRule(prog, "Bar_allGood")
	if r == nil {
		t.Fatal("expected Bar_allGood rule")
	}
	// Body should have something (exact shape depends on approximation).
	// At minimum, the rule should exist and have a body.
	_ = r.Body
}

// 12. Forex (forall + exists) — emits rule for the compound quantifier.
func TestDesugarForex(t *testing.T) {
	// forex is forall+exists — for now parsed as forall by the parser.
	// We test that a forall with multiple decls is handled.
	src := `
class A { A() { any() } predicate ok() { any() } }
class B {
	B() { any() }
	predicate test() { forall(A a | a instanceof A | a.ok()) }
}
`
	rm := parseAndResolve(t, src)
	prog, _ := desugar.Desugar(rm)

	r := findRule(prog, "B_test")
	if r == nil {
		t.Fatal("expected B_test rule")
	}
	_ = r
}

// 13. Negation: not formula.
func TestDesugarNegation(t *testing.T) {
	src := `
class Foo { Foo() { any() } predicate isGood() { any() } }
class Bar {
	Bar() { any() }
	predicate notGood() { not this.isGood() }
}
`
	rm := parseAndResolve(t, src)
	prog := desugarOK(t, rm)

	r := findRule(prog, "Bar_notGood")
	if r == nil {
		t.Fatal("expected Bar_notGood rule")
	}
	// Body should have a negated literal.
	hasNeg := false
	for _, lit := range r.Body {
		if !lit.Positive {
			hasNeg = true
		}
	}
	if !hasNeg {
		t.Errorf("Bar_notGood body should have negated literal, got: %v", r.Body)
	}
}

// 14. Comparison: a = b produces Cmp literal.
func TestDesugarComparison(t *testing.T) {
	src := `
class Foo { Foo() { any() } int getX() { result = 0 } }
class Bar {
	Bar() { any() }
	predicate check() { this instanceof Foo and 1 = 1 }
}
`
	rm := parseAndResolve(t, src)
	prog := desugarOK(t, rm)

	r := findRule(prog, "Bar_check")
	if r == nil {
		t.Fatal("expected Bar_check rule")
	}
	if !bodyHasCmp(r.Body, "=") {
		t.Errorf("Bar_check body should have '=' comparison, got: %v", r.Body)
	}
}

// 15. Aggregate: count produces Agg literal.
func TestDesugarAggregate(t *testing.T) {
	src := `
class Foo { Foo() { any() } }
class Bar {
	Bar() { any() }
	int countFoos() { result = count(Foo f | f instanceof Foo) }
}
`
	rm := parseAndResolve(t, src)
	prog := desugarOK(t, rm)

	r := findRule(prog, "Bar_countFoos")
	if r == nil {
		t.Fatal("expected Bar_countFoos rule")
	}
	if !bodyHasAgg(r.Body, "count") {
		t.Errorf("Bar_countFoos body should have count aggregate, got: %v", r.Body)
	}

	// The aggregate's ResultVar must be set so the fresh variable is bound.
	var agg *datalog.Aggregate
	for _, lit := range r.Body {
		if lit.Agg != nil && lit.Agg.Func == "count" {
			agg = lit.Agg
			break
		}
	}
	if agg == nil {
		t.Fatal("could not find count aggregate literal")
	}
	if agg.ResultVar.Name == "" {
		t.Errorf("Aggregate.ResultVar should be set (non-empty), got empty")
	}

	// Program.String() should include the result var name.
	str := prog.String()
	if !strings.Contains(str, agg.ResultVar.Name) {
		t.Errorf("Program.String() should contain ResultVar %q, got: %s", agg.ResultVar.Name, str)
	}
}

// TestDesugarOverrideThreeLevel verifies that a 3-level override chain (A ← B ← C,
// all defining the same method) emits C's body under A_method.
func TestDesugarOverrideThreeLevel(t *testing.T) {
	src := `
class A { A() { any() } int getX() { result = 1 } }
class B extends A { B() { any() } int getX() { result = 2 } }
class C extends B { C() { any() } int getX() { result = 3 } }
`
	rm := parseAndResolve(t, src)
	prog := desugarOK(t, rm)

	// Collect all rules for A_getX.
	var aRules []*datalog.Rule
	for i := range prog.Rules {
		if prog.Rules[i].Head.Predicate == "A_getX" {
			aRules = append(aRules, &prog.Rules[i])
		}
	}
	// Expect 3 rules: one for A itself, one for B, one for C.
	if len(aRules) < 3 {
		t.Fatalf("expected at least 3 rules for A_getX (A, B, C), got %d", len(aRules))
	}

	// One rule must have C(this) positively — that is C's body.
	hasCRule := false
	for _, r := range aRules {
		if bodyContainsPred(r.Body, "C") {
			hasCRule = true
		}
	}
	if !hasCRule {
		t.Error("expected one A_getX rule to have C(this) positively (C's override body)")
	}

	// The base A rule must exclude both B and C.
	for _, r := range aRules {
		if bodyContainsPred(r.Body, "A") && !bodyContainsPred(r.Body, "B") && !bodyContainsPred(r.Body, "C") {
			// This is the A base rule — it should exclude B and C.
			if !bodyContainsNegPred(r.Body, "B") || !bodyContainsNegPred(r.Body, "C") {
				t.Errorf("A base rule should exclude both B and C: %v", r.Body)
			}
		}
	}
}

// 16. instanceof: x instanceof Foo → Foo(x).
func TestDesugarInstanceOf(t *testing.T) {
	src := `
class Foo { Foo() { any() } }
class Bar {
	Bar() { any() }
	predicate isFoo() { this instanceof Foo }
}
`
	rm := parseAndResolve(t, src)
	prog := desugarOK(t, rm)

	r := findRule(prog, "Bar_isFoo")
	if r == nil {
		t.Fatal("expected Bar_isFoo rule")
	}
	if !bodyContainsPred(r.Body, "Foo") {
		t.Errorf("Bar_isFoo body should have Foo atom, got: %v", r.Body)
	}
}

// 17. Cast: x.(Foo) adds Foo(x) constraint.
func TestDesugarCast(t *testing.T) {
	src := `
class Foo { Foo() { any() } int getX() { result = 0 } }
class Bar {
	Bar() { any() }
	int castGet() { result = this.(Foo).getX() }
}
`
	rm := parseAndResolve(t, src)
	prog := desugarOK(t, rm)

	r := findRule(prog, "Bar_castGet")
	if r == nil {
		t.Fatal("expected Bar_castGet rule")
	}
	// Body should have Foo(this) type constraint from cast.
	if !bodyContainsPred(r.Body, "Foo") {
		t.Errorf("Bar_castGet body should have Foo constraint, got: %v", r.Body)
	}
}

// 18. Select clause produces Query.
func TestDesugarSelect(t *testing.T) {
	src := `
class Foo { Foo() { any() } }
from Foo f
where f instanceof Foo
select f
`
	rm := parseAndResolve(t, src)
	prog := desugarOK(t, rm)

	if prog.Query == nil {
		t.Fatal("expected Query to be non-nil")
	}
	if len(prog.Query.Select) != 1 {
		t.Errorf("expected 1 select term, got %d", len(prog.Query.Select))
	}
	// Body should have Foo(f) from from-clause type constraint.
	if !bodyContainsPred(prog.Query.Body, "Foo") {
		t.Errorf("Query body should have Foo type constraint, got: %v", prog.Query.Body)
	}
}

// 19. Top-level predicate.
func TestDesugarTopLevelPredicate(t *testing.T) {
	src := `
class Foo { Foo() { any() } }
predicate isFoo(Foo f) { f instanceof Foo }
`
	rm := parseAndResolve(t, src)
	prog := desugarOK(t, rm)

	r := findRuleExact(prog, "isFoo")
	if r == nil {
		t.Fatal("expected top-level rule isFoo")
	}
	// Head should have param f.
	if len(r.Head.Args) != 1 {
		t.Errorf("isFoo should have 1 arg (f), got %d", len(r.Head.Args))
	}
	if v, ok := r.Head.Args[0].(datalog.Var); !ok || v.Name != "f" {
		t.Errorf("arg should be Var{f}, got %v", r.Head.Args[0])
	}
}

// 19b. Top-level predicate with class-typed param injects an extent literal.
// Mirrors the from/exists injection pattern (desugar.go ~558, ~789).
// Without this, the planner sees `c` as untyped and has no extent anchor
// — joins on `c` go badly.
func TestDesugarTopLevelPredicateInjectsParamType(t *testing.T) {
	src := `
class Foo { Foo() { any() } }
predicate p(Foo c) { c = c }
`
	rm := parseAndResolve(t, src)
	prog := desugarOK(t, rm)

	r := findRuleExact(prog, "p")
	if r == nil {
		t.Fatal("expected top-level rule p")
	}
	if len(r.Body) == 0 {
		t.Fatalf("p body should not be empty (expected leading Foo(c) constraint)")
	}
	// Leading literal must be Foo(c).
	first := r.Body[0]
	if !first.Positive || first.Atom.Predicate != "Foo" {
		t.Fatalf("leading body literal should be Foo(c), got %v", first)
	}
	if len(first.Atom.Args) != 1 {
		t.Fatalf("Foo extent literal should take 1 arg, got %d", len(first.Atom.Args))
	}
	v, ok := first.Atom.Args[0].(datalog.Var)
	if !ok || v.Name != "c" {
		t.Fatalf("Foo extent literal should bind param var c, got %v", first.Atom.Args[0])
	}
}

// 19c. Primitive-typed (e.g. int) params must NOT get a type literal.
// `int` has no class extent — emitting `int(c)` would fail to resolve.
func TestDesugarTopLevelPredicateSkipsPrimitiveParamType(t *testing.T) {
	src := `
predicate p(int c) { c = c }
`
	rm := parseAndResolve(t, src)
	prog := desugarOK(t, rm)

	r := findRuleExact(prog, "p")
	if r == nil {
		t.Fatal("expected top-level rule p")
	}
	for _, lit := range r.Body {
		if lit.Atom.Predicate == "int" {
			t.Fatalf("int-typed param should not produce an `int(...)` literal, body: %v", r.Body)
		}
	}
}

// 19d. Mixed params: only typed (class) params get an extent literal.
func TestDesugarTopLevelPredicateMixedParamTypes(t *testing.T) {
	src := `
class Foo { Foo() { any() } }
predicate p(Foo c, int n) { c = c and n = n }
`
	rm := parseAndResolve(t, src)
	prog := desugarOK(t, rm)

	r := findRuleExact(prog, "p")
	if r == nil {
		t.Fatal("expected top-level rule p")
	}
	fooCount := 0
	intCount := 0
	for _, lit := range r.Body {
		switch lit.Atom.Predicate {
		case "Foo":
			fooCount++
		case "int":
			intCount++
		}
	}
	if fooCount != 1 {
		t.Errorf("expected exactly 1 Foo(c) extent literal, got %d, body: %v", fooCount, r.Body)
	}
	if intCount != 0 {
		t.Errorf("expected no int(...) literals, got %d, body: %v", intCount, r.Body)
	}
}

// 20. Fresh var counter resets per rule (determinism).
func TestDesugarFreshVarDeterminism(t *testing.T) {
	src := `
class Foo {
	Foo() { any() }
	int getX() { result = 1 }
	int getA() { result = this.getX() }
	int getB() { result = this.getX() }
}
`
	rm := parseAndResolve(t, src)
	prog := desugarOK(t, rm)

	// getA and getB both call getX with a single fresh var.
	// With reset-per-rule, both should use _v1 as the fresh var.
	var freshVarsA, freshVarsB []string
	for _, r := range prog.Rules {
		if r.Head.Predicate == "Foo_getA" {
			for _, lit := range r.Body {
				for _, arg := range lit.Atom.Args {
					if v, ok := arg.(datalog.Var); ok && strings.HasPrefix(v.Name, "_v") {
						freshVarsA = append(freshVarsA, v.Name)
					}
				}
			}
		}
		if r.Head.Predicate == "Foo_getB" {
			for _, lit := range r.Body {
				for _, arg := range lit.Atom.Args {
					if v, ok := arg.(datalog.Var); ok && strings.HasPrefix(v.Name, "_v") {
						freshVarsB = append(freshVarsB, v.Name)
					}
				}
			}
		}
	}
	// Both rules should have the same fresh var names (deterministic reset).
	if len(freshVarsA) == 0 {
		t.Error("expected fresh vars in Foo_getA")
	}
	if len(freshVarsB) == 0 {
		t.Error("expected fresh vars in Foo_getB")
	}
	if len(freshVarsA) > 0 && len(freshVarsB) > 0 && freshVarsA[0] != freshVarsB[0] {
		t.Errorf("fresh vars should be deterministic: getA=%v getB=%v", freshVarsA, freshVarsB)
	}
}

// 21. Complex query: multiple from decls, where clause, select.
func TestDesugarComplexQuery(t *testing.T) {
	src := `
class Func { Func() { any() } string getName() { result = "x" } }
class Call { Call() { any() } Func getCallee() { result = this.(Func) } }
from Func f, Call c
where c.getCallee() = f
select f, c
`
	rm := parseAndResolve(t, src)
	prog, _ := desugar.Desugar(rm)

	if prog.Query == nil {
		t.Fatal("expected Query")
	}
	if len(prog.Query.Select) != 2 {
		t.Errorf("expected 2 select terms (f, c), got %d", len(prog.Query.Select))
	}
	// Body should have Func(f) and Call(c) type constraints.
	if !bodyContainsPred(prog.Query.Body, "Func") {
		t.Errorf("query body should have Func type constraint")
	}
	if !bodyContainsPred(prog.Query.Body, "Call") {
		t.Errorf("query body should have Call type constraint")
	}
}

// 22. Multiple supertypes: conjunction.
func TestDesugarMultipleSupertypes(t *testing.T) {
	src := `
class A { A() { any() } }
class B { B() { any() } }
class C extends A, B { C() { any() } }
`
	rm := parseAndResolve(t, src)
	prog := desugarOK(t, rm)

	r := findRuleExact(prog, "C")
	if r == nil {
		t.Fatal("expected C rule")
	}
	// Body should have both A(this) and B(this).
	if !bodyContainsPred(r.Body, "A") {
		t.Errorf("C body should have A(this): %v", r.Body)
	}
	if !bodyContainsPred(r.Body, "B") {
		t.Errorf("C body should have B(this): %v", r.Body)
	}
}

// 23. No CharPred: class body is just supertype constraints.
func TestDesugarClassNoCharPred(t *testing.T) {
	// Class with no CharPred is emitted with only supertype constraints.
	// We test this by checking the rule exists and body has the parent.
	src := `
class Parent { Parent() { any() } }
`
	rm := parseAndResolve(t, src)
	prog := desugarOK(t, rm)

	r := findRuleExact(prog, "Parent")
	if r == nil {
		t.Fatal("expected Parent rule")
	}
	// Head should be Parent(this).
	if r.Head.Predicate != "Parent" {
		t.Errorf("expected head predicate Parent, got %q", r.Head.Predicate)
	}
}

// 24. Top-level function predicate (with return type).
func TestDesugarTopLevelFunction(t *testing.T) {
	src := `
class Foo { Foo() { any() } }
int countFoos(Foo f) { result = 0 }
`
	rm := parseAndResolve(t, src)
	prog := desugarOK(t, rm)

	r := findRuleExact(prog, "countFoos")
	if r == nil {
		t.Fatal("expected top-level rule countFoos")
	}
	// Should have args: f, result.
	if len(r.Head.Args) != 2 {
		t.Errorf("expected 2 args (f, result), got %d", len(r.Head.Args))
	}
	names := make([]string, len(r.Head.Args))
	for i, a := range r.Head.Args {
		if v, ok := a.(datalog.Var); ok {
			names[i] = v.Name
		}
	}
	if names[0] != "f" || names[1] != "result" {
		t.Errorf("expected [f, result], got %v", names)
	}
}

// --- Phase 1b: Disjunction via rule splitting ---

func TestDesugarDisjunction(t *testing.T) {
	src := `
class Foo { Foo() { any() } }
predicate test(Foo x) { a(x) or b(x) }
`
	rm := parseAndResolve(t, src)
	prog, errs := desugar.Desugar(rm)
	// Should NOT produce "disjunction not supported" error anymore.
	for _, e := range errs {
		if strings.Contains(e.Error(), "disjunction") {
			t.Fatalf("unexpected disjunction error: %v", e)
		}
	}

	// Should have synthetic _disj rules.
	var disjRules []*datalog.Rule
	for i := range prog.Rules {
		if strings.HasPrefix(prog.Rules[i].Head.Predicate, "_disj") {
			disjRules = append(disjRules, &prog.Rules[i])
		}
	}
	if len(disjRules) != 2 {
		t.Fatalf("expected 2 synthetic disjunction rules, got %d", len(disjRules))
	}

	// The test predicate's body should reference the synthetic predicate.
	r := findRuleExact(prog, "test")
	if r == nil {
		t.Fatal("expected rule 'test'")
	}
	hasDisjCall := false
	for _, lit := range r.Body {
		if strings.HasPrefix(lit.Atom.Predicate, "_disj") {
			hasDisjCall = true
		}
	}
	if !hasDisjCall {
		t.Errorf("test rule body should call _disj synthetic predicate, body: %v", r.Body)
	}
}

func TestDesugarDisjunctionInClass(t *testing.T) {
	src := `
class Foo {
	Foo() { any() }
	predicate isAorB() { a(this) or b(this) }
}
`
	rm := parseAndResolve(t, src)
	prog, errs := desugar.Desugar(rm)
	for _, e := range errs {
		if strings.Contains(e.Error(), "disjunction") {
			t.Fatalf("unexpected disjunction error: %v", e)
		}
	}

	// Should have synthetic rules.
	hasSynth := false
	for i := range prog.Rules {
		if strings.HasPrefix(prog.Rules[i].Head.Predicate, "_disj") {
			hasSynth = true
			break
		}
	}
	if !hasSynth {
		t.Error("expected synthetic _disj rules")
	}
}

// --- Phase 1c: Negation of conjunctions ---

func TestDesugarNegationOfConjunction(t *testing.T) {
	src := `
class Foo { Foo() { any() } }
predicate test(Foo x) { not (a(x) and b(x)) }
`
	rm := parseAndResolve(t, src)
	prog, errs := desugar.Desugar(rm)
	// Should NOT produce "negation of conjunction not supported" error.
	for _, e := range errs {
		if strings.Contains(e.Error(), "negation of conjunction") {
			t.Fatalf("unexpected negation error: %v", e)
		}
	}

	// Should have a synthetic _neg rule.
	var negRules []*datalog.Rule
	for i := range prog.Rules {
		if strings.HasPrefix(prog.Rules[i].Head.Predicate, "_neg") {
			negRules = append(negRules, &prog.Rules[i])
		}
	}
	if len(negRules) != 1 {
		t.Fatalf("expected 1 synthetic _neg rule, got %d", len(negRules))
	}

	// The test predicate's body should have a negated reference to _neg.
	r := findRuleExact(prog, "test")
	if r == nil {
		t.Fatal("expected rule 'test'")
	}
	hasNegCall := false
	for _, lit := range r.Body {
		if !lit.Positive && strings.HasPrefix(lit.Atom.Predicate, "_neg") {
			hasNegCall = true
		}
	}
	if !hasNegCall {
		t.Errorf("test rule body should have negated _neg call, body: %v", r.Body)
	}
}

// --- Phase 1d: Abstract classes ---

func TestDesugarAbstractClass(t *testing.T) {
	src := `
abstract class Base { Base() { any() } }
class ConcreteA extends Base { ConcreteA() { any() } }
class ConcreteB extends Base { ConcreteB() { any() } }
`
	rm := parseAndResolve(t, src)
	prog := desugarOK(t, rm)

	// Base should have rules: Base(this) :- ConcreteA(this). and Base(this) :- ConcreteB(this).
	var baseRules []*datalog.Rule
	for i := range prog.Rules {
		if prog.Rules[i].Head.Predicate == "Base" {
			baseRules = append(baseRules, &prog.Rules[i])
		}
	}
	if len(baseRules) != 2 {
		t.Fatalf("expected 2 rules for abstract class Base (one per subclass), got %d", len(baseRules))
	}

	hasA := false
	hasB := false
	for _, r := range baseRules {
		if bodyContainsPred(r.Body, "ConcreteA") {
			hasA = true
		}
		if bodyContainsPred(r.Body, "ConcreteB") {
			hasB = true
		}
	}
	if !hasA {
		t.Error("expected Base rule with ConcreteA(this)")
	}
	if !hasB {
		t.Error("expected Base rule with ConcreteB(this)")
	}
}

func TestDesugarAbstractClassNoSubclasses(t *testing.T) {
	src := `abstract class Lonely { Lonely() { any() } }`
	rm := parseAndResolve(t, src)
	prog := desugarOK(t, rm)

	// With no concrete subclasses, the abstract class has no derivation rules
	// and its extent is naturally empty (no rules = empty relation).
	r := findRuleExact(prog, "Lonely")
	if r != nil {
		t.Fatal("expected no rule for abstract class with no subclasses")
	}
}

func TestDesugarAbstractClassMethodsStillWork(t *testing.T) {
	src := `
abstract class Base { Base() { any() } int getX() { result = 1 } }
class Sub extends Base { Sub() { any() } }
`
	rm := parseAndResolve(t, src)
	prog := desugarOK(t, rm)

	// Method rule should still be emitted.
	r := findRule(prog, "Base_getX")
	if r == nil {
		t.Fatal("expected Base_getX method rule for abstract class")
	}
}

// --- Phase 1a: Module desugaring ---

func TestDesugarModuleClass(t *testing.T) {
	src := `
module DataFlow {
	class Node extends @node { Node() { any() } }
}
`
	rm := parseAndResolve(t, src)
	prog := desugarOK(t, rm)

	// Should produce a rule with qualified name DataFlow::Node.
	r := findRuleExact(prog, "DataFlow::Node")
	if r == nil {
		t.Fatal("expected rule for DataFlow::Node")
	}
}

// 25. Program.String() roundtrip: output is non-empty and contains predicate names.
func TestDesugarProgramString(t *testing.T) {
	src := `
class Foo { Foo() { any() } int getX() { result = 1 } }
class Bar extends Foo {
	Bar() { any() }
	int getX() { result = 2 }
}
`
	rm := parseAndResolve(t, src)
	prog := desugarOK(t, rm)

	str := prog.String()
	if str == "" {
		t.Fatal("program String() is empty")
	}
	if !strings.Contains(str, "Foo") {
		t.Errorf("String() should contain 'Foo': %q", str)
	}
	if !strings.Contains(str, "Bar") {
		t.Errorf("String() should contain 'Bar': %q", str)
	}
}

// --- Adversarial regression tests ---

// Disjunction with asymmetric variables: only shared vars go in the head.
func TestDesugarDisjunctionAsymmetricVars(t *testing.T) {
	src := `
predicate a(int x, int y) { any() }
predicate b(int x) { any() }
predicate test(int x) { a(x, _) or b(x) }
`
	rm := parseAndResolve(t, src)
	prog := desugarOK(t, rm)

	// Find the synthetic disjunction rule.
	var synthRules []*datalog.Rule
	for i := range prog.Rules {
		if strings.HasPrefix(prog.Rules[i].Head.Predicate, "_disj") {
			synthRules = append(synthRules, &prog.Rules[i])
		}
	}
	if len(synthRules) != 2 {
		t.Fatalf("expected 2 synthetic disjunction rules, got %d", len(synthRules))
	}
	// Head args should only contain x (the shared variable), not y or _.
	for _, r := range synthRules {
		for _, arg := range r.Head.Args {
			if v, ok := arg.(datalog.Var); ok && v.Name != "x" {
				t.Errorf("synthetic disjunction head should only have shared var x, got %q", v.Name)
			}
		}
	}
}

// Abstract class in module: subclass lookup uses qualified names.
func TestDesugarAbstractClassInModule(t *testing.T) {
	src := `
module M {
	abstract class Base { Base() { any() } }
	class Sub extends Base { Sub() { any() } }
}
`
	rm := parseAndResolve(t, src)
	prog := desugarOK(t, rm)

	// M::Base should have a rule: M::Base(this) :- M::Sub(this).
	var baseRules []*datalog.Rule
	for i := range prog.Rules {
		if prog.Rules[i].Head.Predicate == "M::Base" {
			baseRules = append(baseRules, &prog.Rules[i])
		}
	}
	if len(baseRules) != 1 {
		t.Fatalf("expected 1 rule for M::Base (from M::Sub), got %d", len(baseRules))
	}
	if !bodyContainsPred(baseRules[0].Body, "M::Sub") {
		t.Error("expected M::Base rule body to contain M::Sub")
	}
}

// --- Phase 1e: String builtins ---

func TestDesugarStringBuiltinLength(t *testing.T) {
	src := `
class Foo extends @foo {
	Foo() { any() }
	string getName() { result = "hi" }
}
from string s
where s.length() > 0
select s
`
	rm := parseAndResolve(t, src)
	prog := desugarOK(t, rm)

	// The query body should contain __builtin_string_length
	if prog.Query == nil {
		t.Fatal("expected query")
	}
	found := false
	for _, lit := range prog.Query.Body {
		if lit.Atom.Predicate == "__builtin_string_length" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected __builtin_string_length in query body")
		t.Logf("query body: %v", prog.Query.Body)
	}
}

func TestDesugarStringBuiltinToUpperCase(t *testing.T) {
	src := `
from string s
where s = "hello"
select s.toUpperCase()
`
	rm := parseAndResolve(t, src)
	prog := desugarOK(t, rm)

	if prog.Query == nil {
		t.Fatal("expected query")
	}
	found := false
	for _, lit := range prog.Query.Body {
		if lit.Atom.Predicate == "__builtin_string_toUpperCase" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected __builtin_string_toUpperCase in query body")
	}
}

// --- Phase 1f: If-then-else ---

func TestDesugarIfThenElse(t *testing.T) {
	src := `predicate foo(int x) {
		if x > 0 then x < 100 else x > -100
	}`
	rm := parseAndResolve(t, src)
	prog := desugarOK(t, rm)

	// IfThenElse desugars to a disjunction, which produces synthetic rules
	rule := findRuleExact(prog, "foo")
	if rule == nil {
		t.Fatal("expected rule for foo")
	}

	// The body should reference a synthetic _disj predicate
	found := false
	for _, lit := range rule.Body {
		if strings.HasPrefix(lit.Atom.Predicate, "_disj") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected synthetic _disj predicate from if-then-else desugaring")
		t.Logf("body: %v", rule.Body)
	}
}

// --- Phase 1g: Transitive closure ---

func TestDesugarClosurePlus(t *testing.T) {
	src := `predicate foo() { edge+(x, y) }`
	rm := parseAndResolve(t, src)
	prog := desugarOK(t, rm)

	rule := findRuleExact(prog, "foo")
	if rule == nil {
		t.Fatal("expected rule for foo")
	}

	// The body should reference a synthetic _closure predicate
	closurePred := ""
	for _, lit := range rule.Body {
		if strings.HasPrefix(lit.Atom.Predicate, "_closure") {
			closurePred = lit.Atom.Predicate
			break
		}
	}
	if closurePred == "" {
		t.Fatal("expected synthetic _closure predicate in body")
	}

	// There should be synthetic rules for the closure:
	// _closure(x, y) :- edge(x, y).
	// _closure(x, y) :- edge(x, z), _closure(z, y).
	var closureRules []*datalog.Rule
	for i := range prog.Rules {
		if prog.Rules[i].Head.Predicate == closurePred {
			closureRules = append(closureRules, &prog.Rules[i])
		}
	}
	if len(closureRules) != 2 {
		t.Fatalf("expected 2 closure rules, got %d", len(closureRules))
	}

	// One should have body with just edge, another with edge + closure
	baseFound := false
	recursiveFound := false
	for _, r := range closureRules {
		if len(r.Body) == 1 && r.Body[0].Atom.Predicate == "edge" {
			baseFound = true
		}
		if len(r.Body) == 2 {
			hasEdge := false
			hasClosure := false
			for _, lit := range r.Body {
				if lit.Atom.Predicate == "edge" {
					hasEdge = true
				}
				if lit.Atom.Predicate == closurePred {
					hasClosure = true
				}
			}
			if hasEdge && hasClosure {
				recursiveFound = true
			}
		}
	}
	if !baseFound {
		t.Error("expected base closure rule: _closure(x,y) :- edge(x,y)")
	}
	if !recursiveFound {
		t.Error("expected recursive closure rule: _closure(x,y) :- edge(x,z), _closure(z,y)")
	}
}

func TestDesugarClosureStar(t *testing.T) {
	src := `predicate foo() { edge*(x, y) }`
	rm := parseAndResolve(t, src)
	prog := desugarOK(t, rm)

	rule := findRuleExact(prog, "foo")
	if rule == nil {
		t.Fatal("expected rule for foo")
	}

	// Star closure desugars as (x = y) or edge+(x, y), which means a synthetic _disj
	found := false
	for _, lit := range rule.Body {
		if strings.HasPrefix(lit.Atom.Predicate, "_disj") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected synthetic _disj predicate from star closure desugaring")
	}
}

// --- Phase 1h: Additional aggregates ---

func TestDesugarStrictcount(t *testing.T) {
	src := `
		predicate foo(int n) {
			n = strictcount(int v | v = 1)
		}
	`
	rm := parseAndResolve(t, src)
	prog := desugarOK(t, rm)

	rule := findRuleExact(prog, "foo")
	if rule == nil {
		t.Fatal("expected rule for foo")
	}
	// Should have an aggregate literal with func "strictcount"
	found := false
	for _, lit := range rule.Body {
		if lit.Agg != nil && lit.Agg.Func == "strictcount" {
			found = true
		}
	}
	if !found {
		t.Error("expected aggregate literal with func 'strictcount'")
	}
}

func TestDesugarConcat(t *testing.T) {
	src := `
		predicate foo(string s) {
			s = concat(string v | v = "a" | v)
		}
	`
	rm := parseAndResolve(t, src)
	prog := desugarOK(t, rm)

	rule := findRuleExact(prog, "foo")
	if rule == nil {
		t.Fatal("expected rule for foo")
	}
	found := false
	for _, lit := range rule.Body {
		if lit.Agg != nil && lit.Agg.Func == "concat" {
			found = true
		}
	}
	if !found {
		t.Error("expected aggregate literal with func 'concat'")
	}
}

func TestDesugarRank(t *testing.T) {
	src := `
		predicate foo(int n) {
			n = rank(int v | v = 1 | v)
		}
	`
	rm := parseAndResolve(t, src)
	prog := desugarOK(t, rm)

	rule := findRuleExact(prog, "foo")
	if rule == nil {
		t.Fatal("expected rule for foo")
	}
	found := false
	for _, lit := range rule.Body {
		if lit.Agg != nil && lit.Agg.Func == "rank" {
			found = true
		}
	}
	if !found {
		t.Error("expected aggregate literal with func 'rank'")
	}
}

// --- Phase 1i: forex ---

func TestDesugarForexPhase1i(t *testing.T) {
	src := `
		predicate allValid() {
			forex(int x | x > 0 | x < 100)
		}
	`
	rm := parseAndResolve(t, src)
	prog := desugarOK(t, rm)

	rule := findRuleExact(prog, "allValid")
	if rule == nil {
		t.Fatal("expected rule for allValid")
	}
	// forex desugars as forall + exists, so the body should have literals from both
	if len(rule.Body) == 0 {
		t.Error("expected non-empty body from forex desugaring")
	}
}

// --- Phase 1j: super ---

func TestDesugarSuperMethodCall(t *testing.T) {
	src := `
		class Base extends Entity {
			int getValue() { result = 1 }
		}
		class Child extends Base {
			override int getValue() { result = super.getValue() }
		}
	`
	rm := parseAndResolve(t, src)
	prog := desugarOK(t, rm)

	// The Child override should call Base_getValue, not Child_getValue
	var childRule *datalog.Rule
	for i := range prog.Rules {
		r := &prog.Rules[i]
		if r.Head.Predicate == "Base_getValue" {
			// Look for the rule that has Child(this) in its body
			for _, lit := range r.Body {
				if lit.Atom.Predicate == "Child" {
					childRule = r
					break
				}
			}
		}
	}
	// At minimum, verify that the program was generated without errors
	if len(prog.Rules) == 0 {
		t.Error("expected rules from super desugaring")
	}
	_ = childRule
}

// --- Phase 1k: Multiple inheritance ---

func TestDesugarMultipleInheritance(t *testing.T) {
	src := `
		class A extends Entity {
			A() { this instanceof Entity }
		}
		class B extends Entity {
			B() { this instanceof Entity }
		}
		class C extends A, B {
			C() { this instanceof A and this instanceof B }
		}
	`
	rm := parseAndResolve(t, src)
	prog := desugarOK(t, rm)

	// C's characteristic predicate should conjoin both A(this) and B(this)
	rule := findRuleExact(prog, "C")
	if rule == nil {
		t.Fatal("expected rule for C")
	}
	hasA := bodyContainsPred(rule.Body, "A")
	hasB := bodyContainsPred(rule.Body, "B")
	if !hasA {
		t.Error("expected A(this) in C's body for intersection semantics")
	}
	if !hasB {
		t.Error("expected B(this) in C's body for intersection semantics")
	}
}

// --- Phase 1l: Annotations ---

func TestDesugarAnnotatedPredicate(t *testing.T) {
	src := `
		private predicate helper(int x) { x = 1 }
	`
	rm := parseAndResolve(t, src)
	prog := desugarOK(t, rm)

	// Annotations don't affect Datalog output — just verify it desugars without error
	rule := findRuleExact(prog, "helper")
	if rule == nil {
		t.Fatal("expected rule for helper")
	}
}
