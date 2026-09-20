package config

// checkRequired reports a *FieldError for every leaf tagged required:"true"
// that no source supplied a value for.
//
// This is a presence check, not a zero-value check: an explicitly supplied
// false, 0, "", or an exact numeric zero like num.Rate("0") satisfies
// required just as any other value does. Load.Resolved is set the moment
// resolveValue finds a value for a leaf in any source (including a failed
// decode — requiredness is about whether an operator supplied something,
// not about whether what they supplied was valid; a bad value is already
// reported as its own parse error), so this check never has to reason about
// the decoded value itself.
func checkRequired(leaves []*leaf) []*FieldError {
	var errs []*FieldError
	for _, l := range leaves {
		if l.Required && !l.Resolved {
			errs = append(errs, &FieldError{Path: l.Path, Err: ErrRequired})
		}
	}
	return errs
}
