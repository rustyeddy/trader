package strategysdk

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	v1 "github.com/rustyeddy/trader/protocol/strategy/v1"
)

func TestFromWireError_NilIsNil(t *testing.T) {
	require.NoError(t, fromWireError(nil))
}

func TestFromWireError_ZeroValueIsNil(t *testing.T) {
	require.NoError(t, fromWireError(&v1.Error{}))
}

func TestFromWireError_StructuredFailure(t *testing.T) {
	err := fromWireError(&v1.Error{Code: v1.ErrorCode_ERROR_CODE_CAPABILITY_MISMATCH, Message: "nope"})
	require.Error(t, err)
	var wireErr *WireError
	require.ErrorAs(t, err, &wireErr)
	require.Equal(t, v1.ErrorCode_ERROR_CODE_CAPABILITY_MISMATCH, wireErr.Code)
	require.Contains(t, err.Error(), "nope")
}

func TestWireError_Error_EmptyMessage(t *testing.T) {
	err := fromWireError(&v1.Error{Code: v1.ErrorCode_ERROR_CODE_DEADLINE_EXCEEDED})
	require.Error(t, err)
	require.NotContains(t, err.Error(), ": : ")
}

func TestToWireError_RoundTrips(t *testing.T) {
	src := errors.New("boom")
	w := toWireError(v1.ErrorCode_ERROR_CODE_CALLBACK_FAILED, src)
	require.Equal(t, v1.ErrorCode_ERROR_CODE_CALLBACK_FAILED, w.GetCode())
	require.Equal(t, "boom", w.GetMessage())
}

func TestToWireError_NilErrIsNormalCompletion(t *testing.T) {
	w := toWireError(v1.ErrorCode_ERROR_CODE_UNSPECIFIED, nil)
	require.NoError(t, fromWireError(w))
}

func TestSessionEndErr(t *testing.T) {
	require.NoError(t, sessionEndErr(nil))
	require.NoError(t, sessionEndErr(&v1.SessionEnd{}))

	err := sessionEndErr(&v1.SessionEnd{Code: v1.ErrorCode_ERROR_CODE_DEADLINE_EXCEEDED, Reason: "timeout"})
	require.Error(t, err)
	var wireErr *WireError
	require.ErrorAs(t, err, &wireErr)
	require.Equal(t, v1.ErrorCode_ERROR_CODE_DEADLINE_EXCEEDED, wireErr.Code)
}
