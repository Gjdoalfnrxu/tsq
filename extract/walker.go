package extract

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"github.com/Gjdoalfnrxu/tsq/extract/db"
)

// FactWalker implements ASTVisitor. It walks a project AST via an
// ExtractorBackend and emits fact tuples into a *db.DB.
//
// Relations deliberately left EMPTY in v1:
//   - Symbol:         partially populated via scope; full population requires cross-file analysis
//   - FunctionSymbol: requires cross-file analysis
//   - CallResultSym:  requires cross-file analysis
//   - TypeFromLib:    requires type checker integration
type FactWalker struct {
	db *db.DB

	// per-file state, reset in EnterFile
	filePath         string
	fileID           uint32
	scope            *ScopeAnalyzer
	rootSeen         bool  // has scope been built (on root Program node)?
	currentDeclConst int32 // 1 if current LexicalDeclaration is const, else 0

	// parent tracking: stack of node IDs (push on Enter, pop on Leave)
	stack []uint32

	// jsxElementStack tracks the current JSX element ID so JsxAttribute nodes
	// can reference their containing element even when nested inside JsxOpeningElement.
	jsxElementStack []uint32
}

// NewFactWalker creates a FactWalker that writes facts into the given DB.
func NewFactWalker(database *db.DB) *FactWalker {
	return &FactWalker{db: database}
}

// Run opens the backend, emits the SchemaVersion tuple, then walks all files.
func (fw *FactWalker) Run(ctx context.Context, backend ExtractorBackend, cfg ProjectConfig) error {
	if err := backend.Open(ctx, cfg); err != nil {
		return fmt.Errorf("walker: open backend: %w", err)
	}
	// SchemaVersion — exactly once at start
	fw.emit("SchemaVersion", int32(db.SchemaVersion))
	return backend.WalkAST(ctx, fw)
}

// EnterFile is called before any node in path is visited.
func (fw *FactWalker) EnterFile(path string) error {
	fw.filePath = path
	fw.fileID = FileID(path)
	fw.rootSeen = false
	fw.currentDeclConst = 0
	fw.stack = fw.stack[:0]
	fw.jsxElementStack = fw.jsxElementStack[:0]
	fw.scope = NewScopeAnalyzer(path)

	content, err := os.ReadFile(path)
	if err != nil {
		fw.emitExtractError(fw.fileID, 0, "read", err.Error())
		return nil
	}
	sum := sha256.Sum256(content)
	contentHash := hex.EncodeToString(sum[:])
	fw.emit("File", fw.fileID, path, contentHash)
	return nil
}

// Enter is called when the walker descends into a node.
func (fw *FactWalker) Enter(node ASTNode) (descend bool, err error) {
	defer func() {
		if r := recover(); r != nil {
			fw.emitExtractError(fw.fileID, node.StartLine(), "emit",
				fmt.Sprintf("panic processing %s: %v", node.Kind(), r))
			descend = true
		}
	}()
	return fw.enterNode(node)
}

// Leave is called after all children of a node have been visited.
func (fw *FactWalker) Leave(node ASTNode) error {
	kind := node.Kind()
	// Reset const tracking when leaving a lexical declaration
	if kind == "LexicalDeclaration" || kind == "VariableDeclaration" {
		fw.currentDeclConst = 0
	}
	// Pop JSX element stack
	if kind == "JsxElement" || kind == "JsxSelfClosingElement" {
		if len(fw.jsxElementStack) > 0 {
			fw.jsxElementStack = fw.jsxElementStack[:len(fw.jsxElementStack)-1]
		}
	}
	if len(fw.stack) > 0 {
		fw.stack = fw.stack[:len(fw.stack)-1]
	}
	return nil
}

// LeaveFile is called after the last node in the current file.
func (fw *FactWalker) LeaveFile(path string) error {
	return nil
}

