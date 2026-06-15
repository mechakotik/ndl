package ndl

import "unicode/utf8"

type tokenType int

const (
	tokenInvalid tokenType = iota
	tokenEOF
	tokenWhitespace
	tokenLineComment
	tokenBlockComment

	tokenKey
	tokenQuotedKey
	tokenDot

	tokenString
	tokenInt
	tokenReal
	tokenBool
	tokenNull

	tokenLBrace
	tokenRBrace
	tokenLBracket
	tokenRBracket
)

// Position represents a byte position in an NDL document.
type Position struct {
	Offset int
	Line   int
	Column int
}

// Span represents a span of bytes in an NDL document.
type Span struct {
	Start Position
	End   Position
}

type token struct {
	Type tokenType
	Raw  string
	Span Span
}

func tokenize(doc string) ([]token, error) {
	if !utf8.ValidString(doc) {
		return nil, makeError(Span{}, "invalid UTF-8 document")
	}

	tokens := []token{}
	sc := newScanner(doc)

	for {
		token, err := sc.next()
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, token)
		if token.Type == tokenEOF {
			break
		}
	}

	return tokens, nil
}

type scanner struct {
	doc  string
	pos  int
	line int
	col  int
}

func newScanner(doc string) *scanner {
	return &scanner{
		doc:  doc,
		pos:  0,
		line: 1,
		col:  1,
	}
}

func (sc *scanner) eof() bool {
	return sc.pos >= len(sc.doc)
}

func (sc *scanner) peek() byte {
	if sc.eof() {
		return 0
	}
	return sc.doc[sc.pos]
}

func (sc *scanner) peek2() byte {
	if sc.pos+1 >= len(sc.doc) {
		return 0
	}
	return sc.doc[sc.pos+1]
}

func (sc *scanner) advance() byte {
	if sc.eof() {
		return 0
	}

	ch := sc.doc[sc.pos]
	sc.pos++

	if ch == '\r' {
		if sc.pos < len(sc.doc) && sc.doc[sc.pos] == '\n' {
			sc.pos++
		}
		sc.line++
		sc.col = 1
		return '\n'
	}

	if ch == '\n' {
		sc.line++
		sc.col = 1
	} else {
		sc.col++
	}

	return ch
}

func (sc *scanner) mark() Position {
	return Position{
		Offset: sc.pos,
		Line:   sc.line,
		Column: sc.col,
	}
}

func (sc *scanner) spanFrom(start Position) Span {
	return Span{
		Start: start,
		End:   sc.mark(),
	}
}

func (sc *scanner) makeToken(typ tokenType, start Position) token {
	return token{
		Type: typ,
		Raw:  sc.doc[start.Offset:sc.pos],
		Span: sc.spanFrom(start),
	}
}

func (sc *scanner) next() (token, error) {
	start := sc.mark()
	if sc.eof() {
		return sc.makeToken(tokenEOF, start), nil
	}

	switch ch := sc.peek(); {
	case isWhitespace(ch):
		return sc.scanWhitespace(), nil
	case ch == '/' && sc.peek2() == '/':
		return sc.scanLineComment(), nil
	case ch == '/' && sc.peek2() == '*':
		return sc.scanBlockComment()
	case ch == '{':
		sc.advance()
		return sc.makeToken(tokenLBrace, start), nil
	case ch == '}':
		sc.advance()
		return sc.makeToken(tokenRBrace, start), nil
	case ch == '[':
		sc.advance()
		return sc.makeToken(tokenLBracket, start), nil
	case ch == ']':
		sc.advance()
		return sc.makeToken(tokenRBracket, start), nil
	case ch == '.':
		sc.advance()
		return sc.makeToken(tokenDot, start), nil
	case ch == '"':
		return sc.scanString()
	case ch == '`':
		return sc.scanRawString()
	case ch == '\'':
		return sc.scanQuotedKey()
	case isNumberStart(ch):
		return sc.scanNumber(), nil
	case isKeyStart(ch):
		return sc.scanKeyOrKeyword(), nil
	default:
		return sc.scanInvalid(), nil
	}
}

func isWhitespace(ch byte) bool {
	return ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r'
}

func (sc *scanner) scanWhitespace() token {
	start := sc.mark()
	for isWhitespace(sc.peek()) {
		sc.advance()
	}
	return sc.makeToken(tokenWhitespace, start)
}

