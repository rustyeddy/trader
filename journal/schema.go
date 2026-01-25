// journal/schema.go
package journal

const Schema = `
CREATE TABLE IF NOT EXISTS trades (
	trade_id TEXT PRIMARY KEY,
	instrument TEXT NOT NULL,
	units REAL NOT NULL,
	entry_price REAL NOT NULL,
	exit_price REAL NOT NULL,
	open_time DATETIME NOT NULL,
	close_time DATETIME NOT NULL,
	realized_pl REAL NOT NULL,
	reason TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS equity (
	time DATETIME NOT NULL,
	balance REAL NOT NULL,
	equity REAL NOT NULL,
	margin_used REAL NOT NULL,
	free_margin REAL NOT NULL,
	margin_level REAL NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_equity_time ON equity(time);
`
