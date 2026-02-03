package journal

import (
	"context"
	"database/sql"

	_ "github.com/mattn/go-sqlite3"
)

type SQLiteJournal struct {
	db *sql.DB
}

func NewSQLite(path string) (*SQLiteJournal, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}

	if _, err := db.Exec(Schema); err != nil {
		return nil, err
	}

	return &SQLiteJournal{db: db}, nil
}

func (j *SQLiteJournal) RecordTrade(t TradeRecord) error {
	_, err := j.db.Exec(`
		INSERT INTO trades
		(trade_id, instrument, units, entry_price, exit_price, open_time, close_time, realized_pl, reason)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.TradeID, t.Instrument, t.Units, t.EntryPrice,
		t.ExitPrice, t.OpenTime, t.CloseTime, t.RealizedPL, t.Reason,
	)
	return err
}

func (j *SQLiteJournal) RecordEquity(e EquitySnapshot) error {
	_, err := j.db.Exec(`
		INSERT INTO equity
		(time, balance, equity, margin_used, free_margin, margin_level)
		VALUES (?, ?, ?, ?, ?, ?)`,
		e.Time, e.Balance, e.Equity, e.MarginUsed, e.FreeMargin, e.MarginLevel,
	)
	return err
}

func (j *SQLiteJournal) RecordBacktest(ctx context.Context, btr BacktestRun) error {

	return nil
}

func (j *SQLiteJournal) GetBacktestRun(ctx context.Context, runID string) (btr BacktestRun, err error) {

	return
}

func (j *SQLiteJournal) ListTradesByRunID(ctx context.Context, runID string) (tr []TradeRecord, err error) {

	return
}

func (j *SQLiteJournal) ListEquityByRunID(ctx context.Context, runID string) (eq []EquitySnapshot, err error) {

	return
}

// ExportBacktestOrg loads everything and returns the Org block.
func (j *SQLiteJournal) ExportBacktestOrg(ctx context.Context, runID string) (ostr string, err error) {

	return
}

func (j *SQLiteJournal) Close() error {
	return j.db.Close()
}
