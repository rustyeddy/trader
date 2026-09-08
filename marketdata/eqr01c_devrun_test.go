//go:build fullarchive

// This file is excluded from normal `go test ./...` / `make check` by
// the fullarchive build tag, mirroring stooq_fullarchive_test.go: it
// imports a real Stooq export and runs the frozen EQR-01C phenomenon
// experiment (issue #319) against real, local SPY D1 development-
// partition data — an operator action producing a durable
// docs/research/ artifact, not a CI assertion.
//
// This lives in the marketdata_test external test package, not
// marketdata's own internal test package (unlike
// stooq_fullarchive_test.go): analysis.RunRSIRegimeEventStudy needs
// analysis, and analysis itself imports marketdata, so a package
// marketdata test file importing analysis would be an import cycle.
// marketdata_test sits outside that cycle (marketdata_test ->
// analysis -> marketdata, and marketdata_test -> marketdata directly;
// marketdata never imports marketdata_test), while remaining inside
// the marketdata/ directory tree, so it can still reach
// marketdata/internal/provider/stooq directly — the one internal
// import this test genuinely needs, since stooq.Import is the only way
// to turn the raw single-file Stooq CSV export into the raw-partition
// layout Manager.Plan/Build expect, and that importer is not, and
// should not become, a public Manager operation of its own (ADR-020's
// "only Manager may invoke acquisition/build" boundary already covers
// it; stooq.Import is what Manager itself calls to satisfy that, not a
// bypass of it — this test performs the one-time raw import an
// operator would otherwise run out-of-band before Manager ever sees
// the data).
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

// fullArchiveEQR01CSPYCSVPath names the real Stooq CSV export this test
// imports and reads back. Empty by default for the identical reason
// every other fullarchive constant in this codebase is: an operator
// edits it locally (for example to "/srv/trading/data/raw/stooq/spy_us_d.csv")
// and never commits the edit.
//
//	go test -tags fullarchive ./marketdata/... \
//	    -run TestEQR01C_DevelopmentPartition -v
const fullArchiveEQR01CSPYCSVPath = ""

// eqr01CDevelopmentStart and eqr01CDevelopmentEnd are the frozen EQR-01
// development partition bounds from
// docs/research/eqr-01-research-protocol.org: 2005-02-25 (the archive's
// actual first available session, not the protocol's nominal
// 2005-01-01) through 2018-12-31 inclusive — expressed here as the
// half-open range [start, end) marketdata.TimeRange requires, so end is
// 2019-01-01.
var (
	eqr01CDevelopmentStart = time.Date(2005, 2, 25, 0, 0, 0, 0, time.UTC)
	eqr01CDevelopmentEnd   = time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)
)

// eqr01CBootstrapSeed1 and eqr01CBootstrapSeed2 are the fixed,
// explicit, recorded bootstrap seed this run uses — issue #319's own
// "no hidden random behavior except bootstrap resampling, whose seed
// must be explicit/fixed and recorded for reproducibility" requirement.
// These are arbitrary but fixed: 319 (this issue's number) and 2000
// (the protocol's own predeclared resample count), chosen only to be
// memorable and never varied between runs of this exact experiment.
const (
	eqr01CBootstrapSeed1     uint64 = 319
	eqr01CBootstrapSeed2     uint64 = 2000
	eqr01CBootstrapResamples        = 2000
	eqr01CMinObservations           = 30
	eqr01CRSIPeriod                 = 2
	eqr01CEMAPeriod                 = 200
)

// eqr01CUSEquityCalendarYears spans 2005 through 2026 — the *full*
// frozen EQR-01 protocol span (development through the frozen final-
// holdout end year), exactly as
// docs/research/eqr-01-research-protocol.org's own Trading-day and
// session provenance section requires: "must be explicitly configured
// with StandardUSEquityHolidays for the full set of calendar years
// this protocol's partitions span (2005 through the frozen holdout end
// year) ... a differently configured calendar (a narrower year range
// ...) would misclassify real NYSE holidays as trading days and is not
// an equivalent, reproducible substitute." This run only ever reads
// development-partition bars, but the calendar configuration itself
// must match the frozen protocol's full range regardless of which
// partition a given run happens to query (PR #321 review).
func eqr01CUSEquityCalendarYears() []int {
	years := make([]int, 0, 22)
	for y := 2005; y <= 2026; y++ {
		years = append(years, y)
	}
	return years
}

// filterCSVBeforeDevelopmentEnd reads srcPath — Stooq's native
// "Date,Open,High,Low,Close,Volume" daily CSV export — and writes a
// copy under t.TempDir() containing only the header plus rows whose
// Date is strictly before eqr01CDevelopmentEnd (2019-01-01), returning
// the copy's path. This is what keeps validation/final-holdout rows
// out of the raw-partition archive stooq.Import builds, rather than
// relying solely on the later Manager query Range to keep them unread
// (see the caller's own comment).
func filterCSVBeforeDevelopmentEnd(t *testing.T, srcPath string) string {
	t.Helper()

	src, err := os.Open(srcPath)
	if err != nil {
		t.Fatalf("filterCSVBeforeDevelopmentEnd: open %s: %v", srcPath, err)
	}
	defer func() { _ = src.Close() }()

	dstPath := filepath.Join(t.TempDir(), "spy_us_d_development_only.csv")
	dst, err := os.Create(dstPath)
	if err != nil {
		t.Fatalf("filterCSVBeforeDevelopmentEnd: create %s: %v", dstPath, err)
	}
	defer func() { _ = dst.Close() }()

	scanner := bufio.NewScanner(src)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	if !scanner.Scan() {
		t.Fatalf("filterCSVBeforeDevelopmentEnd: %s: empty file", srcPath)
	}
	if _, err := fmt.Fprintln(dst, scanner.Text()); err != nil {
		t.Fatalf("filterCSVBeforeDevelopmentEnd: write header: %v", err)
	}

	var kept, dropped int
	for scanner.Scan() {
		row := strings.TrimSpace(scanner.Text())
		if row == "" {
			continue
		}
		fields := strings.SplitN(row, ",", 2)
		date, err := time.Parse("2006-01-02", fields[0])
		if err != nil {
			t.Fatalf("filterCSVBeforeDevelopmentEnd: parse date %q: %v", fields[0], err)
		}
		if !date.Before(eqr01CDevelopmentEnd) {
			dropped++
			continue
		}
		if _, err := fmt.Fprintln(dst, row); err != nil {
			t.Fatalf("filterCSVBeforeDevelopmentEnd: write row: %v", err)
		}
		kept++
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("filterCSVBeforeDevelopmentEnd: scan %s: %v", srcPath, err)
	}
	t.Logf("filtered source CSV to development-partition rows only: kept %d, dropped %d (at/after %s)",
		kept, dropped, eqr01CDevelopmentEnd.Format("2006-01-02"))
	return dstPath
}

