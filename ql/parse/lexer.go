// Package parse implements the QL lexer and recursive descent parser.
package parse

import (
	"unicode"
)

// TokenType identifies a QL token.
type TokenType int

const (
	// Literals
	TokIdent TokenType = iota
	TokInt
	TokString

	// Punctuation
	TokLParen // (
	TokRParen // )
	TokLBrace // {
	TokRBrace // }
	TokComma  // ,
	TokSemi   // ;
	TokDot    // .
	TokPipe   // |
	TokAt     // @
	TokColCol // ::

	// Operators
	TokEq    // =
	TokNeq   // !=
	TokLt    // <
	TokLte   // <=
	TokGt    // >
	TokGte   // >=
	TokPlus  // +
	TokMinus // -
	TokStar  // *
	TokSlash // /
	TokPct   // %

	// Keywords
	TokKwImport
	TokKwAs
	TokKwClass
	TokKwExtends
	TokKwPredicate
	TokKwFrom
	TokKwWhere
	TokKwSelect
	TokKwExists
	TokKwForall
	TokKwNot
	TokKwAnd
	TokKwOr
	TokKwInstanceof
	TokKwResult
	TokKwThis
	TokKwNone
	TokKwAny
	TokKwTrue
	TokKwFalse
	TokKwOverride
	TokKwAbstract
	TokKwModule
	TokKwPrivate
	TokKwCount
	TokKwMin
	TokKwMax
	TokKwSum
	TokKwAvg

	TokEOF
	TokError // malformed input
)

var keywords = map[string]TokenType{
	"import":     TokKwImport,
	"as":         TokKwAs,
	"class":      TokKwClass,
	"extends":    TokKwExtends,
	"predicate":  TokKwPredicate,
	"from":       TokKwFrom,
	"where":      TokKwWhere,
	"select":     TokKwSelect,
	"exists":     TokKwExists,
	"forall":     TokKwForall,
	"not":        TokKwNot,
	"and":        TokKwAnd,
	"or":         TokKwOr,
	"instanceof": TokKwInstanceof,
	"result":     TokKwResult,
	"this":       TokKwThis,
	"none":       TokKwNone,
	"any":        TokKwAny,
	"true":       TokKwTrue,
	"false":      TokKwFalse,
	"override":   TokKwOverride,
	"abstract":   TokKwAbstract,
	"module":     TokKwModule,
	"private":    TokKwPrivate,
	"count":      TokKwCount,
	"min":        TokKwMin,
	"max":        TokKwMax,
	"sum":        TokKwSum,
	"avg":        TokKwAvg,
}

// Token is a single QL token.
type Token struct {
	Type TokenType
	Lit  string // the literal text
	Line int
	Col  int
}

// Lexer tokenises QL source.
type Lexer struct {
	src  []rune
	pos  int
	line int
	col  int
	file string
	err  *Token // pending error token (e.g. unterminated block comment)
}

// NewLexer creates a Lexer for the given source.
func NewLexer(src, file string) *Lexer {
	return &Lexer{src: []rune(src), pos: 0, line: 1, col: 1, file: file}
}

func (l *Lexer) advance() rune {
	if l.pos >= len(l.src) {
		return 0
	}
	ch := l.src[l.pos]
	l.pos++
	if ch == '\n' {
		l.line++
		l.col = 1
	} else {
		l.col++
	}
	return ch
}

func (l *Lexer) skipWhitespace() {
	for l.pos < len(l.src) && unicode.IsSpace(l.src[l.pos]) {
		l.advance()
	}
}

func (l *Lexer) skipLineComment() {
	for l.pos < len(l.src) && l.src[l.pos] != '\n' {
		l.advance()
	}
}

func (l *Lexer) skipBlockComment() bool {
	// already consumed /*
	for l.pos < len(l.src) {
		if l.src[l.pos] == '*' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '/' {
			l.advance() // *
			l.advance() // /
			return true
		}
		l.advance()
	}
	return false // unterminated
}

func (l *Lexer) skipWhitespaceAndComments() {
	for {
		l.skipWhitespace()
		if l.pos >= len(l.src) {
			return
		}
		if l.src[l.pos] == '/' && l.pos+1 < len(l.src) {
			if l.src[l.pos+1] == '/' {
				l.advance()
				l.advance()
				l.skipLineComment()
				continue
			}
			if l.src[l.pos+1] == '*' {
				errLine := l.line
				errCol := l.col
				l.advance()
				l.advance()
				if !l.skipBlockComment() {
					tok := Token{Type: TokError, Lit: "unterminated block comment", Line: errLine, Col: errCol}
					l.err = &tok
					return
				}
				continue
			}
		}
		return
	}
}

