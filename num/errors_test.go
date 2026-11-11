package num

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestErrorMessagesAreStable pins the text of every exported sentinel. Several
// are shared with num/internal/fixed rather than translated, so a message
// edited there must not silently change what callers of num see.
func TestErrorMessagesAreStable(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{ErrOverflow, "num: value overflows exact representation"},
		{ErrUnderflow, "num: value underflows exact representation"},
		{ErrDivideByZero, "num: division by zero"},
		{ErrNotRepresentable, "num: result is not representable"},
		{ErrSyntax, "num: invalid decimal syntax"},
		{ErrPrecision, "num: excess decimal precision"},
		{ErrRange, "num: value out of range"},
		{ErrNegative, "num: negative value not permitted"},
		{ErrCurrencyMismatch, "num: currency mismatch"},
		{ErrInvalidCurrency, "num: invalid currency code"},
		{ErrMissingCurrency, "num: missing currency"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.EqualError(t, tt.err, tt.want)
		})
	}
}

// TestRepresentationErrorsReachCallersAsNumSentinels covers each error that
// originates in num/internal/fixed through the public API: the error a caller
// receives is the exported num sentinel, with num's message.
func TestRepresentationErrorsReachCallersAsNumSentinels(t *testing.T) {
	max := MustParseRate("92233720368.54775807")
	min := MustParseRate("-92233720368.54775808")

	_, overflow := max.Add(MustParseRate("1"))
	_, underflow := min.Sub(MustParseRate("1"))
	_, divZero := MustParseRate("1").DivRate(MustParseRate("0"))
	_, notRepresentable := min.Neg()
	_, syntax := ParseRate("1e3")
	_, precision := ParseRate("0.123456789")
	_, outOfRange := ParseRate("92233720369")

	tests := []struct {
		name string
		got  error
		want error
		msg  string
	}{
		{"overflow", overflow, ErrOverflow, "num: value overflows exact representation"},
		{"underflow", underflow, ErrUnderflow, "num: value underflows exact representation"},
		{"divide by zero", divZero, ErrDivideByZero, "num: division by zero"},
		{"not representable", notRepresentable, ErrNotRepresentable, "num: result is not representable"},
		{"syntax", syntax, ErrSyntax, "num: invalid decimal syntax"},
		{"precision", precision, ErrPrecision, "num: excess decimal precision"},
		{"range", outOfRange, ErrRange, "num: value out of range"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.ErrorIs(t, tt.got, tt.want)
			assert.EqualError(t, tt.got, tt.msg)
		})
	}
}
