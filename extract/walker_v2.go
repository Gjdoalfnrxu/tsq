package extract

import (
	"context"

	"github.com/Gjdoalfnrxu/tsq/extract/db"
)

// TypeAwareWalker wraps FactWalker and adds v2 type-aware fact emission using
// tree-sitter AST patterns. It emits structural facts for:
//   - Class/interface declarations (ClassDecl, InterfaceDecl)
//   - Heritage clauses (Implements, Extends)
//   - Method definitions (MethodDecl)
//   - new expressions (NewExpr)
//   - Member call expressions (MethodCall)
//   - Return statements (ReturnStmt)
//   - Function containment (FunctionContains)
//   - Type alias declarations (TypeDecl)
//   - Symbol/FunctionSymbol population from structural patterns
//
// Semantic relations that require tsgo (ExprType, TypeFromLib, etc.) are
// emitted as empty relations when tsgo is unavailable. The walker degrades
// gracefully: all tests pass without tsgo installed.
type TypeAwareWalker struct {
	fw *FactWalker

	// fnStack tracks the current function node IDs for FunctionContains/ReturnStmt.
	fnStack []uint32

	// classOrIfaceStack tracks the current class/interface node ID for MethodDecl.
	classOrIfaceStack []uint32

	// tsgoAvailable indicates whether a tsgo backend is available for semantic analysis.
	// When false, ExprType and TypeFromLib relations are left empty.
	tsgoAvailable bool
}

// NewTypeAwareWalker creates a TypeAwareWalker wrapping the given FactWalker.
func NewTypeAwareWalker(database *db.DB) *TypeAwareWalker {
	return &TypeAwareWalker{
		fw:            NewFactWalker(database),
		tsgoAvailable: false,
	}
}

// Run opens the backend, emits facts via the inner FactWalker, then overlays
// v2 type-aware facts.
func (tw *TypeAwareWalker) Run(ctx context.Context, backend ExtractorBackend, cfg ProjectConfig) error {
	if err := backend.Open(ctx, cfg); err != nil {
		return err
	}
	// SchemaVersion — exactly once at start (emitted by inner walker's visitor)
	tw.fw.emit("SchemaVersion", int32(db.SchemaVersion))
	return backend.WalkAST(ctx, tw)
}

// EnterFile delegates to the inner FactWalker and resets per-file v2 state.
func (tw *TypeAwareWalker) EnterFile(path string) error {
	tw.fnStack = tw.fnStack[:0]
	tw.classOrIfaceStack = tw.classOrIfaceStack[:0]
	return tw.fw.EnterFile(path)
}

// Enter processes a node: delegates to the inner FactWalker, then emits v2 facts.
func (tw *TypeAwareWalker) Enter(node ASTNode) (descend bool, err error) {
	// Delegate to inner walker first (emits Node, Contains, v1 facts)
	descend, err = tw.fw.Enter(node)
	if err != nil {
		return descend, err
	}

	// Emit v2 facts based on the node kind
	tw.emitV2Facts(node)

	return descend, nil
}

// Leave delegates to the inner FactWalker and pops v2 stacks.
func (tw *TypeAwareWalker) Leave(node ASTNode) error {
	kind := node.Kind()

	// Pop function stack
	if IsFunctionKind(kind) {
		if len(tw.fnStack) > 0 {
			tw.fnStack = tw.fnStack[:len(tw.fnStack)-1]
		}
	}

	// Pop class/interface stack
	switch kind {
	case "ClassDeclaration", "AbstractClassDeclaration", "ClassExpression":
		if len(tw.classOrIfaceStack) > 0 {
			tw.classOrIfaceStack = tw.classOrIfaceStack[:len(tw.classOrIfaceStack)-1]
		}
	case "InterfaceDeclaration":
		if len(tw.classOrIfaceStack) > 0 {
			tw.classOrIfaceStack = tw.classOrIfaceStack[:len(tw.classOrIfaceStack)-1]
		}
	}

	return tw.fw.Leave(node)
}

