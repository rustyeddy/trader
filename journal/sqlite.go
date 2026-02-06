package journal

import (
	"database/sql"

	_ "github.com/mattn/go-sqlite3"
)

type SQLite struct {
	db *sql.DB
}

func NewSQLite(path string) (*SQLite, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}

	if _, err := db.Exec(Schema); err != nil {
		return nil, err
	}

	return &SQLite{db: db}, nil
}

func (j *SQLite) RecordTrade(t TradeRecord) error {
	_, err := j.db.Exec(`
		INSERT INTO trades
		(trade_id, instrument, units, entry_price, exit_price, open_time, close_time, realized_pl, reason)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.TradeID, t.Instrument, t.Units, t.EntryPrice,
		t.ExitPrice, t.OpenTime, t.CloseTime, t.RealizedPL, t.Reason,
	)
	return err
}

func (j *SQLite) RecordEquity(e EquitySnapshot) error {
	_, err := j.db.Exec(`
		INSERT INTO equity
		(time, balance, equity, margin_used, free_margin, margin_level)
		VALUES (?, ?, ?, ?, ?, ?)`,
		e.Time, e.Balance, e.Equity, e.MarginUsed, e.FreeMargin, e.MarginLevel,
	)
	return err
}

func (j *SQLite) Close() error {
	return j.db.Close()
}
