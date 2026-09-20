package config

import (
	"errors"
	"fmt"
)

// Sentinel errors identifying the kind of problem behind a FieldError.
// Inspect them with errors.Is against the error Load returns; Load's error
// may wrap several of these at once (see Error).
var (
	// ErrInvalidTarget reports a destination of the wrong shape: Load's type
	// parameter must itself be a struct type, and Render/Sprint's cfg
	// argument must be a struct or a non-nil pointer to one.
	ErrInvalidTarget = errors.New("config: destination must be a struct, or a non-nil pointer to a struct")

	// ErrUnsupportedType reports a field whose type config does not know how
	// to decode: not one of the supported kinds, not time.Duration or
	// *url.URL, and not an encoding.TextUnmarshaler.
	ErrUnsupportedType = errors.New("config: unsupported field type")

	// ErrParse reports a source value that could not be parsed into a
	// field's type.
	ErrParse = errors.New("config: value could not be parsed")

	// ErrEnum reports a value outside the set named by a field's enum tag.
	ErrEnum = errors.New("config: value is not one of the allowed enum values")

	// ErrRequired reports a field tagged required:"true" that no source
	// supplied a value for. This is a presence check: an explicitly supplied
	// zero value (false, 0, "", num.Rate("0"), ...) satisfies required.
	ErrRequired = errors.New("config: required value is missing")

	// ErrInvalidTag reports a struct tag whose value config could not parse,
	// such as required:"treu". This is a fail-closed check: parseTagSet
	// rejects a malformed required or secret tag outright rather than
	// silently treating it as false, since a typo'd secret:"treu" would
	// otherwise disable redaction without any signal that it had happened.
	ErrInvalidTag = errors.New("config: invalid struct tag")

	// ErrValidation reports a non-nil error returned by the destination's
	// Validate method.
	ErrValidation = errors.New("config: validation failed")
)

// FieldError reports one problem with one field, identified by its dotted
// path (see the package doc comment). Value holds the offending source text;
// it is empty for a secret field or when no source supplied a value, such as
// a required-field failure. Path itself is empty only for a failure from the
// destination's Validate method, which is not about any single field.
type FieldError struct {
	Path  string
	Value string
	Err   error
}

func (e *FieldError) Error() string {
	switch {
	case e.Path == "":
		return fmt.Sprintf("config: %v", e.Err)
	case e.Value == "":
		return fmt.Sprintf("config: %s: %v", e.Path, e.Err)
	default:
		return fmt.Sprintf("config: %s=%q: %v", e.Path, e.Value, e.Err)
	}
}

func (e *FieldError) Unwrap() error {
	return e.Err
}

// Error aggregates every FieldError found while loading configuration, so a
// caller sees every invalid or missing field in one pass instead of only the
// first. A validation failure from the destination's Validate method is
// reported as a *FieldError with an empty Path.
type Error struct {
	Fields []*FieldError
}

func (e *Error) Error() string {
	if len(e.Fields) == 1 {
		return e.Fields[0].Error()
	}
	msg := fmt.Sprintf("config: %d problem(s):", len(e.Fields))
	for _, f := range e.Fields {
		msg += "\n  - " + f.Error()
	}
	return msg
}

// Unwrap lets errors.Is and errors.As reach any individual FieldError's
// wrapped sentinel, per the multi-error Unwrap() []error convention.
func (e *Error) Unwrap() []error {
	errs := make([]error, len(e.Fields))
	for i, f := range e.Fields {
		errs[i] = f
	}
	return errs
}
