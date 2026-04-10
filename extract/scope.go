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
	case "FunctionDeclaration", "FunctionExpression", "ArrowFunction", "MethodDefinition":
		// Functions create a new function scope (and also a block scope).
		newFnScope := newScope(blockScope)
		// Hoist the function name into the *enclosing* function scope (not block scope)
		// for function declarations.
		if kind == "FunctionDeclaration" {
			name := sa.childFieldText(n, "name")
			if name != "" {
				fnScope.declare(name, &Declaration{
					Name:      name,
					FilePath:  sa.filePath,
					StartByte: startByte,
					isTDZ:     false,
				})
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
		sa.iterChildrenWith(n, newBlock, fnScope)

	case "ImportDeclaration":
		// import bindings go into the file/function scope (module level)
		sa.processImportDeclaration(n, blockScope)
		// Don't recurse further — we've extracted what we need

	case "CatchClause":
		// catch (e) { } introduces e into a new scope
		newBlock := newScope(blockScope)
		param := sa.childByField(n, "parameter")
		if param != nil {
			name := sa.nodeText(param)
			if name != "" {
				newBlock.declare(name, &Declaration{
					Name:      name,
					FilePath:  sa.filePath,
					StartByte: sa.nodeStartByte(param),
					isTDZ:     false,
				})
			}
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

// iterChildrenWith calls buildScope on all children with the provided scopes.
func (sa *ScopeAnalyzer) iterChildrenWith(n ASTNode, blockScope, fnScope *Scope) {
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
		name := n.Text()
		if name != "" {
			scope.declare(name, &Declaration{
				Name:      name,
				FilePath:  sa.filePath,
				StartByte: sa.nodeStartByte(n),
				isTDZ:     isTDZ,
			})
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
				name := child.Text()
				if name != "" {
					scope.declare(name, &Declaration{
						Name:      name,
						FilePath:  sa.filePath,
						StartByte: sa.nodeStartByte(child),
						isTDZ:     isTDZ,
					})
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
				if inner != nil {
					name := inner.Text()
					if name != "" {
						scope.declare(name, &Declaration{
							Name:      name,
							FilePath:  sa.filePath,
							StartByte: sa.nodeStartByte(inner),
							isTDZ:     isTDZ,
						})
					}
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
		if inner != nil {
			name := inner.Text()
			if name != "" {
				scope.declare(name, &Declaration{
					Name:      name,
					FilePath:  sa.filePath,
					StartByte: sa.nodeStartByte(inner),
					isTDZ:     isTDZ,
				})
			}
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
			name := param.Text()
			if name != "" {
				fnScope.declare(name, &Declaration{
					Name:      name,
					FilePath:  sa.filePath,
					StartByte: sa.nodeStartByte(param),
					isTDZ:     false,
				})
			}
		case "AssignmentPattern":
			left := sa.childByField(param, "left")
			if left != nil {
				sa.declarePattern(left, fnScope, false)
			}
		case "RestPattern":
			inner := sa.firstChildByKind(param, "Identifier")
			if inner != nil {
				name := inner.Text()
				if name != "" {
					fnScope.declare(name, &Declaration{
						Name:      name,
						FilePath:  sa.filePath,
						StartByte: sa.nodeStartByte(inner),
						isTDZ:     false,
					})
				}
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
			name := child.Text()
			if name != "" {
				scope.declare(name, &Declaration{
					Name:      name,
					FilePath:  sa.filePath,
					StartByte: sa.nodeStartByte(child),
					isTDZ:     false,
				})
			}
		case "NamedImports":
			// import { a, b as c } from '...'
			sa.processNamedImports(child, scope)
		case "NamespaceImport":
			// import * as ns from '...'
			ident := sa.firstChildByKind(child, "Identifier")
			if ident != nil {
				name := ident.Text()
				if name != "" {
					scope.declare(name, &Declaration{
						Name:      name,
						FilePath:  sa.filePath,
						StartByte: sa.nodeStartByte(ident),
						isTDZ:     false,
					})
				}
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
				name := alias.Text()
				if name != "" {
					scope.declare(name, &Declaration{
						Name:      name,
						FilePath:  sa.filePath,
						StartByte: sa.nodeStartByte(alias),
						isTDZ:     false,
					})
				}
			} else {
				// No alias — the local name is the imported name
				nameNode := sa.childByField(child, "name")
				if nameNode != nil {
					name := nameNode.Text()
					if name != "" {
						scope.declare(name, &Declaration{
							Name:      name,
							FilePath:  sa.filePath,
							StartByte: sa.nodeStartByte(nameNode),
							isTDZ:     false,
						})
					}
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

// childFieldText returns the text of the first child with the given field name.
func (sa *ScopeAnalyzer) childFieldText(n ASTNode, field string) string {
	child := sa.childByField(n, field)
	if child == nil {
		return ""
	}
	return child.Text()
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
