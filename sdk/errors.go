package sdk

import (
	"fmt"

	v1 "github.com/rustyeddy/trader/protocol/strategy/v1"
)

// WireError is the Go error representation of a received *v1.Error —
// a structured host-reported failure a caller can branch on by Code
// rather than parsing Message text, mirroring v1.Error's own doc
// comment.
type WireError struct {
	Code    v1.ErrorCode
	Message string
}

// Error implements the error interface.
func (e *WireError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("sdk: %s", e.Code)
	}
	return fmt.Sprintf("sdk: %s: %s", e.Code, e.Message)
}

// fromWireError converts a received *v1.Error into a Go error. A nil
// w, or a w whose Code is ERROR_CODE_UNSPECIFIED with an empty
// Message, both report nil — v1's own documented "absence or normal
// completion" zero value.
func fromWireError(w *v1.Error) error {
	if w == nil {
		return nil
	}
	if w.GetCode() == v1.ErrorCode_ERROR_CODE_UNSPECIFIED && w.GetMessage() == "" {
		return nil
	}
	return &WireError{Code: w.GetCode(), Message: w.GetMessage()}
}

// toWireError builds a *v1.Error carrying code and err's own message
// (empty if err is nil) — used to report a failed OnBar/OnFill
// callback back to the host via OnBarResponse.error/
// OnFillResponse.error.
func toWireError(code v1.ErrorCode, err error) *v1.Error {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	return &v1.Error{Code: code, Message: msg}
}