func (sc *scanner) scanLineComment() token {
	start := sc.mark()
	for !sc.eof() && sc.peek() != '\n' && sc.peek() != '\r' {
		sc.advance()
	}
	return sc.makeToken(tokenLineComment, start)
}

func (sc *scanner) scanBlockComment() (token, error) {
	start := sc.mark()
	balance := 0

	for !sc.eof() {
		if sc.peek() == '/' && sc.peek2() == '*' {
			balance++
			sc.advance()
			sc.advance()
		} else if sc.peek() == '*' && sc.peek2() == '/' {
			balance--
			sc.advance()
			sc.advance()
			if balance == 0 {
				return sc.makeToken(tokenBlockComment, start), nil
			}
		} else {
			sc.advance()
		}
	}

	return token{}, makeError(sc.spanFrom(start), "unterminated block comment")
}

func (sc *scanner) scanString() (token, error) {
	start := sc.mark()
	sc.advance()

	for !sc.eof() {
		if sc.peek() == '\\' {
			sc.advance()
			sc.advance()
			continue
		}
		if sc.advance() == '"' {
			return sc.makeToken(tokenString, start), nil
		}
	}

	return token{}, makeError(sc.spanFrom(start), "unterminated string")
}

func (sc *scanner) scanRawString() (token, error) {
	start := sc.mark()
	sc.advance()

	for !sc.eof() {
		ch := sc.advance()
		if ch == '`' {
			return sc.makeToken(tokenString, start), nil
		}
	}

	return token{}, makeError(sc.spanFrom(start), "unterminated raw string")
}

func (sc *scanner) scanQuotedKey() (token, error) {
	start := sc.mark()
	sc.advance()

	for !sc.eof() {
		if sc.peek() == '\\' {
			sc.advance()
			sc.advance()
			continue
		}
		if sc.advance() == '\'' {
			return sc.makeToken(tokenQuotedKey, start), nil
		}
	}

	return token{}, makeError(sc.spanFrom(start), "unterminated quoted key")
}

func isKeyStart(ch byte) bool {
	return ('a' <= ch && ch <= 'z') || ('A' <= ch && ch <= 'Z') || ch == '_'
}

func isKeyContinue(ch byte) bool {
	return isKeyStart(ch) || ('0' <= ch && ch <= '9') || ch == '-'
}

func (sc *scanner) scanKeyOrKeyword() token {
	start := sc.mark()

	for isKeyContinue(sc.peek()) {
		sc.advance()
	}

	key := sc.doc[start.Offset:sc.pos]
	if key == "true" || key == "false" {
		return sc.makeToken(tokenBool, start)
	}
	if key == "nan" || key == "inf" {
		return sc.makeToken(tokenReal, start)
	}
	if key == "null" {
		return sc.makeToken(tokenNull, start)
	}

	return sc.makeToken(tokenKey, start)
}

func isNumberStart(ch byte) bool {
	return ('0' <= ch && ch <= '9') || ch == '-'
}

func isNumberContinue(ch byte) bool {
	return ('0' <= ch && ch <= '9') || ('a' <= ch && ch <= 'z') || ('A' <= ch && ch <= 'Z') || ch == '.' || ch == '+' || ch == '-'
}

func (sc *scanner) scanNumber() token {
	start := sc.mark()
	sc.advance()
	typ := tokenInt

	for isNumberContinue(sc.peek()) {
		sc.advance()
	}

	raw := sc.doc[start.Offset:sc.pos]
	if !isBasedIntLike(raw) {
		for _, ch := range raw {
			if ch == '.' || ch == 'e' || ch == 'E' {
				typ = tokenReal
				break
			}
		}
	}
	if raw == "-inf" || raw == "-nan" {
		typ = tokenReal
	}

	return sc.makeToken(typ, start)
}

func isBasedIntLike(raw string) bool {
	if len(raw) >= 2 && (raw[0:2] == "0x" || raw[0:2] == "0b") {
		return true
	}
	if len(raw) >= 3 && (raw[0:3] == "-0x" || raw[0:3] == "-0b") {
		return true
	}
	return false
}

func (sc *scanner) scanInvalid() token {
	start := sc.mark()
	sc.advance()
	for !sc.eof() && !isWhitespace(sc.peek()) && !isDelimiter(sc.peek()) {
		sc.advance()
	}
	return sc.makeToken(tokenInvalid, start)
}

func isDelimiter(ch byte) bool {
	switch ch {
	case '{', '}', '[', ']', '.', '\'', '"', '`':
		return true
	default:
		return false
	}
}
