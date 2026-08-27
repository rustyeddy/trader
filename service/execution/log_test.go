package execution

import (
	"context"
	"log/slog"
	"testing"

	"github.com/rustyeddy/trader/logging"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSubmit_LogsSuccessWithCanonicalAttributes proves a successful
// Submit logs exactly one record at Info, scoped with
// logging.ComponentExecution, carrying allowed=true and the resulting
// order's identity.
func TestSubmit_LogsSuccessWithCanonicalAttributes(t *testing.T) {
	logger, rec := logging.Capture()
	h := newHarness(t, "sim", "10000")
	p := newPipelineOver(t, h.broker, h.ids, h.clock)
	svc, err := New(h.broker, p, logger)
	require.NoError(t, err)

	adverse := num.MustParsePrice("0.01000")
	intent := mustEnterIntent(t, h.ids, h.listing.InstrumentID(), order.Buy)

	_, err = svc.Submit(context.Background(), SubmitRequest{
		AccountID:       h.accountID,
		Intent:          intent,
		Listing:         h.listing,
		RiskFraction:    num.MustParseRate("0.01"),
		AdverseDistance: &adverse,
	})
	require.NoError(t, err)

	records := rec.Records()
	require.Len(t, records, 1)
	assert.Equal(t, "execution submitted", records[0].Message)
	assert.Equal(t, slog.LevelInfo, records[0].Level)
	assert.Equal(t, "execution", records[0].Attrs[logging.Component])
	assert.Equal(t, h.accountID.String(), records[0].Attrs["account_id"])
	assert.Equal(t, true, records[0].Attrs["allowed"])
	assert.NotContains(t, records[0].Attrs, "error")
}

// TestSubmit_LogsRejectionAtInfoNotError is #186's own explicit review
// point: a risk rejection is an expected, structured admission
// decision, not an operational service failure, and must not be
// logged at the same severity as OpenAccount/Snapshot/broker failures.
func TestSubmit_LogsRejectionAtInfoNotError(t *testing.T) {
	logger, rec := logging.Capture()
	h := newHarness(t, "sim", "10000")
	p := newPipelineOver(t, h.broker, h.ids, h.clock, rejectingRule{})
	svc, err := New(h.broker, p, logger)
	require.NoError(t, err)

	adverse := num.MustParsePrice("0.01000")
	intent := mustEnterIntent(t, h.ids, h.listing.InstrumentID(), order.Buy)

	_, err = svc.Submit(context.Background(), SubmitRequest{
		AccountID:       h.accountID,
		Intent:          intent,
		Listing:         h.listing,
		RiskFraction:    num.MustParseRate("0.01"),
		AdverseDistance: &adverse,
	})
	require.Error(t, err)

	records := rec.Records()
	require.Len(t, records, 1)
	assert.Equal(t, "execution rejected", records[0].Message)
	assert.Equal(t, slog.LevelInfo, records[0].Level, "a risk rejection must not be logged at error severity")
	assert.Equal(t, false, records[0].Attrs["allowed"])
	assert.NotContains(t, records[0].Attrs, "error", "a rejection is a decision outcome, not an error to attach")
}

// TestSubmit_LogsBrokerFailureAtErrorLevel proves an operational
// failure unrelated to risk admission (OpenAccount) still logs at
// ERROR, unlike a rejection.
func TestSubmit_LogsBrokerFailureAtErrorLevel(t *testing.T) {
	logger, rec := logging.Capture()
	h := newHarness(t, "sim", "10000")
	openErr := assert.AnError
	fb := failingBroker{openErr: openErr}
	p := newPipelineOver(t, fb, h.ids, h.clock)
	svc, err := New(fb, p, logger)
	require.NoError(t, err)

	adverse := num.MustParsePrice("0.01000")
	intent := mustEnterIntent(t, h.ids, h.listing.InstrumentID(), order.Buy)

	_, err = svc.Submit(context.Background(), SubmitRequest{
		AccountID:       h.accountID,
		Intent:          intent,
		Listing:         h.listing,
		RiskFraction:    num.MustParseRate("0.01"),
		AdverseDistance: &adverse,
	})
	require.Error(t, err)

	records := rec.Records()
	require.Len(t, records, 1)
	assert.Equal(t, "open account failed", records[0].Message)
	assert.Equal(t, slog.LevelError, records[0].Level)
	assert.Equal(t, openErr, records[0].Attrs["error"])
}
