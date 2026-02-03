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

CREATE TABLE IF NOT EXISTS backtest_runs (
  run_id TEXT PRIMARY KEY,                    -- ULID

  created_at TEXT NOT NULL,                   -- ISO8601 UTC (when the command ran)

  strategy   TEXT NOT NULL,                   -- e.g. "ema_cross"
  instrument TEXT NOT NULL,                   -- e.g. "EUR/USD"
  timeframe  TEXT NOT NULL,                   -- e.g. "H1"

  -- dataset identity (pick one or all)
  dataset_id     TEXT,                        -- e.g. "oanda:EURUSD:H1:2024"
  dataset_path   TEXT,                        -- e.g. "../../testdata/eurusd-h1-ticks.csv"
  dataset_sha256 TEXT,                        -- content hash (recommended)

  started_at TEXT NOT NULL,                   -- Result.Start in ISO8601 UTC
  ended_at   TEXT NOT NULL,                   -- Result.End   in ISO8601 UTC

  config_json TEXT NOT NULL,                  -- JSON: strategy config + run settings

  start_balance REAL NOT NULL,
  end_balance   REAL NOT NULL,

  trades INTEGER NOT NULL,
  wins   INTEGER NOT NULL,
  losses INTEGER NOT NULL,

  net_pl     REAL NOT NULL,
  return_pct REAL NOT NULL,

  -- optional but useful (nullable so you can add later)
  max_dd_pct    REAL,
  profit_factor REAL,

  -- optional metadata / artifact pointers
  git_commit TEXT,
  org_path   TEXT,
  equity_png TEXT
);

CREATE INDEX IF NOT EXISTS idx_backtest_runs_created_at
  ON backtest_runs(created_at);

CREATE INDEX IF NOT EXISTS idx_backtest_runs_strategy_instr_tf
  ON backtest_runs(strategy, instrument, timeframe);

CREATE INDEX IF NOT EXISTS idx_backtest_runs_time_window
  ON backtest_runs(started_at, ended_at);

ALTER TABLE trades ADD COLUMN run_id TEXT;
CREATE INDEX IF NOT EXISTS idx_trades_run_id ON trades(run_id);

ALTER TABLE equity ADD COLUMN run_id TEXT;
CREATE INDEX IF NOT EXISTS idx_equity_run_id_time ON equity(run_id, time);
`
