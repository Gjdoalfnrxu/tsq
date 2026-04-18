package desugar_test

import (
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
)

// TestDesugar_ClassExtent_ConcreteCharPred verifies that a concrete-class
// characteristic predicate rule emitted by the desugarer carries the
// ClassExtent flag (P2a). The flag is the load-bearing signal that the
// estimator/evaluator may treat the rule as eligible for one-shot
// materialisation.
func TestDesugar_ClassExtent_ConcreteCharPred(t *testing.T) {
	src := `
class Symbol extends @symbol {
    Symbol() { Symbol(this, _, _, _) }
    string toString() { result = "sym" }
}
`
	rm := parseAndResolve(t, src)
	prog := desugarOK(t, rm)

	r := findRuleExact(prog, "Symbol")
	if r == nil {
		t.Fatalf("no Symbol char-pred rule emitted; rules: %s", prog)
	}
	if !r.ClassExtent {
		t.Errorf("expected Symbol char-pred rule to have ClassExtent=true, got false")
	}
	// Method rules MUST NOT carry the flag — they are not extent-shaped.
	for _, rule := range prog.Rules {
		if rule.Head.Predicate == "Symbol_toString" && rule.ClassExtent {
			t.Errorf("Symbol_toString method rule incorrectly tagged ClassExtent")
		}
	}
}

// TestDesugar_ClassExtent_AbstractSubclassUnion verifies that
// Abstract(this) :- Concrete(this) rules synthesised for abstract classes
// also carry the ClassExtent flag — they are extent-shaped by construction.
func TestDesugar_ClassExtent_AbstractSubclassUnion(t *testing.T) {
	src := `
abstract class A extends @symbol { string toString() { result = "a" } }
class B extends A { B() { Symbol(this, _, _, _) } string toString() { result = "b" } }
class C extends A { C() { Symbol(this, _, _, _) } string toString() { result = "c" } }
`
	rm := parseAndResolve(t, src)
	prog := desugarOK(t, rm)

	// Two abstract-union rules: A(this) :- B(this), A(this) :- C(this).
	count := 0
	for _, rule := range prog.Rules {
		if rule.Head.Predicate == "A" {
			count++
			if !rule.ClassExtent {
				t.Errorf("A subclass-union rule (body %v) missing ClassExtent flag", rule.Body)
			}
		}
	}
	if count != 2 {
		t.Errorf("expected 2 A subclass-union rules, got %d", count)
	}
}

// TestDesugar_ClassExtent_TopLevelPredicateNotTagged ensures that a
// top-level predicate definition (NOT a class) is never tagged, even if
// its body looks extent-shaped. The tag is class-declaration-scoped.
func TestDesugar_ClassExtent_TopLevelPredicateNotTagged(t *testing.T) {
	src := `
predicate isThing(int x) { Symbol(x, _, _, _) }
`
	rm := parseAndResolve(t, src)
	prog := desugarOK(t, rm)

	for _, rule := range prog.Rules {
		if rule.Head.Predicate == "isThing" && rule.ClassExtent {
			t.Errorf("top-level predicate isThing should not be ClassExtent-tagged")
		}
	}
}

// Compile-time sanity that we're using the field as expected.
var _ = datalog.Rule{}.ClassExtent
