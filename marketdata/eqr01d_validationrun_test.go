//go:build fullarchive

// This file is excluded from normal `go test ./...` / `make check` by
// the fullarchive build tag, mirroring eqr01c_devrun_test.go: it
// imports a real Stooq export and runs the frozen EQR-01 protocol
// (issue #320, EQR-01D) against real, local SPY D1 validation-
// partition data — an operator action producing a durable
// docs/research/ artifact, not a CI assertion.
//
// See eqr01c_devrun_test.go's own package doc comment for why this
// lives in marketdata_test rather than marketdata's own internal test
// package (the same import-cycle reason applies identically here).
package marketdata_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rustyeddy/trader/analysis"
	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/marketdata/internal/provider/stooq"
	"github.com/rustyeddy/trader/num"
	svcmarketdata "github.com/rustyeddy/trader/service/marketdata"
)

// fullArchiveEQR01DSPYCSVPath names the real Stooq CSV export this test
// imports and reads back. Empty by default, for the identical reason
// fullArchiveEQR01CSPYCSVPath is: an operator edits it locally and
// never commits the edit.
//
//	go test -tags fullarchive ./marketdata/... \
//	    -run TestEQR01D_ValidationPartition -v
const fullArchiveEQR01DSPYCSVPath = ""

// eqr01DValidationStart and eqr01DValidationEnd are the frozen EQR-01
// validation partition bounds from
// docs/research/eqr-01-research-protocol.org: 2019-01-01 through
// 2022-12-31 inclusive — expressed as the half-open range
// [start, end) marketdata.TimeRange requires, so end is 2023-01-01,
// which is also the frozen final-holdout start: this run must
// structurally never read anything at or after eqr01DValidationEnd.
var (
	eqr01DValidationStart = time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)
	eqr01DValidationEnd   = time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
)

// eqr01DBootstrapSeed1 and eqr01DBootstrapSeed2 are this run's own
// fixed, explicit, recorded bootstrap seed — distinct from EQR-01C's
// (Seed1=319) so the two partitions' resampling draws are
// independent, following the same "issue number, resample count"
// mnemonic convention EQR-01C established.
const (
	eqr01DBootstrapSeed1     uint64 = 320
	eqr01DBootstrapSeed2     uint64 = 2000
	eqr01DBootstrapResamples        = 2000
	eqr01DMinObservations           = 30
	eqr01DRSIPeriod                 = 2
	eqr01DEMAPeriod                 = 200
)

