package journal_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/journal"
)

func TestDiscardAcceptsAnyRecordWithoutError(t *testing.T) {
	d := journal.Discard()
	require.NoError(t, d.Record(context.Background(), mustIntentRecord(t)))
	require.NoError(t, d.Record(context.Background(), journal.Record{}))
	require.NoError(t, d.Close())
}
