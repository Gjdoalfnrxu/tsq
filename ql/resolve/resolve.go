// Package resolve implements QL name resolution, producing a ResolvedModule.
package resolve

import (
	"fmt"
	"strings"

	"github.com/Gjdoalfnrxu/tsq/ql/ast"
)

// ResolvedModule is the output of name resolution.
type ResolvedModule struct {
	AST         *ast.Module
	Env         *Environment
	Annotations *Annotations
	Errors      []Error
}

// Environment holds all top-level declarations in scope.
type Environment struct {
	Predicates map[string]*ast.PredicateDecl
	Classes    map[string]*ast.ClassDecl
	Imports    map[string]*ResolvedModule
	Modules    map[string]*ast.ModuleDecl
}

// Error describes a name resolution failure.
type Error struct {
	Pos     ast.Span
	Message string
}

func (e Error) Error() string {
	return fmt.Sprintf("%s:%d:%d: %s", e.Pos.File, e.Pos.StartLine, e.Pos.StartCol, e.Message)
}

// Annotations is a side-table of resolution results.
// Keys are pointer identity into the AST.
type Annotations struct {
	// ExprResolutions maps expressions to what they resolved to.
	ExprResolutions map[ast.Expr]*Resolution
	// VarBindings maps Variable exprs to their binding declaration.
	VarBindings map[*ast.Variable]VarBinding
}

// Resolution describes what a name resolved to.
type Resolution struct {
	DeclClass     *ast.ClassDecl
	DeclMember    *ast.MemberDecl
	DeclPredicate *ast.PredicateDecl
}

// VarBinding records where a variable was bound.
type VarBinding struct {
	Param *ast.ParamDecl // non-nil if bound as parameter
	// Exists/Forall/Forex/From bindings: just the ParamDecl from that construct
}

// primitiveTypes is the set of built-in scalar type names.
var primitiveTypes = map[string]bool{
	"int":     true,
	"float":   true,
	"string":  true,
	"boolean": true,
	"date":    true,
}

// resolver is the internal state for a resolution pass.
type resolver struct {
	env    *Environment
	ann    *Annotations
	errors []Error
	mod    *ast.Module
}

// Resolve performs name resolution on mod.
// importLoader is called for each import path; it may return nil to indicate
// the module is unavailable (resolution continues with errors).
func Resolve(mod *ast.Module, importLoader func(path string) (*ast.Module, error)) (*ResolvedModule, error) {
	env := &Environment{
		Predicates: make(map[string]*ast.PredicateDecl),
		Classes:    make(map[string]*ast.ClassDecl),
		Imports:    make(map[string]*ResolvedModule),
		Modules:    make(map[string]*ast.ModuleDecl),
	}
	ann := &Annotations{
		ExprResolutions: make(map[ast.Expr]*Resolution),
		VarBindings:     make(map[*ast.Variable]VarBinding),
	}
	r := &resolver{env: env, ann: ann, mod: mod}

	// Process imports first so imported names are available.
	r.processImports(mod, importLoader)

	// First pass: register all top-level declarations.
	r.firstPass(mod)

	// Detect cyclic inheritance before second pass.
	r.detectCycles(mod)

	// Second pass: resolve bodies.
	r.secondPass(mod)

	rm := &ResolvedModule{
		AST:         mod,
		Env:         env,
		Annotations: ann,
		Errors:      r.errors,
	}
	return rm, nil
}

// errorf records a resolution error without stopping.
func (r *resolver) errorf(span ast.Span, format string, args ...interface{}) {
	r.errors = append(r.errors, Error{
		Pos:     span,
		Message: fmt.Sprintf(format, args...),
	})
}

// ---- Pass 0: imports ----

