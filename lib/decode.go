package ndl

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

var (
	decimalIntLiteralRe = regexp.MustCompile(`^-?(0|[1-9][0-9]*)$`)
	hexIntLiteralRe     = regexp.MustCompile(`^-?0x[0-9A-Fa-f]+$`)
	binaryIntLiteralRe  = regexp.MustCompile(`^-?0b[01]+$`)

	dotRealLiteralRe = regexp.MustCompile(`^-?(0|[1-9][0-9]*)\.[0-9]+$`)
	eRealLiteralRe   = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?[eE]-?[0-9]+$`)
)

// Decode parses an NDL document from string into a Value. Returns an error if any kind
// of parsing error occurs. If non-nil error is returned, its type is always ErrorList.
func Decode(doc string) (Value, error) {
	tokens, err := tokenize(doc)
	if err != nil {
		return Value{}, err
	}

	root, err := makeAST(tokens)
	if err != nil {
		return Value{}, err
	}

	value, err := valueFromAST(root)
	if err != nil {
		return Value{}, err
	}

	finalizeValue(&value)
	return value, nil
}

func valueFromAST(node astNode) (Value, error) {
	switch node := node.(type) {
	case *mapNode:
		return mapValueFromAST(node)
	case *arrayNode:
		return arrayValueFromAST(node)
	case *stringNode, *intNode, *realNode, *boolNode, *nullNode:
		return primitiveValueFromAST(node)
	default:
		return Value{}, makeError(node.GetSpan(), "invalid value")
	}
}

func mapValueFromAST(node *mapNode) (Value, error) {
	value := newMapValue()
	errs := ErrorList{}

	for _, pair := range node.Pairs {
		path, pathErr := decodeKeyPath(pair.KeyPath)
		child, childErr := valueFromAST(pair.Value)
		errs.Add(pathErr)
		errs.Add(childErr)
		if pathErr != nil || childErr != nil {
			continue
		}
		errs.Add(insertValueAtPath(&value, path, child))
	}

	return value, errs.Err()
}

func arrayValueFromAST(node *arrayNode) (Value, error) {
	values := make([]Value, 0, len(node.Values))
	errs := ErrorList{}

	for _, valueNode := range node.Values {
		value, err := valueFromAST(valueNode)
		errs.Add(err)
		if err == nil {
			values = append(values, value)
		}
	}

	return Value{
		kind:       KindArray,
		arrayValue: values,
	}, errs.Err()
}

func decodeKeyPath(node keyPathNode) ([]pathElement, error) {
	path := make([]pathElement, 0, len(node.Keys))
	errs := ErrorList{}

	for _, tok := range node.Keys {
		key, err := decodeKey(tok)
		errs.Add(err)
		if err != nil {
			continue
		}
		path = append(path, pathElement{
			Key:   key,
			Index: -1,
			Span:  tok.Span,
		})
	}

	return path, errs.Err()
}

func decodeKey(tok token) (string, error) {
	switch tok.Type {
	case tokenKey:
		return tok.Raw, nil
	case tokenQuotedKey:
		return decodeInterpreted(tok, '\'')
	default:
		return "", makeError(tok.Span, "expected key, got %s", tok.Raw)
	}
}

func newMapValue() Value {
	return Value{
		kind:          KindMap,
		mapValue:      map[string]Value{},
		mapValuePairs: []Pair{},
	}
}

func insertValueAtPath(value *Value, path []pathElement, child Value) error {
	if len(path) == 0 {
		return makeError(Span{}, "expected key path")
	}

	elem := path[0]
	if len(path) == 1 {
		return mergeMapEntry(value, elem.Key, child, elem.Span)
	}

	existing, ok := value.mapValue[elem.Key]
	if !ok {
		existing = newMapValue()
		value.mapValue[elem.Key] = existing
		value.mapValuePairs = append(value.mapValuePairs, Pair{Key: elem.Key})
	} else if existing.kind != KindMap {
		return makeError(elem.Span, "key %s redeclared as map", elem.Key)
	}

	if err := insertValueAtPath(&existing, path[1:], child); err != nil {
		return err
	}
	value.mapValue[elem.Key] = existing
	return nil
}

func mergeMapEntry(value *Value, key string, child Value, span Span) error {
	existing, ok := value.mapValue[key]
	if !ok {
		value.mapValue[key] = child
		value.mapValuePairs = append(value.mapValuePairs, Pair{Key: key, Value: child})
		return nil
	}
	if existing.kind != KindMap || child.kind != KindMap {
		return makeError(span, "key %s redeclared", key)
	}
	if err := mergeMapValues(&existing, child, span); err != nil {
		return err
	}
	value.mapValue[key] = existing
	return nil
}

func mergeMapValues(dst *Value, src Value, span Span) error {
	errs := ErrorList{}
	for _, pair := range src.mapValuePairs {
		errs.Add(mergeMapEntry(dst, pair.Key, src.mapValue[pair.Key], span))
	}
	return errs.Err()
}

func finalizeValue(value *Value) {
	switch value.kind {
	case KindMap:
		for idx, pair := range value.mapValuePairs {
			child := value.mapValue[pair.Key]
			finalizeValue(&child)
			value.mapValue[pair.Key] = child
			value.mapValuePairs[idx].Value = child
		}
	case KindArray:
		for idx := range value.arrayValue {
			finalizeValue(&value.arrayValue[idx])
		}
	}
}

func primitiveValueFromAST(node astNode) (Value, error) {
	switch node := node.(type) {
	case *stringNode:
		_, err := decodeString(node.Token)
		return Value{kind: KindString, primValue: node.Token.Raw}, err

	case *intNode:
		var err error
		if !isValidInt(node.Token.Raw) {
			err = makeError(node.Token.Span, "invalid int literal %s", node.Token.Raw)
		}
		return Value{kind: KindInt, primValue: node.Token.Raw}, err

	case *realNode:
		var err error
		if !isValidReal(node.Token.Raw) {
			err = makeError(node.Token.Span, "invalid real literal %s", node.Token.Raw)
		}
		return Value{kind: KindReal, primValue: node.Token.Raw}, err

	case *boolNode:
		var err error
		if node.Token.Raw != "true" && node.Token.Raw != "false" {
			err = makeError(node.Token.Span, "invalid bool literal %s", node.Token.Raw)
		}
		return Value{kind: KindBool, primValue: node.Token.Raw}, err

	case *nullNode:
		return Value{kind: KindNull, primValue: "null"}, nil

	default:
		return Value{}, makeError(node.GetSpan(), "invalid value")
	}
}

func isValidInt(raw string) bool {
	return decimalIntLiteralRe.MatchString(raw) ||
		hexIntLiteralRe.MatchString(raw) ||
		binaryIntLiteralRe.MatchString(raw)
}

func isValidReal(raw string) bool {
	switch raw {
	case "inf", "-inf", "nan":
		return true
	case "-nan":
		return false
	}
	return dotRealLiteralRe.MatchString(raw) ||
		eRealLiteralRe.MatchString(raw)
}

func decodeString(tok token) (string, error) {
	if len(tok.Raw) < 2 {
		return "", makeError(tok.Span, "invalid string literal")
	}

	switch tok.Raw[0] {
	case '`':
		res := tok.Raw[1 : len(tok.Raw)-1]
		if !utf8.ValidString(res) {
			return "", makeError(tok.Span, "invalid UTF-8 in string literal")
		}
		return res, nil
	case '"':
		return decodeInterpreted(tok, '"')
	default:
		return "", makeError(tok.Span, "invalid string literal")
	}
}

