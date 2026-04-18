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
		mod:        mod,
		ann:        mod.Annotations,
		env:        mod.Env,
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
	mod    *resolve.ResolvedModule
	ann    *resolve.Annotations
	env    *resolve.Environment
	errors []error
	// currentClass tracks the class being desugared (for super resolution).
	currentClass *ast.ClassDecl
	// subClasses maps class name → names of classes that directly extend it.
	subClasses map[string][]string
	// syntheticRules accumulates rules generated for disjunction/negation.
	syntheticRules []datalog.Rule
	// synthCounter generates unique names for synthetic predicates.
	synthCounter int
}

func (d *desugarer) errorf(format string, args ...interface{}) {
	d.errors = append(d.errors, fmt.Errorf(format, args...))
}

// freshSynthName generates a unique synthetic predicate name with the given prefix.
func (d *desugarer) freshSynthName(prefix string) string {
	d.synthCounter++
	return fmt.Sprintf("%s_%d", prefix, d.synthCounter)
}

// buildSubclassMap builds the direct subclass relationship from the AST.
func (d *desugarer) buildSubclassMap() {
	// Include imported modules' classes in the subclass map.
	if d.env != nil && d.env.Imports != nil {
		for _, imp := range d.env.Imports {
			if imp == nil || imp.AST == nil {
				continue
			}
			for i := range imp.AST.Classes {
				cd := &imp.AST.Classes[i]
				for _, st := range cd.SuperTypes {
					stName := st.String()
					d.subClasses[stName] = append(d.subClasses[stName], cd.Name)
				}
			}
		}
	}
	for i := range d.mod.AST.Classes {
		cd := &d.mod.AST.Classes[i]
		for _, st := range cd.SuperTypes {
			stName := st.String()
			d.subClasses[stName] = append(d.subClasses[stName], cd.Name)
		}
	}
	// Include module-scoped classes in the subclass map.
	for i := range d.mod.AST.Modules {
		d.buildSubclassMapForModule(&d.mod.AST.Modules[i], "")
	}
}

func (d *desugarer) buildSubclassMapForModule(md *ast.ModuleDecl, prefix string) {
	qualPrefix := md.Name
	if prefix != "" {
		qualPrefix = prefix + "::" + md.Name
	}
	for i := range md.Classes {
		cd := &md.Classes[i]
		qualName := qualPrefix + "::" + cd.Name
		for _, st := range cd.SuperTypes {
			stName := st.String()
			// If the supertype is unqualified and exists within this same module,
			// register under the qualified name too so abstract class lookup works.
			d.subClasses[stName] = append(d.subClasses[stName], qualName)
			qualSuperName := qualPrefix + "::" + stName
			if qualSuperName != stName {
				d.subClasses[qualSuperName] = append(d.subClasses[qualSuperName], qualName)
			}
		}
	}
	for i := range md.Modules {
		d.buildSubclassMapForModule(&md.Modules[i], qualPrefix)
	}
}

// run performs the full desugaring pass.
func (d *desugarer) run() (*datalog.Program, []error) {
	prog := &datalog.Program{}

	// Desugar imported modules' classes and predicates first so their
	// derived relations are available to the user's query.
	if d.env != nil && d.env.Imports != nil {
		for _, imp := range d.env.Imports {
			if imp == nil || imp.AST == nil {
				continue
			}
			// Merge annotations from imported module so that
			// ExprResolutions / VarBindings are available during desugaring.
			if imp.Annotations != nil {
				for k, v := range imp.Annotations.ExprResolutions {
					if _, exists := d.ann.ExprResolutions[k]; !exists {
						d.ann.ExprResolutions[k] = v
					}
				}
				for k, v := range imp.Annotations.VarBindings {
					if _, exists := d.ann.VarBindings[k]; !exists {
						d.ann.VarBindings[k] = v
					}
				}
			}
			for i := range imp.AST.Classes {
				cd := &imp.AST.Classes[i]
				rules := d.desugarClass(cd)
				prog.Rules = append(prog.Rules, rules...)
			}
			for i := range imp.AST.Predicates {
				pd := &imp.AST.Predicates[i]
				rules := d.desugarTopLevelPredicate(pd)
				prog.Rules = append(prog.Rules, rules...)
			}
			for i := range imp.AST.Modules {
				rules := d.desugarModuleDecl(&imp.AST.Modules[i], "")
				prog.Rules = append(prog.Rules, rules...)
			}
		}
	}

	// Desugar each class in the main module.
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

	// Desugar module declarations.
	for i := range d.mod.AST.Modules {
		rules := d.desugarModuleDecl(&d.mod.AST.Modules[i], "")
		prog.Rules = append(prog.Rules, rules...)
	}

	// Desugar select clause.
	if d.mod.AST.Select != nil {
		q := d.desugarSelect(d.mod.AST.Select)
		prog.Query = q
	}

	// Append synthetic rules generated during desugaring (disjunction/negation).
	prog.Rules = append(prog.Rules, d.syntheticRules...)

	return prog, d.errors
}