// enterNode processes a single node: emits Node, Contains, and kind-specific facts.
func (fw *FactWalker) enterNode(node ASTNode) (bool, error) {
	kind := node.Kind()

	// Build scope on the Program root — must happen while tree-sitter nodes are live.
	if kind == "Program" && !fw.rootSeen {
		fw.rootSeen = true
		fw.scope.Build(node)
	}

	// Track const/let/var for child VariableDeclarators
	if kind == "LexicalDeclaration" || kind == "VariableDeclaration" {
		fw.currentDeclConst = fw.detectConst(node)
	}

	id := NodeID(fw.filePath, node.StartLine(), node.StartCol(),
		node.EndLine(), node.EndCol(), kind)

	// Node: (id, file, kind, startLine, startCol, endLine, endCol)
	fw.emit("Node", id, fw.fileID, kind,
		int32(node.StartLine()), int32(node.StartCol()),
		int32(node.EndLine()), int32(node.EndCol()))

	// Contains: (parent, child)
	if len(fw.stack) > 0 {
		fw.emit("Contains", fw.stack[len(fw.stack)-1], id)
	}

	fw.stack = append(fw.stack, id)

	// Kind-specific fact emission
	switch kind {
	case "FunctionDeclaration", "ArrowFunction", "FunctionExpression", "MethodDefinition",
		"GeneratorFunction", "GeneratorFunctionDeclaration":
		fw.emitFunction(node, id, kind)
	case "CallExpression":
		fw.emitCall(node, id)
	case "VariableDeclarator":
		fw.emitVarDecl(node, id)
	case "AssignmentExpression":
		fw.emitAssign(node, id)
	case "Identifier":
		fw.emitExprMayRef(node, id)
	case "MemberExpression":
		fw.emitFieldRead(node, id)
	case "AwaitExpression":
		fw.emitAwait(node, id)
	case "AsExpression", "TypeAssertion", "NonNullExpression", "SatisfiesExpression":
		fw.emitCast(node, id)
	case "ObjectPattern":
		fw.emitDestructureObject(node, id)
	case "ArrayPattern":
		fw.emitDestructureArray(node, id)
	case "ImportDeclaration":
		fw.emitImportBinding(node)
	case "ExportStatement":
		fw.emitExportBinding(node)
	case "JsxElement", "JsxSelfClosingElement":
		fw.emitJsxElement(node, id)
	case "JsxAttribute":
		// Use the jsxElementStack to find the enclosing JsxElement/JsxSelfClosingElement.
		if len(fw.jsxElementStack) > 0 {
			fw.emitJsxAttr(node, fw.jsxElementStack[len(fw.jsxElementStack)-1])
		}
	case "Error":
		fw.emitExtractError(fw.fileID, node.StartLine(), "parse",
			fmt.Sprintf("syntax error at line %d col %d", node.StartLine(), node.StartCol()))
	}

	return true, nil
}

// ---- helpers ----

// emit adds a tuple to the named relation; errors are silently dropped.
func (fw *FactWalker) emit(rel string, vals ...interface{}) {
	r := fw.db.Relation(rel)
	_ = r.AddTuple(fw.db, vals...)
}

func (fw *FactWalker) emitExtractError(fileID uint32, line int, phase, msg string) {
	_ = fw.db.Relation("ExtractError").AddTuple(fw.db, fileID, int32(line), phase, msg)
}

// nid computes the NodeID for an ASTNode in the current file.
func (fw *FactWalker) nid(n ASTNode) uint32 {
	return NodeID(fw.filePath, n.StartLine(), n.StartCol(), n.EndLine(), n.EndCol(), n.Kind())
}

// detectConst scans a LexicalDeclaration / VariableDeclaration for const keyword.
func (fw *FactWalker) detectConst(n ASTNode) int32 {
	count := n.ChildCount()
	for i := 0; i < count; i++ {
		child := n.Child(i)
		if child != nil && child.Text() == "const" {
			return 1
		}
	}
	return 0
}

// childByField returns the first child of n with the given field name.
func childByField(n ASTNode, field string) ASTNode {
	count := n.ChildCount()
	for i := 0; i < count; i++ {
		child := n.Child(i)
		if child != nil && child.FieldName() == field {
			return child
		}
	}
	return nil
}

