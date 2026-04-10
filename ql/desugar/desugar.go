// Package desugar lowers QL OOP constructs to flat Datalog rules.
package desugar

import (
	"fmt"

	"github.com/Gjdoalfnrxu/tsq/ql/ast"
	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/resolve"
)

// Desugar lowers a resolved QL module to a Datalog program.
// Errors are non-fatal; the returned program is always non-nil.
func Desugar(mod *resolve.ResolvedModule) (*datalog.Program, []error) {
	d := &desugarer{
		mod:      mod,
		ann:      mod.Annotations,
		env:      mod.Env,
		subClasses: make(map[string][]string),
	}
	d.buildSubclassMap()
	return d.run()
}

// freshVarGen generates unique variable names within a rule/query scope.
type freshVarGen struct{ n int }

func (g *freshVarGen) next() datalog.Var {
	g.n++
	return datalog.Var{Name: fmt.Sprintf("_v%d", g.n)}
}

// desugarer holds all state for a single desugaring run.
type desugarer struct {
	mod        *resolve.ResolvedModule
	ann        *resolve.Annotations
	env        *resolve.Environment
	errors     []error
	// subClasses maps class name → names of classes that directly extend it.
	subClasses map[string][]string
}

func (d *desugarer) errorf(format string, args ...interface{}) {
	d.errors = append(d.errors, fmt.Errorf(format, args...))
}

// buildSubclassMap builds the direct subclass relationship from the AST.
func (d *desugarer) buildSubclassMap() {
	for i := range d.mod.AST.Classes {
		cd := &d.mod.AST.Classes[i]
		for _, st := range cd.SuperTypes {
			stName := st.String()
			d.subClasses[stName] = append(d.subClasses[stName], cd.Name)
		}
	}
}

// run performs the full desugaring pass.
func (d *desugarer) run() (*datalog.Program, []error) {
	prog := &datalog.Program{}

	// Desugar each class.
	for i := range d.mod.AST.Classes {
		cd := &d.mod.AST.Classes[i]
		rules := d.desugarClass(cd)
		prog.Rules = append(prog.Rules, rules...)
	}

	// Desugar top-level predicates.
	for i := range d.mod.AST.Predicates {
		pd := &d.mod.AST.Predicates[i]
		rules := d.desugarTopLevelPredicate(pd)
		prog.Rules = append(prog.Rules, rules...)
	}

	// Desugar select clause.
	if d.mod.AST.Select != nil {
		q := d.desugarSelect(d.mod.AST.Select)
		prog.Query = q
	}

	return prog, d.errors
}

// ---- Class desugaring ----

// desugarClass emits the characteristic predicate rule and all method rules.
func (d *desugarer) desugarClass(cd *ast.ClassDecl) []datalog.Rule {
	var rules []datalog.Rule

	// Characteristic predicate: Foo(this) :- SuperTypes(this)..., body.
	{
		gen := &freshVarGen{}
		body := d.superTypeConstraints(cd, gen)
		if cd.CharPred != nil {
			body = append(body, d.desugarFormula(*cd.CharPred, gen)...)
		}
		rule := datalog.Rule{
			Head: datalog.Atom{
				Predicate: cd.Name,
				Args:      []datalog.Term{datalog.Var{Name: "this"}},
			},
			Body: body,
		}
		rules = append(rules, rule)
	}

	// Methods.
	for i := range cd.Members {
		md := &cd.Members[i]
		rules = append(rules, d.desugarMethod(cd, md)...)
	}

	return rules
}

// superTypeConstraints returns Literal{Foo(this)} for each supertype.
func (d *desugarer) superTypeConstraints(cd *ast.ClassDecl, _ *freshVarGen) []datalog.Literal {
	var lits []datalog.Literal
	for _, st := range cd.SuperTypes {
		lits = append(lits, datalog.Literal{
			Positive: true,
			Atom: datalog.Atom{
				Predicate: st.String(),
				Args:      []datalog.Term{datalog.Var{Name: "this"}},
			},
		})
	}
	return lits
}

