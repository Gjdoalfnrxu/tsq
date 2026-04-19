package extract

// FunctionKinds lists all tree-sitter node kinds that represent function-like
// declarations. This is the single source of truth — walker.go, walker_v2.go,
// and scope.go all reference this slice instead of maintaining independent lists.
var FunctionKinds = []string{
	"FunctionDeclaration",
	"ArrowFunction",
	"FunctionExpression",
	"MethodDefinition",
	"GeneratorFunction",
	"GeneratorFunctionDeclaration",
}

var functionKindSet map[string]bool

func init() {
	functionKindSet = make(map[string]bool, len(FunctionKinds))
	for _, k := range FunctionKinds {
		functionKindSet[k] = true
	}
}

// IsFunctionKind returns true if kind is a function-like node kind.
func IsFunctionKind(kind string) bool {
	return functionKindSet[kind]
}

// ExpressionKinds lists tree-sitter node kinds that represent expressions.
// Used to emit ExprInFunction for taint analysis scope constraints.
var ExpressionKinds = []string{
	"Identifier",
	"MemberExpression",
	"CallExpression",
	"NewExpression",
	"BinaryExpression",
	"UnaryExpression",
	"AssignmentExpression",
	"ConditionalExpression",
	"TemplateString",
	"TaggedTemplateExpression",
	"AwaitExpression",
	"YieldExpression",
	"ArrowFunction",
	"FunctionExpression",
	"ParenthesizedExpression",
	"ArrayExpression",
	"ObjectExpression",
	"SpreadElement",
	"AsExpression",
	"NonNullExpression",
	"SubscriptExpression",
	"String",
	"Number",
	"True",
	"False",
	"Null",
	"Undefined",
	"UpdateExpression",
	"SequenceExpression",
	"CommaExpression",
	"OptionalChainExpression",
}

var expressionKindSet map[string]bool

func init() {
	expressionKindSet = make(map[string]bool, len(ExpressionKinds))
	for _, k := range ExpressionKinds {
		expressionKindSet[k] = true
	}
}

// isExpressionKind returns true if kind is an expression node kind.
func isExpressionKind(kind string) bool {
	return expressionKindSet[kind]
}

// ValueSourceKinds lists tree-sitter node kinds that are "value-producing
// literals at their own location" — expressions whose runtime value is
// determined entirely by their own subtree. Used to populate the
// `ExprValueSource` relation that grounds the value-flow Phase A
// non-recursive `mayResolveTo`. See docs/design/valueflow-phase-a-plan.md §1.2.
//
// Carve-outs (NOT in this list, by design): identifiers, calls, member
// access, binary ops, await, `as` / `!` / parenthesised casts. Those resolve
// through other relations (ExprMayRef, ExprIsCall, FieldRead, Cast, ...).
//
// TemplateString (template literals) covers the no-substitutions case. The
// emit site checks for embedded TemplateExpression children and skips when
// substitutions are present — those are not deterministic by their own
// subtree.
var ValueSourceKinds = []string{
	// Object / array / function / class literals
	"Object",           // tree-sitter ObjectExpression
	"ObjectExpression", // tsgo / alternative kind name
	"ArrayExpression",
	"Array",
	"ArrowFunction",
	"FunctionExpression",
	"GeneratorFunction",
	"ClassExpression",
	// Primitive literals
	"String",
	"Number",
	"True",
	"False",
	"Null",
	"Undefined",
	"Regex",
	"RegularExpressionLiteral",
	// JSX
	"JsxElement",
	"JsxSelfClosingElement",
	"JsxFragment",
}

var valueSourceKindSet map[string]bool

func init() {
	valueSourceKindSet = make(map[string]bool, len(ValueSourceKinds))
	for _, k := range ValueSourceKinds {
		valueSourceKindSet[k] = true
	}
}

// IsValueSourceKind returns true if kind is a value-producing literal whose
// runtime value equals its own AST subtree. Template literals are NOT
// included here because some template strings have substitutions; the
// per-node check at emit time handles that case.
func IsValueSourceKind(kind string) bool {
	return valueSourceKindSet[kind]
}
