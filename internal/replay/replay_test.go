package replay

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/journal"
	"github.com/rustyeddy/trader/sim"
)

// If your journal uses a different driver name, adjust this.
// We'll try "sqlite" then "sqlite3".
func openSQLite(t *testing.T, path string) *sql.DB {
	t.Helper()

	var db *sql.DB
	var err error

	db, err = sql.Open("sqlite", path)
	if err == nil {
		if pingErr := db.Ping(); pingErr == nil {
			return db
		}
		_ = db.Close()
	}

	db, err = sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("sql.Open failed for sqlite/sqlite3: %v", err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		t.Fatalf("db.Ping failed: %v", err)
	}
	return db
}

func TestReplay_OPEN_SLTP_TakeProfit(t *testing.T) {
	ctx := context.Background()

	tmp := t.TempDir()
	csvPath := filepath.Join(tmp, "ticks.csv")
	dbPath := filepath.Join(tmp, "trader.sqlite")

	// Scripted scenario:
	// - OPEN_SLTP at first tick
	// - price rises to bid==TP (1.1050) -> TP should trigger for a long
	// - CLOSE_ALL safety at end
	csv := `time,instrument,bid,ask,event,arg1,arg2,arg3,arg4
2026-01-24T09:30:00Z,EUR_USD,1.1000,1.1002,OPEN_SLTP,EUR_USD,10000,1.0980,1.1050
2026-01-24T09:30:05Z,EUR_USD,1.1010,1.1012,,,
2026-01-24T09:30:10Z,EUR_USD,1.1020,1.1022,,,
2026-01-24T09:30:15Z,EUR_USD,1.1030,1.1032,,,
2026-01-24T09:30:20Z,EUR_USD,1.1040,1.1042,,,
2026-01-24T09:30:25Z,EUR_USD,1.1050,1.1052,,,
2026-01-24T09:30:30Z,EUR_USD,1.1055,1.1057,CLOSE_ALL,EndOfScenario,,,
`
	if err := os.WriteFile(csvPath, []byte(csv), 0o644); err != nil {
		t.Fatalf("write csv: %v", err)
	}

	j, err := journal.NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
	defer j.Close()

	startBal := 100_000.0
	engine := sim.NewEngine(broker.Account{
		ID:       "SIM-TEST",
		Currency: "USD",
		Balance:  startBal,
		Equity:   startBal,
	}, j)

	if err := CSV(ctx, csvPath, engine, Options{TickThenEvent: true}); err != nil {
		t.Fatalf("CSV: %v", err)
	}

	acct, _ := engine.GetAccount(ctx)
	if acct.Balance <= startBal {
		t.Fatalf("expected balance to increase, start=%.2f end=%.2f", startBal, acct.Balance)
	}

	// Validate at least one trade row exists and reason indicates TP.
	// This is intentionally tolerant because your table/column names may differ slightly.
	db := openSQLite(t, dbPath)
	defer db.Close()

	// Query the reason column from the trades table
	rows, err := db.Query(`SELECT reason FROM trades`)
	if err != nil {
		t.Fatalf("query trades: %v", err)
	}
	defer rows.Close()

	var reasons []string
	for rows.Next() {
		var r sql.NullString
		if err := rows.Scan(&r); err != nil {
			t.Fatalf("scan close_reason: %v", err)
		}
		reasons = append(reasons, r.String)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows err: %v", err)
	}
	if len(reasons) < 1 {
		t.Fatalf("expected at least 1 trade row in DB")
	}

	foundTP := false
	for _, r := range reasons {
		if strings.Contains(strings.ToLower(r), "takeprofit") || strings.EqualFold(r, "TP") {
			foundTP = true
			break
		}
	}
	if !foundTP {
		t.Fatalf("expected a TakeProfit close reason, got: %#v", reasons)
	}
}
