package ndl

import (
	"fmt"
	"math/big"
	"reflect"
	"strconv"
	"strings"
)

var (
	valueType  = reflect.TypeOf(Value{})
	bigIntType = reflect.TypeOf(big.Int{})
)

// Marshal encodes in as an NDL document.
//
// Struct fields are encoded using the field name by default, or the name in the
// ndl tag. The skip option omits a field, and omitempty omits fields with zero
// values.
//
// String fields support raw, keepnonprintable, and keepnonascii. Signed and
// unsigned integer fields support binary and hex. Float fields support decimal,
// scientific, and precision=N. See Value constructors NewStringWithFormat,
// NewIntWithFormat, NewFloatWithFormat for details on these options.
func Marshal(in any) (string, error) {
	value, err := marshalValue(reflect.ValueOf(in), marshalOptions{})
	if err != nil {
		return "", err
	}
	return Encode(value), nil
}

type marshalOptions struct {
	skip      bool
	omitEmpty bool

	stringFormat    StringFormat
	stringFormatSet bool

	intNotation  IntNotation
	intFormatSet bool

	realFormat    RealFormat
	realFormatSet bool
}

func parseFieldTag(tag string) (string, marshalOptions, error) {
	opts := marshalOptions{
		realFormat: RealFormat{Precision: -1},
	}
	if tag == "" {
		return "", opts, nil
	}

	parts := strings.Split(tag, ",")
	name := parts[0]

	for _, option := range parts[1:] {
		if option == "" {
			continue
		}

		switch {
		case option == "skip":
			opts.skip = true
		case option == "omitempty":
			opts.omitEmpty = true

		case option == "raw":
			opts.stringFormat.PreferRaw = true
			opts.stringFormatSet = true
		case option == "keepnonprintable":
			opts.stringFormat.KeepNonPrintable = true
			opts.stringFormatSet = true
		case option == "keepnonascii":
			opts.stringFormat.KeepNonASCII = true
			opts.stringFormatSet = true

		case option == "binary":
			if opts.intFormatSet {
				return "", opts, fmt.Errorf("conflicting int format options")
			}
			opts.intNotation = IntBinary
			opts.intFormatSet = true
		case option == "hex":
			if opts.intFormatSet {
				return "", opts, fmt.Errorf("conflicting int format options")
			}
			opts.intNotation = IntHex
			opts.intFormatSet = true

		case option == "decimal":
			if opts.realFormat.Notation != RealDefault {
				return "", opts, fmt.Errorf("conflicting float notation options")
			}
			opts.realFormat.Notation = RealDecimal
			opts.realFormatSet = true
		case option == "scientific":
			if opts.realFormat.Notation != RealDefault {
				return "", opts, fmt.Errorf("conflicting float notation options")
			}
			opts.realFormat.Notation = RealScientific
			opts.realFormatSet = true
		case strings.HasPrefix(option, "precision="):
			precision, err := strconv.Atoi(strings.TrimPrefix(option, "precision="))
			if err != nil {
				return "", opts, fmt.Errorf("invalid precision option %s", option)
			}
			opts.realFormat.Precision = precision
			if opts.realFormat.Notation == RealDefault {
				opts.realFormat.Notation = RealDecimal
			}
			opts.realFormatSet = true

		default:
			return "", opts, fmt.Errorf("unknown ndl tag option %s", option)
		}
	}

	return name, opts, nil
}