func (r *resolver) processImports(mod *ast.Module, importLoader func(string) (*ast.Module, error)) {
	if importLoader == nil {
		return
	}
	for i := range mod.Imports {
		imp := &mod.Imports[i]
		path := imp.Path
		if _, already := r.env.Imports[path]; already {
			continue
		}
		importedAST, err := importLoader(path)
		if err != nil || importedAST == nil {
			r.errorf(imp.Span, "cannot load import %q: %v", path, err)
			continue
		}
		// Recursively resolve the imported module (no further import loading).
		rm, _ := Resolve(importedAST, nil)
		r.env.Imports[path] = rm
		// Make imported predicates and classes available in our env.
		for name, pd := range rm.Env.Predicates {
			if _, exists := r.env.Predicates[name]; !exists {
				r.env.Predicates[name] = pd
			}
		}
		for name, cd := range rm.Env.Classes {
			if _, exists := r.env.Classes[name]; !exists {
				r.env.Classes[name] = cd
			}
		}
	}
}

// ---- Pass 1: register declarations ----

func (r *resolver) firstPass(mod *ast.Module) {
	for i := range mod.Classes {
		cd := &mod.Classes[i]
		if existing, dup := r.env.Classes[cd.Name]; dup {
			r.errorf(cd.Span, "duplicate class declaration %q (first declared at %s:%d)",
				cd.Name, existing.Span.File, existing.Span.StartLine)
		} else {
			r.env.Classes[cd.Name] = cd
		}
	}
	for i := range mod.Predicates {
		pd := &mod.Predicates[i]
		if existing, dup := r.env.Predicates[pd.Name]; dup {
			r.errorf(pd.Span, "duplicate predicate declaration %q (first declared at %s:%d)",
				pd.Name, existing.Span.File, existing.Span.StartLine)
		} else {
			r.env.Predicates[pd.Name] = pd
		}
	}
	// Register module declarations and their members with qualified names.
	for i := range mod.Modules {
		md := &mod.Modules[i]
		r.registerModule(md, "")
	}
}

// registerModule registers a module and its contents with qualified names.
func (r *resolver) registerModule(md *ast.ModuleDecl, prefix string) {
	qualName := md.Name
	if prefix != "" {
		qualName = prefix + "::" + md.Name
	}
	r.env.Modules[qualName] = md

	// Register classes with qualified names.
	for i := range md.Classes {
		cd := &md.Classes[i]
		qn := qualName + "::" + cd.Name
		if _, dup := r.env.Classes[qn]; !dup {
			r.env.Classes[qn] = cd
		}
	}
	// Register predicates with qualified names.
	for i := range md.Predicates {
		pd := &md.Predicates[i]
		qn := qualName + "::" + pd.Name
		if _, dup := r.env.Predicates[qn]; !dup {
			r.env.Predicates[qn] = pd
		}
	}
	// Recurse for nested modules.
	for i := range md.Modules {
		r.registerModule(&md.Modules[i], qualName)
	}
}

// ---- Cycle detection ----

// detectCycles finds cyclic inheritance in the class hierarchy.
func (r *resolver) detectCycles(mod *ast.Module) {
	// colour: 0=white, 1=grey (in stack), 2=black (done)
	colour := make(map[string]int)
	var visit func(name string, span ast.Span) bool
	visit = func(name string, span ast.Span) bool {
		if colour[name] == 2 {
			return false
		}
		if colour[name] == 1 {
			r.errorf(span, "cyclic class inheritance involving %q", name)
			return true
		}
		cd, ok := r.env.Classes[name]
		if !ok {
			return false
		}
		colour[name] = 1
		for _, st := range cd.SuperTypes {
			stName := st.String()
			if visit(stName, st.Span) {
				colour[name] = 2
				return true
			}
		}
		colour[name] = 2
		return false
	}
	for i := range mod.Classes {
		visit(mod.Classes[i].Name, mod.Classes[i].Span)
	}
}

// ---- Pass 2: resolve bodies ----

// scope maps variable names to their types (for method-call resolution) and
// tracks binding ParamDecl pointers.
type scope struct {
	vars     map[string]varInfo
	inClass  *ast.ClassDecl
	inMethod *ast.MemberDecl
	inPred   *ast.PredicateDecl
}

type varInfo struct {
	typeName string // resolved class name or primitive type name; empty if unknown
	param    *ast.ParamDecl
}

func newScope() *scope {
	return &scope{vars: make(map[string]varInfo)}
}