// desugarMethod emits rules for a method, handling override dispatch.
//
// For single inheritance, if a subclass also defines the same method,
// the base class rule gets a "not SubClass(this)" exclusion.
func (d *desugarer) desugarMethod(cd *ast.ClassDecl, md *ast.MemberDecl) []datalog.Rule {
	mangledName := mangle(cd.Name, md.Name)

	// Collect all subclasses (direct) that override this method.
	overridingSubClasses := d.directSubClassesWithMethod(cd.Name, md.Name)

	// Base rule: this class's own implementation.
	baseRule := d.buildMethodRule(cd, md, mangledName, overridingSubClasses)
	rules := []datalog.Rule{baseRule}

	// Override rules: for each overriding subclass, emit a rule on the
	// *mangled name of the base class* (so callers of Foo_getX get all dispatch).
	// We also emit a rule for the subclass's own mangled name.
	for _, subName := range overridingSubClasses {
		subCD, ok := d.env.Classes[subName]
		if !ok {
			continue
		}
		// Find the overriding member in the subclass.
		var overrideMD *ast.MemberDecl
		for i := range subCD.Members {
			if subCD.Members[i].Name == md.Name {
				overrideMD = &subCD.Members[i]
				break
			}
		}
		if overrideMD == nil {
			continue
		}

		// Emit: BaseName_method(this, result) :- SubClass(this), [sub body].
		// Also further exclude sub-subclasses that override.
		subOverriders := d.directSubClassesWithMethod(subName, md.Name)
		overrideRule := d.buildMethodRule(subCD, overrideMD, mangledName, subOverriders)
		rules = append(rules, overrideRule)
	}

	return rules
}

// buildMethodRule constructs one Datalog rule for a method.
// excludeSubs is the set of direct subclasses that override this method;
// they are added as "not SubClass(this)" constraints to the body.
func (d *desugarer) buildMethodRule(cd *ast.ClassDecl, md *ast.MemberDecl, headPred string, excludeSubs []string) datalog.Rule {
	gen := &freshVarGen{}

	// Head args: (this [, params...] [, result])
	headArgs := []datalog.Term{datalog.Var{Name: "this"}}
	for _, param := range md.Params {
		headArgs = append(headArgs, datalog.Var{Name: param.Name})
	}
	isPredicate := md.ReturnType == nil
	if !isPredicate {
		headArgs = append(headArgs, datalog.Var{Name: "result"})
	}

	head := datalog.Atom{
		Predicate: headPred,
		Args:      headArgs,
	}

	// Body: class constraint, subclass exclusions, formula.
	body := []datalog.Literal{
		{Positive: true, Atom: datalog.Atom{
			Predicate: cd.Name,
			Args:      []datalog.Term{datalog.Var{Name: "this"}},
		}},
	}

	// Exclude overriding subclasses.
	for _, subName := range excludeSubs {
		body = append(body, datalog.Literal{
			Positive: false,
			Atom: datalog.Atom{
				Predicate: subName,
				Args:      []datalog.Term{datalog.Var{Name: "this"}},
			},
		})
	}

	if md.Body != nil {
		body = append(body, d.desugarFormula(*md.Body, gen)...)
	}

	return datalog.Rule{Head: head, Body: body}
}

// directSubClassesWithMethod returns names of classes that:
// (a) directly extend className, AND
// (b) directly declare a method named methodName.
func (d *desugarer) directSubClassesWithMethod(className, methodName string) []string {
	var result []string
	for _, subName := range d.subClasses[className] {
		subCD, ok := d.env.Classes[subName]
		if !ok {
			continue
		}
		for i := range subCD.Members {
			if subCD.Members[i].Name == methodName {
				result = append(result, subName)
				break
			}
		}
	}
	return result
}

// mangle produces the Datalog predicate name for a class method.
func mangle(className, methodName string) string {
	return className + "_" + methodName
}

// ---- Top-level predicate desugaring ----

