package id

import "errors"

var (
	// ErrInvalidID reports text that is not a well-formed identifier of the
	// requested kind: wrong prefix, malformed ULID body, or a body that
	// decodes to the reserved all-zero value.
	ErrInvalidID = errors.New("id: invalid identifier")

	// ErrZeroValue reports an operation attempted on the unset zero value
	// of an ID, where a real identifier is required.
	ErrZeroValue = errors.New("id: zero value is not a valid identifier")
)
