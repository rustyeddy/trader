CREATE TABLE IF NOT EXISTS risk_policy_runs (
  id            TEXT PRIMARY KEY,        -- ULID
  created_at    TEXT NOT NULL,            -- ISO8601
  account_ccy   TEXT NOT NULL,            -- USD
  start_equity  REAL NOT NULL,

  default_risk_pct	REAL NOT NULL,
  max_risk_pct		REAL NOT NULL,
  max_daily_loss_pct	REAL NOT NULL,
  max_weekly_loss_pct	REAL NOT NULL,
  max_open_trades	INTEGER NOT NULL,
  max_margin_pct	REAL NOT NULL,
  min_rr		REAL NOT NULL
);