func (d *desugarer) desugarTopLevelPredicate(pd *ast.PredicateDecl) []datalog.Rule {
	gen := &freshVarGen{}

	// Head args: params, then result if function.
	headArgs := make([]datalog.Term, 0, len(pd.Params)+1)
	for _, param := range pd.Params {
		headArgs = append(headArgs, datalog.Var{Name: param.Name})
	}
	if pd.ReturnType != nil {
		headArgs = append(headArgs, datalog.Var{Name: "result"})
	}

	head := datalog.Atom{
		Predicate: pd.Name,
		Args:      headArgs,
	}

	var body []datalog.Literal
	if pd.Body != nil {
		body = d.desugarFormula(*pd.Body, gen)
	}

	return []datalog.Rule{{Head: head, Body: body}}
}

// ---- Select clause desugaring ----

func (d *desugarer) desugarSelect(sel *ast.SelectClause) *datalog.Query {
	gen := &freshVarGen{}
	var body []datalog.Literal

	// Type constraints for each from declaration.
	for _, vd := range sel.Decls {
		typeName := vd.Type.String()
		if !isPrimitive(typeName) {
			body = append(body, datalog.Literal{
				Positive: true,
				Atom: datalog.Atom{
					Predicate: typeName,
					Args:      []datalog.Term{datalog.Var{Name: vd.Name}},
				},
			})
		}
	}

	// Where clause.
	if sel.Where != nil {
		body = append(body, d.desugarFormula(*sel.Where, gen)...)
	}

	// Select expressions.
	var selectTerms []datalog.Term
	for _, e := range sel.Select {
		t, extraLits := d.desugarExpr(e, gen)
		body = append(body, extraLits...)
		selectTerms = append(selectTerms, t)
	}

	return &datalog.Query{
		Select: selectTerms,
		Body:   body,
	}
}

// ---- Formula desugaring ----

// desugarFormula recursively lowers an ast.Formula to a slice of Datalog literals.
func (d *desugarer) desugarFormula(f ast.Formula, gen *freshVarGen) []datalog.Literal {
	if f == nil {
		return nil
	}
	switch n := f.(type) {
	case *ast.Conjunction:
		left := d.desugarFormula(n.Left, gen)
		right := d.desugarFormula(n.Right, gen)
		return append(left, right...)

	case *ast.Disjunction:
		// Datalog doesn't natively support disjunction in rule bodies.
		// We represent it as a best-effort by emitting both sides.
		// (A full implementation would split into two rules at the call site.)
		// For now, emit left side (document limitation).
		left := d.desugarFormula(n.Left, gen)
		// TODO: full disjunction requires rule splitting; only left branch emitted.
		_ = n.Right
		return left

	case *ast.Negation:
		inner := d.desugarFormula(n.Formula, gen)
		// Wrap each inner literal in negation.
		// If there are multiple literals, wrap in a helper — for simplicity,
		// we negate the first atom or comparison we find.
		if len(inner) == 1 {
			lit := inner[0]
			lit.Positive = !lit.Positive
			return []datalog.Literal{lit}
		}
		// Multiple literals: negate the entire conjunction by negating each.
		for i := range inner {
			inner[i].Positive = !inner[i].Positive
		}
		return inner

	case *ast.Comparison:
		left, leftLits := d.desugarExpr(n.Left, gen)
		right, rightLits := d.desugarExpr(n.Right, gen)
		lits := append(leftLits, rightLits...)
		lits = append(lits, datalog.Literal{
			Positive: true,
			Cmp: &datalog.Comparison{
				Op:    n.Op,
				Left:  left,
				Right: right,
			},
		})
		return lits

	case *ast.PredicateCall:
		return d.desugarPredicateCall(n, gen)

	case *ast.InstanceOf:
		term, extraLits := d.desugarExpr(n.Expr, gen)
		lits := append(extraLits, datalog.Literal{
			Positive: true,
			Atom: datalog.Atom{
				Predicate: n.Type.String(),
				Args:      []datalog.Term{term},
			},
		})
		return lits

	case *ast.Exists:
		return d.desugarExists(n, gen)

	case *ast.Forall:
		return d.desugarForall(n, gen)

	case *ast.None:
		// none() — always false; represent as a self-contradicting literal.
		// Emit: not _none() where _none is never defined → always fails.
		return []datalog.Literal{
			{Positive: false, Atom: datalog.Atom{Predicate: "_none", Args: nil}},
		}

	case *ast.Any:
		// any() — always true; no constraints.
		return nil

	default:
		d.errorf("unknown formula type %T", f)
		return nil
	}
}