// LeaveFile delegates to the inner FactWalker.
func (tw *TypeAwareWalker) LeaveFile(path string) error {
	return tw.fw.LeaveFile(path)
}

// emitV2Facts emits v2 type-aware facts for a node.
func (tw *TypeAwareWalker) emitV2Facts(node ASTNode) {
	kind := node.Kind()
	id := tw.fw.nid(node)

	if IsFunctionKind(kind) {
		tw.pushFunction(node, id)
	}
	switch kind {
	case "ClassDeclaration", "AbstractClassDeclaration", "ClassExpression":
		tw.emitClassDecl(node, id)
	case "InterfaceDeclaration":
		tw.emitInterfaceDecl(node, id)
	case "NewExpression":
		tw.emitNewExpr(node, id)
	case "CallExpression":
		tw.emitMethodCall(node, id)
	case "ReturnStatement":
		tw.emitReturnStmt(node, id)
	case "TypeAliasDeclaration":
		tw.emitTypeDecl(node, id)
	case "VariableDeclarator":
		tw.emitSymbolFromVarDecl(node)
	case "Identifier":
		tw.emitSymInFunction(node)
	}

	// FunctionContains: any node inside a function body
	if len(tw.fnStack) > 0 {
		tw.fw.emit("FunctionContains", tw.fnStack[len(tw.fnStack)-1], id)
	}
}

// emitClassDecl emits ClassDecl and processes heritage clauses.
func (tw *TypeAwareWalker) emitClassDecl(node ASTNode, id uint32) {
	name := ""
	if nameNode := childByField(node, "name"); nameNode != nil {
		name = nameNode.Text()
	}
	tw.fw.emit("ClassDecl", id, name, tw.fw.fileID)

	// Push onto class/interface stack for MethodDecl
	tw.classOrIfaceStack = append(tw.classOrIfaceStack, id)

	// Emit Symbol for the class name
	if name != "" {
		nameNode := childByField(node, "name")
		if nameNode != nil {
			symID := SymID(tw.fw.filePath, name, nameNode.StartLine(), nameNode.StartCol())
			tw.fw.emit("Symbol", symID, name, tw.fw.nid(nameNode), tw.fw.fileID)
		}
	}

	// Walk heritage clauses for Extends/Implements
	tw.processHeritageOfClass(node, id)
}

// processHeritageOfClass walks children looking for extends/implements clauses.
// tree-sitter TypeScript grammar wraps heritage in a ClassHeritage node:
//
//	ClassDeclaration -> ClassHeritage -> ExtendsClause / ImplementsClause
func (tw *TypeAwareWalker) processHeritageOfClass(node ASTNode, classID uint32) {
	count := node.ChildCount()
	for i := 0; i < count; i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		switch child.Kind() {
		case "ClassHeritage":
			// Recurse into ClassHeritage to find ExtendsClause/ImplementsClause
			tw.processHeritageOfClass(child, classID)
		case "ExtendsClause", "HeritageClause", "ExtendsTypeClause":
			tw.processExtendsClause(child, classID)
		case "ImplementsClause":
			tw.processImplementsClause(child, classID)
		}
	}
}

// processExtendsClause processes an extends clause node.
func (tw *TypeAwareWalker) processExtendsClause(clause ASTNode, classID uint32) {
	count := clause.ChildCount()
	for i := 0; i < count; i++ {
		child := clause.Child(i)
		if child == nil {
			continue
		}
		k := child.Kind()
		if k == "," || child.Text() == "extends" {
			continue
		}
		// The child should be a type reference (Identifier or MemberExpression)
		if k == "Identifier" || k == "MemberExpression" || k == "TypeIdentifier" || k == "GenericType" {
			parentID := tw.fw.nid(child)
			tw.fw.emit("Extends", classID, parentID)
		}
	}
}

// processImplementsClause processes an implements clause node.
func (tw *TypeAwareWalker) processImplementsClause(clause ASTNode, classID uint32) {
	count := clause.ChildCount()
	for i := 0; i < count; i++ {
		child := clause.Child(i)
		if child == nil {
			continue
		}
		k := child.Kind()
		if k == "," || child.Text() == "implements" {
			continue
		}
		if k == "Identifier" || k == "MemberExpression" || k == "TypeIdentifier" || k == "GenericType" {
			ifaceID := tw.fw.nid(child)
			tw.fw.emit("Implements", classID, ifaceID)
		}
	}
}