// childByKind returns the first direct child with the given normalised kind.
func childByKind(n ASTNode, kind string) ASTNode {
	count := n.ChildCount()
	for i := 0; i < count; i++ {
		child := n.Child(i)
		if child != nil && child.Kind() == kind {
			return child
		}
	}
	return nil
}

// ---- Function ----

func (fw *FactWalker) emitFunction(node ASTNode, id uint32, kind string) {
	isArrow := boolInt(kind == "ArrowFunction")
	isMethod := boolInt(kind == "MethodDefinition")
	// Generator function declarations have isGenerator=1 by kind
	isGenerator := boolInt(kind == "GeneratorFunction" || kind == "GeneratorFunctionDeclaration")
	isAsync := int32(0)
	name := ""

	if nameNode := childByField(node, "name"); nameNode != nil {
		name = nameNode.Text()
	}

	count := node.ChildCount()
	for i := 0; i < count; i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		switch child.Text() {
		case "async":
			isAsync = 1
		case "*":
			isGenerator = 1
		}
	}

	fw.emit("Function", id, name, isArrow, isAsync, isGenerator, isMethod)
	fw.emitParameters(node, id)
}

func (fw *FactWalker) emitParameters(fnNode ASTNode, fnID uint32) {
	var params ASTNode
	count := fnNode.ChildCount()
	for i := 0; i < count; i++ {
		child := fnNode.Child(i)
		if child == nil {
			continue
		}
		if child.FieldName() == "parameters" || child.Kind() == "FormalParameters" {
			params = child
			break
		}
	}
	if params == nil {
		return
	}

	idx := int32(0)
	pcount := params.ChildCount()
	for i := 0; i < pcount; i++ {
		param := params.Child(i)
		if param == nil {
			continue
		}
		pKind := param.Kind()
		// Skip punctuation
		if pKind == "," || pKind == "(" || pKind == ")" {
			continue
		}

		paramID := fw.nid(param)
		paramName, typeText, isRest, isOptional := fw.extractParamInfo(param, pKind)

		var symID uint32
		if paramName != "" {
			symID = SymID(fw.filePath, paramName, param.StartLine(), param.StartCol())
		}

		isFnType := strings.Contains(typeText, "=>")

		fw.emit("Parameter", fnID, idx, paramName, paramID, symID, typeText)
		if isRest {
			fw.emit("ParameterRest", fnID, idx)
		}
		if isOptional {
			fw.emit("ParameterOptional", fnID, idx)
		}
		if isFnType {
			fw.emit("ParamIsFunctionType", fnID, idx)
		}
		idx++
	}
}

func (fw *FactWalker) extractParamInfo(param ASTNode, pKind string) (name, typeText string, isRest, isOptional bool) {
	switch pKind {
	case "RequiredParameter":
		if tn := childByField(param, "type"); tn != nil {
			typeText = tn.Text()
		}
		patNode := childByField(param, "pattern")
		if patNode == nil {
			patNode = childByField(param, "name")
		}
		if patNode != nil {
			if patNode.Kind() == "RestPattern" {
				// ...rest wrapped in RequiredParameter
				isRest = true
				if inner := childByKind(patNode, "Identifier"); inner != nil {
					name = inner.Text()
				} else {
					name = patNode.Text()
				}
			} else {
				name = patNode.Text()
			}
		}
	case "OptionalParameter":
		isOptional = true
		if nn := childByField(param, "pattern"); nn != nil {
			name = nn.Text()
		} else if nn := childByField(param, "name"); nn != nil {
			name = nn.Text()
		}
		if tn := childByField(param, "type"); tn != nil {
			typeText = tn.Text()
		}
	case "RestPattern", "RestParameter":
		isRest = true
		if inner := childByKind(param, "Identifier"); inner != nil {
			name = inner.Text()
		} else if nn := childByField(param, "pattern"); nn != nil {
			name = nn.Text()
		}
	case "Identifier":
		name = param.Text()
	case "AssignmentPattern":
		if left := childByField(param, "left"); left != nil {
			name = left.Text()
		}
	case "ObjectPattern", "ArrayPattern":
		name = param.Text()
	}
	return
}

