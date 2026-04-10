package parse

import (
	"fmt"
	"strconv"

	"github.com/Gjdoalfnrxu/tsq/ql/ast"
)

// Error is returned for malformed QL input.
type Error struct {
	File    string
	Line    int
	Col     int
	Message string
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s:%d:%d: %s", e.File, e.Line, e.Col, e.Message)
}

// Parser is a recursive descent QL parser.
type Parser struct {
	lexer   *Lexer
	current Token
	file    string
}

// NewParser creates a Parser for the given source.
func NewParser(src, file string) *Parser {
	p := &Parser{lexer: NewLexer(src, file), file: file}
	p.advance()
	return p
}

func (p *Parser) advance() Token {
	prev := p.current
	p.current = p.lexer.Next()
	return prev
}

// parserState captures the parser and lexer state for backtracking.
type parserState struct {
	current Token
	lexPos  int
	lexLine int
	lexCol  int
}

func (p *Parser) saveState() parserState {
	return parserState{
		current: p.current,
		lexPos:  p.lexer.pos,
		lexLine: p.lexer.line,
		lexCol:  p.lexer.col,
	}
}

func (p *Parser) restoreState(s parserState) {
	p.current = s.current
	p.lexer.pos = s.lexPos
	p.lexer.line = s.lexLine
	p.lexer.col = s.lexCol
}

func (p *Parser) at(t TokenType) bool {
	return p.current.Type == t
}

func (p *Parser) expect(t TokenType) (Token, error) {
	if p.current.Type != t {
		return p.current, p.errorf("expected %s, got %q", tokenName(t), p.current.Lit)
	}
	return p.advance(), nil
}

func (p *Parser) errorf(format string, args ...interface{}) error {
	return &Error{
		File:    p.file,
		Line:    p.current.Line,
		Col:     p.current.Col,
		Message: fmt.Sprintf(format, args...),
	}
}

func tokenName(t TokenType) string {
	switch t {
	case TokIdent:
		return "identifier"
	case TokInt:
		return "integer"
	case TokString:
		return "string"
	case TokLParen:
		return "'('"
	case TokRParen:
		return "')'"
	case TokLBrace:
		return "'{'"
	case TokRBrace:
		return "'}'"
	case TokComma:
		return "','"
	case TokSemi:
		return "';'"
	case TokDot:
		return "'.'"
	case TokPipe:
		return "'|'"
	case TokColCol:
		return "'::'"
	case TokEq:
		return "'='"
	case TokEOF:
		return "EOF"
	case TokKwIf:
		return "'if'"
	case TokKwThen:
		return "'then'"
	case TokKwElse:
		return "'else'"
	default:
		return fmt.Sprintf("token(%d)", t)
	}
}

// Parse parses the full module and returns an ast.Module or error.
func (p *Parser) Parse() (*ast.Module, error) {
	mod := &ast.Module{
		Span: ast.Span{File: p.file, StartLine: p.current.Line, StartCol: p.current.Col},
	}

	for !p.at(TokEOF) && !p.at(TokError) {
		switch p.current.Type {
		case TokKwImport:
			imp, err := p.parseImport()
			if err != nil {
				return nil, err
			}
			mod.Imports = append(mod.Imports, *imp)
		case TokKwAbstract:
			// abstract class ...
			cls, err := p.parseAbstractClass()
			if err != nil {
				return nil, err
			}
			mod.Classes = append(mod.Classes, *cls)
		case TokKwClass:
			cls, err := p.parseClass()
			if err != nil {
				return nil, err
			}
			mod.Classes = append(mod.Classes, *cls)
		case TokKwModule:
			m, err := p.parseModule()
			if err != nil {
				return nil, err
			}
			mod.Modules = append(mod.Modules, *m)
		case TokKwPredicate:
			pred, err := p.parsePredicate()
			if err != nil {
				return nil, err
			}
			mod.Predicates = append(mod.Predicates, *pred)
		case TokKwFrom:
			sel, err := p.parseSelectClause()
			if err != nil {
				return nil, err
			}
			mod.Select = sel
		case TokKwSelect:
			// select without from/where
			sel, err := p.parseSelectOnly()
			if err != nil {
				return nil, err
			}
			mod.Select = sel
		case TokIdent:
			// Could be a predicate with a return type like: string foo() { ... }
			pred, err := p.parseTypedPredicate()
			if err != nil {
				return nil, err
			}
			mod.Predicates = append(mod.Predicates, *pred)
		default:
			return nil, p.errorf("unexpected token %q at top level", p.current.Lit)
		}
	}

	if p.at(TokError) {
		return nil, p.errorf("lexer error: %s", p.current.Lit)
	}

	mod.Span.EndLine = p.current.Line
	mod.Span.EndCol = p.current.Col
	return mod, nil
}