// desugarModuleDecl desugars all classes and predicates inside a module declaration.
func (d *desugarer) desugarModuleDecl(md *ast.ModuleDecl, prefix string) []datalog.Rule {
	var rules []datalog.Rule
	qualPrefix := md.Name
	if prefix != "" {
		qualPrefix = prefix + "::" + md.Name
	}

	for i := range md.Classes {
		cd := &md.Classes[i]
		// Temporarily set the class name to the qualified name for desugaring.
		origName := cd.Name
		cd.Name = qualPrefix + "::" + origName
		classRules := d.desugarClass(cd)
		cd.Name = origName
		rules = append(rules, classRules...)
	}

	for i := range md.Predicates {
		pd := &md.Predicates[i]
		origName := pd.Name
		pd.Name = qualPrefix + "::" + origName
		predRules := d.desugarTopLevelPredicate(pd)
		pd.Name = origName
		rules = append(rules, predRules...)
	}

	for i := range md.Modules {
		rules = append(rules, d.desugarModuleDecl(&md.Modules[i], qualPrefix)...)
	}

	return rules
}

// ---- Class desugaring ----

// desugarClass emits the characteristic predicate rule and all method rules.
func (d *desugarer) desugarClass(cd *ast.ClassDecl) []datalog.Rule {
	prevClass := d.currentClass
	d.currentClass = cd
	defer func() { d.currentClass = prevClass }()

	var rules []datalog.Rule

	if cd.IsAbstract {
		// Abstract class: emit one rule per concrete subclass.
		// AbstractClass(this) :- ConcreteSubclass(this) for each subclass.
		subclasses := d.allConcreteSubclasses(cd.Name)
		for _, subName := range subclasses {
			rule := datalog.Rule{
				Head: datalog.Atom{
					Predicate: cd.Name,
					Args:      []datalog.Term{datalog.Var{Name: "this"}},
				},
				Body: []datalog.Literal{{
					Positive: true,
					Atom: datalog.Atom{
						Predicate: subName,
						Args:      []datalog.Term{datalog.Var{Name: "this"}},
					},
				}},
				// Abstract-class subclass-union rule: extent-shaped by
				// construction (single positive atom over `this`). Tagging
				// here lets the P2a pre-pass union-and-materialise the
				// abstract extent once instead of re-walking each subclass
				// extent at every join site.
				ClassExtent: true,
			}
			rules = append(rules, rule)
		}
		// If no subclasses, emit no rules — the abstract class has an empty extent.
		// No synthetic "_none" sentinel needed; an underived predicate is naturally empty.
	} else {
		// Characteristic predicate: Foo(this) :- SuperTypes(this)..., body.
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
			// Concrete-class characteristic predicate. The body is the
			// supertype constraints (a chain of `Foo(this)` and entity-type
			// groundings) plus the optional CharPred formula. The tag is
			// always set here; downstream consumers gate on body shape via
			// plan.IsClassExtentBody before deciding whether to materialise
			// (a class with a heavy CharPred over multiple large extents
			// will fail the structural check and fall through to normal
			// evaluation). See P2a of the planner roadmap.
			ClassExtent: true,
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
// @-prefixed supertypes (database entity types) are skipped — they represent
// raw DB types and have no corresponding derived relation to constrain against.
// entityTypeRelation maps @-prefixed entity types to their backing schema
// relation and arity. This grounds `this` for classes extending entity types.
var entityTypeRelation = map[string]struct {
	name  string
	arity int
}{
	"@symbol":       {"Symbol", 4},
	"@node":         {"Node", 7},
	"@file":         {"File", 3},
	"@function":     {"Function", 6},
	"@call":         {"Call", 3},
	"@parameter":    {"Parameter", 6},
	"@taint_source": {"TaintSource", 2},
	"@taint_sink":   {"TaintSink", 2},
	"@taint_alert":  {"TaintAlert", 4},
}

func (d *desugarer) superTypeConstraints(cd *ast.ClassDecl, _ *freshVarGen) []datalog.Literal {
	visited := make(map[string]bool)
	return d.superTypeConstraintsInner(cd, false, visited)
}

func (d *desugarer) superTypeConstraintsInner(cd *ast.ClassDecl, throughAbstract bool, visited map[string]bool) []datalog.Literal {
	qname := d.qualifiedClassName(cd)
	if visited[qname] {
		return nil // cycle guard: prevent infinite recursion in abstract class hierarchies
	}
	visited[qname] = true

	var lits []datalog.Literal
	for _, st := range cd.SuperTypes {
		stName := st.String()
		if len(stName) > 0 && stName[0] == '@' {
			if !throughAbstract {
				// Direct @-type extension (bridge classes): skip, the char pred grounds this.
				continue
			}
			// Through an abstract supertype: we need the entity type for grounding.
			if rel, ok := entityTypeRelation[stName]; ok {
				args := make([]datalog.Term, rel.arity)
				args[0] = datalog.Var{Name: "this"}
				for i := 1; i < rel.arity; i++ {
					args[i] = datalog.Wildcard{}
				}
				lits = append(lits, datalog.Literal{
					Positive: true,
					Atom: datalog.Atom{
						Predicate: rel.name,
						Args:      args,
					},
				})
			} else {
				d.errorf("unknown entity type %q in supertype constraints for %s (not in entityTypeRelation map)", stName, qname)
			}
			continue
		}
		// For abstract supertypes, substitute the abstract class's own supertype
		// constraints instead. Using the abstract predicate directly creates a
		// non-grounded circular dependency:
		//   Concrete(this) :- Abstract(this) AND Abstract(this) :- Concrete(this)
		// Instead, walk up to find the grounding constraints (entity types or
		// concrete classes) that the abstract class transitively depends on.
		if superCD, ok := d.env.Classes[stName]; ok && superCD.IsAbstract {
			transitiveLits := d.superTypeConstraintsInner(superCD, true, visited)
			lits = append(lits, transitiveLits...)
			continue
		}
		lits = append(lits, datalog.Literal{
			Positive: true,
			Atom: datalog.Atom{
				Predicate: stName,
				Args:      []datalog.Term{datalog.Var{Name: "this"}},
			},
		})
	}
	return lits
}

// desugarMethod emits rules for a method, handling override dispatch.
//
// For a chain A ← B ← C where all define the method, we collect ALL transitive
// overriding subclasses and emit one dispatch rule per overrider under the base
// class predicate name.  Each rule excludes its own direct subclass overriders.
func (d *desugarer) desugarMethod(cd *ast.ClassDecl, md *ast.MemberDecl) []datalog.Rule {
	mangledName := mangle(cd.Name, md.Name)

	// Collect ALL transitive subclasses that override this method.
	allOverriders := d.allSubClassesWithMethod(cd.Name, md.Name)

	// The base class rule excludes every transitive overrider.
	baseRule := d.buildMethodRule(cd, md, mangledName, allOverriders)
	rules := []datalog.Rule{baseRule}

	// For each overriding subclass, emit a rule under the base predicate name.
	// Each such rule only excludes that subclass's own direct overriding sub-subclasses,
	// so the dispatch is precise at each level.
	for _, subName := range allOverriders {
		subCD, ok := d.env.Classes[subName]
		if !ok {
			continue
		}
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
		// Exclude only the direct overriding subclasses of this subclass.
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
	// Set currentClass so super resolution works correctly for this method's body.
	prevClass := d.currentClass
	d.currentClass = cd
	defer func() { d.currentClass = prevClass }()

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

// allSubClassesWithMethod returns all transitive subclasses of className
// that directly declare a member named methodName.
func (d *desugarer) allSubClassesWithMethod(className, methodName string) []string {
	var result []string
	visited := make(map[string]bool)
	d.collectSubClassesWithMethod(className, methodName, &result, visited)
	return result
}

func (d *desugarer) collectSubClassesWithMethod(className, methodName string, out *[]string, visited map[string]bool) {
	for _, subName := range d.subClasses[className] {
		if visited[subName] {
			continue
		}
		visited[subName] = true
		subCD, ok := d.env.Classes[subName]
		if !ok {
			continue
		}
		for i := range subCD.Members {
			if subCD.Members[i].Name == methodName {
				*out = append(*out, subName)
				break
			}
		}
		// Always recurse to capture deeper chains.
		d.collectSubClassesWithMethod(subName, methodName, out, visited)
	}
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
		// Rule splitting: create a synthetic predicate with both branches as separate rules.
		leftLits := d.desugarFormula(n.Left, gen)
		rightLits := d.desugarFormula(n.Right, gen)

		// Use only variables that appear in BOTH branches as head args.
		// Variables in only one branch are unsafe in the other rule's head.
		leftVars := collectVarsFromLiterals(leftLits)
		rightVars := collectVarsFromLiterals(rightLits)
		freeVars := intersectVars(leftVars, rightVars)

		synthName := d.freshSynthName("_disj")
		args := make([]datalog.Term, len(freeVars))
		for i, v := range freeVars {
			args[i] = datalog.Var{Name: v}
		}

		// Create two rules: one for left branch, one for right branch.
		head := datalog.Atom{Predicate: synthName, Args: args}
		d.syntheticRules = append(d.syntheticRules,
			datalog.Rule{Head: head, Body: leftLits},
			datalog.Rule{Head: head, Body: rightLits},
		)

		// Return a call to the synthetic predicate.
		return []datalog.Literal{{
			Positive: true,
			Atom:     datalog.Atom{Predicate: synthName, Args: args},
		}}

	case *ast.Negation:
		inner := d.desugarFormula(n.Formula, gen)
		if len(inner) == 1 {
			lit := inner[0]
			lit.Positive = !lit.Positive
			return []datalog.Literal{lit}
		}
		// Multiple literals: create a helper predicate and negate it.
		freeVars := collectVarsFromLiterals(inner)

		synthName := d.freshSynthName("_neg")
		args := make([]datalog.Term, len(freeVars))
		for i, v := range freeVars {
			args[i] = datalog.Var{Name: v}
		}

		head := datalog.Atom{Predicate: synthName, Args: args}
		d.syntheticRules = append(d.syntheticRules,
			datalog.Rule{Head: head, Body: inner},
		)

		// Return negated call to the helper.
		return []datalog.Literal{{
			Positive: false,
			Atom:     datalog.Atom{Predicate: synthName, Args: args},
		}}

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

	case *ast.IfThenElse:
		// Desugar as: (cond and then) or (not cond and else)
		thenBranch := &ast.Conjunction{Left: n.Cond, Right: n.Then}
		elseBranch := &ast.Conjunction{Left: &ast.Negation{Formula: n.Cond}, Right: n.Else}
		disj := &ast.Disjunction{Left: thenBranch, Right: elseBranch}
		return d.desugarFormula(disj, gen)

	case *ast.Forex:
		return d.desugarForex(n, gen)

	case *ast.ClosureCall:
		return d.desugarClosureCall(n, gen)

	default:
		d.errorf("unknown formula type %T", f)
		return nil
	}
}

// desugarPredicateCall handles a PredicateCall used as a formula.
func (d *desugarer) desugarPredicateCall(pc *ast.PredicateCall, gen *freshVarGen) []datalog.Literal {
	if pc.Recv != nil {
		// Check if receiver is a string type and method is a builtin.
		recvType := d.resolveReceiverType(pc.Recv)
		if recvType == "string" && stringBuiltins[pc.Name] {
			recvTerm, extraLits := d.desugarExpr(pc.Recv, gen)
			predName := "__builtin_string_" + pc.Name
			args := []datalog.Term{recvTerm}
			for _, arg := range pc.Args {
				t, lits := d.desugarExpr(arg, gen)
				extraLits = append(extraLits, lits...)
				args = append(args, t)
			}
			lit := datalog.Literal{
				Positive: true,
				Atom:     datalog.Atom{Predicate: predName, Args: args},
			}
			return append(extraLits, lit)
		}

		// Method call as formula (predicate call on receiver — no result).
		recvTerm, extraLits := d.desugarExpr(pc.Recv, gen)
		args := []datalog.Term{recvTerm}
		for _, arg := range pc.Args {
			t, lits := d.desugarExpr(arg, gen)
			extraLits = append(extraLits, lits...)
			args = append(args, t)
		}

		predName := d.resolvePredicateCallRecvPred(pc)
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
//
//	not G_v  OR  (G_v AND Body_v)
//
// Simplified: we emit the guard literals negated, then the body using double-neg.
//
// Full double-negation in stratified Datalog:
//
//	forall v: G(v) => B(v)
//	≡ not exists v: G(v) and not B(v)
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

// desugarForex: forex(decls | guard | body)
// Desugared as: forall(decls | guard | body) and exists(decls | guard)
func (d *desugarer) desugarForex(n *ast.Forex, gen *freshVarGen) []datalog.Literal {
	// Desugar as conjunction of forall and exists.
	forallNode := &ast.Forall{
		Decls: n.Decls,
		Guard: n.Guard,
		Body:  n.Body,
	}
	existsNode := &ast.Exists{
		Decls: n.Decls,
		Guard: n.Guard,
		Body:  n.Guard, // exists(decls | guard) — body is the guard itself
	}

	forallLits := d.desugarForall(forallNode, gen)
	existsLits := d.desugarExists(existsNode, gen)
	return append(forallLits, existsLits...)
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

	// Check if receiver is a string type — if so, emit a builtin predicate.
	recvType := d.resolveReceiverType(mc.Recv)
	if recvType == "string" && stringBuiltins[mc.Method] {
		predName := "__builtin_string_" + mc.Method
		args := []datalog.Term{recv}
		for _, arg := range mc.Args {
			t, argLits := d.desugarExpr(arg, gen)
			lits = append(lits, argLits...)
			args = append(args, t)
		}
		// matches and regexpMatch are predicates (no result) — they cannot
		// be used in expression context. Other builtins produce a result.
		if mc.Method == "matches" || mc.Method == "regexpMatch" {
			d.errorf("string method %s() is a predicate and cannot be used as an expression", mc.Method)
			return datalog.Wildcard{}, lits
		}
		fresh := gen.next()
		args = append(args, fresh)
		lits = append(lits, datalog.Literal{
			Positive: true,
			Atom:     datalog.Atom{Predicate: predName, Args: args},
		})
		return fresh, lits
	}

	// Determine the predicate name.
	var predName string
	if v, ok := mc.Recv.(*ast.Variable); ok && v.Name == "super" {
		predName = d.resolveSuperMethod(mc.Method)
	} else {
		predName = d.resolveMethodCallPred(mc, mc.Method)
	}

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
// recv must be the *ast.MethodCall node itself (for expression-position calls) — the
// resolver writes ExprResolutions keyed on the MethodCall node.
// For PredicateCall receivers (formula-position method calls), pass nil for recv and
// use resolvePredicateCallRecvPred instead.
func (d *desugarer) resolveMethodCallPred(recv ast.Expr, methodName string) string {
	if d.ann != nil {
		if res, ok := d.ann.ExprResolutions[recv]; ok && res != nil && res.DeclClass != nil {
			qualName := d.qualifiedClassName(res.DeclClass)
			return mangle(qualName, methodName)
		}
	}
	return methodName
}

// resolveReceiverType infers the class name of an expression by consulting
// VarBindings (for Variables) and ExprResolutions (for MethodCall/FieldAccess).
func (d *desugarer) resolveReceiverType(recv ast.Expr) string {
	if d.ann == nil {
		return ""
	}
	switch n := recv.(type) {
	case *ast.Variable:
		if n.Name == "this" && d.currentClass != nil {
			return d.currentClass.Name
		}
		// VarBindings records the ParamDecl, whose Type gives the class name.
		if vb, ok := d.ann.VarBindings[n]; ok && vb.Param != nil {
			return vb.Param.Type.String()
		}
		return ""
	case *ast.MethodCall:
		if res, ok := d.ann.ExprResolutions[n]; ok && res != nil && res.DeclMember != nil && res.DeclMember.ReturnType != nil {
			return res.DeclMember.ReturnType.String()
		}
		return ""
	case *ast.Cast:
		return n.Type.String()
	}
	return ""
}

// resolvePredicateCallRecvPred determines the mangled predicate name for a
// formula-position method call (pc.Recv != nil). The resolver does not annotate
// PredicateCall nodes in ExprResolutions, so we infer from the receiver type.
func (d *desugarer) resolvePredicateCallRecvPred(pc *ast.PredicateCall) string {
	// Handle super: resolve against parent class instead of current class.
	if v, ok := pc.Recv.(*ast.Variable); ok && v.Name == "super" {
		return d.resolveSuperMethod(pc.Name)
	}
	recvType := d.resolveReceiverType(pc.Recv)
	if recvType == "" {
		return pc.Name
	}
	// Walk the class hierarchy to find the defining class (handles inheritance).
	if cd, ok := d.env.Classes[recvType]; ok {
		defClass := d.memberDefiningClass(cd, pc.Name)
		if defClass != nil {
			qualName := d.qualifiedClassName(defClass)
			return mangle(qualName, pc.Name)
		}
	}
	return mangle(recvType, pc.Name)
}

// qualifiedClassName returns the qualified (environment-registered) name for a class,
// e.g. "DataFlow::Configuration" instead of just "Configuration".
func (d *desugarer) qualifiedClassName(cd *ast.ClassDecl) string {
	for name, c := range d.env.Classes {
		if c == cd {
			return name
		}
	}
	return cd.Name
}

// resolveSuperMethod resolves a super.method() call by finding the method in
// the parent class. Uses currentClass to determine which class we're in.
func (d *desugarer) resolveSuperMethod(methodName string) string {
	if d.currentClass == nil {
		d.errorf("super used outside of a class body")
		return methodName
	}
	// Walk the supertype chain (left-to-right priority for multiple inheritance).
	for _, st := range d.currentClass.SuperTypes {
		stName := st.String()
		if parentCD, ok := d.env.Classes[stName]; ok {
			defClass := d.memberDefiningClass(parentCD, methodName)
			if defClass != nil {
				return mangle(defClass.Name, methodName)
			}
		}
	}
	d.errorf("super.%s(): method not found in parent classes of %s", methodName, d.currentClass.Name)
	return methodName
}

// memberDefiningClass walks the supertype chain from cd to find the class
// that directly declares a member named name. Delegates to ast.MemberDefiningClass.
func (d *desugarer) memberDefiningClass(cd *ast.ClassDecl, name string) *ast.ClassDecl {
	return ast.MemberDefiningClass(cd, name, d.env.Classes)
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
		Func:      a.Op,
		Var:       aggVar,
		TypeName:  aggType,
		Body:      bodyLits,
		Expr:      aggExpr,
		ResultVar: fresh,
		Separator: a.Separator,
	}

	lit := datalog.Literal{
		Positive: true,
		Agg:      agg,
	}

	return fresh, []datalog.Literal{lit}
}

// ---- Closure call desugaring ----

// desugarClosureCall handles pred+(args) and pred*(args) transitive closure syntax.
func (d *desugarer) desugarClosureCall(cc *ast.ClosureCall, gen *freshVarGen) []datalog.Literal {
	if len(cc.Args) != 2 {
		d.errorf("closure call %s requires exactly 2 arguments, got %d", cc.Name, len(cc.Args))
		return nil
	}

	arg0, lits0 := d.desugarExpr(cc.Args[0], gen)
	arg1, lits1 := d.desugarExpr(cc.Args[1], gen)
	extraLits := append(lits0, lits1...)

	if cc.Plus {
		// pred+(x, y): transitive closure (one or more steps)
		synthName := d.freshSynthName("_closure")
		z := gen.next()

		// _closure(x, y) :- pred(x, y).
		d.syntheticRules = append(d.syntheticRules, datalog.Rule{
			Head: datalog.Atom{Predicate: synthName, Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "y"}}},
			Body: []datalog.Literal{{
				Positive: true,
				Atom:     datalog.Atom{Predicate: cc.Name, Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "y"}}},
			}},
		})

		// _closure(x, y) :- pred(x, z), _closure(z, y).
		d.syntheticRules = append(d.syntheticRules, datalog.Rule{
			Head: datalog.Atom{Predicate: synthName, Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "y"}}},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: cc.Name, Args: []datalog.Term{datalog.Var{Name: "x"}, z}}},
				{Positive: true, Atom: datalog.Atom{Predicate: synthName, Args: []datalog.Term{z, datalog.Var{Name: "y"}}}},
			},
		})

		lit := datalog.Literal{
			Positive: true,
			Atom:     datalog.Atom{Predicate: synthName, Args: []datalog.Term{arg0, arg1}},
		}
		return append(extraLits, lit)
	}

	// pred*(x, y): reflexive-transitive closure
	// Desugar as: (x = y) or pred+(x, y)
	plusCall := &ast.ClosureCall{
		Name: cc.Name,
		Plus: true,
		Args: cc.Args,
	}
	eqFormula := &ast.Comparison{
		Left:  cc.Args[0],
		Right: cc.Args[1],
		Op:    "=",
	}
	disj := &ast.Disjunction{
		Left:  eqFormula,
		Right: plusCall,
	}
	return d.desugarFormula(disj, gen)
}