// filterCSVBeforeFinalHoldout reads srcPath — Stooq's native
// "Date,Open,High,Low,Close,Volume" daily CSV export — and writes a
// copy under t.TempDir() containing only the header plus rows whose
// Date is strictly before eqr01DValidationEnd (2023-01-01, the frozen
// final-holdout start), returning the copy's path.
//
// This stops scanning srcPath entirely the moment it reaches the
// first row on or after the final-holdout boundary, exactly mirroring
// filterCSVBeforeDevelopmentEnd's own stop-scan strategy (issue #319,
// PR #321 re-review): no OHLCV value from any final-holdout row is
// ever parsed, and no byte of the file past the single boundary-
// crossing row's Date field is ever read. Development and validation
// rows (everything strictly before 2023-01-01) are both kept, since
// this run's raw-partition archive needs to cover the validation
// query range; development rows already went through EQR-01C's own
// identical structural boundary and are not re-examined here.
func filterCSVBeforeFinalHoldout(t *testing.T, srcPath string) string {
	t.Helper()

	src, err := os.Open(srcPath)
	if err != nil {
		t.Fatalf("filterCSVBeforeFinalHoldout: open %s: %v", srcPath, err)
	}
	defer func() { _ = src.Close() }()

	dstPath := filepath.Join(t.TempDir(), "spy_us_d_pre_holdout.csv")
	dst, err := os.Create(dstPath)
	if err != nil {
		t.Fatalf("filterCSVBeforeFinalHoldout: create %s: %v", dstPath, err)
	}
	defer func() { _ = dst.Close() }()

	scanner := bufio.NewScanner(src)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	if !scanner.Scan() {
		t.Fatalf("filterCSVBeforeFinalHoldout: %s: empty file", srcPath)
	}
	if _, err := fmt.Fprintln(dst, scanner.Text()); err != nil {
		t.Fatalf("filterCSVBeforeFinalHoldout: write header: %v", err)
	}

	var kept int
	var lastDate time.Time
	for scanner.Scan() {
		row := strings.TrimSpace(scanner.Text())
		if row == "" {
			continue
		}
		// Only the Date field (fields[0]) is ever inspected — OHLCV
		// values (fields[1]) are never parsed here at all, in range or
		// not.
		fields := strings.SplitN(row, ",", 2)
		date, err := time.Parse("2006-01-02", fields[0])
		if err != nil {
			t.Fatalf("filterCSVBeforeFinalHoldout: parse date %q: %v", fields[0], err)
		}
		if !lastDate.IsZero() && date.Before(lastDate) {
			t.Fatalf("filterCSVBeforeFinalHoldout: %s is not chronologically ascending (row date %s precedes previous %s) — the stop-at-first-boundary-row strategy this helper relies on requires ascending order",
				srcPath, date.Format("2006-01-02"), lastDate.Format("2006-01-02"))
		}
		lastDate = date

		if !date.Before(eqr01DValidationEnd) {
			break
		}
		if _, err := fmt.Fprintln(dst, row); err != nil {
			t.Fatalf("filterCSVBeforeFinalHoldout: write row: %v", err)
		}
		kept++
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("filterCSVBeforeFinalHoldout: scan %s: %v", srcPath, err)
	}
	t.Logf("filtered source CSV to pre-final-holdout rows only: kept %d rows, stopped scanning at the first row on/after %s",
		kept, eqr01DValidationEnd.Format("2006-01-02"))
	return dstPath
}

