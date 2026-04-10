package extract

// scope.go implements in-file scope analysis on top of the ASTNode interface.
// It handles:
//   - var declarations (function-scoped)
//   - let/const declarations (block-scoped)
//   - function declarations (hoisted to enclosing function/file scope)
//   - function parameters
//   - import bindings
//
// Temporal dead zone (TDZ) for let/const: a reference that appears at a byte
// offset before the declaration is not resolved.

// Declaration records a single binding in a scope.
type Declaration struct {
	Name      string
	FilePath  string
	StartByte int // byte offset of the identifier token
	StartLine int // 1-based line of the identifier token
	StartCol  int // 0-based byte column of the identifier token
	// isConst and isLet determine TDZ applicability
	isTDZ bool // true for let/const — subject to temporal dead zone
}

// Scope is a node in the scope tree. Each scope knows its parent.
type Scope struct {
	parent *Scope
	decls  map[string]*Declaration
	// children are not stored — scope analysis is query-driven
}

func newScope(parent *Scope) *Scope {
	return &Scope{parent: parent, decls: make(map[string]*Declaration)}
}

// declare adds a binding to this scope. If a binding with the same name
// already exists it is overwritten (mirrors JS var hoisting semantics where
// redeclaration is allowed).
func (s *Scope) declare(name string, d *Declaration) {
	s.decls[name] = d
}

// Resolve looks up name starting from this scope, walking up the chain.
// atByte is the byte offset of the reference — used for TDZ checks.
// Returns nil, false if not found.
func (s *Scope) Resolve(name string, atByte int) (*Declaration, bool) {
	for cur := s; cur != nil; cur = cur.parent {
		if d, ok := cur.decls[name]; ok {
			// Temporal dead zone: let/const not yet initialised
			if d.isTDZ && atByte < d.StartByte {
				return nil, false
			}
			return d, true
		}
	}
	return nil, false
}

// ScopeAnalyzer builds a scope tree from an ASTNode CST and answers
// in-file resolution queries.
type ScopeAnalyzer struct {
	filePath string
	// nodeScope maps a node's start byte to the scope that contains it.
	// We store the innermost scope at each function/block entry.
	nodeScope map[int]*Scope
	root      *Scope
}

// NewScopeAnalyzer creates a ScopeAnalyzer for filePath.
func NewScopeAnalyzer(filePath string) *ScopeAnalyzer {
	return &ScopeAnalyzer{
		filePath:  filePath,
		nodeScope: make(map[int]*Scope),
	}
}

// Build analyses the AST rooted at root and builds the full scope tree.
// It returns the file-level (root) scope.
func (sa *ScopeAnalyzer) Build(root ASTNode) *Scope {
	fileScope := newScope(nil)
	sa.root = fileScope
	sa.buildScope(root, fileScope, fileScope)
	return fileScope
}