// desugarPredicateCall handles a PredicateCall used as a formula.
func (d *desugarer) desugarPredicateCall(pc *ast.PredicateCall, gen *freshVarGen) []datalog.Literal {
	if pc.Recv != nil {
		// Method call as formula (predicate call on receiver — no result).
		recvTerm, extraLits := d.desugarExpr(pc.Recv, gen)
		args := []datalog.Term{recvTerm}
		for _, arg := range pc.Args {
			t, lits := d.desugarExpr(arg, gen)
			extraLits = append(extraLits, lits...)
			args = append(args, t)
		}

		predName := d.resolveMethodCallPred(pc.Recv, pc.Name)
		if predName == "" {
			predName = pc.Name
		}
		lit := datalog.Literal{
			Positive: true,
			Atom:     datalog.Atom{Predicate: predName, Args: args},
		}
		return append(extraLits, lit)
	}

	// Bare predicate call.
	var allLits []datalog.Literal
	args := make([]datalog.Term, 0, len(pc.Args))
	for _, arg := range pc.Args {
		t, lits := d.desugarExpr(arg, gen)
		allLits = append(allLits, lits...)
		args = append(args, t)
	}
	allLits = append(allLits, datalog.Literal{
		Positive: true,
		Atom:     datalog.Atom{Predicate: pc.Name, Args: args},
	})
	return allLits
}

// desugarExists: exists(decls | [guard |] body)
// Introduces fresh variables for the declared vars and inlines body.
// The declared variables scoped to the exists become literals in the outer conjunction.
func (d *desugarer) desugarExists(n *ast.Exists, gen *freshVarGen) []datalog.Literal {
	var lits []datalog.Literal

	// Type constraints for each declared variable.
	for _, vd := range n.Decls {
		typeName := vd.Type.String()
		if !isPrimitive(typeName) {
			lits = append(lits, datalog.Literal{
				Positive: true,
				Atom: datalog.Atom{
					Predicate: typeName,
					Args:      []datalog.Term{datalog.Var{Name: vd.Name}},
				},
			})
		}
	}

	if n.Guard != nil {
		lits = append(lits, d.desugarFormula(n.Guard, gen)...)
	}
	lits = append(lits, d.desugarFormula(n.Body, gen)...)
	return lits
}

// desugarForall: forall(decls | guard | body)
// Desugared as: not(guard and not(body))
// In Datalog literals: not Guard OR (Guard AND Body) — we use double negation.
// Representation: for each guard literal G, emit:
//   not G_v  OR  (G_v AND Body_v)
// Simplified: we emit the guard literals negated, then the body using double-neg.
//
// Full double-negation in stratified Datalog:
//   forall v: G(v) => B(v)
//   ≡ not exists v: G(v) and not B(v)
//
// We cannot express "not exists" directly in a rule body without a helper predicate.
// We emit it as nested negation literals (relying on the planner to stratify).
func (d *desugarer) desugarForall(n *ast.Forall, gen *freshVarGen) []datalog.Literal {
	// Desugar guard and body independently.
	guardLits := d.desugarFormula(n.Guard, gen)
	bodyLits := d.desugarFormula(n.Body, gen)

	// Double negation pattern:
	// We want: not(exists v: guard(v) and not(body(v)))
	// Represented as: for each guard literal, negate it (not guard),
	// and for each body literal, negate-of-negate (body):
	// not(G) and body  — outer "not exists" is approximated as:
	//   each guard lit negated (the "there is no v violating") combined
	//   with body positively asserted.
	//
	// Faithful representation: emit guard negated as negated literals.
	// This is a stratified-Datalog approximation.
	var lits []datalog.Literal

	// Type constraints for declared vars.
	for _, vd := range n.Decls {
		typeName := vd.Type.String()
		if !isPrimitive(typeName) {
			// In forall, the type constraint for the universally quantified var
			// appears inside the "not exists" scope — negate it.
			// Pattern: not (TypeName(v) and GuardLits(v) and not BodyLits(v))
			// We emit: not TypeName(v), GuardLits(v)..., not BodyLits(v)...
			// This is the double-negation approximation.
			_ = typeName // used below in guard lits
		}
	}

	// Outer negation of the guard (approximation of "not exists v: guard and not body").
	for _, gl := range guardLits {
		neg := gl
		neg.Positive = !neg.Positive
		lits = append(lits, neg)
	}
	// Body positively (the "not not body" = body part).
	lits = append(lits, bodyLits...)

	return lits
}