// Next returns the next token.
func (l *Lexer) Next() Token {
	l.skipWhitespaceAndComments()

	if l.err != nil {
		tok := *l.err
		l.err = nil
		return tok
	}

	if l.pos >= len(l.src) {
		return Token{Type: TokEOF, Lit: "", Line: l.line, Col: l.col}
	}

	startLine := l.line
	startCol := l.col
	ch := l.src[l.pos]

	// Identifier or keyword
	if ch == '_' || unicode.IsLetter(ch) {
		start := l.pos
		for l.pos < len(l.src) && (l.src[l.pos] == '_' || unicode.IsLetter(l.src[l.pos]) || unicode.IsDigit(l.src[l.pos])) {
			l.advance()
		}
		lit := string(l.src[start:l.pos])
		typ := TokIdent
		if kw, ok := keywords[lit]; ok {
			typ = kw
		}
		return Token{Type: typ, Lit: lit, Line: startLine, Col: startCol}
	}

	// Integer literal
	if unicode.IsDigit(ch) {
		start := l.pos
		for l.pos < len(l.src) && unicode.IsDigit(l.src[l.pos]) {
			l.advance()
		}
		return Token{Type: TokInt, Lit: string(l.src[start:l.pos]), Line: startLine, Col: startCol}
	}

	// String literal
	if ch == '"' {
		l.advance() // opening "
		var buf []rune
		for l.pos < len(l.src) && l.src[l.pos] != '"' {
			if l.src[l.pos] == '\\' && l.pos+1 < len(l.src) {
				l.advance() // backslash
				esc := l.advance()
				switch esc {
				case 'n':
					buf = append(buf, '\n')
				case 't':
					buf = append(buf, '\t')
				case '\\':
					buf = append(buf, '\\')
				case '"':
					buf = append(buf, '"')
				default:
					buf = append(buf, '\\', esc)
				}
			} else {
				buf = append(buf, l.advance())
			}
		}
		if l.pos >= len(l.src) {
			return Token{Type: TokError, Lit: "unterminated string", Line: startLine, Col: startCol}
		}
		l.advance() // closing "
		return Token{Type: TokString, Lit: string(buf), Line: startLine, Col: startCol}
	}

	// Single/multi-character operators and punctuation
	l.advance()

	switch ch {
	case '(':
		return Token{Type: TokLParen, Lit: "(", Line: startLine, Col: startCol}
	case ')':
		return Token{Type: TokRParen, Lit: ")", Line: startLine, Col: startCol}
	case '{':
		return Token{Type: TokLBrace, Lit: "{", Line: startLine, Col: startCol}
	case '}':
		return Token{Type: TokRBrace, Lit: "}", Line: startLine, Col: startCol}
	case ',':
		return Token{Type: TokComma, Lit: ",", Line: startLine, Col: startCol}
	case ';':
		return Token{Type: TokSemi, Lit: ";", Line: startLine, Col: startCol}
	case '.':
		return Token{Type: TokDot, Lit: ".", Line: startLine, Col: startCol}
	case '|':
		return Token{Type: TokPipe, Lit: "|", Line: startLine, Col: startCol}
	case '@':
		return Token{Type: TokAt, Lit: "@", Line: startLine, Col: startCol}
	case '+':
		return Token{Type: TokPlus, Lit: "+", Line: startLine, Col: startCol}
	case '-':
		return Token{Type: TokMinus, Lit: "-", Line: startLine, Col: startCol}
	case '*':
		return Token{Type: TokStar, Lit: "*", Line: startLine, Col: startCol}
	case '%':
		return Token{Type: TokPct, Lit: "%", Line: startLine, Col: startCol}
	case '/':
		return Token{Type: TokSlash, Lit: "/", Line: startLine, Col: startCol}
	case ':':
		if l.pos < len(l.src) && l.src[l.pos] == ':' {
			l.advance()
			return Token{Type: TokColCol, Lit: "::", Line: startLine, Col: startCol}
		}
		return Token{Type: TokError, Lit: "unexpected ':'", Line: startLine, Col: startCol}
	case '=':
		return Token{Type: TokEq, Lit: "=", Line: startLine, Col: startCol}
	case '!':
		if l.pos < len(l.src) && l.src[l.pos] == '=' {
			l.advance()
			return Token{Type: TokNeq, Lit: "!=", Line: startLine, Col: startCol}
		}
		return Token{Type: TokError, Lit: "unexpected '!'", Line: startLine, Col: startCol}
	case '<':
		if l.pos < len(l.src) && l.src[l.pos] == '=' {
			l.advance()
			return Token{Type: TokLte, Lit: "<=", Line: startLine, Col: startCol}
		}
		return Token{Type: TokLt, Lit: "<", Line: startLine, Col: startCol}
	case '>':
		if l.pos < len(l.src) && l.src[l.pos] == '=' {
			l.advance()
			return Token{Type: TokGte, Lit: ">=", Line: startLine, Col: startCol}
		}
		return Token{Type: TokGt, Lit: ">", Line: startLine, Col: startCol}
	}

	return Token{Type: TokError, Lit: "unexpected character: " + string(ch), Line: startLine, Col: startCol}
}