// TestEQR01C_DevelopmentPartition runs the frozen EQR-01C phenomenon
// experiment (issue #319): SPY D1, development partition only,
// Regime = Close > EMA(200), pullback observable RSI(2), the six
// frozen RSI(2) buckets, and the five frozen forward-return horizons
// (1/2/3/5/10 trading days). It reports the required measurements
// (count, mean, median, positive-return %, standard deviation, 95%
// bootstrap CI) for both the primary (positive-regime) and secondary
// (all-regime, exploratory) views, and asserts the guardrails issue
// #319 requires: development data only, no validation/holdout access,
// and the frozen parameters exactly as pinned.
//
// This test computes evidence; it makes no Continue/Reject decision
// (reserved for EQR-01D) and performs no threshold or parameter
// search.
func TestEQR01C_DevelopmentPartition(t *testing.T) {
	if fullArchiveEQR01CSPYCSVPath == "" {
		t.Skip("fullArchiveEQR01CSPYCSVPath is empty; edit the constant in this file to point at a real Stooq SPY CSV export to run this test")
	}
	if info, err := os.Stat(fullArchiveEQR01CSPYCSVPath); err != nil || info.IsDir() {
		t.Skipf("fullArchiveEQR01CSPYCSVPath %q is not a readable file: %v", fullArchiveEQR01CSPYCSVPath, err)
	}

	ctx := context.Background()
	rawRoot := t.TempDir()

	// Bound the ingestion path itself, not merely the later query:
	// filter the source CSV down to development-partition rows only
	// before it ever reaches stooq.Import, so the raw-partition archive
	// this test builds under rawRoot never contains a validation or
	// final-holdout row in the first place (PR #321 review) — a query
	// Range restriction alone would leave validation/holdout rows
	// sitting in the local raw store even though this particular run
	// never reads them, which is not the same guarantee.
	developmentOnlyCSVPath := filterCSVBeforeDevelopmentEnd(t, fullArchiveEQR01CSPYCSVPath)

	importResult, err := stooq.Import(ctx, developmentOnlyCSVPath, rawRoot, "SPY")
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

	// Development partition only: the query's own Range is the sole
	// mechanism restricting this run to development data — Manager.Bars
	// never returns a bar outside [Start, End), so validation
	// (2019-01-01 onward) and final holdout are structurally
	// unreachable from this query, not merely unused by convention.
	span, err := marketdata.NewTimeRange(eqr01CDevelopmentStart, eqr01CDevelopmentEnd)
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
			t.Fatalf("expected 0 download-raw actions against already-imported development-partition data, got %+v", a)
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
		if b.Time.Before(eqr01CDevelopmentStart) || !b.Time.Before(eqr01CDevelopmentEnd) {
			t.Fatalf("bar %s is outside the development partition [%s, %s) — this must never happen",
				b.Time, eqr01CDevelopmentStart, eqr01CDevelopmentEnd)
		}
		bars = append(bars, b)
	}
	if len(bars) == 0 {
		t.Fatal("no bars returned for the development partition")
	}
	t.Logf("development partition: %d bars, %s -> %s", len(bars), bars[0].Time.Format("2006-01-02"), bars[len(bars)-1].Time.Format("2006-01-02"))

	cfg := analysis.RSIRegimeEventStudyConfig{
		Instrument:      id,
		Interval:        marketdata.D1,
		RSIPeriod:       eqr01CRSIPeriod,
		EMAPeriod:       eqr01CEMAPeriod,
		Horizons:        analysis.EQR01Horizons(),
		MinObservations: eqr01CMinObservations,
		Bootstrap: analysis.BootstrapConfig{
			Resamples: eqr01CBootstrapResamples,
			Seed1:     eqr01CBootstrapSeed1,
			Seed2:     eqr01CBootstrapSeed2,
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

func logEQR01CStats(t *testing.T, stats []analysis.RSICellStats) {
	t.Helper()
	t.Logf("%-4s %-4s %8s %12s %12s %10s %12s %22s %-12s",
		"bkt", "horz", "count", "mean", "median", "pos_frac", "stddev", "95%_ci", "insufficient")
	for _, s := range stats {
		t.Logf("%-4s %-4s %8d %12.6f %12.6f %10.3f %12.6f [%9.6f, %9.6f] %-12v",
			s.Bucket, s.Horizon.Label, s.Count, s.MeanReturn, s.MedianReturn,
			s.PositiveFraction, s.StdDevReturn, s.CILower, s.CIUpper, s.Insufficient)
	}
}
