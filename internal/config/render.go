package config

import (
	"encoding"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// Redacted is what Render and Sprint write in place of a secret field's
// value, regardless of that value's actual content — including when the
// field is an unset optional (nil pointer). Uniform redaction avoids leaking
// even the fact that a secret was or was not supplied.
const Redacted = "REDACTED"

// Render writes cfg's resolved configuration as "path = value" lines, one
// per leaf, sorted by path — the diagnostic output issue #20 calls for so an
// operator can see what a composition root actually resolved at startup.
// A secret-tagged field's value is always written as Redacted.
//
// cfg is typically the value Load just returned; it may be passed either by
// value or as a pointer to a struct.
func Render(w io.Writer, cfg any) error {
	v, err := structValue(cfg)
	if err != nil {
		return err
	}

	leaves, err := collectLeaves(v)
	if err != nil {
		return err
	}
	sort.Slice(leaves, func(i, j int) bool { return leaves[i].Path < leaves[j].Path })

	for _, l := range leaves {
		value := Redacted
		if !l.Secret {
			value = formatLeaf(l.Value)
		}
		if _, err := fmt.Fprintf(w, "%s = %s\n", l.Path, value); err != nil {
			return err
		}
	}
	return nil
}

// Sprint renders cfg the same way as Render, returning the result as a
// string instead of writing it. Render only fails for a cfg that is not a
// struct (or pointer to one) or one containing an unsupported field type —
// both programmer errors caught by Load itself long before Sprint is
// reached in ordinary use — and strings.Builder's Write never fails, so
// Sprint reports the problem inline rather than through a second error
// return.
func Sprint(cfg any) string {
	var sb strings.Builder
	if err := Render(&sb, cfg); err != nil {
		return fmt.Sprintf("config: %v", err)
	}
	return sb.String()
}

// structValue dereferences cfg down to its underlying struct value, the
// shape Render and collectLeaves need. It accepts a struct or a pointer to
// one; anything else, or a nil pointer, is ErrInvalidTarget.
func structValue(cfg any) (reflect.Value, error) {
	v := reflect.ValueOf(cfg)
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return reflect.Value{}, ErrInvalidTarget
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return reflect.Value{}, ErrInvalidTarget
	}
	return v, nil
}

// formatLeaf renders one leaf's value for diagnostics. It gives the value an
// addressable home first, so a pointer-receiver String or MarshalText
// implementation (the common case: num's types, time.Duration, *url.URL) is
// reachable even when the original field value was not itself addressable.
func formatLeaf(v reflect.Value) string {
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return "<unset>"
		}
		v = v.Elem()
	}

	addr := reflect.New(v.Type())
	addr.Elem().Set(v)

	if s, ok := addr.Interface().(fmt.Stringer); ok {
		return s.String()
	}
	if tm, ok := addr.Interface().(encoding.TextMarshaler); ok {
		if b, err := tm.MarshalText(); err == nil {
			return string(b)
		}
	}

	switch v.Kind() {
	case reflect.String:
		return v.String()
	case reflect.Bool:
		return strconv.FormatBool(v.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(v.Uint(), 10)
	case reflect.Float32, reflect.Float64:
		return strconv.FormatFloat(v.Float(), 'g', -1, v.Type().Bits())
	default:
		return fmt.Sprint(v.Interface())
	}
}
