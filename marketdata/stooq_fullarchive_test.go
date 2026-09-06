//go:build fullarchive

package marketdata

// This file is excluded from normal `go test ./...` / `make check` by
// the fullarchive build tag, mirroring
// marketdata/internal/provider/oanda/inventory_fullarchive_test.go and
// marketdata/internal/provider/stooq/import_fullarchive_test.go:
// importing a real Stooq export and running it through the full
// Manager.Plan/Build/Bars path is an operator action, not a CI
// assertion.

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/rustyeddy/trader/marketdata/internal/provider/stooq"
)

// fullArchiveStooqCSVPath names the real Stooq CSV export
// TestStooqFullArchiveEndToEnd imports and reads back through the full
// Manager path. Empty by default for the identical reason every other
// fullarchive constant in this codebase is: an operator edits it
// locally (for example to "/srv/trading/data/raw/stooq/spy_us_d.csv")
// and never commits the edit.
//
//	go test -tags fullarchive ./marketdata/... \
//	    -run TestStooqFullArchiveEndToEnd -v
const fullArchiveStooqCSVPath = ""

// TestStooqFullArchiveEndToEnd imports fullArchiveStooqCSVPath, runs it
// through Plan -> Build -> Bars against a real marketdata.Manager, and
// spot-checks a few known dates/prices against the raw CSV — issue
// #303 (EQ-03A)'s own acceptance criterion that the full local SPY D1
// dataset can be imported and read through the normal Trader
// marketdata path, exercised against real data rather than only a
// fixture.
func TestStooqFullArchiveEndToEnd(t *testing.T) {
	if fullArchiveStooqCSVPath == "" {
		t.Skip("fullArchiveStooqCSVPath is empty; edit the constant in this file to point at a real Stooq CSV export to run this test")
	}
	if info, err := os.Stat(fullArchiveStooqCSVPath); err != nil || info.IsDir() {
		t.Skipf("fullArchiveStooqCSVPath %q is not a readable file: %v", fullArchiveStooqCSVPath, err)
	}

	ctx := context.Background()
	rawRoot := t.TempDir()

	result, err := stooq.Import(ctx, fullArchiveStooqCSVPath, rawRoot, "SPY")
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	t.Logf("imported %d rows, %d months, %s -> %s", result.RowsImported, result.MonthsWritten,
		result.FirstDate.Format("2006-01-02"), result.LastDate.Format("2006-01-02"))

	mgr := newStooqTestManager(t, rawRoot)
	span, err := NewTimeRange(result.FirstDate, result.LastDate.AddDate(0, 0, 1))
	if err != nil {
		t.Fatalf("NewTimeRange: %v", err)
	}
	query := BarQuery{Instrument: spyID(t), Interval: D1, Range: span}

	plan, err := mgr.Plan(ctx, query)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	var download, normalize int
	for _, a := range plan.Actions {
		switch a.Kind {
		case ActionDownloadRaw:
			download++
		case ActionNormalizeCanonical:
			normalize++
		}
	}
	t.Logf("plan: %d download-raw, %d normalize-canonical", download, normalize)
	if download > 0 {
		t.Errorf("expected 0 download-raw actions against already-imported data, got %d", download)
	}

	buildResult, err := mgr.Build(ctx, plan)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	var totalBars int
	for _, pr := range buildResult.Published {
		totalBars += pr.BarCount
	}
	t.Logf("build: %d months published, %d skipped, %d total bars", len(buildResult.Published), len(buildResult.Skipped), totalBars)

	reader, err := mgr.Bars(ctx, query)
	if err != nil {
		t.Fatalf("Bars: %v", err)
	}
	defer func() { _ = reader.Close() }()

	byDate := make(map[string]Bar)
	var count int
	for {
		b, err := reader.Next(ctx)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("Bars.Next: %v", err)
		}
		byDate[b.Time.Format("2006-01-02")] = b
		count++
	}
	t.Logf("Manager.Bars returned %d bars", count)
	if count != result.RowsImported {
		t.Errorf("Manager.Bars returned %d bars, expected %d (import's own row count)", count, result.RowsImported)
	}

	spotChecks := []struct {
		date                   string
		open, high, low, close string
		volume                 int64
	}{
		{"2005-02-25", "92.7949", "93.8716", "92.7097", "93.6948", 79289004},
	}
	for _, sc := range spotChecks {
		b, ok := byDate[sc.date]
		if !ok {
			t.Errorf("spot check %s: missing from Manager.Bars output", sc.date)
			continue
		}
		if b.Open.String() != sc.open || b.High.String() != sc.high || b.Low.String() != sc.low ||
			b.Close.String() != sc.close || b.Ticks != sc.volume {
			t.Errorf("spot check %s: got O=%s H=%s L=%s C=%s V=%d, want O=%s H=%s L=%s C=%s V=%d",
				sc.date, b.Open, b.High, b.Low, b.Close, b.Ticks, sc.open, sc.high, sc.low, sc.close, sc.volume)
		}
		if !b.AvgSpread.IsZero() || !b.MaxSpread.IsZero() {
			t.Errorf("spot check %s: expected zero AvgSpread/MaxSpread for a BasisTrade bar", sc.date)
		}
	}
}