// buildScope recursively analyses node n within the given block scope and
// function scope. blockScope is the innermost { } scope; fnScope is the
// innermost function scope (for var hoisting).
func (sa *ScopeAnalyzer) buildScope(n ASTNode, blockScope, fnScope *Scope) {
	if n == nil {
		return
	}

	kind := n.Kind()
	startByte := sa.nodeStartByte(n)

	// Record the scope for this node so Resolve can find it later.
	sa.nodeScope[startByte] = blockScope

	switch kind {
	case "FunctionDeclaration", "FunctionExpression", "ArrowFunction", "MethodDefinition",
		"GeneratorFunction", "GeneratorFunctionDeclaration": // kept in sync via FunctionKinds
		// Functions create a new function scope (and also a block scope).
		newFnScope := newScope(blockScope)
		// Hoist the function name into the *enclosing* function scope (not block scope)
		// for function declarations.
		if kind == "FunctionDeclaration" || kind == "GeneratorFunctionDeclaration" {
			nameNode := sa.childByField(n, "name")
			if nameNode != nil && nameNode.Text() != "" {
				fnScope.declare(nameNode.Text(), sa.makeDecl(nameNode, false))
			}
		}
		// Process parameters into the new function scope
		sa.processParams(n, newFnScope)
		// Process body
		body := sa.childByField(n, "body")
		if body != nil {
			sa.buildScope(body, newFnScope, newFnScope)
		}
		// Don't fall through to child iteration — we've handled everything

	case "LexicalDeclaration", "VariableDeclaration":
		// Determine if this is const/let (TDZ) or var (hoisted)
		isVar := sa.isVarDeclaration(n)
		isTDZ := !isVar
		targetScope := blockScope
		if isVar {
			targetScope = fnScope
		}
		// Process each declarator
		sa.processVariableDeclarators(n, targetScope, isTDZ)
		// Still recurse into children for nested expressions
		sa.iterChildren(n, blockScope, fnScope)

	case "Block":
		// A new block scope
		newBlock := newScope(blockScope)
		sa.iterChildren(n, newBlock, fnScope)

	case "ImportDeclaration":
		// import bindings go into the file/function scope (module level)
		sa.processImportDeclaration(n, blockScope)
		// Don't recurse further — we've extracted what we need

	case "CatchClause":
		// catch (e) { } introduces e into a new scope
		newBlock := newScope(blockScope)
		param := sa.childByField(n, "parameter")
		if param != nil && sa.nodeText(param) != "" {
			newBlock.declare(sa.nodeText(param), sa.makeDecl(param, false))
		}
		body := sa.childByField(n, "body")
		if body != nil {
			sa.buildScope(body, newBlock, fnScope)
		}

	default:
		sa.iterChildren(n, blockScope, fnScope)
	}
}

// iterChildren calls buildScope on all children with the same scopes.
func (sa *ScopeAnalyzer) iterChildren(n ASTNode, blockScope, fnScope *Scope) {
	count := n.ChildCount()
	for i := 0; i < count; i++ {
		child := n.Child(i)
		if child != nil {
			sa.buildScope(child, blockScope, fnScope)
		}
	}
}

// isVarDeclaration returns true if the node is a var declaration (not let/const).
func (sa *ScopeAnalyzer) isVarDeclaration(n ASTNode) bool {
	count := n.ChildCount()
	for i := 0; i < count; i++ {
		child := n.Child(i)
		if child == nil {
			continue
		}
		text := child.Text()
		if text == "var" {
			return true
		}
		if text == "let" || text == "const" {
			return false
		}
	}
	return false
}

// processVariableDeclarators extracts variable_declarator nodes and declares them.
func (sa *ScopeAnalyzer) processVariableDeclarators(n ASTNode, scope *Scope, isTDZ bool) {
	count := n.ChildCount()
	for i := 0; i < count; i++ {
		child := n.Child(i)
		if child == nil {
			continue
		}
		if child.Kind() == "VariableDeclarator" {
			nameNode := sa.childByField(child, "name")
			if nameNode != nil {
				sa.declarePattern(nameNode, scope, isTDZ)
			}
		}
	}
}

