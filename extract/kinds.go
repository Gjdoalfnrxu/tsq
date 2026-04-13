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