// ---- Expression desugaring ----

// desugarExpr lowers an ast.Expr to a (Term, []Literal) pair.
// The literals are side-effects (fresh variable bindings, method call atoms).
func (d *desugarer) desugarExpr(e ast.Expr, gen *freshVarGen) (datalog.Term, []datalog.Literal) {
	if e == nil {
		return datalog.Wildcard{}, nil
	}
	switch n := e.(type) {
	case *ast.Variable:
		return datalog.Var{Name: n.Name}, nil

	case *ast.IntLiteral:
		return datalog.IntConst{Value: n.Value}, nil

	case *ast.StringLiteral:
		return datalog.StringConst{Value: n.Value}, nil

	case *ast.BoolLiteral:
		if n.Value {
			return datalog.IntConst{Value: 1}, nil
		}
		return datalog.IntConst{Value: 0}, nil

	case *ast.MethodCall:
		return d.desugarMethodCallExpr(n, gen)

	case *ast.Cast:
		inner, lits := d.desugarExpr(n.Expr, gen)
		// Add type constraint atom.
		lits = append(lits, datalog.Literal{
			Positive: true,
			Atom: datalog.Atom{
				Predicate: n.Type.String(),
				Args:      []datalog.Term{inner},
			},
		})
		return inner, lits

	case *ast.Aggregate:
		return d.desugarAggregateExpr(n, gen)

	case *ast.BinaryExpr:
		// Arithmetic — represent as a fresh variable holding the result.
		// (Full arithmetic requires special handling in the planner.)
		left, leftLits := d.desugarExpr(n.Left, gen)
		right, rightLits := d.desugarExpr(n.Right, gen)
		allLits := append(leftLits, rightLits...)
		// Emit an arithmetic constraint as a comparison-like literal.
		// We use a fresh var and a pseudo-predicate for the planner.
		fresh := gen.next()
		allLits = append(allLits, datalog.Literal{
			Positive: true,
			Cmp: &datalog.Comparison{
				Op:    "=",
				Left:  fresh,
				Right: datalog.Var{Name: fmt.Sprintf("arith(%s%s%s)", termStr(left), n.Op, termStr(right))},
			},
		})
		_ = right
		return fresh, allLits

	case *ast.FieldAccess:
		// Field access: treated as a method call with no args.
		recv, lits := d.desugarExpr(n.Recv, gen)
		predName := d.resolveFieldAccessPred(n)
		fresh := gen.next()
		lits = append(lits, datalog.Literal{
			Positive: true,
			Atom: datalog.Atom{
				Predicate: predName,
				Args:      []datalog.Term{recv, fresh},
			},
		})
		return fresh, lits

	default:
		d.errorf("unknown expr type %T", e)
		return datalog.Wildcard{}, nil
	}
}

// desugarMethodCallExpr lowers expr.method(args...) used as an expression.
// Returns a fresh variable bound to the method's result.
func (d *desugarer) desugarMethodCallExpr(mc *ast.MethodCall, gen *freshVarGen) (datalog.Term, []datalog.Literal) {
	recv, lits := d.desugarExpr(mc.Recv, gen)

	// Determine the predicate name.
	predName := d.resolveMethodCallPred(mc, mc.Method)

	// Desugar args.
	args := []datalog.Term{recv}
	for _, arg := range mc.Args {
		t, argLits := d.desugarExpr(arg, gen)
		lits = append(lits, argLits...)
		args = append(args, t)
	}

	// Fresh variable for the result.
	fresh := gen.next()
	args = append(args, fresh)

	lits = append(lits, datalog.Literal{
		Positive: true,
		Atom:     datalog.Atom{Predicate: predName, Args: args},
	})

	return fresh, lits
}