// declarePattern declares bindings from a potentially complex pattern node
// (identifier, object_pattern, array_pattern).
func (sa *ScopeAnalyzer) declarePattern(n ASTNode, scope *Scope, isTDZ bool) {
	if n == nil {
		return
	}
	kind := n.Kind()
	switch kind {
	case "Identifier":
		if n.Text() != "" {
			scope.declare(n.Text(), sa.makeDecl(n, isTDZ))
		}
	case "ObjectPattern":
		// { a, b: c, ...rest }
		count := n.ChildCount()
		for i := 0; i < count; i++ {
			child := n.Child(i)
			if child == nil {
				continue
			}
			childKind := child.Kind()
			switch childKind {
			case "ShorthandPropertyIdentifierPattern", "ShorthandPropertyIdentifier":
				if child.Text() != "" {
					scope.declare(child.Text(), sa.makeDecl(child, isTDZ))
				}
			case "Pair", "ObjectAssignmentPattern":
				// { key: value } or { key: value = default }
				val := sa.childByField(child, "value")
				if val != nil {
					sa.declarePattern(val, scope, isTDZ)
				}
			case "RestPattern":
				// ...rest
				inner := sa.firstChildByKind(child, "Identifier")
				if inner != nil && inner.Text() != "" {
					scope.declare(inner.Text(), sa.makeDecl(inner, isTDZ))
				}
			case "AssignmentPattern":
				left := sa.childByField(child, "left")
				if left != nil {
					sa.declarePattern(left, scope, isTDZ)
				}
			}
		}
	case "ArrayPattern":
		// [a, b, ...rest]
		count := n.ChildCount()
		for i := 0; i < count; i++ {
			child := n.Child(i)
			if child == nil {
				continue
			}
			sa.declarePattern(child, scope, isTDZ)
		}
	case "AssignmentPattern":
		left := sa.childByField(n, "left")
		if left != nil {
			sa.declarePattern(left, scope, isTDZ)
		}
	case "RestPattern":
		inner := sa.firstChildByKind(n, "Identifier")
		if inner != nil && inner.Text() != "" {
			scope.declare(inner.Text(), sa.makeDecl(inner, isTDZ))
		}
	}
}

// processParams declares function parameters into fnScope.
func (sa *ScopeAnalyzer) processParams(fn ASTNode, fnScope *Scope) {
	count := fn.ChildCount()
	for i := 0; i < count; i++ {
		child := fn.Child(i)
		if child == nil {
			continue
		}
		kind := child.Kind()
		// Parameters are direct children of function nodes with field "parameters"
		if child.FieldName() == "parameters" || kind == "FormalParameters" {
			sa.extractParams(child, fnScope)
		}
	}
}

// extractParams walks a formal_parameters node and declares each param.
func (sa *ScopeAnalyzer) extractParams(params ASTNode, fnScope *Scope) {
	count := params.ChildCount()
	for i := 0; i < count; i++ {
		param := params.Child(i)
		if param == nil {
			continue
		}
		kind := param.Kind()
		switch kind {
		case "Identifier":
			if param.Text() != "" {
				fnScope.declare(param.Text(), sa.makeDecl(param, false))
			}
		case "AssignmentPattern":
			left := sa.childByField(param, "left")
			if left != nil {
				sa.declarePattern(left, fnScope, false)
			}
		case "RestPattern":
			inner := sa.firstChildByKind(param, "Identifier")
			if inner != nil && inner.Text() != "" {
				fnScope.declare(inner.Text(), sa.makeDecl(inner, false))
			}
		case "ObjectPattern", "ArrayPattern":
			sa.declarePattern(param, fnScope, false)
		}
	}
}

// processImportDeclaration extracts bindings from an import declaration.
func (sa *ScopeAnalyzer) processImportDeclaration(n ASTNode, scope *Scope) {
	count := n.ChildCount()
	for i := 0; i < count; i++ {
		child := n.Child(i)
		if child == nil {
			continue
		}
		kind := child.Kind()
		switch kind {
		case "ImportClause":
			sa.processImportClause(child, scope)
		}
	}
}

// processImportClause handles the clause part of an import statement.
func (sa *ScopeAnalyzer) processImportClause(n ASTNode, scope *Scope) {
	count := n.ChildCount()
	for i := 0; i < count; i++ {
		child := n.Child(i)
		if child == nil {
			continue
		}
		kind := child.Kind()
		switch kind {
		case "Identifier":
			// default import: import Foo from '...'
			if child.Text() != "" {
				scope.declare(child.Text(), sa.makeDecl(child, false))
			}
		case "NamedImports":
			// import { a, b as c } from '...'
			sa.processNamedImports(child, scope)
		case "NamespaceImport":
			// import * as ns from '...'
			ident := sa.firstChildByKind(child, "Identifier")
			if ident != nil && ident.Text() != "" {
				scope.declare(ident.Text(), sa.makeDecl(ident, false))
			}
		}
	}
}