func (p *Parser) parseImport() (*ast.ImportDecl, error) {
	tok, _ := p.expect(TokKwImport)
	imp := &ast.ImportDecl{
		Span: ast.Span{File: p.file, StartLine: tok.Line, StartCol: tok.Col},
	}

	// Parse the import path: ident (:: ident)*
	name, err := p.expect(TokIdent)
	if err != nil {
		return nil, err
	}
	path := name.Lit
	for p.at(TokColCol) {
		p.advance()
		next, err := p.expect(TokIdent)
		if err != nil {
			return nil, err
		}
		path += "::" + next.Lit
	}
	imp.Path = path

	// Optional alias
	if p.at(TokKwAs) {
		p.advance()
		alias, err := p.expect(TokIdent)
		if err != nil {
			return nil, err
		}
		imp.Alias = alias.Lit
	}

	imp.Span.EndLine = p.current.Line
	imp.Span.EndCol = p.current.Col
	return imp, nil
}

func (p *Parser) parseClass() (*ast.ClassDecl, error) {
	tok, _ := p.expect(TokKwClass)
	cls := &ast.ClassDecl{
		Span: ast.Span{File: p.file, StartLine: tok.Line, StartCol: tok.Col},
	}

	name, err := p.expect(TokIdent)
	if err != nil {
		return nil, err
	}
	cls.Name = name.Lit

	if p.at(TokKwExtends) {
		p.advance()
		for {
			tr, err := p.parseTypeRef()
			if err != nil {
				return nil, err
			}
			cls.SuperTypes = append(cls.SuperTypes, *tr)
			if !p.at(TokComma) {
				break
			}
			p.advance()
		}
	}

	if _, err := p.expect(TokLBrace); err != nil {
		return nil, err
	}

	for !p.at(TokRBrace) && !p.at(TokEOF) {
		member, isCharPred, err := p.parseClassMember(cls.Name)
		if err != nil {
			return nil, err
		}
		if isCharPred {
			body := member.Body
			cls.CharPred = body
		} else {
			cls.Members = append(cls.Members, *member)
		}
	}

	end, err := p.expect(TokRBrace)
	if err != nil {
		return nil, err
	}
	cls.Span.EndLine = end.Line
	cls.Span.EndCol = end.Col
	return cls, nil
}

// parseAbstractClass parses: abstract class Name extends ... { ... }
func (p *Parser) parseAbstractClass() (*ast.ClassDecl, error) {
	p.advance() // consume 'abstract'
	if !p.at(TokKwClass) {
		return nil, p.errorf("expected 'class' after 'abstract', got %q", p.current.Lit)
	}
	cls, err := p.parseClass()
	if err != nil {
		return nil, err
	}
	cls.IsAbstract = true
	return cls, nil
}

// parseModule parses: module Name { <classes, predicates, nested modules> }
func (p *Parser) parseModule() (*ast.ModuleDecl, error) {
	tok, _ := p.expect(TokKwModule)
	m := &ast.ModuleDecl{
		Span: ast.Span{File: p.file, StartLine: tok.Line, StartCol: tok.Col},
	}

	name, err := p.expect(TokIdent)
	if err != nil {
		return nil, err
	}
	m.Name = name.Lit

	if _, err := p.expect(TokLBrace); err != nil {
		return nil, err
	}

	for !p.at(TokRBrace) && !p.at(TokEOF) {
		switch p.current.Type {
		case TokKwAbstract:
			cls, err := p.parseAbstractClass()
			if err != nil {
				return nil, err
			}
			m.Classes = append(m.Classes, *cls)
		case TokKwClass:
			cls, err := p.parseClass()
			if err != nil {
				return nil, err
			}
			m.Classes = append(m.Classes, *cls)
		case TokKwPredicate:
			pred, err := p.parsePredicate()
			if err != nil {
				return nil, err
			}
			m.Predicates = append(m.Predicates, *pred)
		case TokKwModule:
			nested, err := p.parseModule()
			if err != nil {
				return nil, err
			}
			m.Modules = append(m.Modules, *nested)
		case TokIdent:
			pred, err := p.parseTypedPredicate()
			if err != nil {
				return nil, err
			}
			m.Predicates = append(m.Predicates, *pred)
		default:
			return nil, p.errorf("unexpected token %q in module body", p.current.Lit)
		}
	}

	end, err := p.expect(TokRBrace)
	if err != nil {
		return nil, err
	}
	m.Span.EndLine = end.Line
	m.Span.EndCol = end.Col
	return m, nil
}

