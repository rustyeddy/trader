package journal

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
)

func newTestSQLite(t *testing.T) (*SQLite, string) {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	j, err := NewSQLite(path)
	assert.NoError(t, err)

	return j, path
}

func TestSQLiteSchemaCreated(t *testing.T) {
	t.Parallel()

	j, path := newTestSQLite(t)
	assert.NoError(t, j.Close())

	db, err := sql.Open("sqlite3", path)
	assert.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name IN ('trades','equity')`)
	assert.NoError(t, err)
	defer rows.Close()

	found := map[string]bool{}
	for rows.Next() {
		var name string
		assert.NoError(t, rows.Scan(&name))
		found[name] = true
	}
	assert.NoError(t, rows.Err())

	assert.True(t, found["trades"])
	assert.True(t, found["equity"])
}

func TestSQLiteRecordTrade(t *testing.T) {
	t.Parallel()

	j, path := newTestSQLite(t)

	open := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	closeT := time.Date(2024, 1, 2, 4, 5, 6, 0, time.UTC)

	rec := TradeRecord{
		TradeID:    "T1",
		Instrument: "EUR_USD",
		Units:      123.456,
		EntryPrice: 1.2345678,
		ExitPrice:  1.3456789,
		OpenTime:   open,
		CloseTime:  closeT,
		RealizedPL: -12.5,
		Reason:     "test",
	}

	assert.NoError(t, j.RecordTrade(rec))
	assert.NoError(t, j.Close())

	db, err := sql.Open("sqlite3", path)
	assert.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	var (
		tradeID    string
		instrument string
		units      float64
		entry      float64
		exit       float64
		openTime   time.Time
		closeTime  time.Time
		realizedPL float64
		reason     string
	)

	err = db.QueryRow(`
        SELECT trade_id, instrument, units, entry_price, exit_price, open_time, close_time, realized_pl, reason
        FROM trades LIMIT 1`).Scan(
		&tradeID, &instrument, &units, &entry, &exit, &openTime, &closeTime, &realizedPL, &reason,
	)
	assert.NoError(t, err)

	assert.Equal(t, rec.TradeID, tradeID)
	assert.Equal(t, rec.Instrument, instrument)
	assert.InDelta(t, rec.Units, units, 1e-6)
	assert.InDelta(t, rec.EntryPrice, entry, 1e-9)
	assert.InDelta(t, rec.ExitPrice, exit, 1e-9)
	assert.True(t, openTime.Equal(rec.OpenTime))
	assert.True(t, closeTime.Equal(rec.CloseTime))
	assert.InDelta(t, rec.RealizedPL, realizedPL, 1e-6)
	assert.Equal(t, rec.Reason, reason)
}

func TestSQLiteRecordEquity(t *testing.T) {
	t.Parallel()

	j, path := newTestSQLite(t)

	ts := time.Date(2024, 2, 3, 4, 5, 6, 0, time.UTC)
	rec := EquitySnapshot{
		Time:        ts,
		Balance:     1000.1,
		Equity:      999.9,
		MarginUsed:  10.5,
		FreeMargin:  989.4,
		MarginLevel: 99.99,
	}

	assert.NoError(t, j.RecordEquity(rec))
	assert.NoError(t, j.Close())

	db, err := sql.Open("sqlite3", path)
	assert.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	var (
		gotTime     time.Time
		balance     float64
		equity      float64
		marginUsed  float64
		freeMargin  float64
		marginLevel float64
	)

	err = db.QueryRow(`
        SELECT time, balance, equity, margin_used, free_margin, margin_level
        FROM equity LIMIT 1`).Scan(
		&gotTime, &balance, &equity, &marginUsed, &freeMargin, &marginLevel,
	)
	assert.NoError(t, err)

	assert.True(t, gotTime.Equal(rec.Time))
	assert.InDelta(t, rec.Balance, balance, 1e-6)
	assert.InDelta(t, rec.Equity, equity, 1e-6)
	assert.InDelta(t, rec.MarginUsed, marginUsed, 1e-6)
	assert.InDelta(t, rec.FreeMargin, freeMargin, 1e-6)
	assert.InDelta(t, rec.MarginLevel, marginLevel, 1e-6)
}
