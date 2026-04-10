package extract

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/typescript/tsx"
	"github.com/smacker/go-tree-sitter/typescript/typescript"
)

// kindMap maps tree-sitter node type strings to tsq canonical PascalCase names.
var kindMap = map[string]string{
	"function_declaration":     "FunctionDeclaration",
	"arrow_function":           "ArrowFunction",
	"call_expression":          "CallExpression",
	"identifier":               "Identifier",
	"member_expression":        "MemberExpression",
	"variable_declarator":      "VariableDeclarator",
	"import_declaration":       "ImportDeclaration",
	"import_statement":         "ImportDeclaration", // TS grammar uses import_statement
	"export_statement":         "ExportStatement",
	"jsx_element":              "JsxElement",
	"jsx_self_closing_element": "JsxSelfClosingElement",
	"as_expression":            "AsExpression",
	"await_expression":         "AwaitExpression",
	"assignment_expression":    "AssignmentExpression",
	"binary_expression":        "BinaryExpression",
	"object_pattern":           "ObjectPattern",
	"array_pattern":            "ArrayPattern",
	"rest_pattern":             "RestPattern",
	// additional common nodes
	"program":                               "Program",
	"expression_statement":                  "ExpressionStatement",
	"return_statement":                      "ReturnStatement",
	"if_statement":                          "IfStatement",
	"for_statement":                         "ForStatement",
	"for_in_statement":                      "ForInStatement",
	"while_statement":                       "WhileStatement",
	"block":                                 "Block",
	"variable_declaration":                  "VariableDeclaration",
	"lexical_declaration":                   "LexicalDeclaration",
	"function_expression":                   "FunctionExpression",
	"generator_function":                    "GeneratorFunction",
	"generator_function_declaration":        "GeneratorFunctionDeclaration",
	"method_definition":                     "MethodDefinition",
	"class_declaration":                     "ClassDeclaration",
	"class_body":                            "ClassBody",
	"property_identifier":                   "PropertyIdentifier",
	"string":                                "String",
	"number":                                "Number",
	"template_string":                       "TemplateString",
	"object":                                "Object",
	"array":                                 "Array",
	"pair":                                  "Pair",
	"spread_element":                        "SpreadElement",
	"new_expression":                        "NewExpression",
	"parenthesized_expression":              "ParenthesizedExpression",
	"sequence_expression":                   "SequenceExpression",
	"conditional_expression":                "ConditionalExpression",
	"unary_expression":                      "UnaryExpression",
	"update_expression":                     "UpdateExpression",
	"subscript_expression":                  "SubscriptExpression",
	"type_annotation":                       "TypeAnnotation",
	"type_identifier":                       "TypeIdentifier",
	"predefined_type":                       "PredefinedType",
	"import_clause":                         "ImportClause",
	"named_imports":                         "NamedImports",
	"import_specifier":                      "ImportSpecifier",
	"export_clause":                         "ExportClause",
	"export_specifier":                      "ExportSpecifier",
	"namespace_import":                      "NamespaceImport",
	"interface_declaration":                 "InterfaceDeclaration",
	"type_alias_declaration":                "TypeAliasDeclaration",
	"jsx_opening_element":                   "JsxOpeningElement",
	"jsx_closing_element":                   "JsxClosingElement",
	"jsx_expression":                        "JsxExpression",
	"jsx_attribute":                         "JsxAttribute",
	"shorthand_property_identifier":         "ShorthandPropertyIdentifier",
	"shorthand_property_identifier_pattern": "ShorthandPropertyIdentifierPattern",
	"object_assignment_pattern":             "ObjectAssignmentPattern",
	"assignment_pattern":                    "AssignmentPattern",
	"try_statement":                         "TryStatement",
	"catch_clause":                          "CatchClause",
	"throw_statement":                       "ThrowStatement",
	"switch_statement":                      "SwitchStatement",
	"switch_case":                           "SwitchCase",
	"switch_default":                        "SwitchDefault",
	"break_statement":                       "BreakStatement",
	"continue_statement":                    "ContinueStatement",
	"labeled_statement":                     "LabeledStatement",
	"do_statement":                          "DoStatement",
	"decorator":                             "Decorator",
	"ERROR":                                 "Error",
	"extends_clause":                        "ExtendsClause",
	"implements_clause":                     "ImplementsClause",
	"class":                                 "ClassExpression",
	"abstract_class_declaration":            "AbstractClassDeclaration",
	"public_field_definition":               "PublicFieldDefinition",
	"formal_parameters":                     "FormalParameters",
	"required_parameter":                    "RequiredParameter",
	"optional_parameter":                    "OptionalParameter",
	"rest_parameter":                        "RestParameter",
	"pair_pattern":                          "PairPattern",
	"statement_block":                       "Block",
	"type_assertion":                        "TypeAssertion",
	"non_null_expression":                   "NonNullExpression",
	"satisfies_expression":                  "SatisfiesExpression",
	"heritage_clause":                       "HeritageClause",
	"extends_type_clause":                   "ExtendsTypeClause",
	"class_heritage":                        "ClassHeritage",
}

