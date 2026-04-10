package extract

import (
	"strings"
	"unicode"
)

// tsgoNode wraps a tsgo API response node to implement the ASTNode interface.
// This is used when tsgo returns AST fragment information (e.g. from getDefinition
// or getQuickInfo responses). For full AST walking, the VendoredBackend delegates
// to tree-sitter instead.
//
// tsgo's actual AST types are in internal/ast and cannot be imported. This adapter
// bridges between tsgo's JSON API responses and tsq's ASTNode interface.
type tsgoNode struct {
	kind      string // raw tsgo kind (e.g. "FunctionDeclaration", "Identifier")
	startLine int    // 1-based
	startCol  int    // 0-based byte column
	endLine   int    // 1-based
	endCol    int    // 0-based byte column
	text      string
	children  []*tsgoNode
	fieldName string
}

// tsgoKindMap maps tsgo AST kind strings to tsq canonical PascalCase names.
// tsgo uses numeric SyntaxKind values internally but exposes string names in
// its API responses. Most are already PascalCase.
var tsgoKindMap = map[string]string{
	// tsgo kinds that differ from tsq canonical names
	"FunctionDeclaration":      "FunctionDeclaration",
	"ArrowFunction":            "ArrowFunction",
	"CallExpression":           "CallExpression",
	"Identifier":               "Identifier",
	"PropertyAccessExpression": "MemberExpression",
	"VariableDeclaration":      "VariableDeclarator",
	"ImportDeclaration":        "ImportDeclaration",
	"ExportDeclaration":        "ExportStatement",
	"JsxElement":               "JsxElement",
	"JsxSelfClosingElement":    "JsxSelfClosingElement",
	"AsExpression":             "AsExpression",
	"AwaitExpression":          "AwaitExpression",
	"BinaryExpression":         "BinaryExpression",
	"ObjectBindingPattern":     "ObjectPattern",
	"ArrayBindingPattern":      "ArrayPattern",
	"SpreadElement":            "SpreadElement",
	"SourceFile":               "Program",
	"Block":                    "Block",
	"ReturnStatement":          "ReturnStatement",
	"IfStatement":              "IfStatement",
	"ForStatement":             "ForStatement",
	"ForInStatement":           "ForInStatement",
	"WhileStatement":           "WhileStatement",
	"VariableDeclarationList":  "LexicalDeclaration",
	"VariableStatement":        "VariableDeclaration",
	"FunctionExpression":       "FunctionExpression",
	"MethodDeclaration":        "MethodDefinition",
	"ClassDeclaration":         "ClassDeclaration",
	"StringLiteral":            "String",
	"NumericLiteral":           "Number",
	"TemplateExpression":       "TemplateString",
	"ObjectLiteralExpression":  "Object",
	"ArrayLiteralExpression":   "Array",
	"PropertyAssignment":       "Pair",
	"NewExpression":            "NewExpression",
	"ParenthesizedExpression":  "ParenthesizedExpression",
	"ConditionalExpression":    "ConditionalExpression",
	"PrefixUnaryExpression":    "UnaryExpression",
	"PostfixUnaryExpression":   "UpdateExpression",
	"ElementAccessExpression":  "SubscriptExpression",
	"TypeReference":            "TypeIdentifier",
	"InterfaceDeclaration":     "InterfaceDeclaration",
	"TypeAliasDeclaration":     "TypeAliasDeclaration",
	"ExpressionStatement":      "ExpressionStatement",
	"TryStatement":             "TryStatement",
	"CatchClause":              "CatchClause",
	"ThrowStatement":           "ThrowStatement",
	"SwitchStatement":          "SwitchStatement",
	"CaseClause":               "SwitchCase",
	"DefaultClause":            "SwitchDefault",
	"BreakStatement":           "BreakStatement",
	"ContinueStatement":        "ContinueStatement",
	"LabeledStatement":         "LabeledStatement",
	"DoStatement":              "DoStatement",
	"Decorator":                "Decorator",
}

// normaliseTsgoKind converts a tsgo AST kind string to a tsq canonical PascalCase name.
func normaliseTsgoKind(tsgoKind string) string {
	if mapped, ok := tsgoKindMap[tsgoKind]; ok {
		return mapped
	}
	// Already PascalCase from tsgo? Check if it starts with uppercase.
	if len(tsgoKind) > 0 && unicode.IsUpper(rune(tsgoKind[0])) {
		return tsgoKind
	}
	// Fall back to snake_case conversion (unlikely for tsgo but defensive).
	return tsgoSnakeToPascal(tsgoKind)
}

// tsgoSnakeToPascal converts snake_case to PascalCase.
func tsgoSnakeToPascal(s string) string {
	parts := strings.Split(s, "_")
	var b strings.Builder
	for _, p := range parts {
		if len(p) == 0 {
			continue
		}
		runes := []rune(p)
		b.WriteRune(unicode.ToUpper(runes[0]))
		b.WriteString(string(runes[1:]))
	}
	return b.String()
}

func (n *tsgoNode) Kind() string {
	return normaliseTsgoKind(n.kind)
}

func (n *tsgoNode) StartLine() int {
	return n.startLine
}

func (n *tsgoNode) StartCol() int {
	return n.startCol
}

func (n *tsgoNode) EndLine() int {
	return n.endLine
}

func (n *tsgoNode) EndCol() int {
	return n.endCol
}

func (n *tsgoNode) Text() string {
	return n.text
}

func (n *tsgoNode) ChildCount() int {
	return len(n.children)
}

func (n *tsgoNode) Child(i int) ASTNode {
	if i < 0 || i >= len(n.children) {
		return nil
	}
	return n.children[i]
}

func (n *tsgoNode) FieldName() string {
	return n.fieldName
}

// newTsgoNode creates a tsgoNode from raw fields. Used when constructing
// nodes from tsgo API JSON responses.
func newTsgoNode(kind string, startLine, startCol, endLine, endCol int, text string) *tsgoNode {
	return &tsgoNode{
		kind:      kind,
		startLine: startLine,
		startCol:  startCol,
		endLine:   endLine,
		endCol:    endCol,
		text:      text,
	}
}

// addChild appends a child node.
func (n *tsgoNode) addChild(child *tsgoNode) {
	n.children = append(n.children, child)
}

// setFieldName sets the field name for this node in its parent context.
func (n *tsgoNode) setFieldName(name string) {
	n.fieldName = name
}