// ---- String builtin desugaring ----

// isStringBuiltin returns true if the method name is a known string builtin.
var stringBuiltins = map[string]bool{
	"length":      true,
	"indexOf":     true,
	"substring":   true,
	"matches":     true,
	"regexpMatch": true,
	"toUpperCase": true,
	"toLowerCase": true,
	"trim":        true,
	"replaceAll":  true,
	"charAt":      true,
	"toInt":       true,
	"toString":    true,
	"splitAt":     true,
}

// ---- Helpers ----

// collectVarsFromLiterals collects all unique Var names from a slice of literals.
func collectVarsFromLiterals(lits []datalog.Literal) []string {
	seen := make(map[string]bool)
	var vars []string
	for _, lit := range lits {
		collectVarsFromAtom(lit.Atom, seen, &vars)
		if lit.Cmp != nil {
			collectVarFromTerm(lit.Cmp.Left, seen, &vars)
			collectVarFromTerm(lit.Cmp.Right, seen, &vars)
		}
		if lit.Agg != nil {
			for _, innerLit := range lit.Agg.Body {
				collectVarsFromAtom(innerLit.Atom, seen, &vars)
			}
		}
	}
	return vars
}

func collectVarsFromAtom(a datalog.Atom, seen map[string]bool, vars *[]string) {
	for _, arg := range a.Args {
		collectVarFromTerm(arg, seen, vars)
	}
}

