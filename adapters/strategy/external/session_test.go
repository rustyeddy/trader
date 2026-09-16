package external_test

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/adapters/strategy/external"
	"github.com/rustyeddy/trader/id"
	v1 "github.com/rustyeddy/trader/protocol/strategy/v1"
)

func mustRunID(t *testing.T) id.RunID {
	t.Helper()
	v, err := id.GenerateRunID(testIDs)
	require.NoError(t, err)
	return v
}

func TestFromWireRunOpen(t *testing.T) {
	sessionID, err := external.FromWireRunOpen(&v1.RunOpen{SessionId: "sess-1"})
	require.NoError(t, err)
	require.Equal(t, "sess-1", sessionID)
}

func TestFromWireRunOpen_EmptyRejected(t *testing.T) {
	_, err := external.FromWireRunOpen(&v1.RunOpen{})
	require.ErrorIs(t, err, external.ErrInvalidWireValue)

	_, err = external.FromWireRunOpen(nil)
	require.ErrorIs(t, err, external.ErrInvalidWireValue)
}

func TestToWireSessionStart(t *testing.T) {
	runID := mustRunID(t)
	start := time.Date(2024, time.January, 2, 15, 0, 0, 0, time.UTC)

	w := external.ToWireSessionStart(runID, start)
	require.Equal(t, runID.String(), w.GetRunId())
	require.Equal(t, start.UnixNano(), w.GetStartTimeUnixNanos())
}

func TestToWireSessionEnd_NormalCompletion(t *testing.T) {
	w, err := external.ToWireSessionEnd(v1.ErrorCode_ERROR_CODE_UNSPECIFIED, nil)
	require.NoError(t, err)
	require.Equal(t, v1.ErrorCode_ERROR_CODE_UNSPECIFIED, w.GetCode())
	require.Empty(t, w.GetReason())
}

func TestToWireSessionEnd_Failure(t *testing.T) {
	w, err := external.ToWireSessionEnd(v1.ErrorCode_ERROR_CODE_DEADLINE_EXCEEDED, errors.New("boom"))
	require.NoError(t, err)
	require.Equal(t, v1.ErrorCode_ERROR_CODE_DEADLINE_EXCEEDED, w.GetCode())
	require.Equal(t, "boom", w.GetReason())
}

// TestToWireSessionEnd_FailureWithUnspecifiedCodeRejected is the
// review finding this function's own doc comment names: v1 reserves
// the zero code/reason pair for normal completion, so a non-nil error
// must never be paired with ERROR_CODE_UNSPECIFIED.
func TestToWireSessionEnd_FailureWithUnspecifiedCodeRejected(t *testing.T) {
	_, err := external.ToWireSessionEnd(v1.ErrorCode_ERROR_CODE_UNSPECIFIED, errors.New("boom"))
	require.ErrorIs(t, err, external.ErrInvalidWireValue)
}