// emitInterfaceDecl emits InterfaceDecl and processes extends clauses.
func (tw *TypeAwareWalker) emitInterfaceDecl(node ASTNode, id uint32) {
	name := ""
	if nameNode := childByField(node, "name"); nameNode != nil {
		name = nameNode.Text()
	}
	tw.fw.emit("InterfaceDecl", id, name, tw.fw.fileID)

	// Push onto class/interface stack for MethodDecl (interface methods)
	tw.classOrIfaceStack = append(tw.classOrIfaceStack, id)

	// Emit Symbol for the interface name
	if name != "" {
		nameNode := childByField(node, "name")
		if nameNode != nil {
			symID := SymID(tw.fw.filePath, name, nameNode.StartLine(), nameNode.StartCol())
			tw.fw.emit("Symbol", symID, name, tw.fw.nid(nameNode), tw.fw.fileID)
		}
	}

	// Walk children for extends clauses (interfaces extend other interfaces)
	count := node.ChildCount()
	for i := 0; i < count; i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		switch child.Kind() {
		case "ExtendsClause", "ExtendsTypeClause":
			tw.processExtendsClause(child, id)
		}
		if child.Text() == "extends" {
			// Next siblings are parent interfaces
			for j := i + 1; j < count; j++ {
				next := node.Child(j)
				if next == nil {
					continue
				}
				k := next.Kind()
				if k == "," {
					continue
				}
				if k == "{" || k == "ObjectType" || next.Text() == "implements" {
					break
				}
				if k == "Identifier" || k == "MemberExpression" || k == "TypeIdentifier" || k == "GenericType" {
					parentID := tw.fw.nid(next)
					tw.fw.emit("Extends", id, parentID)
				}
			}
		}
	}
}

// pushFunction pushes a function onto the function stack and emits v2 Symbol-related facts.
func (tw *TypeAwareWalker) pushFunction(node ASTNode, id uint32) {
	tw.fnStack = append(tw.fnStack, id)

	kind := node.Kind()
	// Emit FunctionSymbol for named functions
	if kind == "FunctionDeclaration" || kind == "GeneratorFunctionDeclaration" {
		if nameNode := childByField(node, "name"); nameNode != nil {
			name := nameNode.Text()
			if name != "" {
				symID := SymID(tw.fw.filePath, name, nameNode.StartLine(), nameNode.StartCol())
				tw.fw.emit("Symbol", symID, name, tw.fw.nid(nameNode), tw.fw.fileID)
				tw.fw.emit("FunctionSymbol", symID, id)
			}
		}
	}

	// Emit ReturnSym: synthetic symbol for the function's return value.
	{
		retSymID := ReturnSymID(tw.fw.filePath, node.StartLine(), node.StartCol())
		tw.fw.emit("ReturnSym", id, retSymID)
		// Also emit the return symbol into SymInFunction so dataflow rules can scope it.
		tw.fw.emit("SymInFunction", retSymID, id)
	}

	// MethodDecl: if inside a class or interface
	if kind == "MethodDefinition" && len(tw.classOrIfaceStack) > 0 {
		containerID := tw.classOrIfaceStack[len(tw.classOrIfaceStack)-1]
		name := ""
		if nameNode := childByField(node, "name"); nameNode != nil {
			name = nameNode.Text()
		}
		tw.fw.emit("MethodDecl", containerID, name, id)
	}
}

// emitNewExpr emits NewExpr for `new` expressions.
func (tw *TypeAwareWalker) emitNewExpr(node ASTNode, id uint32) {
	// new_expression: "new" constructor arguments
	constructor := childByField(node, "constructor")
	if constructor == nil {
		// Fallback: find the first non-"new" child
		count := node.ChildCount()
		for i := 0; i < count; i++ {
			child := node.Child(i)
			if child != nil && child.Text() != "new" {
				constructor = child
				break
			}
		}
	}
	var classID uint32
	if constructor != nil {
		classID = tw.fw.nid(constructor)
	}
	tw.fw.emit("NewExpr", id, classID)
}

