package trader

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJSONJournalRecordTradeAndEquity(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	tradesPath := filepath.Join(dir, "trades.jsonl")
	equityPath := filepath.Join(dir, "equity.jsonl")

	j, err := NewJSON(tradesPath, equityPath)
	require.NoError(t, err)

	trade := TradeRecord{
		TradeID:    "T1",
		Instrument: "EUR_USD",
		Units:      123456,
		EntryPrice: PriceFromFloat(1.23456),
		ExitPrice:  PriceFromFloat(1.23567),
		OpenTime:   FromTime(time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)),
		CloseTime:  FromTime(time.Date(2024, 1, 2, 4, 5, 6, 0, time.UTC)),
		RealizedPL: MoneyFromFloat(12.5),
		Reason:     "test",
	}
	equity := EquitySnapshot{
		Timestamp:   FromTime(time.Date(2024, 1, 2, 4, 5, 6, 0, time.UTC)),
		Balance:     MoneyFromFloat(1000),
		Equity:      MoneyFromFloat(1005),
		MarginUsed:  MoneyFromFloat(10),
		FreeMargin:  MoneyFromFloat(995),
		MarginLevel: MoneyFromFloat(100.5),
	}

	require.NoError(t, j.RecordTrade(trade))
	require.NoError(t, j.RecordEquity(equity))
	require.NoError(t, j.Close())

	tradesFile, err := os.Open(tradesPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tradesFile.Close() })

	var gotTrade TradeRecord
	require.True(t, bufio.NewScanner(tradesFile).Scan())
	_, err = tradesFile.Seek(0, 0)
	require.NoError(t, err)
	require.NoError(t, json.NewDecoder(tradesFile).Decode(&gotTrade))
	assert.Equal(t, trade, gotTrade)

	equityFile, err := os.Open(equityPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = equityFile.Close() })

	var gotEquity EquitySnapshot
	require.NoError(t, json.NewDecoder(equityFile).Decode(&gotEquity))
	assert.Equal(t, equity, gotEquity)
}

func TestNewJSONCreateErrors(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	tradesPath := filepath.Join(base, "missing-parent", "trades.jsonl")
	equityPath := filepath.Join(base, "equity.jsonl")

	j, err := NewJSON(tradesPath, equityPath)
	assert.Nil(t, j)
	assert.Error(t, err)
}

func TestJournalRecordPaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		base       string
		wantTrades string
		wantEquity string
	}{
		{"", "./trader-journal-trades.jsonl", "./trader-journal-equity.jsonl"},
		{"./run", "./run-trades.jsonl", "./run-equity.jsonl"},
		{"./run.jsonl", "./run-trades.jsonl", "./run-equity.jsonl"},
		{"./run.db", "./run-trades.jsonl", "./run-equity.jsonl"},
	}

	for _, tt := range tests {
		trades, equity := JournalRecordPaths(tt.base)
		assert.Equal(t, tt.wantTrades, trades)
		assert.Equal(t, tt.wantEquity, equity)
	}
}

func TestJSONJournalCloseError(t *testing.T) {
	t.Parallel()

	tf, err := os.CreateTemp(t.TempDir(), "trades-*.jsonl")
	require.NoError(t, err)
	ef, err := os.CreateTemp(t.TempDir(), "equity-*.jsonl")
	require.NoError(t, err)
	require.NoError(t, tf.Close())

	j := &jsonJournal{
		trades: json.NewEncoder(tf),
		equity: json.NewEncoder(ef),
		tf:     tf,
		ef:     ef,
	}

	assert.Error(t, j.Close())
}

func TestJSONJournalEncodeError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("write failed")
	j := &jsonJournal{
		trades: json.NewEncoder(errWriter{err: wantErr}),
		equity: json.NewEncoder(errWriter{err: wantErr}),
	}

	assert.ErrorIs(t, j.RecordTrade(TradeRecord{TradeID: "T1"}), wantErr)
	assert.ErrorIs(t, j.RecordEquity(EquitySnapshot{}), wantErr)
}

type errWriter struct {
	err error
}

func (w errWriter) Write(_ []byte) (int, error) {
	return 0, w.err
}