// TestEQR01D_ValidationPartition runs the identical, unmodified EQR-01
// protocol (issue #320) against SPY D1 validation-partition data
// (2019-01-01 through 2022-12-31): the same RSI(2)/EMA(200) regime
// classification, the same six frozen RSI buckets, the same five
// forward-return horizons, and the same bootstrap method as EQR-01C
// (#319) used for development — only the bars and the bootstrap seed
// differ. It reports the required measurements for both the primary
// (positive-regime) and secondary (all-regime) views.
//
// This test computes evidence only. The Continue/Reject decision
// itself, evaluated jointly against this result and EQR-01C's
// development result, is recorded in
// docs/research/eqr-01d-validation-result-and-decision.org, not here.
func TestEQR01D_ValidationPartition(t *testing.T) {
	if fullArchiveEQR01DSPYCSVPath == "" {
		t.Skip("fullArchiveEQR01DSPYCSVPath is empty; edit the constant in this file to point at a real Stooq SPY CSV export to run this test")
	}
	if info, err := os.Stat(fullArchiveEQR01DSPYCSVPath); err != nil || info.IsDir() {
		t.Skipf("fullArchiveEQR01DSPYCSVPath %q is not a readable file: %v", fullArchiveEQR01DSPYCSVPath, err)
	}

	ctx := context.Background()
	rawRoot := t.TempDir()

	preHoldoutCSVPath := filterCSVBeforeFinalHoldout(t, fullArchiveEQR01DSPYCSVPath)

	importResult, err := stooq.Import(ctx, preHoldoutCSVPath, rawRoot, "SPY")
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	t.Logf("imported %d rows, %d months, %s -> %s (provider=stooq, adjustment=split-adjusted per ADR-048)",
		importResult.RowsImported, importResult.MonthsWritten,
		importResult.FirstDate.Format("2006-01-02"), importResult.LastDate.Format("2006-01-02"))

	resolver := instrument.NewMemoryResolver()
	id, err := svcmarketdata.RegisterETFInstrument(resolver, svcmarketdata.EquityRegistration{
		Provider: "stooq",
		Exchange: "ARCA",
		Ticker:   "SPY",
		Currency: num.MustParseCurrency("USD"),
	})
	if err != nil {
		t.Fatalf("RegisterETFInstrument: %v", err)
	}

	// Same frozen calendar span as EQR-01C (2005-2026, the full
	// protocol span) — a property of the protocol, not of which
	// partition this run happens to query.
	cal := marketdata.NewUSEquityCalendar(marketdata.StandardUSEquityHolidays(eqr01CUSEquityCalendarYears()...))
	mgr, err := marketdata.New(marketdata.Config{
		Clock:        clock.Real{},
		StoreRoot:    t.TempDir(),
		RawRoot:      rawRoot,
		Resolver:     resolver,
		ProviderName: "stooq",
		Calendar:     cal,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Validation partition only: this query's Range is the sole
	// mechanism selecting validation bars out of the (development +
	// validation) raw archive imported above. Final holdout is
	// structurally unreachable from this query — and was never even
	// ingested, per filterCSVBeforeFinalHoldout above.
	span, err := marketdata.NewTimeRange(eqr01DValidationStart, eqr01DValidationEnd)
	if err != nil {
		t.Fatalf("NewTimeRange: %v", err)
	}
	query := marketdata.BarQuery{Instrument: id, Interval: marketdata.D1, Range: span}

	plan, err := mgr.Plan(ctx, query)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	for _, a := range plan.Actions {
		if a.Kind == marketdata.ActionDownloadRaw {
			t.Fatalf("expected 0 download-raw actions against already-imported data, got %+v", a)
		}
	}
	if len(plan.Actions) > 0 {
		if _, err := mgr.Build(ctx, plan); err != nil {
			t.Fatalf("Build: %v", err)
		}
	}

	reader, err := mgr.Bars(ctx, query)
	if err != nil {
		t.Fatalf("Bars: %v", err)
	}
	defer func() { _ = reader.Close() }()

	var bars []marketdata.Bar
	for {
		b, err := reader.Next(ctx)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("Bars.Next: %v", err)
		}
		if b.Time.Before(eqr01DValidationStart) || !b.Time.Before(eqr01DValidationEnd) {
			t.Fatalf("bar %s is outside the validation partition [%s, %s) — this must never happen",
				b.Time, eqr01DValidationStart, eqr01DValidationEnd)
		}
		bars = append(bars, b)
	}
	if len(bars) == 0 {
		t.Fatal("no bars returned for the validation partition")
	}
	t.Logf("validation partition: %d bars, %s -> %s", len(bars), bars[0].Time.Format("2006-01-02"), bars[len(bars)-1].Time.Format("2006-01-02"))

	cfg := analysis.RSIRegimeEventStudyConfig{
		Instrument:      id,
		Interval:        marketdata.D1,
		RSIPeriod:       eqr01DRSIPeriod,
		EMAPeriod:       eqr01DEMAPeriod,
		Horizons:        analysis.EQR01Horizons(),
		MinObservations: eqr01DMinObservations,
		Bootstrap: analysis.BootstrapConfig{
			Resamples: eqr01DBootstrapResamples,
			Seed1:     eqr01DBootstrapSeed1,
			Seed2:     eqr01DBootstrapSeed2,
		},
	}

	result, err := analysis.RunRSIRegimeEventStudy(bars, cfg)
	if err != nil {
		t.Fatalf("RunRSIRegimeEventStudy: %v", err)
	}
	t.Logf("observations=%d forward_returns=%d", len(result.Observations), len(result.ForwardReturns))

	t.Log("PRIMARY (Regime = Positive, i.e. Close > EMA(200)):")
	logEQR01CStats(t, result.PositiveRegimeStats)
	t.Log("SECONDARY / EXPLORATORY (all regimes, unconditional):")
	logEQR01CStats(t, result.AllRegimeStats)
}
