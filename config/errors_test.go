package config

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFieldErrorMessageWithAndWithoutValue(t *testing.T) {
	withValue := &FieldError{Path: "port", Value: "abc", Err: ErrParse}
	assert.Contains(t, withValue.Error(), "port")
	assert.Contains(t, withValue.Error(), `"abc"`)

	withoutValue := &FieldError{Path: "apikey", Err: ErrRequired}
	assert.Contains(t, withoutValue.Error(), "apikey")
	assert.NotContains(t, withoutValue.Error(), `""`)
}

func TestFieldErrorUnwrap(t *testing.T) {
	fe := &FieldError{Path: "port", Err: ErrParse}
	assert.True(t, errors.Is(fe, ErrParse))
}

func TestErrorMessageSingleField(t *testing.T) {
	agg := &Error{Fields: []*FieldError{{Path: "port", Err: ErrRequired}}}
	assert.Equal(t, agg.Fields[0].Error(), agg.Error())
}

func TestErrorMessageMultipleFields(t *testing.T) {
	agg := &Error{Fields: []*FieldError{
		{Path: "port", Err: ErrRequired},
		{Path: "name", Err: ErrRequired},
	}}
	msg := agg.Error()
	assert.Contains(t, msg, "2 problem(s)")
	assert.Contains(t, msg, "port")
	assert.Contains(t, msg, "name")
}

func TestErrorUnwrapReachesEveryField(t *testing.T) {
	agg := &Error{Fields: []*FieldError{
		{Path: "port", Err: ErrRequired},
		{Path: "name", Err: ErrParse},
	}}
	assert.True(t, errors.Is(agg, ErrRequired))
	assert.True(t, errors.Is(agg, ErrParse))
	assert.False(t, errors.Is(agg, ErrEnum))
}
