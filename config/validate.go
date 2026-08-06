package config

// checkRequired reports a *FieldError for every leaf tagged required:"true"
// that is still its Go zero value after every source has been applied.
//
// This is a zero-value check, not a "no source set this" check: an explicit
// source value equal to the zero value (env FOO=0 for an int field) is
// indistinguishable from an unset field. That is a deliberate simplification
// — precisely tracking provenance per field would complicate every source
// for a case (required field intentionally set to its zero value) that a
// default tag already covers better.
func checkRequired(leaves []*leaf) []*FieldError {
	var errs []*FieldError
	for _, l := range leaves {
		if l.Required && l.Value.IsZero() {
			errs = append(errs, &FieldError{Path: l.Path, Err: ErrRequired})
		}
	}
	return errs
}