func (p *Parser) parseClassMember(className string) (*ast.MemberDecl, bool, error) {
	startLine := p.current.Line
	startCol := p.current.Col
	member := &ast.MemberDecl{
		Span: ast.Span{File: p.file, StartLine: startLine, StartCol: startCol},
	}

	// Check for override modifier
	if p.at(TokKwOverride) {
		member.Override = true
		p.advance()
	}

	// Check for predicate keyword
	if p.at(TokKwPredicate) {
		p.advance()
		name, err := p.expect(TokIdent)
		if err != nil {
			return nil, false, err
		}
		member.Name = name.Lit
		params, err := p.parseParamList()
		if err != nil {
			return nil, false, err
		}
		member.Params = params
		body, err := p.parseBody()
		if err != nil {
			return nil, false, err
		}
		member.Body = body
		member.Span.EndLine = p.current.Line
		member.Span.EndCol = p.current.Col
		return member, false, nil
	}

	// Could be: ClassName() { ... } (characteristic predicate)
	// or: ReturnType name(...) { ... } (method)
	// or: name(...) { ... } (predicate member without return type)
	// We need to look ahead to distinguish.

	// Parse what looks like a type or name
	if !p.at(TokIdent) {
		return nil, false, p.errorf("expected member declaration, got %q", p.current.Lit)
	}

	firstName := p.current
	p.advance()

	// Collect qualified name parts
	qualParts := []string{firstName.Lit}
	for p.at(TokColCol) {
		p.advance()
		next, err := p.expect(TokIdent)
		if err != nil {
			return nil, false, err
		}
		qualParts = append(qualParts, next.Lit)
	}

	// Check if this is the characteristic predicate: ClassName()
	if len(qualParts) == 1 && qualParts[0] == className && p.at(TokLParen) {
		// Characteristic predicate
		p.advance() // (
		if _, err := p.expect(TokRParen); err != nil {
			return nil, false, err
		}
		body, err := p.parseBody()
		if err != nil {
			return nil, false, err
		}
		member.Name = className
		member.Body = body
		member.Span.EndLine = p.current.Line
		member.Span.EndCol = p.current.Col
		return member, true, nil
	}

	// If next is '(' then this is a predicate member with no return type
	if len(qualParts) == 1 && p.at(TokLParen) {
		member.Name = qualParts[0]
		params, err := p.parseParamList()
		if err != nil {
			return nil, false, err
		}
		member.Params = params
		body, err := p.parseBody()
		if err != nil {
			return nil, false, err
		}
		member.Body = body
		member.Span.EndLine = p.current.Line
		member.Span.EndCol = p.current.Col
		return member, false, nil
	}

	// Otherwise it's a typed member: Type name(...)
	retType := &ast.TypeRef{
		Path: qualParts,
		Span: ast.Span{File: p.file, StartLine: firstName.Line, StartCol: firstName.Col},
	}
	member.ReturnType = retType

	name, err := p.expect(TokIdent)
	if err != nil {
		return nil, false, err
	}
	member.Name = name.Lit

	params, err := p.parseParamList()
	if err != nil {
		return nil, false, err
	}
	member.Params = params

	body, err := p.parseBody()
	if err != nil {
		return nil, false, err
	}
	member.Body = body
	member.Span.EndLine = p.current.Line
	member.Span.EndCol = p.current.Col
	return member, false, nil
}

func (p *Parser) parsePredicate() (*ast.PredicateDecl, error) {
	tok, _ := p.expect(TokKwPredicate)
	pred := &ast.PredicateDecl{
		Span: ast.Span{File: p.file, StartLine: tok.Line, StartCol: tok.Col},
	}

	name, err := p.expect(TokIdent)
	if err != nil {
		return nil, err
	}
	pred.Name = name.Lit

	params, err := p.parseParamList()
	if err != nil {
		return nil, err
	}
	pred.Params = params

	body, err := p.parseBody()
	if err != nil {
		return nil, err
	}
	pred.Body = body
	pred.Span.EndLine = p.current.Line
	pred.Span.EndCol = p.current.Col
	return pred, nil
}

