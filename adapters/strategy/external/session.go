package external

import (
	"fmt"
	"time"

	"github.com/rustyeddy/trader/id"
	v1 "github.com/rustyeddy/trader/protocol/strategy/v1"
)

// FromWireRunOpen parses a received *v1.RunOpen, returning the
// session_id the guest named — the Run stream's own mandatory first
// client message, which the host must validate against an active
// Handshake session before writing anything else (strategy.proto's
// own RunOpen doc comment).
func FromWireRunOpen(w *v1.RunOpen) (sessionID string, err error) {
	if w == nil || w.GetSessionId() == "" {
		return "", fmt.Errorf("%w: run open must carry a session_id", ErrInvalidWireValue)
	}
	return w.GetSessionId(), nil
}

// ToWireSessionStart builds the *v1.SessionStart the host writes as
// the Run stream's own first server->client message, the instant its
// Start(ctx, env) call receives the real Environment — the exact
// point runID and start become known (ADR-062).
func ToWireSessionStart(runID id.RunID, start time.Time) *v1.SessionStart {
	return &v1.SessionStart{
		RunId:              runID.String(),
		StartTimeUnixNanos: start.UTC().UnixNano(),
	}
}

// ToWireSessionEnd builds the terminal *v1.SessionEnd the host writes
// once before closing the Run stream. A nil err (normal completion)
// produces the zero ERROR_CODE_UNSPECIFIED/empty-reason value
// SessionEnd's own doc comment documents; otherwise code names why
// the session ended.
func ToWireSessionEnd(code v1.ErrorCode, err error) *v1.SessionEnd {
	reason := ""
	if err != nil {
		reason = err.Error()
	}
	return &v1.SessionEnd{Code: code, Reason: reason}
}
