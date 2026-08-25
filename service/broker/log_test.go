package broker

import (
	"context"
	"log/slog"
	"testing"

	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/logging"
	"github.com/rustyeddy/trader/order"
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

// TestSubmit_LogsInstrumentAttribute proves a Submit record carries
// the canonical logging.InstrumentID attribute (issue #156/M3-13's
// "instrument attribute where available" acceptance criterion) drawn
// from the order's own Listing, both on success and on a mid-pipeline
// failure where the instrument is already known.
func TestSubmit_LogsInstrumentAttribute(t *testing.T) {
	logger, rec := logging.Capture()
	b, accountID, gen := testBroker(t)
	svc, err := New(b, logger)
	require.NoError(t, err)

	req := mustMarketRequest(t, gen, accountID)
	wantInstrument := req.Listing.InstrumentID().String()

	_, err = svc.Submit(context.Background(), SubmitRequest{AccountRequest: AccountRequest{AccountID: accountID}, Order: req})
	require.NoError(t, err)

	records := rec.Records()
	require.Len(t, records, 1)
	assert.Equal(t, wantInstrument, records[0].Attrs[logging.InstrumentID])
}

// TestCancel_LogsOrderIDEvenWhenAccountOpenFails proves the order id
// being cancelled is reported even when the operation fails before an
// order lookup is ever attempted — Cancel/Replace's own requests carry
// no instrument, so order id is the one domain identifier available
// this early, and it must not be silently dropped from a failure
// record.
func TestCancel_LogsOrderIDEvenWhenAccountOpenFails(t *testing.T) {
	logger, rec := logging.Capture()
	b, _, gen := testBroker(t)
	svc, err := New(b, logger)
	require.NoError(t, err)

	unknownAccount, err := id.GenerateAccountID(gen)
	require.NoError(t, err)
	orderID, err := id.GenerateOrderID(gen)
	require.NoError(t, err)

	_, err = svc.Cancel(context.Background(), CancelRequest{
		AccountRequest: AccountRequest{AccountID: unknownAccount},
		Cancel:         order.CancelRequest{OrderID: orderID},
	})
	require.Error(t, err)

	records := rec.Records()
	require.Len(t, records, 1)
	assert.Equal(t, orderID.String(), records[0].Attrs[logging.OrderID])
}

// knownLogAttrKeys is the complete set of attribute keys any Service
// operation is currently allowed to log, per this package's own
// "Logging and credential redaction" doc-comment guarantee that every
// attribute comes from a typed request/result field, never a whole
// request/response/config value logged wholesale.
var knownLogAttrKeys = map[string]bool{
	logging.Component:     true,
	logging.AccountID:     true,
	logging.OrderID:       true,
	logging.InstrumentID:  true,
	logging.CorrelationID: true,
	logging.CausationID:   true,
	"status":              true,
	"account_count":       true,
	"error":               true,
}

// TestService_LogsOnlyKnownAttributeKeys is issue #156/M3-13's own
// redaction-assumption regression: every record produced by exercising
// every Service operation (success and failure) carries only
// attributes from knownLogAttrKeys. This is what would catch a future
// change that accidentally logs a whole request, response, or adapter
// config value wholesale — exactly the mistake that could one day leak
// a real broker adapter's credential — before that value ever has the
// chance to reach a logger unredacted.
func TestService_LogsOnlyKnownAttributeKeys(t *testing.T) {
	logger, rec := logging.Capture()
	b, accountID, gen := testBroker(t)
	svc, err := New(b, logger)
	require.NoError(t, err)
	ctx := context.Background()

	_, _ = svc.Accounts(ctx, AccountsRequest{})
	_, _ = svc.Snapshot(ctx, SnapshotRequest{AccountRequest: AccountRequest{AccountID: accountID}})
	marketReq := mustMarketRequest(t, gen, accountID)
	_, _ = svc.Submit(ctx, SubmitRequest{AccountRequest: AccountRequest{AccountID: accountID}, Order: marketReq})
	limitReq := mustLimitRequest(t, gen, accountID)
	_, _ = svc.Submit(ctx, SubmitRequest{AccountRequest: AccountRequest{AccountID: accountID}, Order: limitReq})
	_, _ = svc.Cancel(ctx, CancelRequest{AccountRequest: AccountRequest{AccountID: accountID}, Cancel: order.CancelRequest{OrderID: limitReq.OrderID}})

	unknownAccount, err := id.GenerateAccountID(gen)
	require.NoError(t, err)
	_, _ = svc.Snapshot(ctx, SnapshotRequest{AccountRequest: AccountRequest{AccountID: unknownAccount}})

	records := rec.Records()
	require.NotEmpty(t, records)
	for _, r := range records {
		for key := range r.Attrs {
			assert.True(t, knownLogAttrKeys[key], "record %q logged an unexpected attribute key %q", r.Message, key)
		}
	}
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
