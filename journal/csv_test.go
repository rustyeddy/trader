package journal

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

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

	err = j.RecordTrade(TradeRecord{
		TradeID:    "T1",
		Instrument: "EUR_USD",
		Units:      123.456,
		EntryPrice: 1.2345678,
		ExitPrice:  1.3456789,
		OpenTime:   open,
		CloseTime:  closeT,
		RealizedPL: -12.5,
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
		"123.456000",
		"1.234568",
		"1.345679",
		open.Format(time.RFC3339),
		closeT.Format(time.RFC3339),
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

	err = j.RecordEquity(EquitySnapshot{
		Time:        ts,
		Balance:     1000.1,
		Equity:      999.9,
		MarginUsed:  10.5,
		FreeMargin:  989.4,
		MarginLevel: 99.99,
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
		ts.Format(time.RFC3339),
		"1000.100000",
		"999.900000",
		"10.500000",
		"989.400000",
		"99.990000",
	}
	assert.Equal(t, want, row)
}