// parseTypedPredicate parses: Type name(...) { ... } at top level
func (p *Parser) parseTypedPredicate() (*ast.PredicateDecl, error) {
	startLine := p.current.Line
	startCol := p.current.Col
	pred := &ast.PredicateDecl{
		Span: ast.Span{File: p.file, StartLine: startLine, StartCol: startCol},
	}

	retType, err := p.parseTypeRef()
	if err != nil {
		return nil, err
	}
	pred.ReturnType = retType

	name, err := p.expect(TokIdent)
	if err != nil {
		return nil, err
	}
	pred.Name = name.Lit

	params, err := p.parseParamList()
	if err != nil {
		return nil, err
	}
	pred.Params = params

	body, err := p.parseBody()
	if err != nil {
		return nil, err
	}
	pred.Body = body
	pred.Span.EndLine = p.current.Line
	pred.Span.EndCol = p.current.Col
	return pred, nil
}

func (p *Parser) parseParamList() ([]ast.ParamDecl, error) {
	if _, err := p.expect(TokLParen); err != nil {
		return nil, err
	}

	var params []ast.ParamDecl
	if !p.at(TokRParen) {
		for {
			param, err := p.parseParamDecl()
			if err != nil {
				return nil, err
			}
			params = append(params, *param)
			if !p.at(TokComma) {
				break
			}
			p.advance()
		}
	}

	if _, err := p.expect(TokRParen); err != nil {
		return nil, err
	}
	return params, nil
}

func (p *Parser) parseParamDecl() (*ast.ParamDecl, error) {
	startLine := p.current.Line
	startCol := p.current.Col

	tr, err := p.parseTypeRef()
	if err != nil {
		return nil, err
	}

	name, err := p.expect(TokIdent)
	if err != nil {
		return nil, err
	}

	return &ast.ParamDecl{
		Type: *tr,
		Name: name.Lit,
		Span: ast.Span{File: p.file, StartLine: startLine, StartCol: startCol, EndLine: name.Line, EndCol: name.Col},
	}, nil
}

func (p *Parser) parseTypeRef() (*ast.TypeRef, error) {
	startLine := p.current.Line
	startCol := p.current.Col

	// Support @type references (database types used in bridge .qll files).
	// After '@', any token with a Lit value (including keywords like "extends")
	// is accepted as the type name. This is necessary because relation names
	// may coincide with QL keywords (e.g., @extends for the Extends relation).
	if p.at(TokAt) {
		p.advance()
		tok := p.current
		if tok.Lit == "" {
			return nil, p.errorf("expected identifier after @, got %s", tokenName(tok.Type))
		}
		p.advance()
		return &ast.TypeRef{
			Path: []string{"@" + tok.Lit},
			Span: ast.Span{File: p.file, StartLine: startLine, StartCol: startCol},
		}, nil
	}

	name, err := p.expect(TokIdent)
	if err != nil {
		return nil, err
	}
	parts := []string{name.Lit}

	for p.at(TokColCol) {
		p.advance()
		next, err := p.expect(TokIdent)
		if err != nil {
			return nil, err
		}
		parts = append(parts, next.Lit)
	}

	return &ast.TypeRef{
		Path: parts,
		Span: ast.Span{File: p.file, StartLine: startLine, StartCol: startCol},
	}, nil
}

func (p *Parser) parseBody() (*ast.Formula, error) {
	if _, err := p.expect(TokLBrace); err != nil {
		return nil, err
	}

	if p.at(TokRBrace) {
		p.advance()
		return nil, nil
	}

	f, err := p.parseFormula()
	if err != nil {
		return nil, err
	}

	if _, err := p.expect(TokRBrace); err != nil {
		return nil, err
	}

	return &f, nil
}

// --- Formula parsing ---

func (p *Parser) parseFormula() (ast.Formula, error) {
	return p.parseOr()
}

func (p *Parser) parseOr() (ast.Formula, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.at(TokKwOr) {
		p.advance()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &ast.Disjunction{
			BaseFormula: ast.BaseFormula{Span: left.GetSpan()},
			Left:        left,
			Right:       right,
		}
	}
	return left, nil
}

func (p *Parser) parseAnd() (ast.Formula, error) {
	left, err := p.parseNot()
	if err != nil {
		return nil, err
	}
	for p.at(TokKwAnd) {
		p.advance()
		right, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		left = &ast.Conjunction{
			BaseFormula: ast.BaseFormula{Span: left.GetSpan()},
			Left:        left,
			Right:       right,
		}
	}
	return left, nil
}

