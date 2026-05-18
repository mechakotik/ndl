package ndl

type astNode interface {
	GetSpan() Span
}

type valueNode interface {
	astNode
	valueNode()
}

type keyPathNode struct {
	Keys []token
	Span Span
}

func (n *keyPathNode) GetSpan() Span { return n.Span }

type pairNode struct {
	KeyPath keyPathNode
	Value   valueNode
	Span    Span
}

func (n *pairNode) GetSpan() Span { return n.Span }

type mapNode struct {
	Pairs []pairNode
	Span  Span
}

func (n *mapNode) GetSpan() Span { return n.Span }
func (n *mapNode) valueNode()    {}

type arrayNode struct {
	Values []valueNode
	Span   Span
}

func (n *arrayNode) GetSpan() Span { return n.Span }
func (n *arrayNode) valueNode()    {}

type stringNode struct {
	Token token
}

func (n *stringNode) GetSpan() Span { return n.Token.Span }
func (n *stringNode) valueNode()    {}

type intNode struct {
	Token token
}

func (n *intNode) GetSpan() Span { return n.Token.Span }
func (n *intNode) valueNode()    {}

type realNode struct {
	Token token
}

func (n *realNode) GetSpan() Span { return n.Token.Span }
func (n *realNode) valueNode()    {}

type boolNode struct {
	Token token
}

func (n *boolNode) GetSpan() Span { return n.Token.Span }
func (n *boolNode) valueNode()    {}

type nullNode struct {
	Token token
}

func (n *nullNode) GetSpan() Span { return n.Token.Span }
func (n *nullNode) valueNode()    {}

type invalidNode struct {
	Span Span
}

func (n *invalidNode) GetSpan() Span { return n.Span }
func (n *invalidNode) valueNode()    {}

type astParser struct {
	Tokens []token
	Pos    int
	Errs   ErrorList
}

func makeAST(tokens []token) (astNode, error) {
	parser := &astParser{
		Tokens: tokens,
		Errs:   ErrorList{},
	}

	doc := parser.parseDocument()
	return doc, parser.Errs.Err()
}

func (p *astParser) peek() token {
	if p.Pos >= len(p.Tokens) {
		return token{Type: tokenEOF}
	}
	return p.Tokens[p.Pos]
}

func (p *astParser) next() token {
	if p.Pos >= len(p.Tokens) {
		return token{Type: tokenEOF}
	}
	res := p.Tokens[p.Pos]
	p.Pos++
	return res
}

func (p *astParser) addError(span Span, format string, args ...any) {
	p.Errs.Add(makeError(span, format, args...))
}

func (p *astParser) skipTrivia() {
	for {
		switch p.peek().Type {
		case tokenWhitespace, tokenLineComment, tokenBlockComment:
			p.next()
		default:
			return
		}
	}
}

func (p *astParser) parseDocument() astNode {
	p.skipTrivia()
	startTok := p.peek()

	if startTok.Type == tokenEOF {
		return &mapNode{
			Pairs: []pairNode{},
			Span:  startTok.Span,
		}
	}

	var res astNode
	switch startTok.Type {
	case tokenKey, tokenQuotedKey:
		res = p.parseMap(true)
	case tokenLBrace:
		p.addError(startTok.Span, "root map braces must be omitted")
		res = p.parseMap(false)
	case tokenLBracket, tokenString, tokenInt, tokenReal, tokenBool, tokenNull:
		res = p.parseValue()
	default:
		p.addError(startTok.Span, "unexpected %s", startTok.Raw)
		res = &invalidNode{startTok.Span}
	}

	p.skipTrivia()
	if p.peek().Type != tokenEOF {
		p.addError(p.peek().Span, "expected EOF, got %s", p.peek().Raw)
	}

	return res
}

func (p *astParser) parseMap(isRoot bool) astNode {
	p.skipTrivia()
	pairs := []pairNode{}
	span := p.peek().Span

	if !isRoot {
		lbrace := p.next()
		if lbrace.Type != tokenLBrace {
			p.addError(lbrace.Span, "expected {, got %s", lbrace.Raw)
			return &invalidNode{span}
		}
	}

	for {
		p.skipTrivia()
		tok := p.peek()
		if tok.Type != tokenEOF {
			span.End = tok.Span.End
		}

		if tok.Type == tokenEOF {
			if !isRoot {
				p.addError(tok.Span, "unterminated map, expected }")
			}
			break
		}
		if !isRoot && tok.Type == tokenRBrace {
			p.next()
			break
		}

		keyPathOrInvalid := p.parseKeyPath()
		keyPath, ok := keyPathOrInvalid.(*keyPathNode)
		if !ok {
			break
		}
		span.End = keyPath.Span.End

		valueOrInvalid := p.parseValue()
		value, ok := valueOrInvalid.(valueNode)
		if !ok {
			break
		}
		span.End = value.GetSpan().End

		pairs = append(pairs, pairNode{
			KeyPath: *keyPath,
			Value:   value,
			Span: Span{
				Start: keyPath.Span.Start,
				End:   value.GetSpan().End,
			},
		})
	}

	return &mapNode{
		Pairs: pairs,
		Span:  span,
	}
}

func (p *astParser) parseArray() astNode {
	p.skipTrivia()
	values := []valueNode{}
	span := p.peek().Span

	lbracket := p.next()
	if lbracket.Type != tokenLBracket {
		p.addError(lbracket.Span, "expected [, got %s", lbracket.Raw)
	}

	for {
		p.skipTrivia()
		if p.peek().Type == tokenRBracket {
			span.End = p.next().Span.End
			break
		}
		if p.peek().Type == tokenEOF {
			p.addError(p.peek().Span, "unterminated array, expected ]")
			break
		}
		if p.peek().Type == tokenRBrace {
			p.addError(p.peek().Span, "unterminated array, expected ], found }")
			break
		}

		valueOrInvalid := p.parseValue()
		value, ok := valueOrInvalid.(valueNode)
		span.End = valueOrInvalid.GetSpan().End
		if !ok {
			break
		}

		values = append(values, value)
	}

	return &arrayNode{
		Values: values,
		Span:   span,
	}
}

func (p *astParser) parseKeyPath() astNode {
	p.skipTrivia()
	keys := []token{}
	span := p.peek().Span

	for {
		tok := p.next()
		span.End = tok.Span.End
		if tok.Type != tokenQuotedKey && tok.Type != tokenKey {
			p.addError(tok.Span, "expected key, got %s", tok.Raw)
			return &invalidNode{span}
		}

		keys = append(keys, tok)

		if p.peek().Type == tokenDot {
			span.End = p.next().Span.End
			continue
		} else {
			break
		}
	}

	return &keyPathNode{
		Keys: keys,
		Span: span,
	}
}

func (p *astParser) parseValue() astNode {
	p.skipTrivia()
	tok := p.peek()

	switch tok.Type {
	case tokenString:
		return &stringNode{Token: p.next()}
	case tokenInt:
		return &intNode{Token: p.next()}
	case tokenReal:
		return &realNode{Token: p.next()}
	case tokenBool:
		return &boolNode{Token: p.next()}
	case tokenNull:
		return &nullNode{Token: p.next()}
	case tokenLBrace:
		return p.parseMap(false)
	case tokenLBracket:
		return p.parseArray()
	case tokenEOF, tokenRBrace, tokenRBracket:
		p.addError(tok.Span, "expected value, got %s", tok.Raw)
		return &invalidNode{tok.Span}
	default:
		p.next()
		p.addError(tok.Span, "expected value, got %s", tok.Raw)
		return &invalidNode{tok.Span}
	}
}
