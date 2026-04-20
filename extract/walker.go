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

	// constStringDecls maps the binding name of a `const X = "literal"` to the
	// underlying string literal (quotes stripped) for the current file. Populated
	// during VariableDeclarator emission. Used by emitObjectLiteral to resolve
	// computed-key identifier references at extraction time:
	//
	//     const KEY = "setX";
	//     const obj = { [KEY]: setSomething };  // emit ObjectLiteralField(obj, "setX", ...)
	//
	// Same-file, same-pass; const decls precede their use under normal TS scope
	// rules so a top-down walk gets the binding before it's referenced. This is
	// reset in EnterFile.
	constStringDecls map[string]string

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
	fw.constStringDecls = make(map[string]string)

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
	if IsFunctionKind(kind) {
		fw.emitFunction(node, id, kind)
	}
	// Value-flow Phase A: ExprValueSource(expr, expr) for value-producing literals.
	// Identity row — the planner uses it as a grounded base predicate for
	// non-recursive mayResolveTo. See docs/design/valueflow-phase-a-plan.md §1.2.
	if IsValueSourceKind(kind) {
		fw.emit("ExprValueSource", id, id)
	} else if kind == "TemplateString" && !templateHasSubstitution(node) {
		// Template literal with no ${...} substitutions — value is its own subtree.
		fw.emit("ExprValueSource", id, id)
	}
	switch kind {
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
	case "Object":
		fw.emitObjectLiteral(node, id)
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
	case "TemplateString":
		fw.emitTemplateLiteral(node, id, 0)
	case "TaggedTemplateExpression":
		fw.emitTaggedTemplate(node, id)
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
// Delegates to the package-level ChildByField helper in tree.go.
func childByField(n ASTNode, field string) ASTNode {
	return ChildByField(n, field)
}

// firstNonPunctChild returns the first direct child of n that is not a
// punctuation token (`[`, `]`, `(`, `)`, `{`, `}`, `,`, `:`, `...`). Used by
// emitObjectLiteral to step into ComputedPropertyName / SpreadElement nodes
// without depending on field-name access (tree-sitter typescript grammar
// doesn't always tag the inner expression with a stable field name).
func firstNonPunctChild(n ASTNode) ASTNode {
	count := n.ChildCount()
	for i := 0; i < count; i++ {
		child := n.Child(i)
		if child == nil {
			continue
		}
		switch child.Kind() {
		case "[", "]", "(", ")", "{", "}", ",", ":", "...":
			continue
		}
		return child
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

		// Detect destructured-pattern parameters. The walker still emits a
		// single Parameter row (for arity bookkeeping), but flags the slot so
		// downstream rules (ParamBinding) can exclude it — the synthesised
		// "name" for a destructured pattern is the literal pattern source
		// text and its symbol id is therefore not a real bound name.
		isDestructured := isDestructuredParamKind(param, pKind)

		var symID uint32
		if paramName != "" {
			symID = SymID(fw.filePath, paramName, param.StartLine(), param.StartCol())
		}

		isFnType := strings.Contains(typeText, "=>")

		fw.emit("Parameter", fnID, idx, paramName, paramID, symID, typeText)
		// Emit Symbol only for real bound identifiers — destructured patterns
		// have no single bound name at this slot.
		if paramName != "" && !isDestructured {
			fw.emit("Symbol", symID, paramName, paramID, fw.fileID)
		}
		if isRest {
			fw.emit("ParameterRest", fnID, idx)
		}
		if isOptional {
			fw.emit("ParameterOptional", fnID, idx)
		}
		if isDestructured {
			fw.emit("ParameterDestructured", fnID, idx)
			// Value-flow Phase C PR8 (#202 Gap A): link the Parameter row's
			// paramNode to the ObjectPattern/ArrayPattern that carries the
			// DestructureField rows so `lfsJsxPropBind` can compose the
			// JSX-prop → destructured-param-use edge. Identity case: when the
			// param node IS the pattern (arrow `({v}) =>` with no
			// RequiredParameter wrapper), emit (paramID, paramID).
			patNode := destructurePatternNode(param, pKind)
			var patID uint32
			if patNode != nil {
				patID = fw.nid(patNode)
			} else {
				patID = paramID
			}
			fw.emit("ParamDestructurePattern", paramID, patID)
		}
		if isFnType {
			fw.emit("ParamIsFunctionType", fnID, idx)
		}
		idx++
	}
}

// destructurePatternNode returns the ObjectPattern/ArrayPattern node
// underlying a destructured parameter slot, peeling the
// RequiredParameter / OptionalParameter / AssignmentPattern wrappers. Returns
// nil when the param slot IS the pattern (bare arrow `({v}) =>`) — the caller
// then emits the identity row.
func destructurePatternNode(param ASTNode, pKind string) ASTNode {
	switch pKind {
	case "ObjectPattern", "ArrayPattern":
		// Bare pattern at the parameter slot (arrow function without a
		// RequiredParameter wrapper). Caller should emit identity.
		return nil
	case "RequiredParameter", "OptionalParameter":
		patNode := childByField(param, "pattern")
		if patNode == nil {
			patNode = childByField(param, "name")
		}
		if patNode != nil {
			pk := patNode.Kind()
			if pk == "ObjectPattern" || pk == "ArrayPattern" {
				return patNode
			}
		}
	case "AssignmentPattern":
		if left := childByField(param, "left"); left != nil {
			lk := left.Kind()
			if lk == "ObjectPattern" || lk == "ArrayPattern" {
				return left
			}
		}
	}
	return nil
}

// isDestructuredParamKind reports whether the parameter slot's pattern is
// an ObjectPattern or ArrayPattern (including when wrapped inside a
// RequiredParameter / OptionalParameter / AssignmentPattern).
func isDestructuredParamKind(param ASTNode, pKind string) bool {
	switch pKind {
	case "ObjectPattern", "ArrayPattern":
		return true
	case "RequiredParameter", "OptionalParameter":
		patNode := childByField(param, "pattern")
		if patNode == nil {
			patNode = childByField(param, "name")
		}
		if patNode != nil {
			pk := patNode.Kind()
			return pk == "ObjectPattern" || pk == "ArrayPattern"
		}
	case "AssignmentPattern":
		if left := childByField(param, "left"); left != nil {
			lk := left.Kind()
			return lk == "ObjectPattern" || lk == "ArrayPattern"
		}
	}
	return false
}

// extractParamInfo derives the bound-name, type text, and modifier flags for
// a parameter slot.
//
// Deliberately unmodelled in Phase A (callers must not assume these are
// captured anywhere — relevant relations stay empty for these shapes):
//   - Getter / setter accessor parameters (`get x()` / `set x(v)`): emitted
//     as Parameter rows when present, but accessor-specific binding semantics
//     are not modelled — they go through Method/Get/SetAccessor relations.
//   - The implicit `arguments` object inside non-arrow functions: not
//     emitted as a parameter symbol; consumers must inspect ExprMayRef.
//   - Decorator parameters (TypeScript decorator factories): treated as
//     ordinary call args; the decorator binding chain is not modelled.
//   - Destructured parameters (ObjectPattern / ArrayPattern): emitted as a
//     single Parameter row with the pattern source text as the synthesised
//     name and flagged via ParameterDestructured (above). Per-bound-name
//     expansion is deferred to Phase C.
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
			symID := SymID(decl.FilePath, decl.Name, decl.StartLine, decl.StartCol)
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

	// Round-3: capture `const X = "literal"` mappings so emitObjectLiteral can
	// resolve computed-key identifier references at extraction time.
	if fw.currentDeclConst == 1 && nameNode != nil && initNode != nil &&
		initNode.Kind() == "String" {
		if s, ok := stripStringLiteralQuotes(initNode.Text()); ok {
			fw.constStringDecls[nameNode.Text()] = s
		}
	}
}

