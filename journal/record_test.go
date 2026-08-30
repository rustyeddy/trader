package journal_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/journal"
	"github.com/rustyeddy/trader/order"
)

func mustIntentRecord(t *testing.T) journal.Record {
	t.Helper()
	intent, err := order.NewIntent(order.Intent{
		IntentID:   mustIntentID(t),
		Kind:       order.IntentEnter,
		Instrument: mustEurUsdListing(t).InstrumentID(),
		Side:       order.Buy,
		Metadata:   id.Metadata{EventID: mustEventID(t), CorrelationID: mustCorrelationID(t)},
	})
	require.NoError(t, err)
	return journal.Record{
		RunID:    mustRunID(t),
		Metadata: id.Metadata{EventID: mustEventID(t), Timestamp: time.Now()},
		Kind:     journal.KindIntent,
		Intent:   &intent,
	}
}

func TestNewRecordValidIntent(t *testing.T) {
	rec, err := journal.NewRecord(mustIntentRecord(t))
	require.NoError(t, err)
	assert.Equal(t, journal.KindIntent, rec.Kind)
}

func TestNewRecordRejectsZeroRunID(t *testing.T) {
	rec := mustIntentRecord(t)
	rec.RunID = id.RunID{}
	_, err := journal.NewRecord(rec)
	assert.ErrorIs(t, err, journal.ErrInvalidRecord)
}

func TestNewRecordRejectsInvalidKind(t *testing.T) {
	rec := mustIntentRecord(t)
	rec.Kind = journal.KindUnknown
	_, err := journal.NewRecord(rec)
	assert.ErrorIs(t, err, journal.ErrInvalidRecord)
}

func TestNewRecordRejectsZeroTimestamp(t *testing.T) {
	rec := mustIntentRecord(t)
	rec.Metadata.Timestamp = time.Time{}
	_, err := journal.NewRecord(rec)
	assert.ErrorIs(t, err, journal.ErrInvalidRecord)
}

func TestNewRecordRejectsNoPayload(t *testing.T) {
	rec := mustIntentRecord(t)
	rec.Intent = nil
	_, err := journal.NewRecord(rec)
	assert.ErrorIs(t, err, journal.ErrInvalidRecord)
}

func TestNewRecordRejectsMultiplePayloads(t *testing.T) {
	rec := mustIntentRecord(t)
	rec.Trade = &order.Trade{}
	_, err := journal.NewRecord(rec)
	assert.ErrorIs(t, err, journal.ErrInvalidRecord)
}

func TestNewRecordRejectsKindPayloadMismatch(t *testing.T) {
	rec := mustIntentRecord(t)
	rec.Kind = journal.KindTrade // payload is still Intent
	_, err := journal.NewRecord(rec)
	assert.ErrorIs(t, err, journal.ErrInvalidRecord)
}

func TestNewRecordValidRunStartedAndCompleted(t *testing.T) {
	runID := mustRunID(t)
	started, err := journal.NewRecord(journal.Record{
		RunID:      runID,
		Metadata:   id.Metadata{Timestamp: time.Now()},
		Kind:       journal.KindRunStarted,
		RunStarted: &journal.RunStarted{RunID: runID, Header: []byte(`{"strategy":"test"}`)},
	})
	require.NoError(t, err)
	assert.Equal(t, journal.KindRunStarted, started.Kind)

	completed, err := journal.NewRecord(journal.Record{
		RunID:        runID,
		Metadata:     id.Metadata{Timestamp: time.Now()},
		Kind:         journal.KindRunCompleted,
		RunCompleted: &journal.RunCompleted{RunID: runID, EntryCount: 42},
	})
	require.NoError(t, err)
	assert.Equal(t, uint64(42), completed.RunCompleted.EntryCount)
}
