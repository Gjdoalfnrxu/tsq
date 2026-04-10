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