func (p *Parser) parseNot() (ast.Formula, error) {
	if p.at(TokKwNot) {
		tok := p.advance()
		f, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		return &ast.Negation{
			BaseFormula: ast.BaseFormula{Span: ast.Span{File: p.file, StartLine: tok.Line, StartCol: tok.Col}},
			Formula:     f,
		}, nil
	}
	if p.at(TokKwIf) {
		return p.parseIfThenElse()
	}
	return p.parseComparisonOrAtom()
}

func (p *Parser) parseIfThenElse() (ast.Formula, error) {
	tok := p.advance() // consume 'if'
	cond, err := p.parseFormula()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(TokKwThen); err != nil {
		return nil, err
	}
	thenF, err := p.parseFormula()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(TokKwElse); err != nil {
		return nil, err
	}
	elseF, err := p.parseFormula()
	if err != nil {
		return nil, err
	}
	return &ast.IfThenElse{
		BaseFormula: ast.BaseFormula{Span: ast.Span{File: p.file, StartLine: tok.Line, StartCol: tok.Col}},
		Cond:        cond,
		Then:        thenF,
		Else:        elseF,
	}, nil
}

func (p *Parser) parseComparisonOrAtom() (ast.Formula, error) {
	// Try to parse an expression, and if followed by comparison op or instanceof, make it a comparison/instanceof.
	// Otherwise, it must be a formula atom (predicate call, exists, forall, etc.)

	// Check for closure call: ident+(args...) or ident*(args...)
	if p.at(TokIdent) {
		saved := p.saveState()
		name := p.advance()
		if p.at(TokPlus) || p.at(TokStar) {
			isPlus := p.at(TokPlus)
			p.advance() // consume + or *
			if p.at(TokLParen) {
				// This is a closure call
				args, err := p.parseArgList()
				if err != nil {
					return nil, err
				}
				return &ast.ClosureCall{
					BaseFormula: ast.BaseFormula{Span: ast.Span{File: p.file, StartLine: name.Line, StartCol: name.Col}},
					Name:        name.Lit,
					Plus:        isPlus,
					Args:        args,
				}, nil
			}
		}
		// Not a closure call — restore state
		p.restoreState(saved)
	}

	// Check for formula atoms first
	switch p.current.Type {
	case TokKwExists:
		return p.parseExists()
	case TokKwForall:
		return p.parseForall()
	case TokKwNone:
		return p.parseNone()
	case TokKwAny:
		return p.parseAnyFormula()
	case TokLParen:
		// Parenthesised formula
		p.advance()
		f, err := p.parseFormula()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(TokRParen); err != nil {
			return nil, err
		}
		return f, nil
	}

	// Parse expression-based formula: comparison, instanceof, or predicate call
	expr, err := p.parseExpr()
	if err != nil {
		return nil, err
	}

	// Check for comparison operators
	if isComparisonOp(p.current.Type) {
		op := p.advance()
		right, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		return &ast.Comparison{
			BaseFormula: ast.BaseFormula{Span: expr.GetSpan()},
			Left:        expr,
			Right:       right,
			Op:          op.Lit,
		}, nil
	}

	// Check for instanceof
	if p.at(TokKwInstanceof) {
		p.advance()
		tr, err := p.parseTypeRef()
		if err != nil {
			return nil, err
		}
		return &ast.InstanceOf{
			BaseFormula: ast.BaseFormula{Span: expr.GetSpan()},
			Expr:        expr,
			Type:        *tr,
		}, nil
	}

	// Must be a predicate call expression used as a formula
	// Convert MethodCall or function call expression to PredicateCall formula
	return exprToFormula(expr)
}

func exprToFormula(e ast.Expr) (ast.Formula, error) {
	switch v := e.(type) {
	case *ast.MethodCall:
		return &ast.PredicateCall{
			BaseFormula: ast.BaseFormula{Span: v.GetSpan()},
			Recv:        v.Recv,
			Name:        v.Method,
			Args:        v.Args,
		}, nil
	case *ast.Variable:
		// Bare name used as formula — treat as zero-arg predicate call
		return &ast.PredicateCall{
			BaseFormula: ast.BaseFormula{Span: v.GetSpan()},
			Name:        v.Name,
		}, nil
	default:
		return nil, fmt.Errorf("expression cannot be used as formula")
	}
}

func isComparisonOp(t TokenType) bool {
	return t == TokEq || t == TokNeq || t == TokLt || t == TokLte || t == TokGt || t == TokGte
}

