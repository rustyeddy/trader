package config

import (
	"encoding"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"time"
)

var (
	textUnmarshalerType = reflect.TypeOf((*encoding.TextUnmarshaler)(nil)).Elem()
	durationType        = reflect.TypeOf(time.Duration(0))
	urlType             = reflect.TypeOf(url.URL{})
)

// isTextUnmarshaler reports whether a value of type t, or a pointer to one,
// implements encoding.TextUnmarshaler. This is what lets any num type
// (num.Price, num.Currency, ...) or a caller's own type be used directly as
// a config field type.
func isTextUnmarshaler(t reflect.Type) bool {
	return reflect.PointerTo(t).Implements(textUnmarshalerType) || t.Implements(textUnmarshalerType)
}

// isLeafType reports whether t is a type collectLeaves should record as a
// scalar leaf rather than recurse into. One level of pointer indirection is
// permitted and marks the field as an optional value.
func isLeafType(t reflect.Type) bool {
	if t.Kind() == reflect.Pointer {
		return isLeafType(t.Elem())
	}
	if t == urlType || isTextUnmarshaler(t) {
		return true
	}
	switch t.Kind() {
	case reflect.String, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	default:
		return false
	}
}

// decodeLeaf parses raw into l.Value. For a pointer (optional) field, it
// allocates the pointee, decodes into it, and only then sets the pointer, so
// a failed parse never leaves a non-nil pointer to a half-set value behind.
func decodeLeaf(l *leaf, raw string) error {
	target := l.Value

	if target.Kind() == reflect.Pointer {
		elem := reflect.New(target.Type().Elem())
		if err := decodeScalar(elem.Elem(), raw); err != nil {
			return err
		}
		if err := checkEnum(l, elem.Elem()); err != nil {
			return err
		}
		target.Set(elem)
		return nil
	}

	if err := decodeScalar(target, raw); err != nil {
		return err
	}
	return checkEnum(l, target)
}

// decodeScalar parses raw into rv, an addressable, non-pointer value.
func decodeScalar(rv reflect.Value, raw string) error {
	if addr := rv.Addr(); addr.Type().Implements(textUnmarshalerType) {
		return addr.Interface().(encoding.TextUnmarshaler).UnmarshalText([]byte(raw))
	}

	switch {
	case rv.Type() == urlType:
		u, err := url.Parse(raw)
		if err != nil {
			return err
		}
		rv.Set(reflect.ValueOf(*u))
		return nil

	case rv.Type() == durationType:
		d, err := time.ParseDuration(raw)
		if err != nil {
			return err
		}
		rv.SetInt(int64(d))
		return nil
	}

	switch rv.Kind() {
	case reflect.String:
		rv.SetString(raw)
		return nil

	case reflect.Bool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return err
		}
		rv.SetBool(b)
		return nil

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(raw, 10, rv.Type().Bits())
		if err != nil {
			return err
		}
		rv.SetInt(n)
		return nil

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(raw, 10, rv.Type().Bits())
		if err != nil {
			return err
		}
		rv.SetUint(n)
		return nil

	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(raw, rv.Type().Bits())
		if err != nil {
			return err
		}
		rv.SetFloat(f)
		return nil

	default:
		return ErrUnsupportedType
	}
}

// checkEnum validates rv against l's enum tag, when both apply. The enum tag
// only constrains string-kind fields; it is silently ignored on any other
// type rather than treated as a configuration error in the destination
// struct itself.
func checkEnum(l *leaf, rv reflect.Value) error {
	if len(l.Enum) == 0 || rv.Kind() != reflect.String {
		return nil
	}
	if slices.Contains(l.Enum, rv.String()) {
		return nil
	}
	return ErrEnum
}
