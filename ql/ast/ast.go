// Package ast defines the QL abstract syntax tree produced by the parser.
package ast

// Span represents a source location range.
type Span struct {
	File      string
	StartLine int
	StartCol  int
	EndLine   int
	EndCol    int
}

// Module is the top-level AST node for a .ql or .qll file.
type Module struct {
	Imports    []ImportDecl
	Classes    []ClassDecl
	Predicates []PredicateDecl
	Modules    []ModuleDecl
	Select     *SelectClause // nil for library modules (.qll)
	Span       Span
}

// ModuleDecl represents a QL module declaration.
type ModuleDecl struct {
	Name       string
	Classes    []ClassDecl
	Predicates []PredicateDecl
	Modules    []ModuleDecl // nested modules
	Span       Span
}

// ImportDecl represents `import <module> as <alias>` or `import <module>`.
type ImportDecl struct {
	Path  string // e.g. "javascript" or "DataFlow::PathGraph"
	Alias string // optional alias; empty if no `as` clause
	Span  Span
}

// ClassDecl represents a QL class declaration.
type ClassDecl struct {
	Name       string
	IsAbstract bool      // true for `abstract class`
	SuperTypes []TypeRef // types listed in `extends` clause
	CharPred   *Formula  // characteristic predicate body (the Foo() { ... } block)
	Members    []MemberDecl
	Span       Span
}

// MemberDecl is a method or field declaration inside a class.
type MemberDecl struct {
	Name       string
	ReturnType *TypeRef // nil for predicates (no return type)
	Params     []ParamDecl
	Body       *Formula
	Override   bool // has `override` modifier
	Span       Span
}

// PredicateDecl is a top-level predicate definition.
type PredicateDecl struct {
	Name       string
	ReturnType *TypeRef // nil for predicates
	Params     []ParamDecl
	Body       *Formula
	Span       Span
}

// ParamDecl is a parameter in a predicate or method.
type ParamDecl struct {
	Type TypeRef
	Name string
	Span Span
}

// TypeRef is a reference to a type (possibly qualified).
type TypeRef struct {
	Path []string // e.g. ["DataFlow", "Node"] for DataFlow::Node
	Span Span
}

// String returns the qualified name.
func (t TypeRef) String() string {
	s := ""
	for i, p := range t.Path {
		if i > 0 {
			s += "::"
		}
		s += p
	}
	return s
}

// SelectClause is the select statement in a .ql query.
type SelectClause struct {
	Decls  []VarDecl // `from` declarations
	Where  *Formula  // `where` clause (may be nil)
	Select []Expr    // `select` expressions
	Labels []string  // optional string labels after select expressions
	Span   Span
}

// VarDecl is a typed variable declaration (used in `from` and `exists`).
type VarDecl struct {
	Type TypeRef
	Name string
	Span Span
}

// --- Formulas ---

// Formula is the interface for all QL formula nodes.
type Formula interface {
	formulaNode()
	GetSpan() Span
}

// BaseFormula provides a shared Span field for formula nodes.
type BaseFormula struct{ Span Span }

// GetSpan returns the source span of the formula.
func (b BaseFormula) GetSpan() Span { return b.Span }

// Conjunction: f1 and f2
type Conjunction struct {
	BaseFormula
	Left, Right Formula
}

func (Conjunction) formulaNode() {}

// Disjunction: f1 or f2
type Disjunction struct {
	BaseFormula
	Left, Right Formula
}

func (Disjunction) formulaNode() {}

// Negation: not f
type Negation struct {
	BaseFormula
	Formula Formula
}

func (Negation) formulaNode() {}

// Comparison: expr op expr
type Comparison struct {
	BaseFormula
	Left, Right Expr
	Op          string // "=", "!=", "<", "<=", ">", ">="
}

func (Comparison) formulaNode() {}

// PredicateCall: pred(args...) or this.method(args...)
type PredicateCall struct {
	BaseFormula
	Recv Expr // nil for non-method calls
	Name string
	Args []Expr
}

func (PredicateCall) formulaNode() {}

// InstanceOf: expr instanceof Type
type InstanceOf struct {
	BaseFormula
	Expr Expr
	Type TypeRef
}

func (InstanceOf) formulaNode() {}

// Exists: exists(decls | formula) or exists(decls | guard | formula)
type Exists struct {
	BaseFormula
	Decls []VarDecl
	Guard Formula // optional; nil if not present
	Body  Formula
}

func (Exists) formulaNode() {}

// Forall: forall(decls | guard | body)
type Forall struct {
	BaseFormula
	Decls []VarDecl
	Guard Formula
	Body  Formula
}

func (Forall) formulaNode() {}

// IfThenElse: if cond then thenBranch else elseBranch
type IfThenElse struct {
	BaseFormula
	Cond Formula
	Then Formula
	Else Formula
}

func (IfThenElse) formulaNode() {}

// ClosureCall: pred+(args...) or pred*(args...)
type ClosureCall struct {
	BaseFormula
	Name string // predicate name
	Plus bool   // true for +, false for *
	Args []Expr
}

func (ClosureCall) formulaNode() {}

// None: none() — always false
type None struct{ BaseFormula }

func (None) formulaNode() {}

// Any: any() — always true
type Any struct{ BaseFormula }

func (Any) formulaNode() {}

// --- Expressions ---

// Expr is the interface for all QL expression nodes.
type Expr interface {
	exprNode()
	GetSpan() Span
}

// BaseExpr provides a shared Span field for expression nodes.
type BaseExpr struct{ Span Span }

// GetSpan returns the source span of the expression.
func (b BaseExpr) GetSpan() Span { return b.Span }

// Variable: a named variable reference (including `this` and `result`)
type Variable struct {
	BaseExpr
	Name string
}

func (Variable) exprNode() {}

// IntLiteral: 42
type IntLiteral struct {
	BaseExpr
	Value int64
}

func (IntLiteral) exprNode() {}

// StringLiteral: "foo"
type StringLiteral struct {
	BaseExpr
	Value string
}

func (StringLiteral) exprNode() {}

// BoolLiteral: true, false
type BoolLiteral struct {
	BaseExpr
	Value bool
}

func (BoolLiteral) exprNode() {}

// FieldAccess: expr.field
type FieldAccess struct {
	BaseExpr
	Recv  Expr
	Field string
}

func (FieldAccess) exprNode() {}

// MethodCall: expr.method(args...) — also used for chained method calls
type MethodCall struct {
	BaseExpr
	Recv   Expr
	Method string
	Args   []Expr
}

func (MethodCall) exprNode() {}

// Cast: expr.(Type)
type Cast struct {
	BaseExpr
	Expr Expr
	Type TypeRef
}

func (Cast) exprNode() {}

// Aggregate: count(Type v | formula) etc.
type Aggregate struct {
	BaseExpr
	Op    string // "count", "min", "max", "sum", "avg"
	Decls []VarDecl
	Guard Formula // optional
	Body  Formula
	Expr  Expr // the expression to aggregate over (for min/max/sum/avg)
}

func (Aggregate) exprNode() {}

// BinaryExpr: arithmetic
type BinaryExpr struct {
	BaseExpr
	Left, Right Expr
	Op          string // "+", "-", "*", "/", "%"
}

func (BinaryExpr) exprNode() {}