func (s *scope) child() *scope {
	c := &scope{
		vars:     make(map[string]varInfo),
		inClass:  s.inClass,
		inMethod: s.inMethod,
		inPred:   s.inPred,
	}
	// Copy parent vars.
	for k, v := range s.vars {
		c.vars[k] = v
	}
	return c
}

func (r *resolver) secondPass(mod *ast.Module) {
	// Resolve extends type references for each class.
	for i := range mod.Classes {
		cd := &mod.Classes[i]
		for _, st := range cd.SuperTypes {
			r.resolveTypeRef(st)
		}
	}

	// Resolve class bodies.
	for i := range mod.Classes {
		cd := &mod.Classes[i]
		s := newScope()
		s.inClass = cd
		// `this` is implicitly bound in class context.
		s.vars["this"] = varInfo{typeName: cd.Name}

		// Resolve member params and bodies.
		for j := range cd.Members {
			md := &cd.Members[j]
			ms := s.child()
			ms.inMethod = md
			// Bind parameters.
			for k := range md.Params {
				param := &md.Params[k]
				r.resolveTypeRef(param.Type)
				ms.vars[param.Name] = varInfo{typeName: param.Type.String(), param: param}
			}
			// `result` is valid when the method has a non-nil ReturnType.
			if md.ReturnType != nil {
				ms.vars["result"] = varInfo{typeName: md.ReturnType.String()}
			}
			if md.Body != nil {
				r.resolveFormula(*md.Body, ms)
			}
		}

		// Resolve characteristic predicate body.
		if cd.CharPred != nil {
			r.resolveFormula(*cd.CharPred, s)
		}
	}

	// Resolve top-level predicate bodies.
	for i := range mod.Predicates {
		pd := &mod.Predicates[i]
		s := newScope()
		s.inPred = pd
		for k := range pd.Params {
			param := &pd.Params[k]
			r.resolveTypeRef(param.Type)
			s.vars[param.Name] = varInfo{typeName: param.Type.String(), param: param}
		}
		if pd.ReturnType != nil {
			s.vars["result"] = varInfo{typeName: pd.ReturnType.String()}
		}
		if pd.Body != nil {
			r.resolveFormula(*pd.Body, s)
		}
	}

	// Resolve select clause.
	if mod.Select != nil {
		r.resolveSelect(mod.Select)
	}
}

// resolveTypeRef checks that a type reference exists (class or primitive).
func (r *resolver) resolveTypeRef(tr ast.TypeRef) {
	name := tr.String()
	if primitiveTypes[name] {
		return
	}
	// @-prefixed types are database entity types (used in bridge .qll files).
	// They are always valid and do not need to be declared as classes.
	if strings.HasPrefix(name, "@") {
		return
	}
	if _, ok := r.env.Classes[name]; !ok {
		r.errorf(tr.Span, "undefined type %q", name)
	}
}

// resolveSelect resolves the from/where/select clause.
func (r *resolver) resolveSelect(sel *ast.SelectClause) {
	s := newScope()
	// Bind from declarations.
	for i := range sel.Decls {
		vd := &sel.Decls[i]
		r.resolveTypeRef(vd.Type)
		// Synthesise a ParamDecl so VarBindings can reference it.
		pd := &ast.ParamDecl{Type: vd.Type, Name: vd.Name, Span: vd.Span}
		s.vars[vd.Name] = varInfo{typeName: vd.Type.String(), param: pd}
	}
	if sel.Where != nil {
		r.resolveFormula(*sel.Where, s)
	}
	for _, e := range sel.Select {
		r.resolveExpr(e, s)
	}
}

// ---- Formula resolution ----

