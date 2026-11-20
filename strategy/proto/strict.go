package proto

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rustyeddy/trader/num"
)

// maxDepth bounds nesting so a hostile frame cannot exhaust the stack.
const maxDepth = 16

// checkJSON verifies that b is exactly one UTF-8 JSON object with no byte-order
// mark, no trailing data and no duplicate member name at any depth. Go's
// encoding/json accepts duplicates and replaces bad UTF-8, so this is checked
// here, which makes every library see the same message (ADR-094 section 2).
func checkJSON(b []byte) error {
	if !utf8.Valid(b) {
		return errorf(CodeProtocol, "frame is not valid UTF-8")
	}
	if bytes.HasPrefix(b, []byte("\xef\xbb\xbf")) {
		return errorf(CodeProtocol, "frame starts with a byte-order mark")
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	tok, err := dec.Token()
	if err != nil {
		return errorf(CodeProtocol, "frame is not JSON: %v", err)
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return errorf(CodeProtocol, "frame is not a JSON object")
	}
	if err := scanObject(dec, 1); err != nil {
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errorf(CodeProtocol, "data after the JSON object")
	}
	return nil
}

// scanObject reads the rest of an object whose '{' was consumed.
func scanObject(dec *json.Decoder, depth int) error {
	seen := map[string]struct{}{}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return errorf(CodeProtocol, "frame is not JSON: %v", err)
		}
		key, _ := kt.(string)
		if _, dup := seen[key]; dup {
			return errorf(CodeProtocol, "duplicate member %q", key)
		}
		seen[key] = struct{}{}
		if err := scanValue(dec, depth); err != nil {
			return err
		}
	}
	if _, err := dec.Token(); err != nil { // the closing '}'
		return errorf(CodeProtocol, "frame is not JSON: %v", err)
	}
	return nil
}

func scanValue(dec *json.Decoder, depth int) error {
	tok, err := dec.Token()
	if err != nil {
		return errorf(CodeProtocol, "frame is not JSON: %v", err)
	}
	d, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	if depth >= maxDepth {
		return errorf(CodeProtocol, "JSON is nested deeper than %d", maxDepth)
	}
	switch d {
	case '{':
		return scanObject(dec, depth+1)
	case '[':
		for dec.More() {
			if err := scanValue(dec, depth+1); err != nil {
				return err
			}
		}
		if _, err := dec.Token(); err != nil { // the closing ']'
			return errorf(CodeProtocol, "frame is not JSON: %v", err)
		}
	}
	return nil
}

// obj reads the members of one JSON object. Every failure is an *Error with the
// object's code, naming where it happened.
type obj struct {
	m     map[string]json.RawMessage
	where string
	code  Code
}

func newObj(raw json.RawMessage, where string, code Code) (obj, error) {
	if len(raw) == 0 || raw[0] != '{' {
		return obj{}, errorf(code, "%s must be a JSON object", where)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return obj{}, errorf(code, "%s: %v", where, err)
	}
	return obj{m: m, where: where, code: code}, nil
}

func (o obj) fail(name, format string, args ...any) *Error {
	return errorf(o.code, "%s.%s: %s", o.where, name, fmt.Sprintf(format, args...))
}

func (o obj) raw(name string, required bool) (json.RawMessage, bool, error) {
	r, ok := o.m[name]
	if !ok {
		if required {
			return nil, false, o.fail(name, "is required")
		}
		return nil, false, nil
	}
	if string(bytes.TrimSpace(r)) == "null" {
		return nil, false, o.fail(name, "must not be null")
	}
	return r, true, nil
}

func (o obj) str(name string, required bool) (string, bool, error) {
	r, ok, err := o.raw(name, required)
	if err != nil || !ok {
		return "", false, err
	}
	if r[0] != '"' {
		return "", false, o.fail(name, "must be a string")
	}
	var s string
	if err := json.Unmarshal(r, &s); err != nil {
		return "", false, o.fail(name, "%v", err)
	}
	return s, true, nil
}

func (o obj) requiredStr(name string) (string, error) {
	s, _, err := o.str(name, true)
	return s, err
}