// stripStringLiteralQuotes removes a single matched pair of leading/trailing
// quote characters (`"`, `'`, or backtick) from the textual form of a tree-sitter
// String node. Returns false if the input is not a valid quote-delimited literal.
// Escape sequences inside the string are NOT processed — for the round-3 use
// case (matching computed property names against object literal field names)
// the literal text suffices because the source-level destructure binding would
// share the same byte sequence.
func stripStringLiteralQuotes(s string) (string, bool) {
	if len(s) < 2 {
		return "", false
	}
	first := s[0]
	last := s[len(s)-1]
	if first != last {
		return "", false
	}
	if first != '"' && first != '\'' && first != '`' {
		return "", false
	}
	return s[1 : len(s)-1], true
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
			lhsSymID = SymID(decl.FilePath, decl.Name, decl.StartLine, decl.StartCol)
		}
	}

	fw.emit("Assign", leftID, rightID, lhsSymID)

	// Value-flow Phase A: AssignExpr(lhsSym, rhsExpr) — symmetric-to-VarDecl
	// projection. Only emit when we have a resolved lhsSym; assigns to member
	// expressions or unresolved identifiers go through other relations.
	if lhsSymID != 0 && rightID != 0 {
		fw.emit("AssignExpr", lhsSymID, rightID)
	}

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
		symID := SymID(decl.FilePath, decl.Name, decl.StartLine, decl.StartCol)
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
			baseSymID = SymID(decl.FilePath, decl.Name, decl.StartLine, decl.StartCol)
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
			baseSymID = SymID(decl.FilePath, decl.Name, decl.StartLine, decl.StartCol)
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

