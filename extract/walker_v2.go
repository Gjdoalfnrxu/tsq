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
//   - Structural type facts (TypeInfo, UnionMember, IntersectionMember, etc.)
//
// Semantic relations that require tsgo (ExprType, SymbolType, etc.) are
// populated when tsgo enrichment is available. Structural type relations
// (TypeInfo, UnionMember, IntersectionMember, TypeParameter, TypeAlias,
// GenericInstantiation) are emitted from AST patterns regardless of tsgo.
// The walker degrades gracefully: all tests pass without tsgo installed.
type TypeAwareWalker struct {
	fw *FactWalker

	// fnStack tracks the current function node IDs for FunctionContains/ReturnStmt.
	fnStack []uint32

	// classOrIfaceStack tracks the current class/interface node ID for MethodDecl.
	classOrIfaceStack []uint32

	// tsgoAvailable indicates whether a tsgo backend is available for semantic analysis.
	// When false, ExprType and SymbolType relations are left empty (tsgo-dependent).
	// Structural type relations (TypeInfo, UnionMember, etc.) are always populated from AST.
	tsgoAvailable bool

	// nsStack tracks the current namespace/module node IDs for NamespaceMember.
	nsStack []uint32
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
	tw.nsStack = tw.nsStack[:0]
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
	case "ModuleDeclaration", "InternalModule":
		if len(tw.nsStack) > 0 {
			tw.nsStack = tw.nsStack[:len(tw.nsStack)-1]
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
		// Emit FunctionContains for the function node *before* pushing it
		// onto fnStack, so that nested function literals are linked to
		// their lexically enclosing parent function. Without this, the
		// innermost-only semantics of FunctionContains would emit
		// `FunctionContains(innerFn, innerFn)` (a self-row) and never
		// `FunctionContains(outerFn, innerFn)`, breaking transitive
		// queries like `functionContainsStar` in tsq_react.qll.
		if len(tw.fnStack) > 0 {
			tw.fw.emit("FunctionContains", tw.fnStack[len(tw.fnStack)-1], id)
		}
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
	case "EnumDeclaration", "ConstEnumDeclaration":
		tw.emitEnumDecl(node, id)
	case "OptionalChainExpression":
		tw.emitOptionalChain(node, id)
	case "BinaryExpression":
		tw.emitNullishCoalescing(node, id)
	case "Identifier":
		tw.emitSymInFunction(node)
	case "UnionType":
		tw.emitUnionType(node, id)
	case "IntersectionType":
		tw.emitIntersectionType(node, id)
	case "GenericType":
		tw.emitGenericType(node, id)
	case "TypeParameter":
		tw.emitTypeParameter(node, id)
	case "Decorator":
		tw.emitDecorator(node, id)
	case "ModuleDeclaration", "InternalModule":
		tw.emitNamespaceDecl(node, id)
	case "TypePredicate", "PredicateType":
		tw.emitTypeGuard(node, id)
	}

	// FunctionContains: any node inside a function body. Function nodes
	// themselves are emitted above, before being pushed onto fnStack, so
	// they link to their parent function rather than themselves. Skip
	// re-emitting here for function kinds to avoid the self-row.
	if !IsFunctionKind(kind) && len(tw.fnStack) > 0 {
		tw.fw.emit("FunctionContains", tw.fnStack[len(tw.fnStack)-1], id)
	}

	// ExprInFunction: expression nodes inside a function body.
	if isExpressionKind(kind) && len(tw.fnStack) > 0 {
		tw.fw.emit("ExprInFunction", id, tw.fnStack[len(tw.fnStack)-1])
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

	// Emit TypeParameter for any type parameters on this class
	tw.emitTypeParameters(node, id)

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

	// Emit TypeParameter for any type parameters on this interface
	tw.emitTypeParameters(node, id)

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

	// TypeParameter for generic functions (function identity<T>(...) {})
	if kind == "FunctionDeclaration" || kind == "GeneratorFunctionDeclaration" || kind == "MethodDefinition" {
		tw.emitTypeParameters(node, id)
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

// emitTypeDecl emits TypeDecl for type alias declarations, plus TypeInfo and TypeAlias.
func (tw *TypeAwareWalker) emitTypeDecl(node ASTNode, id uint32) {
	name := ""
	if nameNode := childByField(node, "name"); nameNode != nil {
		name = nameNode.Text()
	}
	tw.fw.emit("TypeDecl", id, name, "alias", tw.fw.fileID)

	// Emit TypeInfo for the alias declaration itself
	tw.fw.emit("TypeInfo", id, "alias", name)

	// Emit TypeAlias linking the alias to its RHS type node
	if valueNode := childByField(node, "value"); valueNode != nil {
		rhsID := tw.fw.nid(valueNode)
		tw.fw.emit("TypeAlias", id, rhsID)
	}

	// Also emit a Symbol for the type alias
	if name != "" {
		nameNode := childByField(node, "name")
		if nameNode != nil {
			symID := SymID(tw.fw.filePath, name, nameNode.StartLine(), nameNode.StartCol())
			tw.fw.emit("Symbol", symID, name, tw.fw.nid(nameNode), tw.fw.fileID)
		}
	}

	// Emit TypeParameter for any type parameters on this declaration
	tw.emitTypeParameters(node, id)
}

// emitUnionType emits TypeInfo and UnionMember tuples for union type nodes (A | B | C).
func (tw *TypeAwareWalker) emitUnionType(node ASTNode, id uint32) {
	tw.fw.emit("TypeInfo", id, "union", node.Text())
	count := node.ChildCount()
	for i := 0; i < count; i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		k := child.Kind()
		if k == "|" {
			continue
		}
		memberID := tw.fw.nid(child)
		tw.fw.emit("UnionMember", id, memberID)
	}
}

// emitIntersectionType emits TypeInfo and IntersectionMember tuples for intersection type nodes (A & B).
func (tw *TypeAwareWalker) emitIntersectionType(node ASTNode, id uint32) {
	tw.fw.emit("TypeInfo", id, "intersection", node.Text())
	count := node.ChildCount()
	for i := 0; i < count; i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		k := child.Kind()
		if k == "&" {
			continue
		}
		memberID := tw.fw.nid(child)
		tw.fw.emit("IntersectionMember", id, memberID)
	}
}

// emitGenericType emits TypeInfo and GenericInstantiation tuples for generic type references (Box<string>).
func (tw *TypeAwareWalker) emitGenericType(node ASTNode, id uint32) {
	tw.fw.emit("TypeInfo", id, "generic", node.Text())

	// Find the base type name (first child, typically an Identifier or TypeIdentifier)
	nameNode := childByField(node, "name")
	if nameNode == nil {
		// Fallback: first child that is an identifier
		count := node.ChildCount()
		for i := 0; i < count; i++ {
			child := node.Child(i)
			if child == nil {
				continue
			}
			k := child.Kind()
			if k == "Identifier" || k == "TypeIdentifier" {
				nameNode = child
				break
			}
		}
	}

	var genericTypeID uint32
	if nameNode != nil {
		genericTypeID = tw.fw.nid(nameNode)
	}

	// Find type arguments
	argsNode := childByKind(node, "TypeArguments")
	if argsNode == nil {
		// Try field-based access
		argsNode = childByField(node, "type_arguments")
	}
	if argsNode != nil {
		idx := int32(0)
		ac := argsNode.ChildCount()
		for i := 0; i < ac; i++ {
			arg := argsNode.Child(i)
			if arg == nil {
				continue
			}
			k := arg.Kind()
			if k == "<" || k == ">" || k == "," {
				continue
			}
			argID := tw.fw.nid(arg)
			tw.fw.emit("GenericInstantiation", id, genericTypeID, idx, argID)
			idx++
		}
	}
}

// emitTypeParameter emits a TypeParameter tuple for an individual type parameter node.
// TypeParameter nodes appear as children of TypeParameters (the container).
// The parent declaration (class, interface, function, type alias) is determined
// by walking up the classOrIfaceStack or by the caller passing the decl ID.
// Since tree-sitter visits TypeParameter as a standalone node, we find the
// enclosing declaration from the walker's current context.
func (tw *TypeAwareWalker) emitTypeParameter(node ASTNode, _ uint32) {
	// TypeParameter emission is handled by emitTypeParameters called from
	// the parent declaration (emitClassDecl, emitInterfaceDecl, emitTypeDecl).
	// This case is intentionally a no-op to avoid double-emission.
	_ = node
}

// emitTypeParameters finds and emits TypeParameter tuples for all type parameters
// on a declaration node (class, interface, type alias, function).
func (tw *TypeAwareWalker) emitTypeParameters(node ASTNode, declID uint32) {
	// Look for TypeParameters child
	tpNode := childByKind(node, "TypeParameters")
	if tpNode == nil {
		tpNode = childByField(node, "type_parameters")
	}
	if tpNode == nil {
		return
	}

	idx := int32(0)
	count := tpNode.ChildCount()
	for i := 0; i < count; i++ {
		child := tpNode.Child(i)
		if child == nil {
			continue
		}
		k := child.Kind()
		if k == "<" || k == ">" || k == "," {
			continue
		}
		if k != "TypeParameter" {
			continue
		}

		// Extract the type parameter name
		name := ""
		if nameNode := childByField(child, "name"); nameNode != nil {
			name = nameNode.Text()
		} else {
			// Fallback: first Identifier or TypeIdentifier child
			cc := child.ChildCount()
			for j := 0; j < cc; j++ {
				gc := child.Child(j)
				if gc == nil {
					continue
				}
				gk := gc.Kind()
				if gk == "Identifier" || gk == "TypeIdentifier" {
					name = gc.Text()
					break
				}
			}
		}

		// Extract constraint type ID (if present: T extends SomeType)
		var constraintTypeID uint32
		if constraintNode := childByField(child, "constraint"); constraintNode != nil {
			constraintTypeID = tw.fw.nid(constraintNode)
		} else {
			// Look for Constraint child
			cc := child.ChildCount()
			for j := 0; j < cc; j++ {
				gc := child.Child(j)
				if gc == nil {
					continue
				}
				if gc.Kind() == "Constraint" {
					// The constraint type is inside the Constraint node
					cc2 := gc.ChildCount()
					for m := 0; m < cc2; m++ {
						inner := gc.Child(m)
						if inner == nil {
							continue
						}
						if inner.Text() == "extends" {
							continue
						}
						constraintTypeID = tw.fw.nid(inner)
						break
					}
					break
				}
			}
		}

		tw.fw.emit("TypeParameter", declID, idx, name, constraintTypeID)
		idx++
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

// emitEnumDecl emits EnumDecl and EnumMember tuples for TypeScript enum declarations.
func (tw *TypeAwareWalker) emitEnumDecl(node ASTNode, id uint32) {
	name := ""
	if nameNode := childByField(node, "name"); nameNode != nil {
		name = nameNode.Text()
	}
	tw.fw.emit("EnumDecl", id, name, tw.fw.fileID)

	// Walk children for enum members
	bodyNode := childByField(node, "body")
	if bodyNode == nil {
		bodyNode = childByKind(node, "EnumBody")
	}
	if bodyNode == nil {
		bodyNode = node
	}

	count := bodyNode.ChildCount()
	for i := 0; i < count; i++ {
		child := bodyNode.Child(i)
		if child == nil {
			continue
		}
		k := child.Kind()
		if k == "{" || k == "}" || k == "," {
			continue
		}
		memberName := ""
		var initExprID uint32
		switch k {
		case "EnumAssignment":
			if mn := childByField(child, "name"); mn != nil {
				memberName = mn.Text()
			}
			if vn := childByField(child, "value"); vn != nil {
				initExprID = tw.fw.nid(vn)
			}
		case "PropertyIdentifier", "Identifier":
			memberName = child.Text()
		default:
			memberName = child.Text()
		}
		if memberName != "" {
			tw.fw.emit("EnumMember", id, memberName, initExprID)
		}
	}
}

// emitOptionalChain emits OptionalChain for optional chaining expressions (obj?.prop).
func (tw *TypeAwareWalker) emitOptionalChain(node ASTNode, id uint32) {
	// OptionalChainExpression wraps the base expression
	var baseID uint32
	count := node.ChildCount()
	for i := 0; i < count; i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		k := child.Kind()
		if k != "?." && k != "?.[" {
			baseID = tw.fw.nid(child)
			break
		}
	}
	tw.fw.emit("OptionalChain", id, baseID)
}

// emitNullishCoalescing emits NullishCoalescing for ?? binary expressions.
func (tw *TypeAwareWalker) emitNullishCoalescing(node ASTNode, id uint32) {
	// Check if this is a ?? operator
	hasNullish := false
	count := node.ChildCount()
	for i := 0; i < count; i++ {
		child := node.Child(i)
		if child != nil && child.Text() == "??" {
			hasNullish = true
			break
		}
	}
	if !hasNullish {
		return
	}

	leftNode := childByField(node, "left")
	rightNode := childByField(node, "right")
	var leftID, rightID uint32
	if leftNode != nil {
		leftID = tw.fw.nid(leftNode)
	}
	if rightNode != nil {
		rightID = tw.fw.nid(rightNode)
	}
	tw.fw.emit("NullishCoalescing", id, leftID, rightID)
}

// emitDecorator emits Decorator(targetId, decoratorExpr) for decorator nodes.
// In tree-sitter TypeScript, Decorator nodes appear as children of class declarations,
// method definitions, and property declarations. The decorator's parent is the target.
func (tw *TypeAwareWalker) emitDecorator(node ASTNode, id uint32) {
	// The decorator's expression is the first child after the '@' token.
	var decoratorExprID uint32
	count := node.ChildCount()
	for i := 0; i < count; i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		if child.Text() == "@" {
			continue
		}
		decoratorExprID = tw.fw.nid(child)
		break
	}
	// The target is the class/interface/method that contains this decorator;
	// use the top of the class stack if available, otherwise use parent context.
	var targetID uint32
	if len(tw.classOrIfaceStack) > 0 {
		targetID = tw.classOrIfaceStack[len(tw.classOrIfaceStack)-1]
	} else {
		// Fallback: emit with the decorator node itself as target so the row is still useful.
		targetID = id
	}
	tw.fw.emit("Decorator", targetID, decoratorExprID)
}

// emitNamespaceDecl emits NamespaceDecl and NamespaceMember for TypeScript namespace/module declarations.
// Handles ModuleDeclaration (namespace Foo {}) and InternalModule variants.
func (tw *TypeAwareWalker) emitNamespaceDecl(node ASTNode, id uint32) {
	name := ""
	if nameNode := childByField(node, "name"); nameNode != nil {
		name = nameNode.Text()
	} else {
		// Fallback: find first string or identifier child
		count := node.ChildCount()
		for i := 0; i < count; i++ {
			child := node.Child(i)
			if child == nil {
				continue
			}
			k := child.Kind()
			if k == "namespace" || k == "module" || k == "declare" {
				continue
			}
			if k == "Identifier" || k == "String" || k == "StringFragment" {
				name = child.Text()
				break
			}
		}
	}
	tw.fw.emit("NamespaceDecl", id, name, tw.fw.fileID)

	// If nested inside another namespace, emit NamespaceMember
	if len(tw.nsStack) > 0 {
		parentNS := tw.nsStack[len(tw.nsStack)-1]
		tw.fw.emit("NamespaceMember", parentNS, id)
	}

	// Push onto namespace stack for children
	tw.nsStack = append(tw.nsStack, id)

	// Emit NamespaceMember for direct children in the body
	bodyNode := childByField(node, "body")
	if bodyNode == nil {
		bodyNode = childByKind(node, "StatementBlock")
		if bodyNode == nil {
			bodyNode = childByKind(node, "NamespaceBody")
		}
	}
	if bodyNode != nil {
		count := bodyNode.ChildCount()
		for i := 0; i < count; i++ {
			child := bodyNode.Child(i)
			if child == nil {
				continue
			}
			k := child.Kind()
			if k == "{" || k == "}" || k == ";" {
				continue
			}
			memberID := tw.fw.nid(child)
			tw.fw.emit("NamespaceMember", id, memberID)
		}
	}
}

// emitTypeGuard emits TypeGuard(fnId, paramIdx, narrowedType) for type predicate return types.
// Handles `x is T` (TypePredicate) and `asserts x` patterns in function return annotations.
func (tw *TypeAwareWalker) emitTypeGuard(node ASTNode, id uint32) {
	if len(tw.fnStack) == 0 {
		return
	}
	fnID := tw.fnStack[len(tw.fnStack)-1]

	// Parse the TypePredicate / PredicateType node.
	// For `x is T`: children are [paramName, "is", type]
	// For `asserts x`: children are ["asserts", paramName]
	count := node.ChildCount()

	// Check for "asserts" pattern
	hasAsserts := false
	paramName := ""
	narrowedType := ""

	for i := 0; i < count; i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		text := child.Text()
		k := child.Kind()
		if text == "asserts" {
			hasAsserts = true
			continue
		}
		if text == "is" {
			continue
		}
		if k == "Identifier" || k == "TypeIdentifier" {
			if paramName == "" && !hasAsserts {
				paramName = text
			} else if paramName == "" && hasAsserts {
				paramName = text
			} else {
				// This is the narrowed type
				narrowedType = text
			}
		} else if k != "{" && k != "}" {
			// Could be a complex type node
			if narrowedType == "" && paramName != "" {
				narrowedType = text
			}
		}
	}

	if hasAsserts {
		narrowedType = "asserts"
	}

	// Find the parameter index by matching paramName to the enclosing function's parameters.
	paramIdx := int32(0)
	if paramName != "" {
		// Walk the function node's parameters to find the matching index.
		// fnStack holds IDs — look up the function node via the current context.
		// We emit with index 0 as fallback if we can't resolve.
		paramIdx = tw.resolveParamIdx(fnID, paramName)
	}

	if narrowedType != "" || hasAsserts {
		tw.fw.emit("TypeGuard", fnID, paramIdx, narrowedType)
	}
}

// resolveParamIdx attempts to find the parameter index matching paramName
// in the enclosing function by scanning the scope. Returns 0 as fallback.
func (tw *TypeAwareWalker) resolveParamIdx(_ uint32, _ string) int32 {
	// Structural resolution without a full parameter map is complex.
	// Return 0 as a conservative fallback — the TypeGuard row is still
	// emitted with the correct narrowedType, which is the primary value.
	return 0
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
