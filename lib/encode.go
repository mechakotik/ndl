package ndl

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

var bareKeyRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_-]*$`)

// Encode encodes Value to NDL document. Panics if value is invalid
// / unknown kind or contains invalid / unknown kind value nested.
func Encode(value Value) string {
	builder := strings.Builder{}
	writeValue(&builder, value, true)
	doc := builder.String()

	doc, err := Format(doc)
	if err != nil {
		panic(err)
	}

	return doc
}

func writeValue(builder *strings.Builder, value Value, isRoot bool) {
	switch value.kind {
	case KindMap:
		writeMap(builder, value, isRoot)
	case KindArray:
		writeArray(builder, value)
	case KindInvalid:
		panic("invalid value")
	default:
		builder.WriteString(value.primValue)
	}
}

func writeMap(builder *strings.Builder, value Value, isRoot bool) {
	if !isRoot {
		builder.WriteString("{ ")
	}

	for _, pair := range value.mapValuePairs {
		keyPath := encodeKey(pair.Key)
		for pair.Value.kind == KindMap && len(pair.Value.mapValuePairs) == 1 {
			pair = pair.Value.mapValuePairs[0]
			keyPath += "." + encodeKey(pair.Key)
		}

		builder.WriteString(keyPath)
		builder.WriteByte(' ')
		writeValue(builder, pair.Value, false)
		builder.WriteByte(' ')
	}

	if !isRoot {
		builder.WriteByte('}')
	}
}

func writeArray(builder *strings.Builder, value Value) {
	builder.WriteString("[ ")
	for _, elem := range value.arrayValue {
		writeValue(builder, elem, false)
		builder.WriteByte(' ')
	}
	builder.WriteByte(']')
}

func encodeKey(key string) string {
	if isBareKey(key) {
		return key
	}
	return encodeInterpretedString(key, '\'', StringFormat{
		KeepNonASCII: true,
	})
}

func isBareKey(key string) bool {
	return bareKeyRe.MatchString(key) &&
		key != "null" &&
		key != "true" &&
		key != "false" &&
		key != "inf" &&
		key != "nan"
}

func encodeInterpretedString(str string, enclosing rune, format StringFormat) string {
	builder := strings.Builder{}
	builder.Grow(len(str) + 2)
	builder.WriteRune(enclosing)

	for _, r := range str {
		switch {
		case r == enclosing:
			builder.WriteByte('\\')
			builder.WriteRune(r)
		case r == '\\':
			builder.WriteString(`\\`)
		case !format.KeepNonPrintable && r == '\n':
			builder.WriteString(`\n`)
		case !format.KeepNonPrintable && r == '\t':
			builder.WriteString(`\t`)
		case !format.KeepNonPrintable && !unicode.IsPrint(r):
			fmt.Fprintf(&builder, `\u{%X}`, r)
		case !format.KeepNonASCII && r > 0x7f:
			fmt.Fprintf(&builder, `\u{%X}`, r)
		default:
			builder.WriteRune(r)
		}
	}

	builder.WriteRune(enclosing)
	return builder.String()
}