func (p *Parser) parseExists() (ast.Formula, error) {
	tok := p.advance() // exists
	if _, err := p.expect(TokLParen); err != nil {
		return nil, err
	}

	decls, err := p.parseVarDeclList()
	if err != nil {
		return nil, err
	}

	if _, err := p.expect(TokPipe); err != nil {
		return nil, err
	}

	body, err := p.parseFormula()
	if err != nil {
		return nil, err
	}

	// Check for second pipe (guard | body form)
	var guard ast.Formula
	if p.at(TokPipe) {
		p.advance()
		guard = body
		body, err = p.parseFormula()
		if err != nil {
			return nil, err
		}
	}

	if _, err := p.expect(TokRParen); err != nil {
		return nil, err
	}

	return &ast.Exists{
		BaseFormula: ast.BaseFormula{Span: ast.Span{File: p.file, StartLine: tok.Line, StartCol: tok.Col}},
		Decls:       decls,
		Guard:       guard,
		Body:        body,
	}, nil
}

func (p *Parser) parseForall() (ast.Formula, error) {
	tok := p.advance() // forall
	if _, err := p.expect(TokLParen); err != nil {
		return nil, err
	}

	decls, err := p.parseVarDeclList()
	if err != nil {
		return nil, err
	}

	if _, err := p.expect(TokPipe); err != nil {
		return nil, err
	}

	guard, err := p.parseFormula()
	if err != nil {
		return nil, err
	}

	if _, err := p.expect(TokPipe); err != nil {
		return nil, err
	}

	body, err := p.parseFormula()
	if err != nil {
		return nil, err
	}

	if _, err := p.expect(TokRParen); err != nil {
		return nil, err
	}

	return &ast.Forall{
		BaseFormula: ast.BaseFormula{Span: ast.Span{File: p.file, StartLine: tok.Line, StartCol: tok.Col}},
		Decls:       decls,
		Guard:       guard,
		Body:        body,
	}, nil
}

func (p *Parser) parseNone() (ast.Formula, error) {
	tok := p.advance() // none
	if _, err := p.expect(TokLParen); err != nil {
		return nil, err
	}
	if _, err := p.expect(TokRParen); err != nil {
		return nil, err
	}
	return &ast.None{
		BaseFormula: ast.BaseFormula{Span: ast.Span{File: p.file, StartLine: tok.Line, StartCol: tok.Col}},
	}, nil
}

func (p *Parser) parseAnyFormula() (ast.Formula, error) {
	tok := p.advance() // any
	if _, err := p.expect(TokLParen); err != nil {
		return nil, err
	}
	if _, err := p.expect(TokRParen); err != nil {
		return nil, err
	}
	return &ast.Any{
		BaseFormula: ast.BaseFormula{Span: ast.Span{File: p.file, StartLine: tok.Line, StartCol: tok.Col}},
	}, nil
}

func (p *Parser) parseVarDeclList() ([]ast.VarDecl, error) {
	var decls []ast.VarDecl
	for {
		tr, err := p.parseTypeRef()
		if err != nil {
			return nil, err
		}
		name, err := p.expect(TokIdent)
		if err != nil {
			return nil, err
		}
		decls = append(decls, ast.VarDecl{
			Type: *tr,
			Name: name.Lit,
			Span: ast.Span{File: p.file, StartLine: tr.Span.StartLine, StartCol: tr.Span.StartCol},
		})
		if !p.at(TokComma) {
			break
		}
		p.advance()
	}
	return decls, nil
}

// --- Expression parsing ---

func (p *Parser) parseExpr() (ast.Expr, error) {
	return p.parseAddSub()
}

func (p *Parser) parseAddSub() (ast.Expr, error) {
	left, err := p.parseMulDiv()
	if err != nil {
		return nil, err
	}
	for p.at(TokPlus) || p.at(TokMinus) {
		op := p.advance()
		right, err := p.parseMulDiv()
		if err != nil {
			return nil, err
		}
		left = &ast.BinaryExpr{
			BaseExpr: ast.BaseExpr{Span: left.GetSpan()},
			Left:     left,
			Right:    right,
			Op:       op.Lit,
		}
	}
	return left, nil
}

func (p *Parser) parseMulDiv() (ast.Expr, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for p.at(TokStar) || p.at(TokSlash) || p.at(TokPct) {
		op := p.advance()
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		left = &ast.BinaryExpr{
			BaseExpr: ast.BaseExpr{Span: left.GetSpan()},
			Left:     left,
			Right:    right,
			Op:       op.Lit,
		}
	}
	return left, nil
}

