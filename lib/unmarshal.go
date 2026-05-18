package ndl

import (
	"fmt"
	"reflect"
	"strings"
)

var valueType = reflect.TypeOf(Value{})

// Unmarshal parses an NDL document and stores the result in the value pointed to by
// out. Out must be a non-nil pointer.
//
// NDL maps unmarshal into structs, maps with string keys, interfaces, or Value.
// Struct fields are matched by exported field name or by the name in an ndl tag,
// fields tagged with ndl:"-" are ignored. NDL arrays unmarshal into slices,
// arrays, interfaces, or Value.
//
// NDL strings unmarshal into string, ints into signed or unsigned integer types,
// reals into float32 or float64, bools into bool, and null into the zero value of
// the destination. Pointer destinations are allocated as needed, except null sets
// them to nil.
//
// Interface destinations receive map[string]any for maps, []any for arrays, string
// for strings, int64 for ints, float64 for reals, bool for bools, and nil for null.
func Unmarshal(data []byte, out any) error {
	if out == nil {
		return fmt.Errorf("called Unmarshal on nil")
	}

	dst := reflect.ValueOf(out)
	if dst.Kind() != reflect.Pointer || dst.IsNil() {
		return fmt.Errorf("called Unmarshal on non-pointer out value")
	}

	value, err := Decode(string(data))
	if err != nil {
		return err
	}

	return unmarshalValue(value, dst.Elem())
}

func unmarshalValue(value Value, dst reflect.Value) error {
	if !dst.CanSet() {
		return nil
	}

	if dst.Type() == valueType {
		dst.Set(reflect.ValueOf(value))
		return nil
	}

	if dst.Kind() == reflect.Pointer {
		if value.kind == KindNull {
			dst.SetZero()
			return nil
		}
		if dst.IsNil() {
			dst.Set(reflect.New(dst.Type().Elem()))
		}
		return unmarshalValue(value, dst.Elem())
	}

	if dst.Kind() == reflect.Interface {
		native, err := nativeValue(value)
		if err != nil {
			return err
		}
		if native == nil {
			dst.SetZero()
			return nil
		}
		nativeValue := reflect.ValueOf(native)
		if !nativeValue.Type().AssignableTo(dst.Type()) {
			return fmt.Errorf("cannot unmarshal %s into %s", kindName(value.kind), dst.Type())
		}
		dst.Set(nativeValue)
		return nil
	}

	if value.kind == KindNull {
		dst.SetZero()
		return nil
	}

	switch dst.Kind() {
	case reflect.Struct:
		return unmarshalStruct(value, dst)

	case reflect.Map:
		return unmarshalMap(value, dst)

	case reflect.Slice:
		return unmarshalSlice(value, dst)

	case reflect.Array:
		return unmarshalArray(value, dst)

	case reflect.String:
		if value.kind != KindString {
			return typeError(value, dst)
		}
		str, err := value.String()
		if err != nil {
			return err
		}
		dst.SetString(str)
		return nil

	case reflect.Bool:
		if value.kind != KindBool {
			return typeError(value, dst)
		}
		dst.SetBool(value.primValue == "true")
		return nil

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if value.kind != KindInt {
			return typeError(value, dst)
		}
		n, err := value.Int()
		if err != nil {
			return err
		}
		if dst.OverflowInt(n) {
			return fmt.Errorf("int literal %s overflows %s", value.primValue, dst.Type())
		}
		dst.SetInt(n)
		return nil

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		if value.kind != KindInt {
			return typeError(value, dst)
		}
		n, err := value.BigInt()
		if err != nil {
			return err
		}
		if n.Sign() < 0 || !n.IsUint64() || dst.OverflowUint(n.Uint64()) {
			return fmt.Errorf("int literal %s overflows %s", value.primValue, dst.Type())
		}
		dst.SetUint(n.Uint64())
		return nil

	case reflect.Float32, reflect.Float64:
		if value.kind != KindReal {
			return typeError(value, dst)
		}
		n, err := value.Float()
		if err != nil {
			return err
		}
		if dst.OverflowFloat(n) {
			return fmt.Errorf("real literal %s overflows %s", value.primValue, dst.Type())
		}
		dst.SetFloat(n)
		return nil

	default:
		return fmt.Errorf("cannot unmarshal %s into %s", kindName(value.kind), dst.Type())
	}
}