func collectVarFromTerm(t datalog.Term, seen map[string]bool, vars *[]string) {
	if v, ok := t.(datalog.Var); ok && v.Name != "" {
		if !seen[v.Name] {
			seen[v.Name] = true
			*vars = append(*vars, v.Name)
		}
	}
}

// intersectVars returns variables present in both a and b, preserving order from a.
func intersectVars(a, b []string) []string {
	bSet := make(map[string]bool, len(b))
	for _, v := range b {
		bSet[v] = true
	}
	var result []string
	for _, v := range a {
		if bSet[v] {
			result = append(result, v)
		}
	}
	return result
}

// allConcreteSubclasses returns all transitive subclass names that are not abstract.
func (d *desugarer) allConcreteSubclasses(className string) []string {
	var result []string
	visited := make(map[string]bool)
	d.collectConcreteSubclasses(className, &result, visited)
	return result
}

func (d *desugarer) collectConcreteSubclasses(className string, out *[]string, visited map[string]bool) {
	for _, subName := range d.subClasses[className] {
		if visited[subName] {
			continue
		}
		visited[subName] = true
		// Check if subclass is abstract.
		if cd, ok := d.env.Classes[subName]; ok && cd.IsAbstract {
			// Recurse into abstract subclass to find its concrete subclasses.
			d.collectConcreteSubclasses(subName, out, visited)
		} else {
			*out = append(*out, subName)
		}
	}
}

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