// ---- Call ----

func (fw *FactWalker) emitCall(node ASTNode, id uint32) {
	calleeNode := childByField(node, "function")
	if calleeNode == nil && node.ChildCount() > 0 {
		calleeNode = node.Child(0)
	}

	var calleeID uint32
	if calleeNode != nil {
		calleeID = fw.nid(calleeNode)
	}

	argsNode := childByField(node, "arguments")
	arity := int32(0)
	var argList []ASTNode

	if argsNode != nil {
		ac := argsNode.ChildCount()
		for i := 0; i < ac; i++ {
			child := argsNode.Child(i)
			if child == nil {
				continue
			}
			k := child.Kind()
			if k == "," || k == "(" || k == ")" {
				continue
			}
			argList = append(argList, child)
		}
		arity = int32(len(argList))
	}

	fw.emit("Call", id, calleeID, arity)
	fw.emit("ExprIsCall", id, id)

	for i, arg := range argList {
		argID := fw.nid(arg)
		fw.emit("CallArg", id, int32(i), argID)
		if arg.Kind() == "SpreadElement" {
			fw.emit("CallArgSpread", id, int32(i))
		}
	}

	// CallCalleeSym — resolve if callee is a simple Identifier
	if calleeNode != nil && calleeNode.Kind() == "Identifier" {
		if decl, ok := fw.scope.Resolve(calleeNode.Text(), calleeNode); ok {
			symID := SymID(decl.FilePath, decl.Name, 0, decl.StartByte)
			fw.emit("CallCalleeSym", id, symID)
		}
	}
}

// ---- VarDecl ----

func (fw *FactWalker) emitVarDecl(node ASTNode, id uint32) {
	nameNode := childByField(node, "name")
	initNode := childByField(node, "value")

	var symID uint32
	if nameNode != nil {
		name := nameNode.Text()
		symID = SymID(fw.filePath, name, nameNode.StartLine(), nameNode.StartCol())
	}

	var initID uint32
	if initNode != nil {
		initID = fw.nid(initNode)
	}

	fw.emit("VarDecl", id, symID, initID, fw.currentDeclConst)
}

// ---- Assign ----

func (fw *FactWalker) emitAssign(node ASTNode, id uint32) {
	leftNode := childByField(node, "left")
	rightNode := childByField(node, "right")

	var leftID, rightID uint32
	if leftNode != nil {
		leftID = fw.nid(leftNode)
	}
	if rightNode != nil {
		rightID = fw.nid(rightNode)
	}

	var lhsSymID uint32
	if leftNode != nil && leftNode.Kind() == "Identifier" {
		if decl, ok := fw.scope.Resolve(leftNode.Text(), leftNode); ok {
			lhsSymID = SymID(decl.FilePath, decl.Name, 0, decl.StartByte)
		}
	}

	fw.emit("Assign", leftID, rightID, lhsSymID)

	// FieldWrite if LHS is a member expression
	if leftNode != nil && leftNode.Kind() == "MemberExpression" {
		fw.emitFieldWrite(leftNode, id, rightID)
	}
}

// ---- ExprMayRef ----

func (fw *FactWalker) emitExprMayRef(node ASTNode, id uint32) {
	name := node.Text()
	if name == "" {
		return
	}
	if decl, ok := fw.scope.Resolve(name, node); ok {
		symID := SymID(decl.FilePath, decl.Name, 0, decl.StartByte)
		fw.emit("ExprMayRef", id, symID)
	}
}

// ---- FieldRead / FieldWrite ----

