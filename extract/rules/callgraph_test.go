package rules

import (
	"context"
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/eval"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

// makeRel creates a relation with the given tuples. Each tuple is arity values.
func makeRel(name string, arity int, vals ...eval.Value) *eval.Relation {
	r := eval.NewRelation(name, arity)
	for i := 0; i < len(vals); i += arity {
		t := make(eval.Tuple, arity)
		for j := 0; j < arity; j++ {
			t[j] = vals[i+j]
		}
		r.Add(t)
	}
	return r
}

func iv(n int64) eval.IntVal  { return eval.IntVal{V: n} }
func sv(s string) eval.StrVal { return eval.StrVal{V: s} }

// planAndEval plans the given rules+query and evaluates over baseRels.
func planAndEval(t *testing.T, rules []datalog.Rule, query *datalog.Query, baseRels map[string]*eval.Relation) *eval.ResultSet {
	t.Helper()
	prog := &datalog.Program{Rules: rules, Query: query}
	ep, errs := plan.Plan(prog, nil)
	if len(errs) > 0 {
		t.Fatalf("Plan errors: %v", errs)
	}
	rs, err := eval.Evaluate(context.Background(), ep, baseRels)
	if err != nil {
		t.Fatalf("Evaluate error: %v", err)
	}
	return rs
}

// resultContains checks if any row matches the given tuple.
func resultContains(rs *eval.ResultSet, vals ...eval.Value) bool {
	for _, row := range rs.Rows {
		if len(row) != len(vals) {
			continue
		}
		match := true
		for i, v := range vals {
			if row[i] != v {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// TestDirectResolution tests rule 1: direct call through symbol.
func TestDirectResolution(t *testing.T) {
	// call=10, sym=20, fn=30
	baseRels := map[string]*eval.Relation{
		"CallCalleeSym":  makeRel("CallCalleeSym", 2, iv(10), iv(20)),
		"FunctionSymbol": makeRel("FunctionSymbol", 2, iv(20), iv(30)),
		// Empty relations needed by other rules
		"MethodCall":    eval.NewRelation("MethodCall", 3),
		"ExprType":      eval.NewRelation("ExprType", 2),
		"ClassDecl":     eval.NewRelation("ClassDecl", 3),
		"InterfaceDecl": eval.NewRelation("InterfaceDecl", 3),
		"MethodDecl":    eval.NewRelation("MethodDecl", 3),
		"Implements":    eval.NewRelation("Implements", 2),
		"Extends":       eval.NewRelation("Extends", 2),
		"NewExpr":       eval.NewRelation("NewExpr", 2),
	}

	query := &datalog.Query{
		Select: []datalog.Term{v("call"), v("fn")},
		Body: []datalog.Literal{
			pos("CallTarget", v("call"), v("fn")),
		},
	}

	rs := planAndEval(t, CallGraphRules(), query, baseRels)
	if len(rs.Rows) != 1 {
		t.Fatalf("expected 1 CallTarget row, got %d: %v", len(rs.Rows), rs.Rows)
	}
	if !resultContains(rs, iv(10), iv(30)) {
		t.Errorf("expected CallTarget(10, 30), got %v", rs.Rows)
	}
}

// TestMethodOnConcreteClass tests rule 2: method dispatch on concrete class.
func TestMethodOnConcreteClass(t *testing.T) {
	// call=1, recv=2, classId=100, name="foo", fn=200
	baseRels := map[string]*eval.Relation{
		"MethodCall":     makeRel("MethodCall", 3, iv(1), iv(2), sv("foo")),
		"ExprType":       makeRel("ExprType", 2, iv(2), iv(100)),
		"ClassDecl":      makeRel("ClassDecl", 3, iv(100), sv("MyClass"), iv(999)),
		"MethodDecl":     makeRel("MethodDecl", 3, iv(100), sv("foo"), iv(200)),
		"CallCalleeSym":  eval.NewRelation("CallCalleeSym", 2),
		"FunctionSymbol": eval.NewRelation("FunctionSymbol", 2),
		"InterfaceDecl":  eval.NewRelation("InterfaceDecl", 3),
		"Implements":     eval.NewRelation("Implements", 2),
		"Extends":        eval.NewRelation("Extends", 2),
		"NewExpr":        eval.NewRelation("NewExpr", 2),
	}

	query := &datalog.Query{
		Select: []datalog.Term{v("call"), v("fn")},
		Body: []datalog.Literal{
			pos("CallTarget", v("call"), v("fn")),
		},
	}

	rs := planAndEval(t, CallGraphRules(), query, baseRels)
	if !resultContains(rs, iv(1), iv(200)) {
		t.Errorf("expected CallTarget(1, 200), got %v", rs.Rows)
	}
}

// TestCHAInterfaceDispatch tests rule 3: CHA dispatches to all implementors.
func TestCHAInterfaceDispatch(t *testing.T) {
	// Interface IFoo (id=50), implemented by ClassA (id=100) and ClassB (id=101).
	// Both have method "bar". call=1, recv=2, ifaceId=50.
	baseRels := map[string]*eval.Relation{
		"MethodCall":    makeRel("MethodCall", 3, iv(1), iv(2), sv("bar")),
		"ExprType":      makeRel("ExprType", 2, iv(2), iv(50)),
		"InterfaceDecl": makeRel("InterfaceDecl", 3, iv(50), sv("IFoo"), iv(999)),
		"Implements": makeRel("Implements", 2,
			iv(100), iv(50),
			iv(101), iv(50),
		),
		"MethodDecl": makeRel("MethodDecl", 3,
			iv(100), sv("bar"), iv(300),
			iv(101), sv("bar"), iv(301),
		),
		"ClassDecl": makeRel("ClassDecl", 3,
			iv(100), sv("ClassA"), iv(999),
			iv(101), sv("ClassB"), iv(999),
		),
		"CallCalleeSym":  eval.NewRelation("CallCalleeSym", 2),
		"FunctionSymbol": eval.NewRelation("FunctionSymbol", 2),
		"Extends":        eval.NewRelation("Extends", 2),
		"NewExpr":        eval.NewRelation("NewExpr", 2),
	}

	query := &datalog.Query{
		Select: []datalog.Term{v("call"), v("fn")},
		Body: []datalog.Literal{
			pos("CallTarget", v("call"), v("fn")),
		},
	}

	rs := planAndEval(t, CallGraphRules(), query, baseRels)
	if len(rs.Rows) != 2 {
		t.Fatalf("CHA: expected 2 targets, got %d: %v", len(rs.Rows), rs.Rows)
	}
	if !resultContains(rs, iv(1), iv(300)) {
		t.Errorf("missing CallTarget(1, 300)")
	}
	if !resultContains(rs, iv(1), iv(301)) {
		t.Errorf("missing CallTarget(1, 301)")
	}
}

// TestRTAOnlyInstantiated tests rule 7: RTA prunes non-instantiated classes.
func TestRTAOnlyInstantiated(t *testing.T) {
	// Same setup as CHA but only ClassA is instantiated.
	baseRels := map[string]*eval.Relation{
		"MethodCall":    makeRel("MethodCall", 3, iv(1), iv(2), sv("bar")),
		"ExprType":      makeRel("ExprType", 2, iv(2), iv(50)),
		"InterfaceDecl": makeRel("InterfaceDecl", 3, iv(50), sv("IFoo"), iv(999)),
		"Implements": makeRel("Implements", 2,
			iv(100), iv(50),
			iv(101), iv(50),
		),
		"MethodDecl": makeRel("MethodDecl", 3,
			iv(100), sv("bar"), iv(300),
			iv(101), sv("bar"), iv(301),
		),
		"ClassDecl": makeRel("ClassDecl", 3,
			iv(100), sv("ClassA"), iv(999),
			iv(101), sv("ClassB"), iv(999),
		),
		"NewExpr":        makeRel("NewExpr", 2, iv(500), iv(100)), // only ClassA instantiated
		"CallCalleeSym":  eval.NewRelation("CallCalleeSym", 2),
		"FunctionSymbol": eval.NewRelation("FunctionSymbol", 2),
		"Extends":        eval.NewRelation("Extends", 2),
	}

	query := &datalog.Query{
		Select: []datalog.Term{v("call"), v("fn")},
		Body: []datalog.Literal{
			pos("CallTargetRTA", v("call"), v("fn")),
		},
	}

	rs := planAndEval(t, CallGraphRules(), query, baseRels)
	if len(rs.Rows) != 1 {
		t.Fatalf("RTA: expected 1 target, got %d: %v", len(rs.Rows), rs.Rows)
	}
	if !resultContains(rs, iv(1), iv(300)) {
		t.Errorf("expected CallTargetRTA(1, 300), got %v", rs.Rows)
	}
}

// TestInheritance tests rules 4 and 5: inherited methods.
func TestInheritance(t *testing.T) {
	// Parent class (id=100) has method "greet" (fn=200).
	// Child class (id=101) extends Parent but does NOT override "greet".
	// MethodDeclInherited should derive (101, "greet", 200).
	baseRels := map[string]*eval.Relation{
		"ClassDecl": makeRel("ClassDecl", 3,
			iv(100), sv("Parent"), iv(999),
			iv(101), sv("Child"), iv(999),
		),
		"MethodDecl": makeRel("MethodDecl", 3,
			iv(100), sv("greet"), iv(200),
		),
		"Extends":        makeRel("Extends", 2, iv(101), iv(100)),
		"InterfaceDecl":  eval.NewRelation("InterfaceDecl", 3),
		"Implements":     eval.NewRelation("Implements", 2),
		"MethodCall":     eval.NewRelation("MethodCall", 3),
		"ExprType":       eval.NewRelation("ExprType", 2),
		"CallCalleeSym":  eval.NewRelation("CallCalleeSym", 2),
		"FunctionSymbol": eval.NewRelation("FunctionSymbol", 2),
		"NewExpr":        eval.NewRelation("NewExpr", 2),
	}

	query := &datalog.Query{
		Select: []datalog.Term{v("childId"), v("name"), v("fn")},
		Body: []datalog.Literal{
			pos("MethodDeclInherited", v("childId"), v("name"), v("fn")),
		},
	}

	rs := planAndEval(t, CallGraphRules(), query, baseRels)
	if len(rs.Rows) != 1 {
		t.Fatalf("Inheritance: expected 1 inherited method, got %d: %v", len(rs.Rows), rs.Rows)
	}
	if !resultContains(rs, iv(101), sv("greet"), iv(200)) {
		t.Errorf("expected MethodDeclInherited(101, greet, 200), got %v", rs.Rows)
	}
}

// TestInheritanceOverrideBlocks tests that a child override blocks inheritance.
func TestInheritanceOverrideBlocks(t *testing.T) {
	// Parent has method "greet" (fn=200), Child also has "greet" (fn=201).
	// MethodDeclInherited should be EMPTY because child overrides.
	baseRels := map[string]*eval.Relation{
		"ClassDecl": makeRel("ClassDecl", 3,
			iv(100), sv("Parent"), iv(999),
			iv(101), sv("Child"), iv(999),
		),
		"MethodDecl": makeRel("MethodDecl", 3,
			iv(100), sv("greet"), iv(200),
			iv(101), sv("greet"), iv(201),
		),
		"Extends":        makeRel("Extends", 2, iv(101), iv(100)),
		"InterfaceDecl":  eval.NewRelation("InterfaceDecl", 3),
		"Implements":     eval.NewRelation("Implements", 2),
		"MethodCall":     eval.NewRelation("MethodCall", 3),
		"ExprType":       eval.NewRelation("ExprType", 2),
		"CallCalleeSym":  eval.NewRelation("CallCalleeSym", 2),
		"FunctionSymbol": eval.NewRelation("FunctionSymbol", 2),
		"NewExpr":        eval.NewRelation("NewExpr", 2),
	}

	query := &datalog.Query{
		Select: []datalog.Term{v("childId"), v("name"), v("fn")},
		Body: []datalog.Literal{
			pos("MethodDeclInherited", v("childId"), v("name"), v("fn")),
		},
	}

	rs := planAndEval(t, CallGraphRules(), query, baseRels)
	if len(rs.Rows) != 0 {
		t.Fatalf("Override: expected 0 inherited methods, got %d: %v", len(rs.Rows), rs.Rows)
	}
}

// TestCHASupersetsRTA verifies the property: CHA >= RTA (every RTA target is a CHA target).
func TestCHASupersetsRTA(t *testing.T) {
	// 3 classes implement IFoo, only 2 are instantiated.
	baseRels := map[string]*eval.Relation{
		"MethodCall": makeRel("MethodCall", 3,
			iv(1), iv(2), sv("do"),
			iv(3), iv(4), sv("do"),
		),
		"ExprType": makeRel("ExprType", 2,
			iv(2), iv(50),
			iv(4), iv(50),
		),
		"InterfaceDecl": makeRel("InterfaceDecl", 3, iv(50), sv("IFoo"), iv(999)),
		"Implements": makeRel("Implements", 2,
			iv(100), iv(50),
			iv(101), iv(50),
			iv(102), iv(50),
		),
		"MethodDecl": makeRel("MethodDecl", 3,
			iv(100), sv("do"), iv(300),
			iv(101), sv("do"), iv(301),
			iv(102), sv("do"), iv(302),
		),
		"ClassDecl": makeRel("ClassDecl", 3,
			iv(100), sv("A"), iv(999),
			iv(101), sv("B"), iv(999),
			iv(102), sv("C"), iv(999),
		),
		"NewExpr": makeRel("NewExpr", 2,
			iv(500), iv(100), // A instantiated
			iv(501), iv(101), // B instantiated
			// C NOT instantiated
		),
		"CallCalleeSym":  eval.NewRelation("CallCalleeSym", 2),
		"FunctionSymbol": eval.NewRelation("FunctionSymbol", 2),
		"Extends":        eval.NewRelation("Extends", 2),
	}

	// Get CHA targets
	chaQuery := &datalog.Query{
		Select: []datalog.Term{v("call"), v("fn")},
		Body:   []datalog.Literal{pos("CallTarget", v("call"), v("fn"))},
	}
	chaRS := planAndEval(t, CallGraphRules(), chaQuery, baseRels)

	// Get RTA targets
	rtaQuery := &datalog.Query{
		Select: []datalog.Term{v("call"), v("fn")},
		Body:   []datalog.Literal{pos("CallTargetRTA", v("call"), v("fn"))},
	}
	rtaRS := planAndEval(t, CallGraphRules(), rtaQuery, baseRels)

	// Every RTA target must be in CHA
	chaSet := make(map[[2]int64]bool)
	for _, row := range chaRS.Rows {
		call := row[0].(eval.IntVal).V
		fn := row[1].(eval.IntVal).V
		chaSet[[2]int64{call, fn}] = true
	}

	for _, row := range rtaRS.Rows {
		call := row[0].(eval.IntVal).V
		fn := row[1].(eval.IntVal).V
		if !chaSet[[2]int64{call, fn}] {
			t.Errorf("RTA target (%d, %d) not in CHA set", call, fn)
		}
	}

	// CHA should have 6 targets (2 calls x 3 classes), RTA should have 4 (2 calls x 2 instantiated classes)
	if len(chaRS.Rows) != 6 {
		t.Errorf("CHA: expected 6 targets, got %d", len(chaRS.Rows))
	}
	if len(rtaRS.Rows) != 4 {
		t.Errorf("RTA: expected 4 targets, got %d", len(rtaRS.Rows))
	}
}

// TestMergeSystemRules tests the merge function.
func TestMergeSystemRules(t *testing.T) {
	userProg := &datalog.Program{
		Rules: []datalog.Rule{
			{Head: datalog.Atom{Predicate: "UserRule", Args: []datalog.Term{v("x")}}},
		},
		Query: &datalog.Query{
			Select: []datalog.Term{v("x")},
		},
	}

	sysRules := []datalog.Rule{
		{Head: datalog.Atom{Predicate: "SysRule", Args: []datalog.Term{v("y")}}},
	}

	merged := MergeSystemRules(userProg, sysRules)

	if len(merged.Rules) != 2 {
		t.Fatalf("expected 2 rules in merged program, got %d", len(merged.Rules))
	}
	// System rules first, then user rules.
	if merged.Rules[0].Head.Predicate != "SysRule" {
		t.Errorf("expected system rule first, got %s", merged.Rules[0].Head.Predicate)
	}
	if merged.Rules[1].Head.Predicate != "UserRule" {
		t.Errorf("expected user rule second, got %s", merged.Rules[1].Head.Predicate)
	}
	// Original should be unmodified.
	if len(userProg.Rules) != 1 {
		t.Errorf("original program was modified")
	}
	// Query should be preserved.
	if merged.Query == nil {
		t.Error("merged query is nil")
	}
}

// TestInstantiatedRelation tests rule 6: Instantiated derived from NewExpr.
func TestInstantiatedRelation(t *testing.T) {
	baseRels := map[string]*eval.Relation{
		"NewExpr": makeRel("NewExpr", 2,
			iv(1), iv(100),
			iv(2), iv(100), // duplicate class, should deduplicate
			iv(3), iv(200),
		),
		"MethodCall":     eval.NewRelation("MethodCall", 3),
		"ExprType":       eval.NewRelation("ExprType", 2),
		"ClassDecl":      eval.NewRelation("ClassDecl", 3),
		"InterfaceDecl":  eval.NewRelation("InterfaceDecl", 3),
		"MethodDecl":     eval.NewRelation("MethodDecl", 3),
		"Implements":     eval.NewRelation("Implements", 2),
		"Extends":        eval.NewRelation("Extends", 2),
		"CallCalleeSym":  eval.NewRelation("CallCalleeSym", 2),
		"FunctionSymbol": eval.NewRelation("FunctionSymbol", 2),
	}

	query := &datalog.Query{
		Select: []datalog.Term{v("classId")},
		Body:   []datalog.Literal{pos("Instantiated", v("classId"))},
	}

	rs := planAndEval(t, CallGraphRules(), query, baseRels)
	if len(rs.Rows) != 2 {
		t.Fatalf("expected 2 instantiated classes, got %d: %v", len(rs.Rows), rs.Rows)
	}
}

// TestCallGraphRulesCount verifies we produce exactly 7 rules.
func TestCallGraphRulesCount(t *testing.T) {
	rules := CallGraphRules()
	if len(rules) != 7 {
		t.Errorf("expected 7 call graph rules, got %d", len(rules))
	}
}

// TestCallGraphRulesValidate verifies all rules pass the planner's validation.
func TestCallGraphRulesValidate(t *testing.T) {
	for i, r := range CallGraphRules() {
		errs := plan.ValidateRule(r)
		if len(errs) > 0 {
			t.Errorf("rule %d (%s) validation errors: %v", i, r.Head.Predicate, errs)
		}
	}
}

// TestCallGraphRulesStratify verifies the rules can be stratified (no recursive negation).
func TestCallGraphRulesStratify(t *testing.T) {
	prog := &datalog.Program{Rules: CallGraphRules()}
	_, errs := plan.Plan(prog, nil)
	if len(errs) > 0 {
		t.Fatalf("call graph rules failed to plan: %v", errs)
	}
}
