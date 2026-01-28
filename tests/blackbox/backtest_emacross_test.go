//go:build blackbox

package blackbox

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestBacktestEmaCross_ProducesTrades(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "trader.sqlite")
	ticksPath := filepath.Join(dir, "ticks.csv")

	// Build enough ticks to make EMA(20/50) "ready" and force at least one cross.
	// Phase 1: flat
	// Phase 2: ramp up
	// Phase 3: ramp down (creates opposite cross)
	writeTicksCSV(t, ticksPath, "EUR_USD", 200, func(i int) (bid, ask float64) {
		var mid float64
		switch {
		case i < 80:
			mid = 1.1000
		case i < 140:
			mid = 1.1000 + float64(i-80)*0.00010
		default:
			mid = 1.1060 - float64(i-140)*0.00010
		}
		return mid - 0.0001, mid + 0.0001
	})

	out, _ := run(t,
		"backtest", "ema-cross",
		"--ticks", ticksPath,
		"--instrument", "EUR_USD",
		"--fast", "20",
		"--slow", "50",
		"--risk", "0.005",
		"--stop-pips", "20",
		"--rr", "2.0",
		"--db", dbPath,
		"--account", "SIM-BACKTEST",
		"--starting-balance", "100000",
		"--close-end=true",
	)

	if !contains(out, "Done.") {
		t.Fatalf("expected Done. in output, got:\n%s", out)
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM trades`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n < 1 {
		t.Fatalf("expected >= 1 trade, got %d", n)
	}
}