func (fw *FactWalker) emitFieldRead(node ASTNode, id uint32) {
	objNode := childByField(node, "object")
	propNode := childByField(node, "property")
	if objNode == nil || propNode == nil {
		return
	}

	var baseSymID uint32
	if objNode.Kind() == "Identifier" {
		if decl, ok := fw.scope.Resolve(objNode.Text(), objNode); ok {
			baseSymID = SymID(decl.FilePath, decl.Name, 0, decl.StartByte)
		}
	}

	fw.emit("FieldRead", id, baseSymID, propNode.Text())
}

func (fw *FactWalker) emitFieldWrite(memberNode ASTNode, assignID uint32, rhsID uint32) {
	objNode := childByField(memberNode, "object")
	propNode := childByField(memberNode, "property")
	if objNode == nil || propNode == nil {
		return
	}

	var baseSymID uint32
	if objNode.Kind() == "Identifier" {
		if decl, ok := fw.scope.Resolve(objNode.Text(), objNode); ok {
			baseSymID = SymID(decl.FilePath, decl.Name, 0, decl.StartByte)
		}
	}

	fw.emit("FieldWrite", assignID, baseSymID, propNode.Text(), rhsID)
}

// ---- Await ----

func (fw *FactWalker) emitAwait(node ASTNode, id uint32) {
	var innerID uint32
	count := node.ChildCount()
	for i := 0; i < count; i++ {
		child := node.Child(i)
		if child == nil || child.Text() == "await" {
			continue
		}
		innerID = fw.nid(child)
		break
	}
	fw.emit("Await", id, innerID)
}

// ---- Cast ----

func (fw *FactWalker) emitCast(node ASTNode, id uint32) {
	innerNode := childByField(node, "expression")
	if innerNode == nil {
		// Scan for first non-punctuation child
		count := node.ChildCount()
		skip := map[string]bool{"!": true, "as": true, "satisfies": true, "<": true, ">": true}
		for i := 0; i < count; i++ {
			child := node.Child(i)
			if child == nil || skip[child.Text()] {
				continue
			}
			innerNode = child
			break
		}
	}
	var innerID uint32
	if innerNode != nil {
		innerID = fw.nid(innerNode)
	}
	fw.emit("Cast", id, innerID)
}

// ---- Destructuring ----

func (fw *FactWalker) emitDestructureObject(node ASTNode, id uint32) {
	count := node.ChildCount()
	idx := int32(0)
	for i := 0; i < count; i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		switch child.Kind() {
		case ",", "{", "}":
			continue
		case "ShorthandPropertyIdentifierPattern", "ShorthandPropertyIdentifier":
			name := child.Text()
			symID := SymID(fw.filePath, name, child.StartLine(), child.StartCol())
			fw.emit("DestructureField", id, name, name, symID, idx)
			idx++
		case "Pair", "PairPattern":
			keyNode := childByField(child, "key")
			valNode := childByField(child, "value")
			sourceField := ""
			if keyNode != nil {
				sourceField = keyNode.Text()
			}
			bindName := ""
			var bindSymID uint32
			if valNode != nil {
				bindName = valNode.Text()
				bindSymID = SymID(fw.filePath, bindName, valNode.StartLine(), valNode.StartCol())
			}
			fw.emit("DestructureField", id, sourceField, bindName, bindSymID, idx)
			idx++
		case "RestPattern":
			inner := childByKind(child, "Identifier")
			if inner != nil {
				name := inner.Text()
				symID := SymID(fw.filePath, name, inner.StartLine(), inner.StartCol())
				fw.emit("DestructureRest", id, symID)
			}
		case "AssignmentPattern":
			left := childByField(child, "left")
			if left != nil {
				name := left.Text()
				symID := SymID(fw.filePath, name, left.StartLine(), left.StartCol())
				fw.emit("DestructureField", id, name, name, symID, idx)
				idx++
			}
		}
	}
}