func decodeInterpreted(tok token, quote byte) (string, error) {
	if len(tok.Raw) < 2 || tok.Raw[0] != quote || tok.Raw[len(tok.Raw)-1] != quote {
		return "", makeError(tok.Span, "invalid string literal")
	}

	content := tok.Raw[1 : len(tok.Raw)-1]
	if strings.IndexByte(content, '\\') == -1 {
		if !utf8.ValidString(content) {
			return "", makeError(tok.Span, "invalid UTF-8 in string literal")
		}
		return content, nil
	}

	var builder strings.Builder
	builder.Grow(len(content))
	for idx := 0; idx < len(content); idx++ {
		ch := content[idx]
		if ch != '\\' {
			builder.WriteByte(ch)
			continue
		}

		idx++
		if idx >= len(content) {
			return "", makeError(tok.Span, "invalid escape sequence")
		}
		switch content[idx] {
		case 'n':
			builder.WriteByte('\n')
		case 't':
			builder.WriteByte('\t')
		case '\'':
			builder.WriteByte('\'')
		case '"':
			builder.WriteByte('"')
		case '\\':
			builder.WriteByte('\\')
		case 'u':
			r, end, err := decodeUnicodeEscape(content, idx+1)
			if err != nil {
				return "", wrapError(tok.Span, err, "invalid unicode escape")
			}
			builder.WriteRune(r)
			idx = end
		default:
			return "", makeError(tok.Span, "invalid escape sequence \\%c", content[idx])
		}
	}

	res := builder.String()
	if !utf8.ValidString(res) {
		return "", makeError(tok.Span, "invalid UTF-8 in string literal")
	}
	return res, nil
}

func decodeUnicodeEscape(content string, start int) (rune, int, error) {
	if start >= len(content) || content[start] != '{' {
		return 0, 0, fmt.Errorf("expected {")
	}

	hexStart := start + 1
	hexEnd := hexStart
	for hexEnd < len(content) && content[hexEnd] != '}' {
		if !isHexDigit(content[hexEnd]) {
			return 0, 0, fmt.Errorf("expected hex digit")
		}
		hexEnd++
	}
	if hexEnd == len(content) {
		return 0, 0, fmt.Errorf("expected }")
	}
	if hexEnd == hexStart {
		return 0, 0, fmt.Errorf("expected hex digit")
	}

	codepoint, err := strconv.ParseInt(content[hexStart:hexEnd], 16, 32)
	if err != nil {
		return 0, 0, err
	}
	r := rune(codepoint)
	if !utf8.ValidRune(r) {
		return 0, 0, fmt.Errorf("invalid code point")
	}
	return r, hexEnd, nil
}

func isHexDigit(ch byte) bool {
	return ('0' <= ch && ch <= '9') || ('a' <= ch && ch <= 'f') || ('A' <= ch && ch <= 'F')
}
