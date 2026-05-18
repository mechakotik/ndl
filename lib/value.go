// Package ndl implements a support library for the NDL language, including
// encoding, decoding, and formatting.
package ndl

import (
	"fmt"
	"maps"
	"math"
	"math/big"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Kind represents the type of an NDL value.
type Kind int

const (
	KindInvalid Kind = iota
	KindMap
	KindArray
	KindString
	KindInt
	KindReal
	KindBool
	KindNull
)

// Pair represents a key-value pair in an NDL map.
type Pair struct {
	Key   string
	Value Value
}

// Value represents parsed NDL value. It is typed and validated, and preserves
// the original NDL representation of primitive values.
type Value struct {
	kind          Kind
	mapValue      map[string]Value
	mapValuePairs []Pair
	arrayValue    []Value
	primValue     string
}

// Kind returns the type of the NDL value.
func (v Value) Kind() Kind {
	return v.kind
}

// Map returns the underlying map of Value. If Value is not a map, returns an error.
func (v Value) Map() (map[string]Value, error) {
	if v.kind != KindMap {
		return nil, fmt.Errorf("value is not a map")
	}
	return maps.Clone(v.mapValue), nil
}

// Pairs returns the underlying map of Value as list of pairs, preserving original
// declaration order. If Value is not a map, returns an error.
func (v Value) Pairs() ([]Pair, error) {
	if v.kind != KindMap {
		return nil, fmt.Errorf("value is not a map")
	}
	return append([]Pair{}, v.mapValuePairs...), nil
}

// Array returns the underlying array of Value. If Value is not an array, returns an
// error.
func (v Value) Array() ([]Value, error) {
	if v.kind != KindArray {
		return nil, fmt.Errorf("value is not an array")
	}
	return append([]Value{}, v.arrayValue...), nil
}

// String returns the decoded string value. If Value is not a string, returns an
// error.
func (v Value) String() (string, error) {
	if v.kind != KindString {
		return "", fmt.Errorf("value is not a string")
	}
	return decodeString(token{Raw: v.primValue})
}

// Int returns the underlying int of Value as Go int64. If value kind is not an int,
// or it is out of range of Go int64, returns an error.
func (v Value) Int() (int64, error) {
	if v.kind != KindInt {
		return 0, fmt.Errorf("value is not an int")
	}
	return strconv.ParseInt(v.primValue, 0, 64)
}

// BigInt returns the underlying int of Value as Go big.Int. If Value kind is not an
// int, returns an error.
func (v Value) BigInt() (*big.Int, error) {
	if v.kind != KindInt {
		return nil, fmt.Errorf("value is not an int")
	}

	raw := v.primValue
	negative := false
	if strings.HasPrefix(raw, "-") {
		negative = true
		raw = raw[1:]
	}

	base := 10
	if strings.HasPrefix(raw, "0x") {
		base = 16
		raw = raw[2:]
	} else if strings.HasPrefix(raw, "0b") {
		base = 2
		raw = raw[2:]
	}

	res, ok := new(big.Int).SetString(raw, base)
	if !ok {
		return nil, fmt.Errorf("invalid int %s", v.primValue)
	}
	if negative {
		res.Neg(res)
	}
	return res, nil
}

// Float returns the underlying real of Value as Go float64. If Value kind is not a
// real, or it is out of range of Go float64, returns an error.
func (v Value) Float() (float64, error) {
	if v.kind != KindReal {
		return 0, fmt.Errorf("value is not a real")
	}
	return strconv.ParseFloat(v.primValue, 64)
}

// Bool returns the underlying bool of Value. If Value kind is not a bool, returns an
// error.
func (v Value) Bool() (bool, error) {
	if v.kind != KindBool {
		return false, fmt.Errorf("value is not a bool")
	}
	return v.primValue == "true", nil
}

// Raw returns raw NDL representation of primitive Value. If Value kind is a map,
// array, or invalid, returns an error.
func (v Value) Raw() (string, error) {
	if v.kind == KindMap || v.kind == KindArray || v.kind == KindInvalid {
		return "", fmt.Errorf("value is not a primitive")
	}
	return v.primValue, nil
}

// NewMap creates a map Value from mp. Pair order is unspecified because Go map
// iteration order is unspecified.
func NewMap(mp map[string]Value) Value {
	mapValue := maps.Clone(mp)
	if mapValue == nil {
		mapValue = map[string]Value{}
	}

	pairs := make([]Pair, 0, len(mp))
	for key, value := range mp {
		pairs = append(pairs, Pair{
			Key:   key,
			Value: value,
		})
	}

	return Value{
		kind:          KindMap,
		mapValue:      mapValue,
		mapValuePairs: pairs,
	}
}

// NewPairs creates a map Value from key-value pairs, preserving pair order.
// Returns an error if pairs contains duplicate keys.
func NewPairs(pairs []Pair) (Value, error) {
	mapValue := make(map[string]Value, len(pairs))
	pairCopy := make([]Pair, len(pairs))

	for idx, pair := range pairs {
		if _, ok := mapValue[pair.Key]; ok {
			return Value{}, fmt.Errorf("duplicate key %s", pair.Key)
		}
		mapValue[pair.Key] = pair.Value
		pairCopy[idx] = pair
	}

	return Value{
		kind:          KindMap,
		mapValue:      mapValue,
		mapValuePairs: pairCopy,
	}, nil
}

// NewArray creates an array Value from values.
func NewArray(values []Value) Value {
	return Value{
		kind:       KindArray,
		arrayValue: append([]Value{}, values...),
	}
}

// StringFormat controls how string values are represented in NDL document.
type StringFormat struct {
	// Use raw string representation over interpreted if possible, i.e. if
	// string does not contain `.
	PreferRaw bool

	// Escape non-printable characters in interpreted strings. Newline and
	// tab are escaped as \n and \t, and other control runes are escaped
	// as \u{...}. Ignored in raw strings.
	EscapeNonPrintable bool

	// Escape non-ASCII characters in interpreted strings as \u{...}.
	// Ignored in raw strings.
	EscapeNonASCII bool
}

// NewString creates a string Value from str with default formatting options.
// Returns an error if str is not valid UTF-8 string.
func NewString(str string) (Value, error) {
	return NewStringWithFormat(str, StringFormat{
		EscapeNonPrintable: true,
		EscapeNonASCII:     true,
	})
}

// NewStringWithFormat creates a string Value from str with custom formatting
// options. Returns an error if str is not valid UTF-8 string.
func NewStringWithFormat(str string, format StringFormat) (Value, error) {
	if !utf8.ValidString(str) {
		return Value{}, fmt.Errorf("invalid UTF-8 string")
	}
	if format.PreferRaw && !strings.ContainsRune(str, '`') {
		return Value{kind: KindString, primValue: "`" + str + "`"}, nil
	}

	builder := strings.Builder{}
	builder.Grow(len(str) + 2)
	builder.WriteByte('"')

	for _, r := range str {
		switch {
		case r == '"':
			builder.WriteString(`\"`)
		case r == '\\':
			builder.WriteString(`\\`)
		case format.EscapeNonPrintable && r == '\n':
			builder.WriteString(`\n`)
		case format.EscapeNonPrintable && r == '\t':
			builder.WriteString(`\t`)
		case format.EscapeNonPrintable && !unicode.IsPrint(r):
			fmt.Fprintf(&builder, `\u{%X}`, r)
		case format.EscapeNonASCII && r > 0x7f:
			fmt.Fprintf(&builder, `\u{%X}`, r)
		default:
			builder.WriteRune(r)
		}
	}

	builder.WriteByte('"')
	return Value{kind: KindString, primValue: builder.String()}, nil
}

// IntNotation controls how int values are represented in NDL document.
type IntNotation int

const (
	IntDecimal IntNotation = iota
	IntBinary
	IntHex
)

// NewInt creates an int Value from x in decimal notation.
func NewInt(x int64) Value {
	return NewIntWithFormat(x, IntDecimal)
}

// NewIntWithFormat creates an int Value from x in custom notation.
// Panics if notation is is not IntDecimal, IntBinary, or IntHex.
func NewIntWithFormat(x int64, notation IntNotation) Value {
	switch notation {
	case IntDecimal:
		raw := strconv.FormatInt(x, 10)
		return Value{kind: KindInt, primValue: raw}

	case IntBinary:
		raw := strconv.FormatInt(x, 2)
		if x < 0 {
			raw = "-0b" + raw[1:]
		} else {
			raw = "0b" + raw
		}
		return Value{kind: KindInt, primValue: raw}

	case IntHex:
		raw := strconv.FormatInt(x, 16)
		if x < 0 {
			raw = "-0x" + raw[1:]
		} else {
			raw = "0x" + raw
		}
		return Value{kind: KindInt, primValue: raw}

	default:
		panic("unknown int notation")
	}
}

// NewBigInt creates an int Value from x in decimal notation.
func NewBigInt(x *big.Int) Value {
	return NewBigIntWithFormat(x, IntDecimal)
}

// NewBigIntWithFormat creates an int Value from x in custom notation.
// Panics if x is nil or notation is is not IntDecimal, IntBinary, or
// IntHex.
func NewBigIntWithFormat(x *big.Int, notation IntNotation) Value {
	if x == nil {
		panic("nil big.Int")
	}

	switch notation {
	case IntDecimal:
		return Value{kind: KindInt, primValue: x.Text(10)}

	case IntBinary:
		if x.Sign() < 0 {
			raw := new(big.Int).Neg(x).Text(2)
			return Value{kind: KindInt, primValue: "-0b" + raw}
		}
		return Value{kind: KindInt, primValue: "0b" + x.Text(2)}

	case IntHex:
		if x.Sign() < 0 {
			raw := new(big.Int).Neg(x).Text(16)
			return Value{kind: KindInt, primValue: "-0x" + raw}
		}
		return Value{kind: KindInt, primValue: "0x" + x.Text(16)}

	default:
		panic("unknown int notation")
	}
}

// RealNotation controls the notation of real values in NDL document.
type RealNotation int

const (
	// Choose readable decimal/scientific notation, always lossless,
	// ignores precision.
	RealDefault RealNotation = iota

	// Decimal notation, precision applies.
	RealDecimal

	// Scientific notation, precision applies.
	RealScientific
)

// RealFormat controls how real values are represented in NDL document.
type RealFormat struct {
	// Notation of real value.
	Notation RealNotation

	// Number of digits after decimal point. If negative, representation
	// is lossless. If zero, value is rounded to zero decimal places.
	// Ignored if Notation is RealDefault.
	Precision int
}

// NewFloat creates a real Value from x with default formatting options.
func NewFloat(x float64) Value {
	return NewFloatWithFormat(x, RealFormat{})
}

// NewFloatWithFormat creates a real Value from x with custom formatting
// options. Panics if format.Notation is not RealDefault, RealDecimal, or
// RealScientific.
func NewFloatWithFormat(x float64, format RealFormat) Value {
	switch {
	case math.IsInf(x, 1):
		return Value{kind: KindReal, primValue: "inf"}
	case math.IsInf(x, -1):
		return Value{kind: KindReal, primValue: "-inf"}
	case math.IsNaN(x):
		return Value{kind: KindReal, primValue: "nan"}
	}

	raw := ""

	switch format.Notation {
	case RealDefault:
		scientific := strconv.FormatFloat(x, 'e', -1, 64)
		expIndex := strings.LastIndexByte(scientific, 'e')
		exp, err := strconv.Atoi(scientific[expIndex+1:])
		if err != nil {
			panic(err)
		}
		if exp < -6 || exp > 6 {
			raw = scientific
		} else {
			raw = strconv.FormatFloat(x, 'f', -1, 64)
		}

	case RealDecimal:
		precision := format.Precision
		if precision < 0 {
			precision = -1
		}
		raw = strconv.FormatFloat(x, 'f', precision, 64)

	case RealScientific:
		precision := format.Precision
		if precision < 0 {
			precision = -1
		}
		raw = strconv.FormatFloat(x, 'e', precision, 64)

	default:
		panic("unknown real notation")
	}

	if expIndex := strings.LastIndexByte(raw, 'e'); expIndex != -1 {
		if raw[expIndex+1] == '+' {
			raw = raw[:expIndex+1] + raw[expIndex+2:]
		}
	} else if !strings.Contains(raw, ".") {
		raw += ".0"
	}

	return Value{kind: KindReal, primValue: raw}
}

// NewBool creates a bool Value from b.
func NewBool(b bool) Value {
	return Value{kind: KindBool, primValue: strconv.FormatBool(b)}
}

// NewNull creates a null Value.
func NewNull() Value {
	return Value{kind: KindNull, primValue: "null"}
}

// NewRaw creates a primitive Value of given Kind from raw NDL representation.
// Returns an error if raw is not valid NDL representation of primitive value
// of kind. Panics if kind is not KindString, KindInt, KindReal, KindBool, or
// KindNull.
func NewRaw(kind Kind, raw string) (Value, error) {
	switch kind {
	case KindString:
		_, err := decodeString(token{Raw: raw})
		if err != nil {
			return Value{}, err
		}
		return Value{kind: kind, primValue: raw}, nil

	case KindInt:
		if !isValidInt(raw) {
			return Value{}, fmt.Errorf("invalid int %s", raw)
		}
		return Value{kind: kind, primValue: raw}, nil

	case KindReal:
		if !isValidReal(raw) {
			return Value{}, fmt.Errorf("invalid real %s", raw)
		}
		return Value{kind: kind, primValue: raw}, nil

	case KindBool:
		if raw != "true" && raw != "false" {
			return Value{}, fmt.Errorf("invalid bool %s", raw)
		}
		return Value{kind: kind, primValue: raw}, nil

	case KindNull:
		if raw != "null" {
			return Value{}, fmt.Errorf("invalid null %s", raw)
		}
		return Value{kind: kind, primValue: raw}, nil

	default:
		panic("invalid kind")
	}
}

func kindName(kind Kind) string {
	switch kind {
	case KindMap:
		return "map"
	case KindArray:
		return "array"
	case KindString:
		return "string"
	case KindInt:
		return "int"
	case KindReal:
		return "real"
	case KindBool:
		return "bool"
	case KindNull:
		return "null"
	default:
		return "invalid"
	}
}
