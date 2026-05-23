package ndl

import (
	"strings"
	"unicode/utf8"
)

func Format(doc string) (string, error) {
	tokens, err := tokenize(doc)
	if err != nil {
		return doc, err
	}

	formatter := newFormatter(tokens)
	return formatter.formatDocument(), nil
}

type formatter struct {
	tokens []token
	pos    int
}

type formatKind int

const (
	formatPrimitive formatKind = iota
	formatMap
	formatArray
	formatPair
	formatCommentKind
)

type formatted struct {
	inline          string
	block           string
	mapBody         string
	kind            formatKind
	open            string
	close           string
	blankLineBefore bool
}

func newFormatter(tokens []token) *formatter {
	filtered := make([]token, 0, len(tokens))
	for _, tok := range tokens {
		if tok.Type != tokenEOF {
			filtered = append(filtered, tok)
		}
	}
	return &formatter{tokens: filtered}
}

func (f *formatter) peek() token {
	if f.pos >= len(f.tokens) {
		return token{Type: tokenEOF}
	}
	return f.tokens[f.pos]
}

func (f *formatter) next() token {
	tok := f.peek()
	if f.pos < len(f.tokens) {
		f.pos++
	}
	return tok
}

func (f *formatter) formatDocument() string {
	f.skipWhitespace()

	if f.documentIsRootMap() {
		items, _ := f.formatItems(tokenEOF, 0, true)
		return joinMultiline(items)
	}

	if f.peek().Type == tokenLBrace {
		f.next()
		items, _ := f.formatItems(tokenRBrace, 0, true)
		return joinMultiline(items)
	}

	items, _ := f.formatItems(tokenEOF, 0, false)
	return joinMultiline(items)
}

func (f *formatter) documentIsRootMap() bool {
	for idx := f.pos; idx < len(f.tokens); idx++ {
		tok := f.tokens[idx]
		if tok.Type == tokenWhitespace || isCommentToken(tok) {
			continue
		}
		return tok.Type == tokenKey || tok.Type == tokenQuotedKey
	}
	return true
}

func (f *formatter) formatValue(indent int) formatted {
	f.skipWhitespace()

	switch f.peek().Type {
	case tokenLBrace:
		return f.formatMap(indent)
	case tokenLBracket:
		return f.formatArray(indent)
	case tokenLineComment, tokenBlockComment:
		return f.formatComment()
	case tokenEOF:
		return formatted{}
	default:
		tok := f.next()
		inline := tok.Raw
		if tok.Type == tokenString && countLineBreaks(tok.Raw) != 0 {
			inline = ""
		}
		return formatted{
			inline: inline,
			block:  tok.Raw,
			kind:   formatPrimitive,
		}
	}
}

func (f *formatter) formatMap(indent int) formatted {
	open := ""
	if f.peek().Type == tokenLBrace {
		open = f.next().Raw
	}

	items, close := f.formatItems(tokenRBrace, indent+1, true)
	body := formatIndentedItems(items, indent+1)
	inlineParts := make([]string, 0, len(items))
	inlineOK := true
	for _, item := range items {
		inlineOK = inlineOK && item.kind == formatPair && item.inline != ""
		inlineParts = append(inlineParts, item.inline)
	}

	inline := ""
	if open != "" && close != "" {
		inlineCandidate := open + close
		if len(inlineParts) != 0 {
			inlineCandidate = open + " " + strings.Join(inlineParts, " ") + " " + close
		}
		if inlineOK && utf8.RuneCountInString(inlineCandidate) <= 60 {
			inline = inlineCandidate
		}
	}

	block := open
	if body != "" {
		block += body + "\n" + tabs(indent)
	}
	block += close

	return formatted{
		inline:  inline,
		block:   block,
		mapBody: body,
		kind:    formatMap,
		open:    open,
		close:   close,
	}
}

func (f *formatter) formatItems(end tokenType, indent int, inMap bool) ([]formatted, string) {
	items := []formatted{}
	for f.peek().Type != tokenEOF && f.peek().Type != end {
		lineBreaks := f.skipWhitespace()
		if f.peek().Type == tokenEOF || f.peek().Type == end {
			break
		}

		var item formatted
		if isCommentToken(f.peek()) {
			item = f.formatComment()
		} else if inMap && isKeyToken(f.peek()) {
			item = f.formatPair(indent)
		} else {
			item = f.formatValue(indent)
		}
		item.blankLineBefore = lineBreaks >= 2 && len(items) != 0
		items = append(items, item)
	}

	close := ""
	f.skipWhitespace()
	if f.peek().Type == end {
		close = f.next().Raw
	}
	return items, close
}

func (f *formatter) formatPair(indent int) formatted {
	key := f.formatKeyPath()
	value := f.formatValue(indent)

	inline := key
	block := key
	if value.inline != "" {
		inline += " " + value.inline
		block += " " + chooseBlock(value)
	} else {
		inline = ""
		block += " " + chooseBlock(value)
	}

	if value.kind != formatPrimitive {
		inline = ""
	}

	return formatted{
		inline: inline,
		block:  block,
		kind:   formatPair,
	}
}

