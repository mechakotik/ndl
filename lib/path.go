package ndl

import (
	"fmt"
	"strconv"
)

// Get returns the value at path. String path elements index maps by key, and
// int path elements index arrays.
//
// If the path does not exist, or a path element does not match the current
// value kind, returns null value. Panics if a path element is not string or
// int.
func (v Value) Get(path ...any) Value {
	for _, elem := range path {
		switch elem := elem.(type) {
		case string:
			if v.kind != KindMap {
				return NewNull()
			}
			child, ok := v.mapValue[elem]
			if !ok {
				return NewNull()
			}
			v = child

		case int:
			if v.kind != KindArray || elem < 0 || elem >= len(v.arrayValue) {
				return NewNull()
			}
			v = v.arrayValue[elem]

		default:
			panic("path element is not string or int")
		}
	}

	return v
}

// ByPath returns the value at a dotted path. Bare path elements index maps by
// key, decimal integer elements index arrays, and single-quoted elements index
// maps by decoded key.
//
// If the path is syntactically invalid, returns an error. If the path does not
// exist, or a path element does not match the current value kind, returns null
// value and nil error.
func (v Value) ByPath(path string) (Value, error) {
	elements, err := parseValuePath(path)
	if err != nil {
		return Value{}, err
	}

	for _, elem := range elements {
		if elem.index >= 0 {
			if v.kind != KindArray || elem.index >= len(v.arrayValue) {
				return NewNull(), nil
			}
			v = v.arrayValue[elem.index]
			continue
		}

		if v.kind != KindMap {
			return NewNull(), nil
		}
		child, ok := v.mapValue[elem.key]
		if !ok {
			return NewNull(), nil
		}
		v = child
	}

	return v, nil
}

type pathElement struct {
	key   string
	index int
	span  Span
}

func parseValuePath(path string) ([]pathElement, error) {
	if path == "" {
		return nil, nil
	}

	elements := []pathElement{}
	pos := 0
	for {
		elem, next, err := parseValuePathElement(path, pos)
		if err != nil {
			return nil, err
		}
		elements = append(elements, elem)
		pos = next

		if pos == len(path) {
			return elements, nil
		}
		if path[pos] != '.' {
			return nil, pathError(pos, "expected dot")
		}
		pos++
		if pos == len(path) {
			return nil, pathError(pos, "expected path element")
		}
	}
}

func parseValuePathElement(path string, pos int) (pathElement, int, error) {
	if path[pos] == '\'' {
		key, next, err := parseQuotedPathKey(path, pos)
		return pathElement{key: key, index: -1}, next, err
	}

	start := pos
	for pos < len(path) && path[pos] != '.' {
		pos++
	}
	raw := path[start:pos]
	if raw == "" {
		return pathElement{}, 0, pathError(start, "expected path element")
	}

	if decimalIntLiteralRe.MatchString(raw) {
		if raw[0] == '-' {
			return pathElement{}, 0, pathError(start, "negative array index %s", raw)
		}
		index, err := strconv.Atoi(raw)
		if err != nil {
			return pathElement{}, 0, pathError(start, "invalid array index %s", raw)
		}
		return pathElement{index: index}, pos, nil
	}
	if !isBareKey(raw) {
		return pathElement{}, 0, pathError(start, "invalid path element %s", raw)
	}
	return pathElement{key: raw, index: -1}, pos, nil
}

func parseQuotedPathKey(path string, pos int) (string, int, error) {
	start := pos
	pos++

	for pos < len(path) {
		switch path[pos] {
		case '\\':
			pos++
			if pos == len(path) {
				return "", 0, pathError(start, "unterminated quoted key")
			}
		case '\'':
			pos++
			key, err := decodeInterpreted(token{Raw: path[start:pos]}, '\'')
			if err != nil {
				return "", 0, pathError(start, "invalid quoted key: %s", err)
			}
			return key, pos, nil
		}
		pos++
	}

	return "", 0, pathError(start, "unterminated quoted key")
}

func pathError(pos int, format string, args ...any) error {
	return fmt.Errorf("invalid path at byte %d: %s", pos, fmt.Sprintf(format, args...))
}
