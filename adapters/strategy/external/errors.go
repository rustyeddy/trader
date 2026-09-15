package external

import (
	"errors"
	"fmt"

	v1 "github.com/rustyeddy/trader/protocol/strategy/v1"
)

// ErrInvalidWireValue reports a wire message field that is malformed,
// missing, or outside what this v1 surface defines — an unparseable
// exact-value string, an unrecognized enum value, an unset required
// field, or (for IntentsFromWire) an IntentKind this package does not
// know how to translate. Conversion never guesses or repairs an
// invalid value; it fails explicitly (issue #378's own acceptance
// criterion).
var ErrInvalidWireValue = errors.New("external: invalid wire value")

// WireError is the Go error representation of a received *v1.Error
// (FromWireError): a structured, guest- or host-reported failure a
// caller can branch on by Code rather than parsing Message text,
// mirroring v1.Error's own doc comment.
type WireError struct {
	Code    v1.ErrorCode
	Message string
}

// Error implements the error interface.
func (e *WireError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("external: %s", e.Code)
	}
	return fmt.Sprintf("external: %s: %s", e.Code, e.Message)
}

// FromWireError converts a received *v1.Error into a Go error.
//
// A nil w, or a w whose Code is ERROR_CODE_UNSPECIFIED with an empty
// Message, both report nil: this is v1's own documented "absence or
// normal completion" zero value (SessionEnd's own doc comment;
// OnBarResponse.error and OnFillResponse.error are unset on success),
// never an error to a caller of this function.
func FromWireError(w *v1.Error) error {
	if w == nil {
		return nil
	}
	if w.GetCode() == v1.ErrorCode_ERROR_CODE_UNSPECIFIED && w.GetMessage() == "" {
		return nil
	}
	return &WireError{Code: w.GetCode(), Message: w.GetMessage()}
}

// ToWireError builds a *v1.Error carrying code and err's own message
// (empty if err is nil). Passing ERROR_CODE_UNSPECIFIED with a nil err
// reproduces the same "absence or normal completion" zero value
// FromWireError recognizes.
func ToWireError(code v1.ErrorCode, err error) *v1.Error {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	return &v1.Error{Code: code, Message: msg}
}
