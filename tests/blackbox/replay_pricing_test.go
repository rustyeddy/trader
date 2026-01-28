//go:build blackbox

package blackbox

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func TestReplayPricing_WritesEquity(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "trader.sqlite")
	ticksPath := filepath.Join(dir, "ticks.csv")

	writeTicksCSV(t, ticksPath, "EUR_USD", 120, func(i int) (bid, ask float64) {
		// small drift, keeps sim engine busy
		mid := 1.1000 + float64(i)*0.00001
		return mid - 0.0001, mid + 0.0001
	})

	out, _ := run(t,
		"replay", "pricing",
		"--ticks", ticksPath,
		"--db", dbPath,
		"--account", "SIM-REPLAY",
		"--starting-balance", "100000",
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
	if err := db.QueryRow(`SELECT COUNT(*) FROM equity`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n <= 0 {
		t.Fatalf("expected equity rows > 0, got %d", n)
	}
}

func writeTicksCSV(t *testing.T, path, instrument string, n int, priceFn func(i int) (bid, ask float64)) {
	t.Helper()

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	_, _ = f.WriteString("time,instrument,bid,ask\n")
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	for i := 0; i < n; i++ {
		bid, ask := priceFn(i)
		ts := start.Add(time.Second * time.Duration(i)).Format(time.RFC3339Nano)
		_, _ = f.WriteString(
			ts + "," + instrument + "," + f64(bid) + "," + f64(ask) + "\n",
		)
	}
}