func (r *resolver) resolveFormula(f ast.Formula, s *scope) {
	if f == nil {
		return
	}
	switch n := f.(type) {
	case *ast.Conjunction:
		r.resolveFormula(n.Left, s)
		r.resolveFormula(n.Right, s)
	case *ast.Disjunction:
		r.resolveFormula(n.Left, s)
		r.resolveFormula(n.Right, s)
	case *ast.Negation:
		r.resolveFormula(n.Formula, s)
	case *ast.Comparison:
		r.resolveExpr(n.Left, s)
		r.resolveExpr(n.Right, s)
	case *ast.PredicateCall:
		r.resolvePredicateCall(n, s)
	case *ast.InstanceOf:
		r.resolveExpr(n.Expr, s)
		r.resolveTypeRef(n.Type)
	case *ast.Exists:
		r.resolveQuantified(n.Decls, n.Guard, n.Body, s)
	case *ast.Forall:
		r.resolveQuantified(n.Decls, n.Guard, n.Body, s)
	case *ast.Forex:
		r.resolveQuantified(n.Decls, n.Guard, n.Body, s)
	case *ast.IfThenElse:
		r.resolveFormula(n.Cond, s)
		r.resolveFormula(n.Then, s)
		r.resolveFormula(n.Else, s)
	case *ast.ClosureCall:
		for _, arg := range n.Args {
			r.resolveExpr(arg, s)
		}
	case *ast.None, *ast.Any:
		// nothing to resolve
	}
}

// resolveQuantified handles exists/forall bodies (shared logic).
func (r *resolver) resolveQuantified(decls []ast.VarDecl, guard ast.Formula, body ast.Formula, s *scope) {
	inner := s.child()
	for i := range decls {
		vd := &decls[i]
		r.resolveTypeRef(vd.Type)
		pd := &ast.ParamDecl{Type: vd.Type, Name: vd.Name, Span: vd.Span}
		inner.vars[vd.Name] = varInfo{typeName: vd.Type.String(), param: pd}
	}
	if guard != nil {
		r.resolveFormula(guard, inner)
	}
	r.resolveFormula(body, inner)
}

// resolvePredicateCall resolves a predicate/method call used as a formula.
func (r *resolver) resolvePredicateCall(pc *ast.PredicateCall, s *scope) {
	if pc.Recv != nil {
		// Method call on receiver: resolve receiver then look up method on its type.
		r.resolveExpr(pc.Recv, s)
		recvType := r.exprType(pc.Recv, s)
		if recvType != "" {
			if cd, ok := r.env.Classes[recvType]; ok {
				if md := r.lookupMember(cd, pc.Name); md == nil {
					r.errorf(pc.GetSpan(), "class %q has no member %q", recvType, pc.Name)
				}
			}
		}
		for _, arg := range pc.Args {
			r.resolveExpr(arg, s)
		}
		return
	}
	// Bare call — look up in predicates.
	if _, ok := r.env.Predicates[pc.Name]; !ok {
		r.errorf(pc.GetSpan(), "undefined predicate %q", pc.Name)
	}
	for _, arg := range pc.Args {
		r.resolveExpr(arg, s)
	}
}

// ---- Expression resolution ----

func (r *resolver) resolveExpr(e ast.Expr, s *scope) {
	if e == nil {
		return
	}
	switch n := e.(type) {
	case *ast.Variable:
		r.resolveVariable(n, s)
	case *ast.IntLiteral, *ast.StringLiteral, *ast.BoolLiteral:
		// nothing
	case *ast.FieldAccess:
		r.resolveExpr(n.Recv, s)
	case *ast.MethodCall:
		r.resolveMethodCall(n, s)
	case *ast.Cast:
		r.resolveExpr(n.Expr, s)
		r.resolveTypeRef(n.Type)
	case *ast.Aggregate:
		r.resolveAggregate(n, s)
	case *ast.BinaryExpr:
		r.resolveExpr(n.Left, s)
		r.resolveExpr(n.Right, s)
	}
}

// resolveVariable checks that a variable is bound, handling `this` and `result` specially.
func (r *resolver) resolveVariable(v *ast.Variable, s *scope) {
	switch v.Name {
	case "this":
		if s.inClass == nil {
			r.errorf(v.GetSpan(), "`this` used outside a class body")
			return
		}
		// `this` is pre-bound in s; no additional annotation needed.
		return
	case "super":
		if s.inClass == nil {
			r.errorf(v.GetSpan(), "`super` used outside a class body")
			return
		}
		// `super` refers to the parent class instance; valid in override methods.
		return
	case "result":
		// Valid if we're inside a method with a return type, or a predicate with return type.
		if s.inMethod != nil && s.inMethod.ReturnType != nil {
			return
		}
		if s.inPred != nil && s.inPred.ReturnType != nil {
			return
		}
		r.errorf(v.GetSpan(), "`result` used in predicate/method without a return type")
		return
	}
	info, ok := s.vars[v.Name]
	if !ok {
		r.errorf(v.GetSpan(), "undefined variable %q", v.Name)
		return
	}
	if info.param != nil {
		r.ann.VarBindings[v] = VarBinding{Param: info.param}
	}
}

