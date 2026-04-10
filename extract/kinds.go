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