// resolveMethodCallPred determines the mangled Datalog predicate name for a method call.
// It uses the resolver annotations to find the defining class.
func (d *desugarer) resolveMethodCallPred(recv ast.Expr, methodName string) string {
	if d.ann != nil {
		if res, ok := d.ann.ExprResolutions[recv]; ok && res != nil && res.DeclClass != nil {
			return mangle(res.DeclClass.Name, methodName)
		}
	}
	// Fallback: try to infer from variable type in the expression itself.
	// For MethodCall receivers, the resolution is keyed on the mc node, not recv.
	return methodName
}

// resolveFieldAccessPred determines the predicate name for a field access.
func (d *desugarer) resolveFieldAccessPred(fa *ast.FieldAccess) string {
	if d.ann != nil {
		if res, ok := d.ann.ExprResolutions[fa]; ok && res != nil && res.DeclClass != nil {
			return mangle(res.DeclClass.Name, fa.Field)
		}
	}
	return fa.Field
}

// desugarAggregateExpr lowers an ast.Aggregate expression.
func (d *desugarer) desugarAggregateExpr(a *ast.Aggregate, gen *freshVarGen) (datalog.Term, []datalog.Literal) {
	// Build body literals from the aggregate's guard and body.
	var bodyLits []datalog.Literal

	// Type constraints for declared vars.
	for _, vd := range a.Decls {
		typeName := vd.Type.String()
		if !isPrimitive(typeName) {
			bodyLits = append(bodyLits, datalog.Literal{
				Positive: true,
				Atom: datalog.Atom{
					Predicate: typeName,
					Args:      []datalog.Term{datalog.Var{Name: vd.Name}},
				},
			})
		}
	}

	innerGen := &freshVarGen{}
	if a.Guard != nil {
		bodyLits = append(bodyLits, d.desugarFormula(a.Guard, innerGen)...)
	}
	if a.Body != nil {
		bodyLits = append(bodyLits, d.desugarFormula(a.Body, innerGen)...)
	}

	// Determine aggregated variable name and type.
	aggVar := ""
	aggType := ""
	if len(a.Decls) > 0 {
		aggVar = a.Decls[0].Name
		aggType = a.Decls[0].Type.String()
	}

	// Aggregated expression (for min/max/sum/avg).
	var aggExpr datalog.Term
	if a.Expr != nil {
		var exprLits []datalog.Literal
		aggExpr, exprLits = d.desugarExpr(a.Expr, innerGen)
		bodyLits = append(bodyLits, exprLits...)
	}

	// A fresh variable holds the aggregate result.
	fresh := gen.next()
	agg := &datalog.Aggregate{
		Func:     a.Op,
		Var:      aggVar,
		TypeName: aggType,
		Body:     bodyLits,
		Expr:     aggExpr,
	}

	lit := datalog.Literal{
		Positive: true,
		Agg:      agg,
	}

	// Bind fresh var to the aggregate result via an equality comparison.
	bindLit := datalog.Literal{
		Positive: true,
		Cmp: &datalog.Comparison{
			Op:    "=",
			Left:  fresh,
			Right: datalog.Var{Name: fmt.Sprintf("agg_result_%s", a.Op)},
		},
	}
	_ = bindLit

	return fresh, []datalog.Literal{lit}
}

// ---- Helpers ----

// isPrimitive returns true for built-in scalar types that don't have class predicates.
func isPrimitive(typeName string) bool {
	switch typeName {
	case "int", "float", "string", "boolean", "date":
		return true
	}
	return false
}

func termStr(t datalog.Term) string {
	switch v := t.(type) {
	case datalog.Var:
		return v.Name
	case datalog.IntConst:
		return fmt.Sprintf("%d", v.Value)
	case datalog.StringConst:
		return fmt.Sprintf("%q", v.Value)
	default:
		return "_"
	}
}
