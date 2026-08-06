package config

import (
	"reflect"
	"strconv"
	"strings"
)

// leaf is one decodable field found by walking a destination struct: a
// scalar, a supported special-cased type, or an encoding.TextUnmarshaler
// implementer. Nested structs are not leaves; walk recurses into them
// instead of recording them.
type leaf struct {
	Path         string // dotted path, e.g. "server.port"
	EnvOverride  string // explicit env tag value; empty if not set
	FlagOverride string // explicit flag tag value; empty if not set
	Default      string
	HasDefault   bool
	Enum         []string
	Required     bool
	Secret       bool
	Value        reflect.Value // addressable, settable
}

// EnvName returns l's environment variable name: the env tag verbatim if
// set, otherwise prefix + "_" + the dotted path, uppercased with "." turned
// into "_". See the package doc comment.
func (l *leaf) EnvName(prefix string) string {
	if l.EnvOverride != "" {
		return l.EnvOverride
	}
	name := strings.ToUpper(strings.ReplaceAll(l.Path, ".", "_"))
	if prefix == "" {
		return name
	}
	return strings.ToUpper(prefix) + "_" + name
}

// FlagName returns l's Overrides map key: the flag tag verbatim if set,
// otherwise the dotted path with "." turned into "-".
func (l *leaf) FlagName() string {
	if l.FlagOverride != "" {
		return l.FlagOverride
	}
	return strings.ReplaceAll(l.Path, ".", "-")
}

// tagSet is one struct field's parsed config-related tags.
type tagSet struct {
	Name       string
	Env        string
	Flag       string
	Default    string
	HasDefault bool
	Enum       []string
	Required   bool
	Secret     bool
}

func parseTagSet(sf reflect.StructField) tagSet {
	ts := tagSet{
		Name: sf.Tag.Get("config"),
		Env:  sf.Tag.Get("env"),
		Flag: sf.Tag.Get("flag"),
	}
	if v, ok := sf.Tag.Lookup("default"); ok {
		ts.Default, ts.HasDefault = v, true
	}
	if v := sf.Tag.Get("enum"); v != "" {
		for e := range strings.SplitSeq(v, ",") {
			ts.Enum = append(ts.Enum, strings.TrimSpace(e))
		}
	}
	if v, err := strconv.ParseBool(sf.Tag.Get("required")); err == nil {
		ts.Required = v
	}
	if v, err := strconv.ParseBool(sf.Tag.Get("secret")); err == nil {
		ts.Secret = v
	}
	return ts
}

// collectLeaves walks dst — which must be an addressable struct value,
// typically obtained from reflect.ValueOf(ptr).Elem() — and returns every
// leaf it finds. A field whose type is neither a leaf type nor a nested
// struct reports ErrUnsupportedType.
func collectLeaves(dst reflect.Value) ([]*leaf, error) {
	return collect(dst, nil)
}

func collect(v reflect.Value, path []string) ([]*leaf, error) {
	t := v.Type()

	var out []*leaf
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if sf.PkgPath != "" {
			continue // unexported
		}
		fv := v.Field(i)
		tg := parseTagSet(sf)

		fieldPath := path
		if sf.Anonymous && tg.Name == "" {
			// Embedded struct with no explicit name: flatten into the
			// parent path rather than adding a segment for the type name.
		} else {
			segment := tg.Name
			if segment == "" {
				segment = strings.ToLower(sf.Name)
			}
			fieldPath = append(append([]string{}, path...), segment)
		}

		if isLeafType(fv.Type()) {
			out = append(out, &leaf{
				Path:         strings.Join(fieldPath, "."),
				EnvOverride:  tg.Env,
				FlagOverride: tg.Flag,
				Default:      tg.Default,
				HasDefault:   tg.HasDefault,
				Enum:         tg.Enum,
				Required:     tg.Required,
				Secret:       tg.Secret,
				Value:        fv,
			})
			continue
		}

		if fv.Kind() == reflect.Struct {
			nested, err := collect(fv, fieldPath)
			if err != nil {
				return nil, err
			}
			out = append(out, nested...)
			continue
		}

		return nil, &FieldError{Path: strings.Join(fieldPath, "."), Err: ErrUnsupportedType}
	}
	return out, nil
}