func (f *formatter) formatKeyPath() string {
	var builder strings.Builder
	for {
		builder.WriteString(f.next().Raw)
		if f.peek().Type != tokenDot {
			return builder.String()
		}
		builder.WriteString(f.next().Raw)
	}
}

func (f *formatter) formatArray(indent int) formatted {
	open := ""
	if f.peek().Type == tokenLBracket {
		open = f.next().Raw
	}

	items, close := f.formatItems(tokenRBracket, indent+1, false)
	inlineParts := make([]string, 0, len(items))
	inlineOK := true
	allMaps := len(items) != 0
	for _, item := range items {
		inlineOK = inlineOK && item.kind == formatPrimitive && item.inline != ""
		allMaps = allMaps && item.kind == formatMap
		inlineParts = append(inlineParts, item.inline)
	}

	inline := ""
	if open != "" && close != "" {
		inlineCandidate := open + close
		if len(inlineParts) != 0 {
			inlineCandidate = open + " " + strings.Join(inlineParts, " ") + " " + close
		}
		if inlineOK && utf8.RuneCountInString(inlineCandidate) <= 60 {
			inline = inlineCandidate
		}
	}

	block := ""
	if allMaps && open != "" && close != "" {
		block = formatArrayOfMaps(open, close, items, indent)
	} else {
		block = open
		body := formatIndentedItems(items, indent+1)
		if body != "" {
			block += body + "\n" + tabs(indent)
		}
		block += close
	}

	return formatted{
		inline: inline,
		block:  block,
		kind:   formatArray,
		open:   open,
		close:  close,
	}
}

func (f *formatter) formatComment() formatted {
	text := formatComment(f.next().Raw)
	return formatted{
		block: text,
		kind:  formatCommentKind,
	}
}

func (f *formatter) skipWhitespace() int {
	lineBreaks := 0
	for f.peek().Type == tokenWhitespace {
		lineBreaks += countLineBreaks(f.next().Raw)
	}
	return lineBreaks
}

func joinMultiline(items []formatted) string {
	lines := []string{}
	for _, item := range items {
		if item.blankLineBefore && len(lines) != 0 {
			lines = append(lines, "")
		}
		lines = append(lines, chooseBlock(item))
	}
	return strings.Join(lines, "\n")
}

func chooseBlock(item formatted) string {
	if item.inline != "" {
		return item.inline
	}
	return item.block
}

func formatIndentedItems(items []formatted, indent int) string {
	var builder strings.Builder
	for idx, item := range items {
		if idx != 0 && item.blankLineBefore {
			builder.WriteByte('\n')
		}
		builder.WriteByte('\n')
		builder.WriteString(tabs(indent))
		builder.WriteString(chooseBlock(item))
	}
	return builder.String()
}

func formatArrayOfMaps(open string, close string, items []formatted, indent int) string {
	var builder strings.Builder
	builder.WriteString(open)
	builder.WriteByte(' ')
	builder.WriteString(items[0].open)
	for idx, item := range items {
		if idx != 0 {
			builder.WriteByte(' ')
			builder.WriteString(item.open)
		}
		builder.WriteString(unindentOneTab(item.mapBody))
		builder.WriteByte('\n')
		builder.WriteString(tabs(indent))
		builder.WriteString(item.close)
	}
	builder.WriteByte(' ')
	builder.WriteString(close)
	return builder.String()
}

func tabs(indent int) string {
	return strings.Repeat("\t", indent)
}

func unindentOneTab(text string) string {
	return strings.ReplaceAll(text, "\n\t", "\n")
}

func isCommentToken(tok token) bool {
	return tok.Type == tokenLineComment || tok.Type == tokenBlockComment
}

func isKeyToken(tok token) bool {
	return tok.Type == tokenKey || tok.Type == tokenQuotedKey
}

func formatComment(raw string) string {
	if strings.HasPrefix(raw, "//") {
		content := strings.TrimSpace(raw[2:])
		if content == "" {
			return "//"
		}
		return "// " + content
	}

	if strings.HasPrefix(raw, "/*") && strings.HasSuffix(raw, "*/") {
		content := raw[2 : len(raw)-2]
		if strings.ContainsAny(content, "\r\n") {
			return raw
		}
		content = strings.TrimSpace(content)
		if content == "" {
			return "/* */"
		}
		return "/* " + content + " */"
	}

	return raw
}

func countLineBreaks(raw string) int {
	lineBreaks := 0
	for idx := 0; idx < len(raw); idx++ {
		switch raw[idx] {
		case '\n':
			lineBreaks++
		case '\r':
			lineBreaks++
			if idx+1 < len(raw) && raw[idx+1] == '\n' {
				idx++
			}
		}
	}
	return lineBreaks
}