func unmarshalStruct(value Value, dst reflect.Value) error {
	if value.kind != KindMap {
		return typeError(value, dst)
	}

	fields := structFields(dst.Type())
	for _, pair := range value.mapValuePairs {
		fieldIdx, ok := fields[pair.Key]
		if !ok {
			continue
		}
		if err := unmarshalValue(value.mapValue[pair.Key], dst.Field(fieldIdx)); err != nil {
			return err
		}
	}
	return nil
}

func structFields(typ reflect.Type) map[string]int {
	fields := map[string]int{}
	for idx := 0; idx < typ.NumField(); idx++ {
		field := typ.Field(idx)
		if field.PkgPath != "" {
			continue
		}

		name := field.Name
		tag := field.Tag.Get("ndl")
		if tag == "-" {
			continue
		}
		if tagName, _, ok := strings.Cut(tag, ","); ok {
			if tagName != "" {
				name = tagName
			}
		} else if tag != "" {
			name = tag
		}
		fields[name] = idx
	}
	return fields
}

func unmarshalMap(value Value, dst reflect.Value) error {
	if value.kind != KindMap {
		return typeError(value, dst)
	}
	if dst.Type().Key().Kind() != reflect.String {
		return fmt.Errorf("cannot unmarshal map into %s with non-string keys", dst.Type())
	}
	if dst.IsNil() {
		dst.Set(reflect.MakeMapWithSize(dst.Type(), len(value.mapValuePairs)))
	}

	elemType := dst.Type().Elem()
	for _, pair := range value.mapValuePairs {
		elem := reflect.New(elemType).Elem()
		if err := unmarshalValue(value.mapValue[pair.Key], elem); err != nil {
			return err
		}
		key := reflect.ValueOf(pair.Key).Convert(dst.Type().Key())
		dst.SetMapIndex(key, elem)
	}
	return nil
}

func unmarshalSlice(value Value, dst reflect.Value) error {
	if value.kind != KindArray {
		return typeError(value, dst)
	}
	values := value.arrayValue
	res := reflect.MakeSlice(dst.Type(), len(values), len(values))
	for idx, child := range values {
		if err := unmarshalValue(child, res.Index(idx)); err != nil {
			return err
		}
	}
	dst.Set(res)
	return nil
}

func unmarshalArray(value Value, dst reflect.Value) error {
	if value.kind != KindArray {
		return typeError(value, dst)
	}
	if len(value.arrayValue) != dst.Len() {
		return fmt.Errorf("cannot unmarshal array of length %d into %s", len(value.arrayValue), dst.Type())
	}
	for idx, child := range value.arrayValue {
		if err := unmarshalValue(child, dst.Index(idx)); err != nil {
			return err
		}
	}
	return nil
}

func nativeValue(value Value) (any, error) {
	switch value.kind {
	case KindMap:
		res := make(map[string]any, len(value.mapValuePairs))
		for _, pair := range value.mapValuePairs {
			child, err := nativeValue(value.mapValue[pair.Key])
			if err != nil {
				return nil, err
			}
			res[pair.Key] = child
		}
		return res, nil

	case KindArray:
		res := make([]any, 0, len(value.arrayValue))
		for _, child := range value.arrayValue {
			nativeChild, err := nativeValue(child)
			if err != nil {
				return nil, err
			}
			res = append(res, nativeChild)
		}
		return res, nil

	case KindString:
		return value.String()

	case KindInt:
		return value.Int()

	case KindReal:
		return value.Float()

	case KindBool:
		return value.primValue == "true", nil

	case KindNull:
		return nil, nil

	default:
		return nil, fmt.Errorf("invalid value")
	}
}

func typeError(value Value, dst reflect.Value) error {
	return fmt.Errorf("cannot unmarshal %s into %s", kindName(value.kind), dst.Type())
}