func (p *Parser) parseUnary() (ast.Expr, error) {
	if p.at(TokMinus) {
		op := p.advance()
		expr, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &ast.BinaryExpr{
			BaseExpr: ast.BaseExpr{Span: ast.Span{File: p.file, StartLine: op.Line, StartCol: op.Col}},
			Left:     &ast.IntLiteral{BaseExpr: ast.BaseExpr{Span: ast.Span{File: p.file, StartLine: op.Line, StartCol: op.Col}}, Value: 0},
			Right:    expr,
			Op:       "-",
		}, nil
	}
	return p.parsePostfix()
}

func (p *Parser) parsePostfix() (ast.Expr, error) {
	expr, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}

	for p.at(TokDot) {
		p.advance() // .

		// Cast: .(Type)
		if p.at(TokLParen) {
			p.advance()
			tr, err := p.parseTypeRef()
			if err != nil {
				return nil, err
			}
			if _, err := p.expect(TokRParen); err != nil {
				return nil, err
			}
			expr = &ast.Cast{
				BaseExpr: ast.BaseExpr{Span: expr.GetSpan()},
				Expr:     expr,
				Type:     *tr,
			}
			continue
		}

		name, err := p.expect(TokIdent)
		if err != nil {
			return nil, err
		}

		// Method call: .name(args...)
		if p.at(TokLParen) {
			args, err := p.parseArgList()
			if err != nil {
				return nil, err
			}
			expr = &ast.MethodCall{
				BaseExpr: ast.BaseExpr{Span: expr.GetSpan()},
				Recv:     expr,
				Method:   name.Lit,
				Args:     args,
			}
		} else {
			// Field access: .name
			expr = &ast.FieldAccess{
				BaseExpr: ast.BaseExpr{Span: expr.GetSpan()},
				Recv:     expr,
				Field:    name.Lit,
			}
		}
	}

	return expr, nil
}

func (p *Parser) parsePrimary() (ast.Expr, error) {
	switch p.current.Type {
	case TokInt:
		tok := p.advance()
		val, err := strconv.ParseInt(tok.Lit, 10, 64)
		if err != nil {
			return nil, &Error{File: p.file, Line: tok.Line, Col: tok.Col, Message: "invalid integer: " + tok.Lit}
		}
		return &ast.IntLiteral{
			BaseExpr: ast.BaseExpr{Span: ast.Span{File: p.file, StartLine: tok.Line, StartCol: tok.Col}},
			Value:    val,
		}, nil

	case TokString:
		tok := p.advance()
		return &ast.StringLiteral{
			BaseExpr: ast.BaseExpr{Span: ast.Span{File: p.file, StartLine: tok.Line, StartCol: tok.Col}},
			Value:    tok.Lit,
		}, nil

	case TokKwTrue:
		tok := p.advance()
		return &ast.BoolLiteral{
			BaseExpr: ast.BaseExpr{Span: ast.Span{File: p.file, StartLine: tok.Line, StartCol: tok.Col}},
			Value:    true,
		}, nil

	case TokKwFalse:
		tok := p.advance()
		return &ast.BoolLiteral{
			BaseExpr: ast.BaseExpr{Span: ast.Span{File: p.file, StartLine: tok.Line, StartCol: tok.Col}},
			Value:    false,
		}, nil

	case TokKwThis:
		tok := p.advance()
		return &ast.Variable{
			BaseExpr: ast.BaseExpr{Span: ast.Span{File: p.file, StartLine: tok.Line, StartCol: tok.Col}},
			Name:     "this",
		}, nil

	case TokKwResult:
		tok := p.advance()
		return &ast.Variable{
			BaseExpr: ast.BaseExpr{Span: ast.Span{File: p.file, StartLine: tok.Line, StartCol: tok.Col}},
			Name:     "result",
		}, nil

	case TokKwCount, TokKwMin, TokKwMax, TokKwSum, TokKwAvg:
		return p.parseAggregate()

	case TokIdent:
		tok := p.advance()
		// Check for function call: ident(args...)
		if p.at(TokLParen) {
			// But first check if this might be a qualified name function call
			// For now, simple ident(args...) — treat as method call with nil recv
			args, err := p.parseArgList()
			if err != nil {
				return nil, err
			}
			return &ast.MethodCall{
				BaseExpr: ast.BaseExpr{Span: ast.Span{File: p.file, StartLine: tok.Line, StartCol: tok.Col}},
				Recv:     nil,
				Method:   tok.Lit,
				Args:     args,
			}, nil
		}
		return &ast.Variable{
			BaseExpr: ast.BaseExpr{Span: ast.Span{File: p.file, StartLine: tok.Line, StartCol: tok.Col}},
			Name:     tok.Lit,
		}, nil

	case TokLParen:
		p.advance()
		expr, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(TokRParen); err != nil {
			return nil, err
		}
		return expr, nil

	default:
		return nil, p.errorf("unexpected token %q in expression", p.current.Lit)
	}
}