func (fw *FactWalker) emitDestructureArray(node ASTNode, id uint32) {
	count := node.ChildCount()
	idx := int32(0)
	for i := 0; i < count; i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		switch child.Kind() {
		case ",", "[", "]":
			continue
		case "RestPattern":
			inner := childByKind(child, "Identifier")
			if inner != nil {
				name := inner.Text()
				symID := SymID(fw.filePath, name, inner.StartLine(), inner.StartCol())
				fw.emit("DestructureRest", id, symID)
			}
		default:
			name := child.Text()
			var symID uint32
			if name != "" {
				symID = SymID(fw.filePath, name, child.StartLine(), child.StartCol())
			}
			fw.emit("ArrayDestructure", id, idx, symID)
			idx++
		}
	}
}

// ---- Imports ----

func (fw *FactWalker) emitImportBinding(node ASTNode) {
	sourceNode := childByField(node, "source")
	moduleSpec := ""
	if sourceNode != nil {
		moduleSpec = strings.Trim(sourceNode.Text(), `'"`)
	}

	count := node.ChildCount()
	for i := 0; i < count; i++ {
		child := node.Child(i)
		if child != nil && child.Kind() == "ImportClause" {
			fw.emitImportClause(child, moduleSpec)
		}
	}
}

func (fw *FactWalker) emitImportClause(clause ASTNode, moduleSpec string) {
	count := clause.ChildCount()
	for i := 0; i < count; i++ {
		child := clause.Child(i)
		if child == nil {
			continue
		}
		switch child.Kind() {
		case "Identifier":
			// default import
			name := child.Text()
			symID := SymID(fw.filePath, name, child.StartLine(), child.StartCol())
			fw.emit("ImportBinding", symID, moduleSpec, "default")
		case "NamedImports":
			fw.emitNamedImports(child, moduleSpec)
		case "NamespaceImport":
			if ident := childByKind(child, "Identifier"); ident != nil {
				name := ident.Text()
				symID := SymID(fw.filePath, name, ident.StartLine(), ident.StartCol())
				fw.emit("ImportBinding", symID, moduleSpec, "*")
			}
		}
	}
}

func (fw *FactWalker) emitNamedImports(namedImports ASTNode, moduleSpec string) {
	count := namedImports.ChildCount()
	for i := 0; i < count; i++ {
		child := namedImports.Child(i)
		if child == nil || child.Kind() != "ImportSpecifier" {
			continue
		}
		nameNode := childByField(child, "name")
		aliasNode := childByField(child, "alias")

		importedName := ""
		if nameNode != nil {
			importedName = nameNode.Text()
		}
		localNode := aliasNode
		if localNode == nil {
			localNode = nameNode
		}
		if localNode == nil {
			continue
		}
		localName := localNode.Text()
		symID := SymID(fw.filePath, localName, localNode.StartLine(), localNode.StartCol())
		fw.emit("ImportBinding", symID, moduleSpec, importedName)
	}
}

// ---- Exports ----

func (fw *FactWalker) emitExportBinding(node ASTNode) {
	count := node.ChildCount()
	for i := 0; i < count; i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		switch child.Kind() {
		case "ExportClause":
			fw.emitExportClause(child)
		case "LexicalDeclaration", "VariableDeclaration":
			fw.emitExportFromDecl(child)
		case "FunctionDeclaration":
			if nameNode := childByField(child, "name"); nameNode != nil {
				name := nameNode.Text()
				symID := SymID(fw.filePath, name, nameNode.StartLine(), nameNode.StartCol())
				fw.emit("ExportBinding", name, symID, fw.fileID)
			}
		case "ClassDeclaration":
			if nameNode := childByField(child, "name"); nameNode != nil {
				name := nameNode.Text()
				symID := SymID(fw.filePath, name, nameNode.StartLine(), nameNode.StartCol())
				fw.emit("ExportBinding", name, symID, fw.fileID)
			}
		case "Identifier":
			// export default ident
			name := child.Text()
			symID := SymID(fw.filePath, name, child.StartLine(), child.StartCol())
			fw.emit("ExportBinding", "default", symID, fw.fileID)
		}
	}
}

