package config

import (
	"errors"
	"fmt"
	"os"
	"reflect"
)

// Load resolves configuration into a new T, applying Options' four sources
// in precedence order: defaults, then the file source, then the
// environment, then Options.Overrides. See the package doc comment for the
// full precedence, tag, and naming rules.
//
// T must be a struct type, not a pointer to one; Load constructs and
// returns a T directly.
//
// Load aggregates every field-level problem it finds — an unsupported
// field type, a value that failed to parse, an enum rejection, a missing
// required field, or a failed Validate() — into one returned *Error rather
// than stopping at the first, so a misconfigured startup reports everything
// wrong in one pass. Use errors.Is against the sentinels in errors.go, or
// errors.As against *FieldError, to inspect individual failures.
func Load[T any](opts Options) (T, error) {
	var dst T

	v := reflect.ValueOf(&dst).Elem()
	if v.Kind() != reflect.Struct {
		return dst, ErrInvalidTarget
	}

	leaves, err := collectLeaves(v)
	if err != nil {
		return dst, err
	}

	files, err := fileValues(opts)
	if err != nil {
		return dst, err
	}

	environ := opts.Environ
	if !opts.environIsSet() {
		environ = os.Environ()
	}
	envs := envValues(environ)

	var errs []*FieldError
	for _, l := range leaves {
		raw, ok := resolveValue(l, opts, files, envs)
		if !ok {
			continue
		}
		if err := decodeLeaf(l, raw); err != nil {
			errs = append(errs, fieldErr(l, raw, err))
		}
	}

	errs = append(errs, checkRequired(leaves)...)

	if len(errs) == 0 {
		if err := validateDestination(&dst); err != nil {
			errs = append(errs, &FieldError{Err: fmt.Errorf("%w: %v", ErrValidation, err)})
		}
	}

	if len(errs) > 0 {
		return dst, &Error{Fields: errs}
	}
	return dst, nil
}

// resolveValue returns the value to use for l — applying default < file <
// env < overrides precedence — and whether any source supplied one at all.
func resolveValue(l *leaf, opts Options, files, envs map[string]string) (string, bool) {
	value, ok := "", false

	if l.HasDefault {
		value, ok = l.Default, true
	}
	if v, found := files[l.Path]; found {
		value, ok = v, true
	}
	if v, found := envs[l.EnvName(opts.EnvPrefix)]; found {
		value, ok = v, true
	}
	if v, found := opts.Overrides[l.FlagName()]; found {
		value, ok = v, true
	}

	return value, ok
}

// fieldErr wraps a decode failure as a *FieldError.
//
// For a secret field, both the raw value and the underlying error's message
// are dropped: a parse error's text routinely echoes its malformed input
// (strconv's, for instance, always does), so keeping the message would leak
// the secret through the back door redaction was supposed to close. A secret
// field's FieldError therefore names only the sentinel, never the cause.
func fieldErr(l *leaf, raw string, err error) *FieldError {
	if l.Secret {
		return &FieldError{Path: l.Path, Err: redactedError(err)}
	}
	if !errors.Is(err, ErrEnum) {
		err = fmt.Errorf("%w: %v", ErrParse, err)
	}
	return &FieldError{Path: l.Path, Value: raw, Err: err}
}

// redactedError reports the sentinel identifying a secret field's decode
// failure without any detail that might repeat the offending value.
func redactedError(err error) error {
	if errors.Is(err, ErrEnum) {
		return ErrEnum
	}
	return ErrParse
}

// validator is implemented by a destination struct that needs validation
// spanning more than one field.
type validator interface {
	Validate() error
}

func validateDestination(dst any) error {
	v, ok := dst.(validator)
	if !ok {
		return nil
	}
	return v.Validate()
}
