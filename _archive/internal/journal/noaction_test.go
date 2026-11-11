package journal_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/internal/id"
	"github.com/rustyeddy/trader/internal/journal"
)

func TestNewRecordNoAction(t *testing.T) {
	rec := func(na *journal.NoAction) journal.Record {
		return journal.Record{RunID: mustRunID(t), Metadata: id.Metadata{Timestamp: time.Now()}, Kind: journal.KindNoAction, NoAction: na}
	}

	_, err := journal.NewRecord(rec(&journal.NoAction{IntentID: mustIntentID(t), Reason: "no open position to exit"}))
	require.NoError(t, err)
	assert.Equal(t, "no-action", journal.KindNoAction.String())

	_, err = journal.NewRecord(rec(nil))
	assert.ErrorIs(t, err, journal.ErrInvalidRecord, "kind requires its payload")
	_, err = journal.NewRecord(rec(&journal.NoAction{Reason: "x"}))
	assert.ErrorIs(t, err, journal.ErrInvalidRecord, "intent id required")
	_, err = journal.NewRecord(rec(&journal.NoAction{IntentID: mustIntentID(t)}))
	assert.ErrorIs(t, err, journal.ErrInvalidRecord, "reason required")
}
