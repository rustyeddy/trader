//go:build fullarchive

package marketdata

// This file is excluded from normal `go test ./...` / `make check` by
// the fullarchive build tag, mirroring stooq_fullarchive_test.go:
// importing a real Stooq export and running it through the full
// Manager.Plan/Build/Bars path is an operator action, not a CI
// assertion.

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"github.com/rustyeddy/trader/marketdata/internal/provider/stooq"
)

// fullArchiveAAPLCSVPath names the real Stooq AAPL CSV export
// TestStooqAAPLFullArchive imports and reads back through the full
// Manager path, to demonstrate issue #298 (EQ-05)'s split-adjustment
// finding against AAPL's complete real history rather than only the
// small committed excerpt TestStooqEndToEnd_AAPLRecordsSplitAdjustedPolicy
// covers. Empty by default; an operator edits it locally (for example
// to "/srv/trading/data/raw/stooq/aapl_us_d.csv") and never commits
// the edit.
//
//	go test -tags fullarchive ./marketdata/... \
//	    -run TestStooqAAPLFullArchive -v
const fullArchiveAAPLCSVPath = ""

// aaplSplitDates are AAPL's two real historical stock splits within
// Stooq's covered history: a 7-for-1 split effective 2014-06-09, and a
// 4-for-1 split effective 2020-08-31.
var aaplSplitDates = []struct {
	name   string
	before string // trading day immediately before the split
	after  string // the split's own effective (first post-split) day
	ratio  float64
}{
	{"2014 7-for-1", "2014-06-06", "2014-06-09", 7},
	{"2020 4-for-1", "2020-08-28", "2020-08-31", 4},
}

// TestStooqAAPLFullArchive imports fullArchiveAAPLCSVPath, runs it
// through Plan -> Build -> Bars against a real marketdata.Manager, and
// confirms two things issue #298 requires: every published Manifest
// records AdjustmentSplitAdjusted, and close-to-close price movement
// across both of AAPL's real historical split dates is small (no
// discontinuous ~7x or ~4x jump), which is what confirms Stooq's data
// is already split-adjusted rather than raw.
func TestStooqAAPLFullArchive(t *testing.T) {
	if fullArchiveAAPLCSVPath == "" {
		t.Skip("fullArchiveAAPLCSVPath is empty; edit the constant in this file to point at a real Stooq AAPL CSV export to run this test")
	}
	if info, err := os.Stat(fullArchiveAAPLCSVPath); err != nil || info.IsDir() {
		t.Skipf("fullArchiveAAPLCSVPath %q is not a readable file: %v", fullArchiveAAPLCSVPath, err)
	}

	ctx := context.Background()
	rawRoot := t.TempDir()

	result, err := stooq.Import(ctx, fullArchiveAAPLCSVPath, rawRoot, "AAPL")
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	t.Logf("imported %d rows, %d months, %s -> %s", result.RowsImported, result.MonthsWritten,
		result.FirstDate.Format("2006-01-02"), result.LastDate.Format("2006-01-02"))

	mgr := newStooqAAPLTestManager(t, rawRoot)
	span, err := NewTimeRange(result.FirstDate, result.LastDate.AddDate(0, 0, 1))
	if err != nil {
		t.Fatalf("NewTimeRange: %v", err)
	}
	query := BarQuery{Instrument: aaplID(t), Interval: D1, Range: span}

	plan, err := mgr.Plan(ctx, query)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	buildResult, err := mgr.Build(ctx, plan)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(buildResult.Published) == 0 {
		t.Fatal("expected at least one published canonical month")
	}
	for _, pr := range buildResult.Published {
		if pr.Manifest.AdjustmentPolicy != AdjustmentSplitAdjusted {
			t.Errorf("month %04d-%02d: AdjustmentPolicy = %s, want %s",
				pr.Manifest.Span.Start().Year(), pr.Manifest.Span.Start().Month(),
				pr.Manifest.AdjustmentPolicy, AdjustmentSplitAdjusted)
		}
	}

	reader, err := mgr.Bars(ctx, query)
	if err != nil {
		t.Fatalf("Bars: %v", err)
	}
	defer func() { _ = reader.Close() }()

	byDate := make(map[string]Bar)
	for {
		b, err := reader.Next(ctx)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("Bars.Next: %v", err)
		}
		byDate[b.Time.Format(time.DateOnly)] = b
	}
	t.Logf("Manager.Bars returned %d bars", len(byDate))

	for _, sd := range aaplSplitDates {
		before, ok := byDate[sd.before]
		if !ok {
			t.Errorf("%s: missing bar for %s", sd.name, sd.before)
			continue
		}
		after, ok := byDate[sd.after]
		if !ok {
			t.Errorf("%s: missing bar for %s", sd.name, sd.after)
			continue
		}
		ratio := after.Close.Float64() / before.Close.Float64()
		t.Logf("%s: close %s (%s) -> close %s (%s), ratio=%.4f", sd.name,
			before.Close.String(), sd.before, after.Close.String(), sd.after, ratio)
		// Unadjusted raw data would show ratio near 1/sd.ratio (a real
		// N-for-1 split divides the pre-split price by N); split-adjusted
		// data keeps ordinary day-to-day movement, comfortably within
		// 50% either way. The two are not remotely close to each other,
		// so a generous tolerance here still cleanly distinguishes them.
		if ratio < 0.5 || ratio > 1.5 {
			t.Errorf("%s: close-to-close ratio across the split date = %.4f, want ~1 (split-adjusted); "+
				"a raw/unadjusted series would show ~%.4f here", sd.name, ratio, 1/sd.ratio)
		}
	}
}