// ---- Object Literals ----

// emitObjectLiteral emits an ObjectLiteralField row for each named field of an
// object expression `{ a, b: expr, c }`. Spread elements (`...rest`) and
// computed-key properties are skipped silently — v1 limitations documented in
// the schema. The valueExpr column points at the value-position node:
//   - shorthand `{ foo }`        → valueExpr is the Identifier `foo`
//   - pair       `{ foo: expr }` → valueExpr is `expr`
//
// This is consumed by the React context-alias tracking in
// `bridge/tsq_react.qll` to look up which symbol a Provider value object
// exposes under a given field name. See round-2 of the setState-alias work.
func (fw *FactWalker) emitObjectLiteral(node ASTNode, id uint32) {
	count := node.ChildCount()
	for i := 0; i < count; i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		switch child.Kind() {
		case ",", "{", "}":
			continue
		case "ShorthandPropertyIdentifier":
			// `{ foo }` — value is the identifier itself. The visitor
			// dispatches `case "Identifier"` to emit ExprMayRef, but
			// tree-sitter classifies the shorthand-property identifier as
			// `ShorthandPropertyIdentifier` (NOT `Identifier`), so the
			// visitor never emits the expected ExprMayRef row. Emit it
			// explicitly here so downstream context-alias predicates can
			// resolve the shorthand binding back to its declaration.
			name := child.Text()
			valID := fw.nid(child)
			fw.emit("ObjectLiteralField", id, name, valID)
			if decl, ok := fw.scope.Resolve(name, child); ok {
				symID := SymID(decl.FilePath, decl.Name, decl.StartLine, decl.StartCol)
				fw.emit("ExprMayRef", valID, symID)
			}
		case "Pair":
			keyNode := childByField(child, "key")
			valNode := childByField(child, "value")
			if keyNode == nil || valNode == nil {
				continue
			}
			switch keyNode.Kind() {
			case "PropertyIdentifier", "Identifier":
				name := keyNode.Text()
				valID := fw.nid(valNode)
				fw.emit("ObjectLiteralField", id, name, valID)
			case "String":
				// String-literal key, e.g. `{ "setX": foo }`. Treat as a
				// stable named field equal to the string's content.
				if name, ok := stripStringLiteralQuotes(keyNode.Text()); ok {
					valID := fw.nid(valNode)
					fw.emit("ObjectLiteralField", id, name, valID)
				}
			case "ComputedPropertyName":
				// Round-3: `{ [KEY]: foo }` — resolve at extraction time when
				// the key is a string literal, OR an Identifier that resolves
				// to a `const KEY = "..."` binding earlier in the same file.
				// Anything else (computed expression, non-const var, cross-file
				// import) is silently skipped — over-approximating to all field
				// names would generate noisy false positives.
				inner := firstNonPunctChild(keyNode)
				if inner == nil {
					continue
				}
				var name string
				var ok bool
				switch inner.Kind() {
				case "String":
					name, ok = stripStringLiteralQuotes(inner.Text())
				case "Identifier":
					name, ok = fw.constStringDecls[inner.Text()]
				}
				if ok {
					valID := fw.nid(valNode)
					fw.emit("ObjectLiteralField", id, name, valID)
				}
			default:
				// numeric keys, template-literal keys, etc. — skip.
				continue
			}
		case "MethodDefinition":
			// `{ foo() { ... } }` — method shorthand. Skip for v1; not
			// load-bearing for context-alias tracking.
			continue
		case "SpreadElement":
			// Round-3: `{ ...base }` — emit ObjectLiteralSpread so the bridge
			// can union spread-contributed fields into the parent object's
			// effective field set. The spread's value expression is the inner
			// expression after `...`.
			inner := firstNonPunctChild(child)
			if inner == nil {
				continue
			}
			innerID := fw.nid(inner)
			fw.emit("ObjectLiteralSpread", id, innerID)
		}
	}
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
			tagSymID = SymID(decl.FilePath, decl.Name, decl.StartLine, decl.StartCol)
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
			valueNode = child
			break
		}
	}

	fw.emit("JsxAttribute", elementID, attrName, valueID)

	// Emit JsxExpressionInner(wrapperNode, innerNode) so the value-flow
	// layer can bridge across the `{…}` punctuation wrapper without the
	// whole bridge stack having to relearn the wrapper shape. Consumed
	// by `lfsJsxPropBind` (see extract/rules/localflowstep.go). See #202
	// Gap A for why this runs off an explicit helper relation rather than
	// hoisting the unwrap into `JsxAttribute.valueExpr` directly — the
	// existing `tsq_react.qll` Provider-value path already relies on
	// `valueExpr` pointing at the JsxExpression wrapper and uses
	// `Contains` to descend.
	if valueNode != nil && valueNode.Kind() == "JsxExpression" {
		if inner := firstNonPunctChild(valueNode); inner != nil {
			fw.emit("JsxExpressionInner", fw.nid(valueNode), fw.nid(inner))
		}
	}
}