// processNamedImports handles { a, b as c } clauses.
func (sa *ScopeAnalyzer) processNamedImports(n ASTNode, scope *Scope) {
	count := n.ChildCount()
	for i := 0; i < count; i++ {
		child := n.Child(i)
		if child == nil {
			continue
		}
		if child.Kind() == "ImportSpecifier" {
			// import specifier: name or alias
			alias := sa.childByField(child, "alias")
			if alias != nil {
				if alias.Text() != "" {
					scope.declare(alias.Text(), sa.makeDecl(alias, false))
				}
			} else {
				// No alias — the local name is the imported name
				nameNode := sa.childByField(child, "name")
				if nameNode != nil && nameNode.Text() != "" {
					scope.declare(nameNode.Text(), sa.makeDecl(nameNode, false))
				}
			}
		}
	}
}

// Resolve looks up name at the given node (by its start byte offset).
// It finds the innermost scope containing atNode and walks up the chain.
func (sa *ScopeAnalyzer) Resolve(name string, atNode ASTNode) (*Declaration, bool) {
	if atNode == nil {
		return nil, false
	}
	startByte := sa.nodeStartByte(atNode)
	scope := sa.findScope(startByte)
	if scope == nil {
		scope = sa.root
	}
	return scope.Resolve(name, startByte)
}

// findScope finds the innermost scope recorded for a byte offset.
// It uses the closest recorded entry <= startByte.
func (sa *ScopeAnalyzer) findScope(startByte int) *Scope {
	// We stored scope entries at the start byte of each node that opens/is in a scope.
	// Use the closest one <= startByte.
	best := -1
	var bestScope *Scope
	for k, s := range sa.nodeScope {
		if k <= startByte && k > best {
			best = k
			bestScope = s
		}
	}
	return bestScope
}

// nodeStartByte returns the start byte of a node by summing line/col info.
// Since smacker's Node doesn't expose StartByte directly in all paths, we
// use the node's StartByte method if available via type assertion.
func (sa *ScopeAnalyzer) nodeStartByte(n ASTNode) int {
	// tsASTNode wraps sitter.Node which has StartByte()
	if ts, ok := n.(*tsASTNode); ok {
		return int(ts.n.StartByte())
	}
	// Fallback: use line/col as a proxy (not accurate for multi-byte chars,
	// but acceptable for scope key purposes in tests with ASCII source)
	return n.StartLine()*10000 + n.StartCol()
}

// makeDecl creates a Declaration for a given identifier node.
func (sa *ScopeAnalyzer) makeDecl(n ASTNode, isTDZ bool) *Declaration {
	return &Declaration{
		Name:      n.Text(),
		FilePath:  sa.filePath,
		StartByte: sa.nodeStartByte(n),
		StartLine: n.StartLine(),
		StartCol:  n.StartCol(),
		isTDZ:     isTDZ,
	}
}

// childByField returns the first child of n with the given field name.
func (sa *ScopeAnalyzer) childByField(n ASTNode, field string) ASTNode {
	count := n.ChildCount()
	for i := 0; i < count; i++ {
		child := n.Child(i)
		if child != nil && child.FieldName() == field {
			return child
		}
	}
	return nil
}

// firstChildByKind returns the first direct child with the given normalised kind.
func (sa *ScopeAnalyzer) firstChildByKind(n ASTNode, kind string) ASTNode {
	count := n.ChildCount()
	for i := 0; i < count; i++ {
		child := n.Child(i)
		if child != nil && child.Kind() == kind {
			return child
		}
	}
	return nil
}

// nodeText returns the text of a node.
func (sa *ScopeAnalyzer) nodeText(n ASTNode) string {
	if n == nil {
		return ""
	}
	return n.Text()
}
