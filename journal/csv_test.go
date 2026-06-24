package journal

import (
	"encoding/csv"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rustyeddy/trader/market"
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

	assert.Equal(t, tradeCSVHeader, tradesHeader)
	assert.Equal(t, equityCSVHeader, equityHeader)
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

	units := market.Units(123456)
	entryPrice := market.PriceFromFloat(1.2345678)
	exitPrice := market.PriceFromFloat(1.3456789)
	realizedPL := market.MoneyFromFloat(-12.5)

	err = j.RecordTrade(TradeRecord{
		TradeID:    "T1",
		Instrument: "EUR_USD",
		Units:      units,
		EntryPrice: entryPrice,
		ExitPrice:  exitPrice,
		OpenTime:   market.FromTime(open),
		CloseTime:  market.FromTime(closeT),
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
		"123456",
		"1.23457",
		"1.34568",
		market.FromTime(open).String(),
		market.FromTime(closeT).String(),
		"-12.500000",
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

	balance := market.MoneyFromFloat(1000.1)
	equity := market.MoneyFromFloat(999.9)
	marginUsed := market.MoneyFromFloat(10.5)
	freeMargin := market.MoneyFromFloat(989.4)
	marginLevel := market.MoneyFromFloat(99.99)

	err = j.RecordEquity(EquitySnapshot{
		Timestamp:   market.FromTime(ts),
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
		market.FromTime(ts).String(),
		"1000.100000",
		"999.900000",
		"10.500000",
		"989.400000",
		"99.990000",
	}
	assert.Equal(t, want, row)
}

func TestCSVJournalRecordTradeFlushError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("write failed")
	j := &csvJournal{
		tradeWriter:  csv.NewWriter(failingWriter{err: wantErr}),
		equityWriter: csv.NewWriter(io.Discard),
	}

	err := j.RecordTrade(TradeRecord{TradeID: "T1", Instrument: "EUR_USD"})
	assert.ErrorIs(t, err, wantErr)
}

func TestCSVJournalRecordEquityFlushError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("write failed")
	j := &csvJournal{
		tradeWriter:  csv.NewWriter(io.Discard),
		equityWriter: csv.NewWriter(failingWriter{err: wantErr}),
	}

	err := j.RecordEquity(EquitySnapshot{Timestamp: market.FromTime(time.Now().UTC())})
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
		tradeWriter:  trades,
		equityWriter: csv.NewWriter(io.Discard),
		tradesFile:   tf,
		equityFile:   ef,
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
		tradeWriter:  csv.NewWriter(io.Discard),
		equityWriter: csv.NewWriter(io.Discard),
		tradesFile:   tf,
		equityFile:   ef,
	}

	err = j.Close()
	assert.Error(t, err)
}

func TestCSVJournalCloseEquityFileError(t *testing.T) {
	t.Parallel()

	tf, err := os.CreateTemp(t.TempDir(), "trades-*.csv")
	assert.NoError(t, err)
	defer tf.Close()

	ef, err := os.CreateTemp(t.TempDir(), "equity-*.csv")
	assert.NoError(t, err)
	require.NoError(t, ef.Close())

	j := &csvJournal{
		tradeWriter:  csv.NewWriter(io.Discard),
		equityWriter: csv.NewWriter(io.Discard),
		tradesFile:   tf,
		equityFile:   ef,
	}

	err = j.Close()
	assert.Error(t, err)
}

func TestNewCSV_CreateErrors(t *testing.T) {
	t.Parallel()

	t.Run("trades create error", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		tradesPath := filepath.Join(base, "missing-parent", "trades.csv")
		equityPath := filepath.Join(base, "equity.csv")

		j, err := NewCSV(tradesPath, equityPath)
		assert.Nil(t, j)
		assert.Error(t, err)
	})

	t.Run("equity create error", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		tradesPath := filepath.Join(base, "trades.csv")
		equityPath := filepath.Join(base, "missing-parent", "equity.csv")

		j, err := NewCSV(tradesPath, equityPath)
		assert.Nil(t, j)
		assert.Error(t, err)
	})
}

func TestCSVJournalReopenAppendsWithoutDuplicatingHeaders(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	tradesPath := filepath.Join(dir, "trades.csv")
	equityPath := filepath.Join(dir, "equity.csv")

	j1, err := NewCSV(tradesPath, equityPath)
	require.NoError(t, err)
	require.NoError(t, j1.RecordTrade(TradeRecord{
		TradeID:    "T1",
		Instrument: "EUR_USD",
		Units:      1000,
		EntryPrice: market.PriceFromFloat(1.1),
		ExitPrice:  market.PriceFromFloat(1.2),
		OpenTime:   market.FromTime(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)),
		CloseTime:  market.FromTime(time.Date(2024, 1, 1, 1, 0, 0, 0, time.UTC)),
		RealizedPL: market.MoneyFromFloat(10),
		Reason:     "first",
	}))
	require.NoError(t, j1.RecordEquity(EquitySnapshot{
		Timestamp:   market.FromTime(time.Date(2024, 1, 1, 1, 0, 0, 0, time.UTC)),
		Balance:     market.MoneyFromFloat(1000),
		Equity:      market.MoneyFromFloat(1001),
		MarginUsed:  market.MoneyFromFloat(10),
		FreeMargin:  market.MoneyFromFloat(991),
		MarginLevel: market.MoneyFromFloat(100),
	}))
	require.NoError(t, j1.Close())

	j2, err := NewCSV(tradesPath, equityPath)
	require.NoError(t, err)
	require.NoError(t, j2.RecordTrade(TradeRecord{
		TradeID:    "T2",
		Instrument: "GBP_USD",
		Units:      2000,
		EntryPrice: market.PriceFromFloat(1.3),
		ExitPrice:  market.PriceFromFloat(1.4),
		OpenTime:   market.FromTime(time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)),
		CloseTime:  market.FromTime(time.Date(2024, 1, 2, 1, 0, 0, 0, time.UTC)),
		RealizedPL: market.MoneyFromFloat(20),
		Reason:     "second",
	}))
	require.NoError(t, j2.RecordEquity(EquitySnapshot{
		Timestamp:   market.FromTime(time.Date(2024, 1, 2, 1, 0, 0, 0, time.UTC)),
		Balance:     market.MoneyFromFloat(1002),
		Equity:      market.MoneyFromFloat(1003),
		MarginUsed:  market.MoneyFromFloat(12),
		FreeMargin:  market.MoneyFromFloat(991),
		MarginLevel: market.MoneyFromFloat(83.5),
	}))
	require.NoError(t, j2.Close())

	tradesFile, err := os.Open(tradesPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tradesFile.Close() })

	tradesRows, err := csv.NewReader(tradesFile).ReadAll()
	require.NoError(t, err)
	require.Len(t, tradesRows, 3)
	assert.Equal(t, tradeCSVHeader, tradesRows[0])
	assert.Equal(t, "T1", tradesRows[1][0])
	assert.Equal(t, "T2", tradesRows[2][0])

	equityFile, err := os.Open(equityPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = equityFile.Close() })

	equityRows, err := csv.NewReader(equityFile).ReadAll()
	require.NoError(t, err)
	require.Len(t, equityRows, 3)
	assert.Equal(t, equityCSVHeader, equityRows[0])
}
