package trader

import (
	"encoding/csv"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type failingWriter struct {
	err error
}

func (w failingWriter) Write(_ []byte) (int, error) {
	return 0, w.err
}

func TestCSVJournalHeaders(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	tradesPath := filepath.Join(dir, "trades.csv")
	equityPath := filepath.Join(dir, "equity.csv")

	j, err := NewCSV(tradesPath, equityPath)
	assert.NoError(t, err)
	assert.NoError(t, j.Close())

	tradesData, err := os.ReadFile(tradesPath)
	assert.NoError(t, err)
	equityData, err := os.ReadFile(equityPath)
	assert.NoError(t, err)

	tradesReader := csv.NewReader(strings.NewReader(string(tradesData)))
	tradesHeader, err := tradesReader.Read()
	assert.NoError(t, err)

	equityReader := csv.NewReader(strings.NewReader(string(equityData)))
	equityHeader, err := equityReader.Read()
	assert.NoError(t, err)

	wantTrades := []string{"trade_id", "instrument", "units", "entry_price", "exit_price", "open_time", "close_time", "realized_pl", "reason"}
	assert.Equal(t, wantTrades, tradesHeader)

	wantEquity := []string{"time", "balance", "equity", "margin_used", "free_margin", "margin_level"}
	assert.Equal(t, wantEquity, equityHeader)
}

func TestCSVJournalRecordTrade(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	tradesPath := filepath.Join(dir, "trades.csv")
	equityPath := filepath.Join(dir, "equity.csv")

	j, err := NewCSV(tradesPath, equityPath)
	assert.NoError(t, err)

	open := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	closeT := time.Date(2024, 1, 2, 4, 5, 6, 0, time.UTC)

	units := Units(123456)
	entryPrice := PriceFromFloat(1.2345678)
	exitPrice := PriceFromFloat(1.3456789)
	realizedPL := MoneyFromFloat(-12.5)

	err = j.RecordTrade(TradeRecord{
		TradeID:    "T1",
		Instrument: "EUR_USD",
		Units:      units,
		EntryPrice: entryPrice,
		ExitPrice:  exitPrice,
		OpenTime:   FromTime(open),
		CloseTime:  FromTime(closeT),
		RealizedPL: realizedPL,
		Reason:     "test",
	})
	assert.NoError(t, err)

	assert.NoError(t, j.Close())

	tradesData, err := os.ReadFile(tradesPath)
	assert.NoError(t, err)

	reader := csv.NewReader(strings.NewReader(string(tradesData)))
	_, err = reader.Read() // header
	assert.NoError(t, err)
	row, err := reader.Read()
	assert.NoError(t, err)

	want := []string{
		"T1",
		"EUR_USD",
		units.String(),
		entryPrice.String(),
		exitPrice.String(),
		FromTime(open).String(),
		FromTime(closeT).String(),
		realizedPL.String(),
		"test",
	}
	assert.Equal(t, want, row)
}

func TestCSVJournalRecordEquity(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	tradesPath := filepath.Join(dir, "trades.csv")
	equityPath := filepath.Join(dir, "equity.csv")

	j, err := NewCSV(tradesPath, equityPath)
	assert.NoError(t, err)

	ts := time.Date(2024, 2, 3, 4, 5, 6, 0, time.UTC)

	balance := MoneyFromFloat(1000.1)
	equity := MoneyFromFloat(999.9)
	marginUsed := MoneyFromFloat(10.5)
	freeMargin := MoneyFromFloat(989.4)
	marginLevel := MoneyFromFloat(99.99)

	err = j.RecordEquity(EquitySnapshot{
		Timestamp:   FromTime(ts),
		Balance:     balance,
		Equity:      equity,
		MarginUsed:  marginUsed,
		FreeMargin:  freeMargin,
		MarginLevel: marginLevel,
	})
	assert.NoError(t, err)

	assert.NoError(t, j.Close())

	equityData, err := os.ReadFile(equityPath)
	assert.NoError(t, err)

	reader := csv.NewReader(strings.NewReader(string(equityData)))
	_, err = reader.Read() // header
	assert.NoError(t, err)
	row, err := reader.Read()
	assert.NoError(t, err)

	want := []string{
		FromTime(ts).String(),
		balance.String(),
		equity.String(),
		marginUsed.String(),
		freeMargin.String(),
		marginLevel.String(),
	}
	assert.Equal(t, want, row)
}

func TestCSVJournalRecordTradeFlushError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("write failed")
	j := &csvJournal{
		trades: csv.NewWriter(failingWriter{err: wantErr}),
		equity: csv.NewWriter(io.Discard),
	}

	err := j.RecordTrade(TradeRecord{TradeID: "T1", Instrument: "EUR_USD"})
	assert.ErrorIs(t, err, wantErr)
}

func TestCSVJournalRecordEquityFlushError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("write failed")
	j := &csvJournal{
		trades: csv.NewWriter(io.Discard),
		equity: csv.NewWriter(failingWriter{err: wantErr}),
	}

	err := j.RecordEquity(EquitySnapshot{Timestamp: FromTime(time.Now().UTC())})
	assert.ErrorIs(t, err, wantErr)
}

func TestCSVJournalCloseFlushError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("flush failed")
	tf, err := os.CreateTemp(t.TempDir(), "trades-*.csv")
	assert.NoError(t, err)
	defer tf.Close()
	ef, err := os.CreateTemp(t.TempDir(), "equity-*.csv")
	assert.NoError(t, err)
	defer ef.Close()

	trades := csv.NewWriter(failingWriter{err: wantErr})
	assert.NoError(t, trades.Write([]string{"header"}))

	j := &csvJournal{
		trades: trades,
		equity: csv.NewWriter(io.Discard),
		tf:     tf,
		ef:     ef,
	}

	err = j.Close()
	assert.ErrorIs(t, err, wantErr)
}

func TestCSVJournalCloseFileError(t *testing.T) {
	t.Parallel()

	tf, err := os.CreateTemp(t.TempDir(), "trades-*.csv")
	assert.NoError(t, err)
	ef, err := os.CreateTemp(t.TempDir(), "equity-*.csv")
	assert.NoError(t, err)
	defer ef.Close()

	require.NoError(t, tf.Close())

	j := &csvJournal{
		trades: csv.NewWriter(io.Discard),
		equity: csv.NewWriter(io.Discard),
		tf:     tf,
		ef:     ef,
	}

	err = j.Close()
	assert.Error(t, err)
}
