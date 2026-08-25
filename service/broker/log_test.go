package broker

import (
	"context"
	"log/slog"
	"testing"

	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNew_NilLoggerDefaultsToDiscard proves New(b, nil) does not panic
// on the first log call a Service operation makes.
func TestNew_NilLoggerDefaultsToDiscard(t *testing.T) {
	b, accountID, _ := testBroker(t)
	svc, err := New(b, nil)
	require.NoError(t, err)

	require.NotPanics(t, func() {
		_, _ = svc.Snapshot(context.Background(), SnapshotRequest{AccountRequest: AccountRequest{AccountID: accountID}})
	})
}

// TestSnapshot_LogsCompletionWithCanonicalAttributes is issue #154's
// own demonstration that a broker service operation's log record uses
// canonical component/domain attributes — the logging.ComponentBroker
// scope every Service record carries, and the account_id it acted on —
// rather than embedding that information only inside the message
// string.
func TestSnapshot_LogsCompletionWithCanonicalAttributes(t *testing.T) {
	logger, rec := logging.Capture()
	b, accountID, _ := testBroker(t)
	svc, err := New(b, logger)
	require.NoError(t, err)

	_, err = svc.Snapshot(context.Background(), SnapshotRequest{AccountRequest: AccountRequest{AccountID: accountID}})
	require.NoError(t, err)

	records := rec.Records()
	require.Len(t, records, 1)
	rec0 := records[0]
	assert.Equal(t, "snapshot read", rec0.Message)
	assert.Equal(t, slog.LevelDebug, rec0.Level)
	assert.Equal(t, "broker", rec0.Attrs[logging.Component])
	assert.Equal(t, accountID.String(), rec0.Attrs["account_id"])
	assert.NotContains(t, rec0.Attrs, "error", "a successful record must not carry a stray error attribute")
}

// TestSnapshot_LogsFailureAtErrorLevel proves a failing operation is
// logged once, at ERROR, with the actual error attached under the
// canonical "error" key.
func TestSnapshot_LogsFailureAtErrorLevel(t *testing.T) {
	logger, rec := logging.Capture()
	b, _, gen := testBroker(t)
	svc, err := New(b, logger)
	require.NoError(t, err)

	unknown, err := id.GenerateAccountID(gen)
	require.NoError(t, err)
	_, err = svc.Snapshot(context.Background(), SnapshotRequest{AccountRequest: AccountRequest{AccountID: unknown}})
	require.Error(t, err)

	records := rec.Records()
	require.Len(t, records, 1)
	assert.Equal(t, "open account failed", records[0].Message)
	assert.Equal(t, slog.LevelError, records[0].Level)
	assert.Equal(t, err, records[0].Attrs["error"],
		"the logged error attribute must be the exact error returned to the caller")
}

// TestSubmit_LogsCorrelationAndCausationFromContext proves
// context-propagated correlation metadata reaches Service's own
// records automatically, the same generic *Context logger mechanism
// service/marketdata already relies on — Service needs no
// correlation-specific code of its own.
func TestSubmit_LogsCorrelationAndCausationFromContext(t *testing.T) {
	logger, rec := logging.Capture()
	b, accountID, gen := testBroker(t)
	svc, err := New(b, logger)
	require.NoError(t, err)

	ctx := logging.WithCorrelationID(context.Background(), "corr-submit-1")
	ctx = logging.WithCausationID(ctx, "cause-submit-1")
	req := mustMarketRequest(t, gen, accountID)
	_, err = svc.Submit(ctx, SubmitRequest{AccountRequest: AccountRequest{AccountID: accountID}, Order: req})
	require.NoError(t, err)

	records := rec.Records()
	require.Len(t, records, 1)
	assert.Equal(t, "order submitted", records[0].Message)
	assert.Equal(t, "corr-submit-1", records[0].Attrs[logging.CorrelationID])
	assert.Equal(t, "cause-submit-1", records[0].Attrs[logging.CausationID])
}