// resolveMethodCall resolves expr.method(args...) and records annotation.
func (r *resolver) resolveMethodCall(mc *ast.MethodCall, s *scope) {
	r.resolveExpr(mc.Recv, s)
	recvType := r.exprType(mc.Recv, s)
	if recvType != "" {
		if cd, ok := r.env.Classes[recvType]; ok {
			defClass := r.memberDefiningClass(cd, mc.Method)
			if defClass != nil {
				md := r.lookupMember(defClass, mc.Method)
				r.ann.ExprResolutions[mc] = &Resolution{
					DeclClass:  defClass,
					DeclMember: md,
				}
			} else {
				r.errorf(mc.GetSpan(), "class %q has no member %q", recvType, mc.Method)
			}
		}
	}
	for _, arg := range mc.Args {
		r.resolveExpr(arg, s)
	}
}

func (r *resolver) resolveAggregate(a *ast.Aggregate, s *scope) {
	inner := s.child()
	for i := range a.Decls {
		vd := &a.Decls[i]
		r.resolveTypeRef(vd.Type)
		pd := &ast.ParamDecl{Type: vd.Type, Name: vd.Name, Span: vd.Span}
		inner.vars[vd.Name] = varInfo{typeName: vd.Type.String(), param: pd}
	}
	if a.Guard != nil {
		r.resolveFormula(a.Guard, inner)
	}
	if a.Body != nil {
		r.resolveFormula(a.Body, inner)
	}
	if a.Expr != nil {
		r.resolveExpr(a.Expr, inner)
	}
}

// ---- Type inference helpers ----

// exprType returns the inferred class name for an expression, or "" if unknown.
func (r *resolver) exprType(e ast.Expr, s *scope) string {
	switch n := e.(type) {
	case *ast.Variable:
		if info, ok := s.vars[n.Name]; ok {
			return info.typeName
		}
	case *ast.Cast:
		return n.Type.String()
	case *ast.MethodCall:
		// Look up the resolution to infer the return type.
		if res, ok := r.ann.ExprResolutions[n]; ok && res.DeclMember != nil && res.DeclMember.ReturnType != nil {
			return res.DeclMember.ReturnType.String()
		}
	}
	return ""
}

// ---- Member lookup helpers (supertype chain) ----

// lookupMember searches cd and its supertype chain for a member named name.
// Returns the first (most-derived) match found, or nil.
func (r *resolver) lookupMember(cd *ast.ClassDecl, name string) *ast.MemberDecl {
	visited := make(map[string]bool)
	return r.lookupMemberRec(cd, name, visited)
}

func (r *resolver) lookupMemberRec(cd *ast.ClassDecl, name string, visited map[string]bool) *ast.MemberDecl {
	if cd == nil || visited[cd.Name] {
		return nil
	}
	visited[cd.Name] = true
	for i := range cd.Members {
		if cd.Members[i].Name == name {
			return &cd.Members[i]
		}
	}
	// Walk supertype chain.
	for _, st := range cd.SuperTypes {
		if superCD, ok := r.env.Classes[st.String()]; ok {
			if md := r.lookupMemberRec(superCD, name, visited); md != nil {
				return md
			}
		}
	}
	return nil
}

// memberDefiningClass returns the ClassDecl that directly defines name,
// walking up the supertype chain from cd. Delegates to ast.MemberDefiningClass.
func (r *resolver) memberDefiningClass(cd *ast.ClassDecl, name string) *ast.ClassDecl {
	return ast.MemberDefiningClass(cd, name, r.env.Classes)
}