func marshalValue(src reflect.Value, opts marshalOptions) (Value, error) {
	if !src.IsValid() {
		return NewNull(), nil
	}

	if src.Type() == valueType {
		if opts.stringFormatSet || opts.intFormatSet || opts.realFormatSet {
			return Value{}, fmt.Errorf("format options do not apply to Value")
		}
		value := src.Interface().(Value)
		if value.kind == KindInvalid {
			return Value{}, fmt.Errorf("cannot marshal invalid Value")
		}
		return value, nil
	}

	if src.Type() == bigIntType {
		if opts.stringFormatSet || opts.realFormatSet {
			return Value{}, fmt.Errorf("non-int format option on %s", src.Type())
		}
		x := src.Interface().(big.Int)
		return NewBigIntWithFormat(&x, opts.intNotation), nil
	}

	switch src.Kind() {
	case reflect.Pointer:
		if src.IsNil() {
			return NewNull(), nil
		}
		return marshalValue(src.Elem(), opts)

	case reflect.Interface:
		if src.IsNil() {
			return NewNull(), nil
		}
		return marshalValue(src.Elem(), opts)

	case reflect.Struct:
		return marshalStruct(src, opts)

	case reflect.Map:
		return marshalMap(src, opts)

	case reflect.Slice, reflect.Array:
		return marshalArray(src, opts)

	case reflect.String:
		if opts.intFormatSet || opts.realFormatSet {
			return Value{}, fmt.Errorf("non-string format option on %s", src.Type())
		}
		return NewStringWithFormat(src.String(), opts.stringFormat)

	case reflect.Bool:
		if opts.stringFormatSet || opts.intFormatSet || opts.realFormatSet {
			return Value{}, fmt.Errorf("format option on %s", src.Type())
		}
		return NewBool(src.Bool()), nil

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if opts.stringFormatSet || opts.realFormatSet {
			return Value{}, fmt.Errorf("non-int format option on %s", src.Type())
		}
		return NewIntWithFormat(src.Int(), opts.intNotation), nil

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		if opts.stringFormatSet || opts.realFormatSet {
			return Value{}, fmt.Errorf("non-int format option on %s", src.Type())
		}
		x := new(big.Int).SetUint64(src.Uint())
		return NewBigIntWithFormat(x, opts.intNotation), nil

	case reflect.Float32, reflect.Float64:
		if opts.stringFormatSet || opts.intFormatSet {
			return Value{}, fmt.Errorf("non-float format option on %s", src.Type())
		}
		return NewFloatWithFormat(src.Float(), opts.realFormat), nil

	default:
		return Value{}, fmt.Errorf("cannot marshal %s", src.Type())
	}
}

func marshalStruct(src reflect.Value, opts marshalOptions) (Value, error) {
	if opts.stringFormatSet || opts.intFormatSet || opts.realFormatSet {
		return Value{}, fmt.Errorf("format option on %s", src.Type())
	}

	pairs := []Pair{}
	typ := src.Type()
	for idx := 0; idx < src.NumField(); idx++ {
		field := typ.Field(idx)
		if field.PkgPath != "" {
			continue
		}

		name, fieldOpts, err := parseFieldTag(field.Tag.Get("ndl"))
		if err != nil {
			return Value{}, err
		}
		if fieldOpts.skip {
			continue
		}
		if name == "" {
			name = field.Name
		}

		fieldValue := src.Field(idx)
		if fieldOpts.omitEmpty && isEmptyMarshalValue(fieldValue) {
			continue
		}

		value, err := marshalValue(fieldValue, fieldOpts)
		if err != nil {
			return Value{}, err
		}
		pairs = append(pairs, Pair{
			Key:   name,
			Value: value,
		})
	}

	return NewPairs(pairs)
}

func marshalMap(src reflect.Value, opts marshalOptions) (Value, error) {
	if opts.stringFormatSet || opts.intFormatSet || opts.realFormatSet {
		return Value{}, fmt.Errorf("format option on %s", src.Type())
	}
	if src.Type().Key().Kind() != reflect.String {
		return Value{}, fmt.Errorf("cannot marshal map with non-string keys")
	}
	if src.IsNil() {
		return NewMap(nil), nil
	}

	pairs := make([]Pair, 0, src.Len())
	iter := src.MapRange()
	for iter.Next() {
		value, err := marshalValue(iter.Value(), marshalOptions{})
		if err != nil {
			return Value{}, err
		}
		pairs = append(pairs, Pair{
			Key:   iter.Key().String(),
			Value: value,
		})
	}
	return NewPairs(pairs)
}

func marshalArray(src reflect.Value, opts marshalOptions) (Value, error) {
	if opts.stringFormatSet || opts.intFormatSet || opts.realFormatSet {
		return Value{}, fmt.Errorf("format option on %s", src.Type())
	}

	values := make([]Value, 0, src.Len())
	for idx := 0; idx < src.Len(); idx++ {
		value, err := marshalValue(src.Index(idx), marshalOptions{})
		if err != nil {
			return Value{}, err
		}
		values = append(values, value)
	}
	return NewArray(values), nil
}

func isEmptyMarshalValue(value reflect.Value) bool {
	if !value.IsValid() {
		return true
	}
	if value.Type() == valueType {
		return value.Interface().(Value).kind == KindInvalid
	}

	switch value.Kind() {
	case reflect.Bool:
		return !value.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return value.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return value.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return value.Float() == 0
	case reflect.String:
		return value.Len() == 0
	case reflect.Array, reflect.Map, reflect.Slice:
		return value.Len() == 0
	case reflect.Interface, reflect.Pointer:
		return value.IsNil()
	default:
		return false
	}
}
