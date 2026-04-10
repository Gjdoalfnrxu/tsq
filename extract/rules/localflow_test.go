package rules

import (
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/eval"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

// localFlowBaseRels returns a base relation map with all required relations
// for LocalFlow rules, populated with the given overrides.
func localFlowBaseRels(overrides map[string]*eval.Relation) map[string]*eval.Relation {
	base := map[string]*eval.Relation{
		"Assign":           eval.NewRelation("Assign", 3),
		"ExprMayRef":       eval.NewRelation("ExprMayRef", 2),
		"SymInFunction":    eval.NewRelation("SymInFunction", 2),
		"VarDecl":          eval.NewRelation("VarDecl", 4),
		"ReturnStmt":       eval.NewRelation("ReturnStmt", 3),
		"ReturnSym":        eval.NewRelation("ReturnSym", 2),
		"DestructureField": eval.NewRelation("DestructureField", 5),
		"FieldRead":        eval.NewRelation("FieldRead", 3),
		"FieldWrite":       eval.NewRelation("FieldWrite", 4),
	}
	for k, v := range overrides {
		base[k] = v
	}
	return base
}

// TestAssignmentFlow tests rule 1: x = y → LocalFlow(fn, sym_y, sym_x).
func TestAssignmentFlow(t *testing.T) {
	// fn=1, sym_x=10, sym_y=20, lhsNode=100, rhsExpr=200
	baseRels := localFlowBaseRels(map[string]*eval.Relation{
		"Assign":        makeRel("Assign", 3, iv(100), iv(200), iv(10)),
		"ExprMayRef":    makeRel("ExprMayRef", 2, iv(200), iv(20)),
		"SymInFunction": makeRel("SymInFunction", 2, iv(10), iv(1), iv(20), iv(1)),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("fn"), v("src"), v("dst")},
		Body:   []datalog.Literal{pos("LocalFlow", v("fn"), v("src"), v("dst"))},
	}

	rs := planAndEval(t, LocalFlowRules(), query, baseRels)
	if len(rs.Rows) != 1 {
		t.Fatalf("expected 1 LocalFlow row, got %d: %v", len(rs.Rows), rs.Rows)
	}
	if !resultContains(rs, iv(1), iv(20), iv(10)) {
		t.Errorf("expected LocalFlow(1, 20, 10), got %v", rs.Rows)
	}
}

// TestVarDeclFlow tests rule 2: const x = y → LocalFlow(fn, sym_y, sym_x).
func TestVarDeclFlow(t *testing.T) {
	// fn=1, sym_x=10, sym_y=20, declId=100, initExpr=200
	baseRels := localFlowBaseRels(map[string]*eval.Relation{
		"VarDecl":       makeRel("VarDecl", 4, iv(100), iv(10), iv(200), iv(1)),
		"ExprMayRef":    makeRel("ExprMayRef", 2, iv(200), iv(20)),
		"SymInFunction": makeRel("SymInFunction", 2, iv(10), iv(1), iv(20), iv(1)),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("fn"), v("src"), v("dst")},
		Body:   []datalog.Literal{pos("LocalFlow", v("fn"), v("src"), v("dst"))},
	}

	rs := planAndEval(t, LocalFlowRules(), query, baseRels)
	if len(rs.Rows) != 1 {
		t.Fatalf("expected 1 LocalFlow row, got %d: %v", len(rs.Rows), rs.Rows)
	}
	if !resultContains(rs, iv(1), iv(20), iv(10)) {
		t.Errorf("expected LocalFlow(1, 20, 10), got %v", rs.Rows)
	}
}

// TestReturnFlow tests rule 3: return x → LocalFlow(fn, sym_x, returnSym).
func TestReturnFlow(t *testing.T) {
	// fn=1, sym_x=10, returnSym=99, stmtNode=50, retExpr=200
	baseRels := localFlowBaseRels(map[string]*eval.Relation{
		"ReturnStmt":    makeRel("ReturnStmt", 3, iv(1), iv(50), iv(200)),
		"ExprMayRef":    makeRel("ExprMayRef", 2, iv(200), iv(10)),
		"ReturnSym":     makeRel("ReturnSym", 2, iv(1), iv(99)),
		"SymInFunction": makeRel("SymInFunction", 2, iv(10), iv(1), iv(99), iv(1)),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("fn"), v("src"), v("dst")},
		Body:   []datalog.Literal{pos("LocalFlow", v("fn"), v("src"), v("dst"))},
	}

	rs := planAndEval(t, LocalFlowRules(), query, baseRels)
	if len(rs.Rows) != 1 {
		t.Fatalf("expected 1 LocalFlow row, got %d: %v", len(rs.Rows), rs.Rows)
	}
	if !resultContains(rs, iv(1), iv(10), iv(99)) {
		t.Errorf("expected LocalFlow(1, 10, 99), got %v", rs.Rows)
	}
}

// TestChainFlow tests transitivity: x = y; z = x → LocalFlowStar(fn, sym_y, sym_z).
func TestChainFlow(t *testing.T) {
	// fn=1, sym_x=10, sym_y=20, sym_z=30
	// Assign x = y: lhsNode=100, rhsExpr=200
	// Assign z = x: lhsNode=101, rhsExpr=201
	baseRels := localFlowBaseRels(map[string]*eval.Relation{
		"Assign": makeRel("Assign", 3,
			iv(100), iv(200), iv(10), // x = y
			iv(101), iv(201), iv(30), // z = x
		),
		"ExprMayRef": makeRel("ExprMayRef", 2,
			iv(200), iv(20), // rhsExpr for y
			iv(201), iv(10), // rhsExpr for x
		),
		"SymInFunction": makeRel("SymInFunction", 2,
			iv(10), iv(1),
			iv(20), iv(1),
			iv(30), iv(1),
		),
	})

	// Check LocalFlowStar for the transitive edge
	query := &datalog.Query{
		Select: []datalog.Term{v("fn"), v("src"), v("dst")},
		Body:   []datalog.Literal{pos("LocalFlowStar", v("fn"), v("src"), v("dst"))},
	}

	rs := planAndEval(t, LocalFlowRules(), query, baseRels)
	// Direct: (1,20,10) and (1,10,30); Transitive: (1,20,30)
	if !resultContains(rs, iv(1), iv(20), iv(30)) {
		t.Errorf("expected transitive LocalFlowStar(1, 20, 30), got %v", rs.Rows)
	}
	if !resultContains(rs, iv(1), iv(20), iv(10)) {
		t.Errorf("expected direct LocalFlowStar(1, 20, 10), got %v", rs.Rows)
	}
	if !resultContains(rs, iv(1), iv(10), iv(30)) {
		t.Errorf("expected direct LocalFlowStar(1, 10, 30), got %v", rs.Rows)
	}
	if len(rs.Rows) != 3 {
		t.Errorf("expected 3 LocalFlowStar rows, got %d: %v", len(rs.Rows), rs.Rows)
	}
}

// TestNoCrossFunctionFlow verifies that flow does not cross function boundaries.
func TestNoCrossFunctionFlow(t *testing.T) {
	// fn1=1, fn2=2, sym_x=10 in fn1, sym_y=20 in fn2
	// Assign x = y but they are in different functions → no LocalFlow.
	baseRels := localFlowBaseRels(map[string]*eval.Relation{
		"Assign":     makeRel("Assign", 3, iv(100), iv(200), iv(10)),
		"ExprMayRef": makeRel("ExprMayRef", 2, iv(200), iv(20)),
		"SymInFunction": makeRel("SymInFunction", 2,
			iv(10), iv(1), // sym_x in fn1
			iv(20), iv(2), // sym_y in fn2
		),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("fn"), v("src"), v("dst")},
		Body:   []datalog.Literal{pos("LocalFlow", v("fn"), v("src"), v("dst"))},
	}

	rs := planAndEval(t, LocalFlowRules(), query, baseRels)
	if len(rs.Rows) != 0 {
		t.Errorf("expected 0 cross-function LocalFlow rows, got %d: %v", len(rs.Rows), rs.Rows)
	}
}

// TestSelfAssignment tests x = x → LocalFlow(fn, sym_x, sym_x).
func TestSelfAssignment(t *testing.T) {
	// fn=1, sym_x=10, lhsNode=100, rhsExpr=200
	baseRels := localFlowBaseRels(map[string]*eval.Relation{
		"Assign":        makeRel("Assign", 3, iv(100), iv(200), iv(10)),
		"ExprMayRef":    makeRel("ExprMayRef", 2, iv(200), iv(10)),
		"SymInFunction": makeRel("SymInFunction", 2, iv(10), iv(1)),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("fn"), v("src"), v("dst")},
		Body:   []datalog.Literal{pos("LocalFlow", v("fn"), v("src"), v("dst"))},
	}

	rs := planAndEval(t, LocalFlowRules(), query, baseRels)
	if len(rs.Rows) != 1 {
		t.Fatalf("expected 1 self-assignment LocalFlow row, got %d: %v", len(rs.Rows), rs.Rows)
	}
	if !resultContains(rs, iv(1), iv(10), iv(10)) {
		t.Errorf("expected LocalFlow(1, 10, 10), got %v", rs.Rows)
	}
}

// TestFieldWriteFlow tests rule 6: obj.f = x → LocalFlow(fn, sym_x, sym_obj).
func TestFieldWriteFlow(t *testing.T) {
	// fn=1, sym_obj=10, sym_x=20, assignNode=100, rhsExpr=200
	baseRels := localFlowBaseRels(map[string]*eval.Relation{
		"FieldWrite":    makeRel("FieldWrite", 4, iv(100), iv(10), sv("f"), iv(200)),
		"ExprMayRef":    makeRel("ExprMayRef", 2, iv(200), iv(20)),
		"SymInFunction": makeRel("SymInFunction", 2, iv(10), iv(1), iv(20), iv(1)),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("fn"), v("src"), v("dst")},
		Body:   []datalog.Literal{pos("LocalFlow", v("fn"), v("src"), v("dst"))},
	}

	rs := planAndEval(t, LocalFlowRules(), query, baseRels)
	if len(rs.Rows) != 1 {
		t.Fatalf("expected 1 FieldWrite LocalFlow row, got %d: %v", len(rs.Rows), rs.Rows)
	}
	if !resultContains(rs, iv(1), iv(20), iv(10)) {
		t.Errorf("expected LocalFlow(1, 20, 10), got %v", rs.Rows)
	}
}

// TestFieldReadFlow tests rule 5: y = obj.f → LocalFlow(fn, sym_obj, sym_y).
func TestFieldReadFlow(t *testing.T) {
	// fn=1, sym_obj=10, sym_y=20, expr=300
	// FieldRead(expr=300, baseSym=10, fieldName="f")
	// ExprMayRef(expr=300, exprSym=20) — the read expression refers to sym_y
	baseRels := localFlowBaseRels(map[string]*eval.Relation{
		"FieldRead":     makeRel("FieldRead", 3, iv(300), iv(10), sv("f")),
		"ExprMayRef":    makeRel("ExprMayRef", 2, iv(300), iv(20)),
		"SymInFunction": makeRel("SymInFunction", 2, iv(10), iv(1), iv(20), iv(1)),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("fn"), v("src"), v("dst")},
		Body:   []datalog.Literal{pos("LocalFlow", v("fn"), v("src"), v("dst"))},
	}

	rs := planAndEval(t, LocalFlowRules(), query, baseRels)
	if len(rs.Rows) != 1 {
		t.Fatalf("expected 1 FieldRead LocalFlow row, got %d: %v", len(rs.Rows), rs.Rows)
	}
	if !resultContains(rs, iv(1), iv(10), iv(20)) {
		t.Errorf("expected LocalFlow(1, 10, 20), got %v", rs.Rows)
	}
}

// TestLocalFlowRulesCount verifies we produce exactly 8 rules.
func TestLocalFlowRulesCount(t *testing.T) {
	rules := LocalFlowRules()
	if len(rules) != 8 {
		t.Errorf("expected 8 local flow rules, got %d", len(rules))
	}
}

// TestLocalFlowRulesValidate verifies all rules pass the planner's validation.
func TestLocalFlowRulesValidate(t *testing.T) {
	for i, r := range LocalFlowRules() {
		errs := plan.ValidateRule(r)
		if len(errs) > 0 {
			t.Errorf("rule %d (%s) validation errors: %v", i, r.Head.Predicate, errs)
		}
	}
}

// TestLocalFlowRulesStratify verifies the rules can be stratified (no recursive negation).
func TestLocalFlowRulesStratify(t *testing.T) {
	prog := &datalog.Program{Rules: LocalFlowRules()}
	_, errs := plan.Plan(prog, nil)
	if len(errs) > 0 {
		t.Fatalf("local flow rules failed to plan: %v", errs)
	}
}

// TestAllSystemRulesStratify verifies call graph + local flow rules together can be stratified.
func TestAllSystemRulesStratify(t *testing.T) {
	prog := &datalog.Program{Rules: AllSystemRules()}
	_, errs := plan.Plan(prog, nil)
	if len(errs) > 0 {
		t.Fatalf("all system rules failed to plan: %v", errs)
	}
}

// TestAllSystemRulesCount verifies AllSystemRules returns combined count.
func TestAllSystemRulesCount(t *testing.T) {
	all := AllSystemRules()
	cg := CallGraphRules()
	lf := LocalFlowRules()
	if len(all) != len(cg)+len(lf) {
		t.Errorf("expected %d rules, got %d", len(cg)+len(lf), len(all))
	}
}

// TestLocalFlowStarTransitivity property: if LocalFlowStar(fn, a, b) and LocalFlow(fn, b, c)
// then LocalFlowStar(fn, a, c).
func TestLocalFlowStarTransitivity(t *testing.T) {
	// Chain: a→b→c→d (all in same fn=1)
	baseRels := localFlowBaseRels(map[string]*eval.Relation{
		"Assign": makeRel("Assign", 3,
			iv(100), iv(200), iv(20), // b = a
			iv(101), iv(201), iv(30), // c = b
			iv(102), iv(202), iv(40), // d = c
		),
		"ExprMayRef": makeRel("ExprMayRef", 2,
			iv(200), iv(10), // a
			iv(201), iv(20), // b
			iv(202), iv(30), // c
		),
		"SymInFunction": makeRel("SymInFunction", 2,
			iv(10), iv(1),
			iv(20), iv(1),
			iv(30), iv(1),
			iv(40), iv(1),
		),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("fn"), v("src"), v("dst")},
		Body:   []datalog.Literal{pos("LocalFlowStar", v("fn"), v("src"), v("dst"))},
	}

	rs := planAndEval(t, LocalFlowRules(), query, baseRels)
	// Direct: (10→20), (20→30), (30→40)
	// Transitive: (10→30), (10→40), (20→40)
	// Total: 6
	if len(rs.Rows) != 6 {
		t.Fatalf("expected 6 LocalFlowStar rows for 4-node chain, got %d: %v", len(rs.Rows), rs.Rows)
	}
	// Check the longest transitive path
	if !resultContains(rs, iv(1), iv(10), iv(40)) {
		t.Errorf("expected transitive LocalFlowStar(1, 10, 40)")
	}
}

// TestFunctionScoping verifies that two parallel chains in different functions
// do not interfere with each other.
func TestFunctionScoping(t *testing.T) {
	// fn1=1: a(10)→b(20)
	// fn2=2: c(30)→d(40)
	baseRels := localFlowBaseRels(map[string]*eval.Relation{
		"Assign": makeRel("Assign", 3,
			iv(100), iv(200), iv(20), // b = a (fn1)
			iv(101), iv(201), iv(40), // d = c (fn2)
		),
		"ExprMayRef": makeRel("ExprMayRef", 2,
			iv(200), iv(10),
			iv(201), iv(30),
		),
		"SymInFunction": makeRel("SymInFunction", 2,
			iv(10), iv(1),
			iv(20), iv(1),
			iv(30), iv(2),
			iv(40), iv(2),
		),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("fn"), v("src"), v("dst")},
		Body:   []datalog.Literal{pos("LocalFlow", v("fn"), v("src"), v("dst"))},
	}

	rs := planAndEval(t, LocalFlowRules(), query, baseRels)
	if len(rs.Rows) != 2 {
		t.Fatalf("expected 2 LocalFlow rows (one per function), got %d: %v", len(rs.Rows), rs.Rows)
	}
	if !resultContains(rs, iv(1), iv(10), iv(20)) {
		t.Errorf("expected LocalFlow(1, 10, 20)")
	}
	if !resultContains(rs, iv(2), iv(30), iv(40)) {
		t.Errorf("expected LocalFlow(2, 30, 40)")
	}
}

// TestDestructuringFlow tests rule 4: const {a} = obj → LocalFlow(fn, sym_obj, sym_a).
func TestDestructuringFlow(t *testing.T) {
	// fn=1, sym_obj=10, sym_a=20, parent(VarDecl)=100, initExpr=200
	baseRels := localFlowBaseRels(map[string]*eval.Relation{
		"DestructureField": makeRel("DestructureField", 5,
			iv(100), sv("a"), sv("a"), iv(20), iv(0)),
		"VarDecl":       makeRel("VarDecl", 4, iv(100), iv(99), iv(200), iv(1)),
		"ExprMayRef":    makeRel("ExprMayRef", 2, iv(200), iv(10)),
		"SymInFunction": makeRel("SymInFunction", 2, iv(10), iv(1), iv(20), iv(1)),
	})

	query := &datalog.Query{
		Select: []datalog.Term{v("fn"), v("src"), v("dst")},
		Body:   []datalog.Literal{pos("LocalFlow", v("fn"), v("src"), v("dst"))},
	}

	rs := planAndEval(t, LocalFlowRules(), query, baseRels)
	if !resultContains(rs, iv(1), iv(10), iv(20)) {
		t.Errorf("expected LocalFlow(1, 10, 20) for destructuring, got %v", rs.Rows)
	}
}

// TestEmptyRelationsNoFlow verifies no flow is produced from empty base relations.
func TestEmptyRelationsNoFlow(t *testing.T) {
	baseRels := localFlowBaseRels(nil)

	query := &datalog.Query{
		Select: []datalog.Term{v("fn"), v("src"), v("dst")},
		Body:   []datalog.Literal{pos("LocalFlow", v("fn"), v("src"), v("dst"))},
	}

	rs := planAndEval(t, LocalFlowRules(), query, baseRels)
	if len(rs.Rows) != 0 {
		t.Errorf("expected 0 LocalFlow rows from empty relations, got %d", len(rs.Rows))
	}
}