// emitMethodCall emits MethodCall for member call expressions (obj.method()).
func (tw *TypeAwareWalker) emitMethodCall(node ASTNode, id uint32) {
	// CallExpression with a MemberExpression as the callee = method call
	calleeNode := childByField(node, "function")
	if calleeNode == nil && node.ChildCount() > 0 {
		calleeNode = node.Child(0)
	}
	if calleeNode == nil || calleeNode.Kind() != "MemberExpression" {
		return
	}

	objNode := childByField(calleeNode, "object")
	propNode := childByField(calleeNode, "property")
	if objNode == nil || propNode == nil {
		return
	}

	receiverID := tw.fw.nid(objNode)
	methodName := propNode.Text()
	tw.fw.emit("MethodCall", id, receiverID, methodName)
}

// emitReturnStmt emits ReturnStmt, associating the return with its enclosing function.
func (tw *TypeAwareWalker) emitReturnStmt(node ASTNode, id uint32) {
	if len(tw.fnStack) == 0 {
		return
	}
	fnID := tw.fnStack[len(tw.fnStack)-1]

	// Find the return expression (first non-keyword child)
	var exprID uint32
	count := node.ChildCount()
	for i := 0; i < count; i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		text := child.Text()
		if text == "return" || text == ";" {
			continue
		}
		exprID = tw.fw.nid(child)
		break
	}

	tw.fw.emit("ReturnStmt", fnID, id, exprID)
}

// emitTypeDecl emits TypeDecl for type alias declarations.
func (tw *TypeAwareWalker) emitTypeDecl(node ASTNode, id uint32) {
	name := ""
	if nameNode := childByField(node, "name"); nameNode != nil {
		name = nameNode.Text()
	}
	tw.fw.emit("TypeDecl", id, name, "alias", tw.fw.fileID)

	// Also emit a Symbol for the type alias
	if name != "" {
		nameNode := childByField(node, "name")
		if nameNode != nil {
			symID := SymID(tw.fw.filePath, name, nameNode.StartLine(), nameNode.StartCol())
			tw.fw.emit("Symbol", symID, name, tw.fw.nid(nameNode), tw.fw.fileID)
		}
	}
}

// emitSymbolFromVarDecl emits Symbol for variable declarations (populating the
// previously-empty v1 Symbol relation structurally).
func (tw *TypeAwareWalker) emitSymbolFromVarDecl(node ASTNode) {
	nameNode := childByField(node, "name")
	if nameNode == nil || nameNode.Kind() != "Identifier" {
		return
	}
	name := nameNode.Text()
	if name == "" {
		return
	}
	symID := SymID(tw.fw.filePath, name, nameNode.StartLine(), nameNode.StartCol())
	tw.fw.emit("Symbol", symID, name, tw.fw.nid(nameNode), tw.fw.fileID)

	// If the init expression is a function, also emit FunctionSymbol
	initNode := childByField(node, "value")
	if initNode != nil {
		k := initNode.Kind()
		if k == "ArrowFunction" || k == "FunctionExpression" {
			fnID := tw.fw.nid(initNode)
			tw.fw.emit("FunctionSymbol", symID, fnID)
		}
	}
}

// emitSymInFunction emits SymInFunction when an identifier reference appears inside a function.
func (tw *TypeAwareWalker) emitSymInFunction(node ASTNode) {
	if len(tw.fnStack) == 0 {
		return
	}
	name := node.Text()
	if name == "" {
		return
	}
	if decl, ok := tw.fw.scope.Resolve(name, node); ok {
		symID := SymID(decl.FilePath, decl.Name, decl.StartLine, decl.StartCol)
		fnID := tw.fnStack[len(tw.fnStack)-1]
		tw.fw.emit("SymInFunction", symID, fnID)
	}
}
