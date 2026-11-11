package external_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/internal/adapters/strategy/external"
	v1 "github.com/rustyeddy/trader/protocol/strategy/v1"
)

func TestFromWireError_NilIsNil(t *testing.T) {
	require.NoError(t, external.FromWireError(nil))
}

func TestFromWireError_ZeroValueIsNil(t *testing.T) {
	require.NoError(t, external.FromWireError(&v1.Error{}))
}

func TestWireError_Error_EmptyMessage(t *testing.T) {
	err := external.FromWireError(&v1.Error{Code: v1.ErrorCode_ERROR_CODE_DEADLINE_EXCEEDED})
	require.Error(t, err)
	require.NotContains(t, err.Error(), ": : ")
}

func TestFromWireError_StructuredFailure(t *testing.T) {
	err := external.FromWireError(&v1.Error{
		Code:    v1.ErrorCode_ERROR_CODE_CALLBACK_FAILED,
		Message: "OnBar panicked",
	})
	require.Error(t, err)

	var wireErr *external.WireError
	require.ErrorAs(t, err, &wireErr)
	require.Equal(t, v1.ErrorCode_ERROR_CODE_CALLBACK_FAILED, wireErr.Code)
	require.Equal(t, "OnBar panicked", wireErr.Message)
	require.Contains(t, err.Error(), "OnBar panicked")
}

func TestToWireError_RoundTrips(t *testing.T) {
	src := errors.New("capability mismatch")
	w := external.ToWireError(v1.ErrorCode_ERROR_CODE_CAPABILITY_MISMATCH, src)
	require.Equal(t, v1.ErrorCode_ERROR_CODE_CAPABILITY_MISMATCH, w.GetCode())
	require.Equal(t, "capability mismatch", w.GetMessage())

	got := external.FromWireError(w)
	var wireErr *external.WireError
	require.ErrorAs(t, got, &wireErr)
	require.Equal(t, v1.ErrorCode_ERROR_CODE_CAPABILITY_MISMATCH, wireErr.Code)
}

func TestToWireError_NilErrIsNormalCompletion(t *testing.T) {
	w := external.ToWireError(v1.ErrorCode_ERROR_CODE_UNSPECIFIED, nil)
	require.NoError(t, external.FromWireError(w))
}
