package proto

import (
	"errors"
	"fmt"
)

// Code is a protocol error code. The set is closed (ADR-094).
type Code string

// The error codes.
const (
	CodeProtocol     Code = "protocol"
	CodeVersion      Code = "version"
	CodeRejected     Code = "rejected"
	CodeInvalidOrder Code = "invalid_order"
	CodeTimeout      Code = "timeout"
	CodeCancelled    Code = "cancelled"
	CodeInternal     Code = "internal"
)

// Valid reports whether c is one of the codes of the protocol.
func (c Code) Valid() bool {
	switch c {
	case CodeProtocol, CodeVersion, CodeRejected, CodeInvalidOrder, CodeTimeout, CodeCancelled, CodeInternal:
		return true
	}
	return false
}

// Error is a violation of the protocol found while framing, decoding or
// validating a message. Code is the code to send back in an error message.
type Error struct {
	Code Code
	Msg  string
}

func (e *Error) Error() string { return string(e.Code) + ": " + e.Msg }

func errorf(c Code, format string, args ...any) *Error {
	return &Error{Code: c, Msg: fmt.Sprintf(format, args...)}
}

// CodeOf returns the protocol code of err, or CodeInternal when err is not an
// *Error.
func CodeOf(err error) Code {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return CodeInternal
}