func (fw *FactWalker) emitExportClause(clause ASTNode) {
	count := clause.ChildCount()
	for i := 0; i < count; i++ {
		child := clause.Child(i)
		if child == nil || child.Kind() != "ExportSpecifier" {
			continue
		}
		nameNode := childByField(child, "name")
		aliasNode := childByField(child, "alias")
		if nameNode == nil {
			continue
		}
		localName := nameNode.Text()
		exportedName := localName
		if aliasNode != nil {
			exportedName = aliasNode.Text()
		}
		symID := SymID(fw.filePath, localName, nameNode.StartLine(), nameNode.StartCol())
		fw.emit("ExportBinding", exportedName, symID, fw.fileID)
	}
}

func (fw *FactWalker) emitExportFromDecl(decl ASTNode) {
	count := decl.ChildCount()
	for i := 0; i < count; i++ {
		child := decl.Child(i)
		if child == nil || child.Kind() != "VariableDeclarator" {
			continue
		}
		if nameNode := childByField(child, "name"); nameNode != nil {
			name := nameNode.Text()
			symID := SymID(fw.filePath, name, nameNode.StartLine(), nameNode.StartCol())
			fw.emit("ExportBinding", name, symID, fw.fileID)
		}
	}
}

// ---- JSX ----

func (fw *FactWalker) emitJsxElement(node ASTNode, id uint32) {
	var tagNode ASTNode
	var opening ASTNode
	if node.Kind() == "JsxSelfClosingElement" {
		tagNode = childByField(node, "name")
		opening = node
	} else {
		// Opening element is in field "open_tag" OR by kind
		opening = childByField(node, "open_tag")
		if opening == nil {
			opening = childByKind(node, "JsxOpeningElement")
		}
		if opening != nil {
			tagNode = childByField(opening, "name")
			if tagNode == nil {
				// Fallback: first Identifier child of opening element
				tagNode = childByKind(opening, "Identifier")
			}
		}
	}

	var tagID uint32
	if tagNode != nil {
		tagID = fw.nid(tagNode)
	}

	var tagSymID uint32
	if tagNode != nil && tagNode.Kind() == "Identifier" {
		if decl, ok := fw.scope.Resolve(tagNode.Text(), tagNode); ok {
			tagSymID = SymID(decl.FilePath, decl.Name, 0, decl.StartByte)
		}
	}

	fw.emit("JsxElement", id, tagID, tagSymID)

	// Push element ID so JsxAttribute nodes (visited later as children of
	// JsxOpeningElement or JsxSelfClosingElement) can reference this element.
	fw.jsxElementStack = append(fw.jsxElementStack, id)
	_ = opening
}

func (fw *FactWalker) emitJsxAttr(node ASTNode, elementID uint32) {
	// JsxAttribute structure: PropertyIdentifier = value
	// The name is in field "name" OR is the first child (PropertyIdentifier / Identifier).
	// The value is in field "value" OR is the third child (after name and "=").
	attrName := ""
	var valueID uint32

	// Try field-based access first
	nameNode := childByField(node, "name")
	valueNode := childByField(node, "value")

	if nameNode != nil {
		attrName = nameNode.Text()
	} else {
		// Fallback: first child is the attribute name token
		if node.ChildCount() > 0 {
			first := node.Child(0)
			if first != nil {
				attrName = first.Text()
			}
		}
	}

	if valueNode != nil {
		valueID = fw.nid(valueNode)
	} else {
		// Fallback: find first non-name, non-"=" child
		count := node.ChildCount()
		nameFound := false
		for i := 0; i < count; i++ {
			child := node.Child(i)
			if child == nil {
				continue
			}
			t := child.Text()
			if !nameFound {
				nameFound = true
				continue // skip name
			}
			if t == "=" {
				continue
			}
			valueID = fw.nid(child)
			break
		}
	}

	fw.emit("JsxAttribute", elementID, attrName, valueID)
}

// ---- utilities ----

func boolInt(b bool) int32 {
	if b {
		return 1
	}
	return 0
}