// uint reads a non-negative integer written as digits only: no fraction, no
// exponent, no sign.
func (o obj) uint(name string, required bool) (uint64, bool, error) {
	r, ok, err := o.raw(name, required)
	if err != nil || !ok {
		return 0, false, err
	}
	n, err := strconv.ParseUint(string(r), 10, 63)
	if err != nil {
		return 0, false, o.fail(name, "must be a non-negative integer")
	}
	return n, true, nil
}

func (o obj) boolean(name string, required bool) (bool, bool, error) {
	r, ok, err := o.raw(name, required)
	if err != nil || !ok {
		return false, false, err
	}
	switch string(r) {
	case "true":
		return true, true, nil
	case "false":
		return false, true, nil
	}
	return false, false, o.fail(name, "must be true or false")
}

func (o obj) timestamp(name string, required bool) (time.Time, bool, error) {
	s, ok, err := o.str(name, required)
	if err != nil || !ok {
		return time.Time{}, false, err
	}
	t, err := parseTime(s)
	if err != nil {
		return time.Time{}, false, o.fail(name, "%v", err)
	}
	return t, true, nil
}

// decimal reads a price written as a decimal string. A JSON number is refused.
func (o obj) decimal(name string, required bool) (num.Price, bool, error) {
	s, ok, err := o.str(name, required)
	if err != nil || !ok {
		return num.Price{}, false, err
	}
	p, err := parseDecimal(s)
	if err != nil {
		return num.Price{}, false, o.fail(name, "%v", err)
	}
	return p, true, nil
}

func (o obj) object(name string, required bool, code Code) (obj, bool, error) {
	r, ok, err := o.raw(name, required)
	if err != nil || !ok {
		return obj{}, false, err
	}
	sub, err := newObj(r, o.where+"."+name, code)
	return sub, err == nil, err
}

func (o obj) array(name string, required bool) ([]json.RawMessage, bool, error) {
	r, ok, err := o.raw(name, required)
	if err != nil || !ok {
		return nil, false, err
	}
	if r[0] != '[' {
		return nil, false, o.fail(name, "must be an array")
	}
	var a []json.RawMessage
	if err := json.Unmarshal(r, &a); err != nil {
		return nil, false, o.fail(name, "%v", err)
	}
	return a, true, nil
}

func (o obj) strings(name string, required bool) ([]string, bool, error) {
	a, ok, err := o.array(name, required)
	if err != nil || !ok {
		return nil, false, err
	}
	out := make([]string, len(a))
	for i, r := range a {
		if len(r) == 0 || r[0] != '"' {
			return nil, false, o.fail(name, "element %d must be a string", i)
		}
		if err := json.Unmarshal(r, &out[i]); err != nil {
			return nil, false, o.fail(name, "element %d: %v", i, err)
		}
	}
	return out, true, nil
}

// only fails on a member that is not in allowed, naming it (in name order, so the
// error is the same every time).
func (o obj) only(allowed ...string) error {
	for _, k := range sortedKeys(o.m) {
		if !slices.Contains(allowed, k) {
			return errorf(o.code, "%s: unknown member %q", o.where, k)
		}
	}
	return nil
}

func sortedKeys(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// timeLayout is the one timestamp form of the protocol: RFC 3339 UTC, whole seconds.
const timeLayout = "2006-01-02T15:04:05Z"

func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(timeLayout, s)
	if err != nil || t.Format(timeLayout) != s {
		return time.Time{}, fmt.Errorf("%q is not a UTC timestamp like 2024-03-01T11:00:00Z", s)
	}
	return t, nil
}

func formatTime(t time.Time) string { return t.UTC().Format(timeLayout) }

// parseDecimal reads exact decimal text: digits, an optional point and digits, at
// most 8 decimals, no sign, no exponent. Nothing is rounded.
func parseDecimal(s string) (num.Price, error) {
	whole, frac, hasPoint := strings.Cut(s, ".")
	if whole == "" || strings.Trim(whole, "0123456789") != "" ||
		(hasPoint && (frac == "" || strings.Trim(frac, "0123456789") != "")) {
		return num.Price{}, fmt.Errorf("%q is not a decimal string like 1.08764", s)
	}
	if len(frac) > 8 {
		return num.Price{}, fmt.Errorf("%q has more than 8 decimals", s)
	}
	p, err := num.ParsePrice(s)
	if err != nil {
		return num.Price{}, fmt.Errorf("%q: %v", s, err)
	}
	return p, nil
}