func (p *Parser) parseAggregate() (ast.Expr, error) {
	tok := p.advance() // count/min/max/sum/avg
	opName := tok.Lit

	if _, err := p.expect(TokLParen); err != nil {
		return nil, err
	}

	decls, err := p.parseVarDeclList()
	if err != nil {
		return nil, err
	}

	if _, err := p.expect(TokPipe); err != nil {
		return nil, err
	}

	body, err := p.parseFormula()
	if err != nil {
		return nil, err
	}

	// Check for optional expr after another pipe (for min/max/sum/avg: | expr)
	var aggExpr ast.Expr
	var guard ast.Formula
	if p.at(TokPipe) {
		p.advance()
		// Could be guard | body pattern OR body | expr pattern
		// For aggregates, the pattern is: decls | guard | expr
		// So body was actually the guard, and now we parse the expression
		guard = body
		aggExpr, err = p.parseExpr()
		if err != nil {
			return nil, err
		}
		// The "body" for aggregate in this case is the guard
		body = nil
	}

	if _, err := p.expect(TokRParen); err != nil {
		return nil, err
	}

	return &ast.Aggregate{
		BaseExpr: ast.BaseExpr{Span: ast.Span{File: p.file, StartLine: tok.Line, StartCol: tok.Col}},
		Op:       opName,
		Decls:    decls,
		Guard:    guard,
		Body:     body,
		Expr:     aggExpr,
	}, nil
}

func (p *Parser) parseArgList() ([]ast.Expr, error) {
	if _, err := p.expect(TokLParen); err != nil {
		return nil, err
	}

	var args []ast.Expr
	if !p.at(TokRParen) {
		for {
			arg, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			args = append(args, arg)
			if !p.at(TokComma) {
				break
			}
			p.advance()
		}
	}

	if _, err := p.expect(TokRParen); err != nil {
		return nil, err
	}
	return args, nil
}

// --- Select clause parsing ---

func (p *Parser) parseSelectClause() (*ast.SelectClause, error) {
	tok, _ := p.expect(TokKwFrom)
	sel := &ast.SelectClause{
		Span: ast.Span{File: p.file, StartLine: tok.Line, StartCol: tok.Col},
	}

	// Parse from declarations
	if !p.at(TokKwWhere) && !p.at(TokKwSelect) {
		decls, err := p.parseVarDeclList()
		if err != nil {
			return nil, err
		}
		sel.Decls = decls
	}

	// Optional where
	if p.at(TokKwWhere) {
		p.advance()
		f, err := p.parseFormula()
		if err != nil {
			return nil, err
		}
		sel.Where = &f
	}

	// select
	if _, err := p.expect(TokKwSelect); err != nil {
		return nil, err
	}

	exprs, labels, err := p.parseSelectExprs()
	if err != nil {
		return nil, err
	}
	sel.Select = exprs
	sel.Labels = labels

	sel.Span.EndLine = p.current.Line
	sel.Span.EndCol = p.current.Col
	return sel, nil
}

// parseSelectOnly parses `select expr, ...` without from/where.
func (p *Parser) parseSelectOnly() (*ast.SelectClause, error) {
	tok, _ := p.expect(TokKwSelect)
	sel := &ast.SelectClause{
		Span: ast.Span{File: p.file, StartLine: tok.Line, StartCol: tok.Col},
	}

	exprs, labels, err := p.parseSelectExprs()
	if err != nil {
		return nil, err
	}
	sel.Select = exprs
	sel.Labels = labels

	sel.Span.EndLine = p.current.Line
	sel.Span.EndCol = p.current.Col
	return sel, nil
}

func (p *Parser) parseSelectExprs() ([]ast.Expr, []string, error) {
	var exprs []ast.Expr
	var labels []string
	for {
		expr, err := p.parseExpr()
		if err != nil {
			return nil, nil, err
		}
		exprs = append(exprs, expr)

		// Optional label
		label := ""
		if p.at(TokKwAs) {
			p.advance()
			lt, err := p.expect(TokString)
			if err != nil {
				return nil, nil, err
			}
			label = lt.Lit
		}
		labels = append(labels, label)

		if !p.at(TokComma) {
			break
		}
		p.advance()
	}
	return exprs, labels, nil
}