// normalise converts a tree-sitter node type string to a tsq canonical
// PascalCase name. It first checks the explicit map, then falls back to
// converting snake_case to PascalCase.
func normalise(tsType string) string {
	if mapped, ok := kindMap[tsType]; ok {
		return mapped
	}
	return snakeToPascal(tsType)
}

// snakeToPascal converts a snake_case string to PascalCase.
func snakeToPascal(s string) string {
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

// tsconfig is a minimal subset of tsconfig.json for include/exclude parsing.
type tsconfig struct {
	Include []string `json:"include"`
	Exclude []string `json:"exclude"`
}

// TreeSitterBackend implements ExtractorBackend using the smacker/go-tree-sitter
// bindings with the TypeScript grammar. .tsx files are parsed with the TSX
// grammar so that JSX syntax is handled correctly.
type TreeSitterBackend struct {
	parser    *sitter.Parser // TypeScript grammar parser
	tsxParser *sitter.Parser // TSX grammar parser (for .tsx files)
	files     []string
	rootDir   string
}

// Open initialises the backend. It resolves source files by walking rootDir
// and collecting .ts/.tsx files, optionally filtered by a tsconfig.json.
func (b *TreeSitterBackend) Open(ctx context.Context, cfg ProjectConfig) error {
	b.parser = sitter.NewParser()
	b.parser.SetLanguage(typescript.GetLanguage())
	b.tsxParser = sitter.NewParser()
	b.tsxParser.SetLanguage(tsx.GetLanguage())
	b.rootDir = cfg.RootDir

	var files []string
	var err error

	if cfg.TSConfig != "" {
		files, err = b.resolveFromTSConfig(cfg.TSConfig, cfg.RootDir)
		if err != nil {
			// Fall back to glob walk if tsconfig parse fails
			files, err = b.walkFiles(cfg.RootDir)
			if err != nil {
				return fmt.Errorf("treesitter backend: walk: %w", err)
			}
		}
	} else {
		files, err = b.walkFiles(cfg.RootDir)
		if err != nil {
			return fmt.Errorf("treesitter backend: walk: %w", err)
		}
	}

	b.files = files
	return nil
}

// walkFiles returns all .ts and .tsx files under root.
func (b *TreeSitterBackend) walkFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Skip common non-source directories
			name := d.Name()
			if name == "node_modules" || name == ".git" || name == "dist" || name == "build" {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".ts" || ext == ".tsx" {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

// resolveFromTSConfig reads a tsconfig.json and resolves included TS/TSX files.
// This is a best-effort implementation: it reads "include" and "exclude" arrays
// and performs simple glob matching.
func (b *TreeSitterBackend) resolveFromTSConfig(tsconfigPath, root string) ([]string, error) {
	data, err := os.ReadFile(tsconfigPath)
	if err != nil {
		return nil, fmt.Errorf("read tsconfig: %w", err)
	}

	var cfg tsconfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse tsconfig: %w", err)
	}

	// Build exclude set (directory names to skip)
	excludeSet := make(map[string]bool)
	for _, ex := range cfg.Exclude {
		excludeSet[filepath.Clean(filepath.Join(root, ex))] = true
	}

	// If no include patterns, walk everything
	if len(cfg.Include) == 0 {
		return b.walkFilesWithExcludes(root, excludeSet)
	}

	// Resolve include patterns.
	// Note: filepath.Glob does not support ** globbing. Patterns like "src/**/*.ts"
	// will silently match nothing. Use a simple directory prefix or rely on WalkDir
	// for full recursive discovery.
	var files []string
	seen := make(map[string]bool)
	for _, pattern := range cfg.Include {
		absPattern := filepath.Join(root, pattern)
		// Check if it ends with ** or has a glob
		if strings.Contains(pattern, "*") {
			matches, err := filepath.Glob(absPattern)
			if err == nil {
				for _, m := range matches {
					if !seen[m] && !excludeSet[m] {
						seen[m] = true
						files = append(files, m)
					}
				}
			}
		} else {
			// Treat as directory to walk or direct file
			info, err := os.Stat(absPattern)
			if err != nil {
				continue
			}
			if info.IsDir() {
				walked, err := b.walkFilesWithExcludes(absPattern, excludeSet)
				if err == nil {
					for _, f := range walked {
						if !seen[f] {
							seen[f] = true
							files = append(files, f)
						}
					}
				}
			} else {
				ext := strings.ToLower(filepath.Ext(absPattern))
				if (ext == ".ts" || ext == ".tsx") && !seen[absPattern] {
					seen[absPattern] = true
					files = append(files, absPattern)
				}
			}
		}
	}
	return files, nil
}

func (b *TreeSitterBackend) walkFilesWithExcludes(root string, excludeSet map[string]bool) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name == "node_modules" || name == ".git" || name == "dist" || name == "build" {
				return filepath.SkipDir
			}
			if excludeSet[filepath.Clean(path)] {
				return filepath.SkipDir
			}
			return nil
		}
		if excludeSet[filepath.Clean(path)] {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".ts" || ext == ".tsx" {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

// WalkAST walks all source files in depth-first order, calling the visitor
// for each node. Parsing errors are reported as ERROR nodes in the tree
// (tree-sitter always produces a complete tree even for syntax errors).
func (b *TreeSitterBackend) WalkAST(ctx context.Context, v ASTVisitor) error {
	for _, path := range b.files {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := b.walkFile(ctx, path, v); err != nil {
			return err
		}
	}
	return nil
}

func (b *TreeSitterBackend) walkFile(ctx context.Context, path string, v ASTVisitor) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	// Use the TSX grammar for .tsx files to correctly handle JSX syntax.
	parser := b.parser
	if strings.EqualFold(filepath.Ext(path), ".tsx") && b.tsxParser != nil {
		parser = b.tsxParser
	}

	tree, err := parser.ParseCtx(ctx, nil, content)
	if err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	defer tree.Close()

	if err := v.EnterFile(path); err != nil {
		return err
	}

	root := tree.RootNode()
	if err := walkNode(root, content, path, -1, "", v); err != nil {
		return err
	}

	return v.LeaveFile(path)
}

// walkNode recursively walks the tree in depth-first order.
func walkNode(n *sitter.Node, src []byte, path string, parentChildIdx int, fieldName string, v ASTVisitor) error {
	node := &tsASTNode{
		n:         n,
		src:       src,
		fieldName: fieldName,
	}

	descend, err := v.Enter(node)
	if err != nil {
		return err
	}

	if descend {
		count := int(n.ChildCount())
		for i := 0; i < count; i++ {
			child := n.Child(i)
			if child == nil {
				continue
			}
			childField := n.FieldNameForChild(i)
			if err := walkNode(child, src, path, i, childField, v); err != nil {
				return err
			}
		}
	}

	return v.Leave(node)
}

// ResolveSymbol returns ErrUnsupported. Symbol resolution is handled by
// scope.go (in-file) or a future LSP-enriched backend (cross-file).
func (b *TreeSitterBackend) ResolveSymbol(_ context.Context, _ SymbolRef) (SymbolDecl, error) {
	return SymbolDecl{}, ErrUnsupported
}

// ResolveType returns ErrUnsupported. Tree-sitter has no type information.
func (b *TreeSitterBackend) ResolveType(_ context.Context, _ NodeRef) (string, error) {
	return "", ErrUnsupported
}

// CrossFileRefs returns ErrUnsupported. Cross-file resolution requires a
// semantic backend (LSP or tsgo IPC).
func (b *TreeSitterBackend) CrossFileRefs(_ context.Context, _ SymbolRef) ([]NodeRef, error) {
	return nil, ErrUnsupported
}

// Close releases the tree-sitter parsers.
func (b *TreeSitterBackend) Close() error {
	if b.parser != nil {
		b.parser.Close()
		b.parser = nil
	}
	if b.tsxParser != nil {
		b.tsxParser.Close()
		b.tsxParser = nil
	}
	return nil
}

// tsASTNode wraps a sitter.Node and implements the ASTNode interface.
type tsASTNode struct {
	n         *sitter.Node
	src       []byte
	fieldName string
}

func (a *tsASTNode) Kind() string {
	return normalise(a.n.Type())
}

func (a *tsASTNode) StartLine() int {
	return int(a.n.StartPoint().Row) + 1
}

func (a *tsASTNode) StartCol() int {
	return int(a.n.StartPoint().Column)
}

func (a *tsASTNode) EndLine() int {
	return int(a.n.EndPoint().Row) + 1
}

func (a *tsASTNode) EndCol() int {
	return int(a.n.EndPoint().Column)
}

func (a *tsASTNode) Text() string {
	return a.n.Content(a.src)
}

func (a *tsASTNode) ChildCount() int {
	return int(a.n.ChildCount())
}

func (a *tsASTNode) Child(i int) ASTNode {
	child := a.n.Child(i)
	if child == nil {
		return nil
	}
	// Field name for child must be retrieved from the parent
	fieldName := a.n.FieldNameForChild(i)
	return &tsASTNode{
		n:         child,
		src:       a.src,
		fieldName: fieldName,
	}
}

func (a *tsASTNode) FieldName() string {
	return a.fieldName
}