// ---- Template Literals ----

// templateHasSubstitution reports whether a TemplateString node contains any
// `${...}` substitution children. Used by ExprValueSource emission to decide
// whether a template literal is itself a value-source (no substitutions ⇒
// runtime value is determined by the literal subtree alone).
func templateHasSubstitution(node ASTNode) bool {
	count := node.ChildCount()
	for i := 0; i < count; i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		if child.Kind() == "TemplateSubstitution" {
			return true
		}
	}
	return false
}

func (fw *FactWalker) emitTemplateLiteral(node ASTNode, id uint32, tagID uint32) {
	fw.emit("TemplateLiteral", id, tagID)

	// Walk children: TemplateSubstitution contains expressions,
	// everything else is a string fragment.
	idx := int32(0)
	count := node.ChildCount()
	for i := 0; i < count; i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		k := child.Kind()
		switch k {
		case "`", "${", "}":
			continue
		case "TemplateSubstitution":
			// The expression is inside the substitution
			cc := child.ChildCount()
			for j := 0; j < cc; j++ {
				gc := child.Child(j)
				if gc == nil {
					continue
				}
				gk := gc.Kind()
				if gk == "${" || gk == "}" {
					continue
				}
				fw.emit("TemplateExpression", id, idx, fw.nid(gc))
				idx++
				break
			}
		default:
			// String fragment (TemplateChars or similar)
			fw.emit("TemplateElement", id, idx, child.Text())
			idx++
		}
	}
}

func (fw *FactWalker) emitTaggedTemplate(node ASTNode, id uint32) {
	// TaggedTemplateExpression: tag `template`
	var tagID uint32
	tagNode := childByField(node, "function")
	if tagNode == nil && node.ChildCount() > 0 {
		tagNode = node.Child(0)
	}
	if tagNode != nil {
		tagID = fw.nid(tagNode)
	}

	// Find the template string child
	templateNode := childByField(node, "arguments")
	if templateNode == nil {
		// Fallback: find TemplateString child
		templateNode = childByKind(node, "TemplateString")
	}
	if templateNode != nil {
		fw.emitTemplateLiteral(templateNode, fw.nid(templateNode), tagID)
	} else {
		// Emit the tagged template itself as a TemplateLiteral with the tag
		fw.emit("TemplateLiteral", id, tagID)
	}
}

// ---- utilities ----

func boolInt(b bool) int32 {
	if b {
		return 1
	}
	return 0
}
