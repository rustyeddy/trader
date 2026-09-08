//go:build alpacasmoke

package marketdata

// This file is issue #326 (EQ-13)'s opt-in, real-credential
// integration/smoke path: proving the complete Alpaca historical
// equity pipeline end-to-end —
//
//	Alpaca Go SDK -> provider adapter -> raw bars -> canonical
//	derivation -> canonical persistence -> marketdata.Manager ->
//	consumer read
//
// — through Trader's normal architecture, using a real Alpaca paper
// credential, rather than bypassing Manager/storage or comparing
// provider-native values directly in downstream code. It complements
// (does not replace) two existing, narrower smoke paths:
//
//   - marketdata/internal/provider/alpaca/smoke_test.go
//     (TestSmokeFetchBars, issue #297/EQ-04) exercises only the raw
//     provider client's FetchBars call — no canonicalization, no
//     Manager, no persistence.
//   - alpaca_integration_test.go's own offline, fixture-backed tests
//     already deterministically prove the sync/idempotency/revision/
//     coverage mechanisms this file exercises live — this file is not
//     re-litigating those mechanisms, it is confirming they hold
//     against the real service and producing a durable, reproducible
//     record of having done so.
//
// It reuses the "alpacasmoke" build tag already established by
// #297/#301/#302 rather than introducing a fourth tag for the
// identical "real Alpaca credentials, never committed" purpose (see
// docs/research/eq-09-spy-paper-smoke.org). As with every other
// smoke/fullarchive file in this codebase, this is excluded from
// normal `go test ./...` / `make check` by its build tag; running it
// is an explicit operator action.
//
// # Credentials: a local file, not environment variables
//
// config/arch_test.go's TestDomainPackagesDoNotReadEnvOrFlags fails
// the build for any os.Getenv call outside config/, cmd/, or test/ —
// test file or not — and marketdata cannot be relocated there. This
// file follows examples/eq09/smoke_test.go's own established
// alternative instead: loadAlpacaSmokeCredentials reads a local,
// uncommitted YAML profile file (default
// alpacaSmokeCredentialFile = "~/.config/alpaca/profiles/paper.yaml")
// rather than an environment variable — reading an explicit local file
// path is not restricted by that same check — falling back to the
// alpacaSmokeKeyID/SecretKey constants (edited locally, never
// committed) every other "alpacasmoke" file in this codebase already
// uses when no such file is present.
//
// # No Alpaca SDK or provider-native types escape
//
// This file never imports marketdata/internal/provider/alpaca or the
// alpacahq SDK directly. alpacaSmokeCredential below satisfies
// Config.AlpacaCredential structurally (the same technique
// cmd/trader/data/service.go's oandaTokenCredential already uses for
// OANDA), without importing the interface's own defining package.
// That this file compiles and drives the full Alpaca pipeline without
// ever importing those packages is itself the proof of EQ-13's "no
// Alpaca SDK types escape the provider boundary" acceptance criterion.
//
// This file does import marketdata/internal/provider/stooq — but only
// for TestSmokeAlpacaVsStooqComparison's own comparison leg, mirroring
// stooq_fullarchive_test.go's identical import for the identical
// reason (importing a real Stooq CSV). That is a deliberate, narrow
// dependency on Stooq's own provider package for a cross-provider
// sanity check, not a leak of Alpaca-specific internals, and does not
// weaken the claim above (PR #332 review).
//
// # Running this
//
// Either place a real Alpaca paper-account key pair at
// alpacaSmokeCredentialFile's default path, or edit
// alpacaSmokeKeyID/alpacaSmokeSecretKey below locally (never commit
// the edit). Optionally edit stooqSmokeSPYCSVPath to a real local
// Stooq export (for example "/srv/trading/data/raw/stooq/spy_us_d.csv"
// — TestSmokeAlpacaVsStooqComparison skips itself if empty, the other
// two tests do not need it), then:
//
//	go test -tags alpacasmoke ./marketdata/... -run TestSmokeAlpaca -v
//
// A failure decoding Alpaca's response, or an unexpected shape in
// what Manager hands back, is a real signal worth investigating; it
// is not expected to be flaky under normal conditions, since every
// range requested ends several days before "now" specifically to stay
// clear of any still-forming trading day.

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata/internal/provider/stooq"
	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// alpacaSmokeCredentialFile names a local YAML file this file's tests
// read their credential pair from at run time — api_key/secret_key
// fields, matching examples/eq09/smoke_test.go's own
// alpacaEQ09CredentialFile exactly, including its reasoning: this is
// the safer alternative to pasting a real secret into a tracked source
// file, and reading an explicit local file path (unlike os.Getenv) is
// not restricted by config/arch_test.go's
// TestDomainPackagesDoNotReadEnvOrFlags, so no architectural exemption
// is needed. "~/" is expanded against the current user's home
// directory. A missing file is not an error — see
// loadAlpacaSmokeCredentials.
const alpacaSmokeCredentialFile = "~/.config/alpaca/profiles/paper.yaml"

// alpacaSmokeKeyID/alpacaSmokeSecretKey are the fallback credential
// pair, used only when alpacaSmokeCredentialFile is empty or does not
// resolve to both fields — the same "edit locally, never commit"
// constants every other "alpacasmoke" file in this codebase already
// establishes.
const alpacaSmokeKeyID = ""
const alpacaSmokeSecretKey = ""
const alpacaSmokeBaseURL = "https://data.alpaca.markets"

// stooqSmokeSPYCSVPath names a real local Stooq export
// TestSmokeAlpacaVsStooqComparison compares live Alpaca data against —
// the same file stooq_fullarchive_test.go already uses. Only that one
// test needs it; it skips itself (independently of the Alpaca
// credential check) whenever this path is empty or unreadable.
const stooqSmokeSPYCSVPath = ""

// alpacaSmokeYAMLCredential is alpacaSmokeCredentialFile's expected
// shape. Fields beyond these two are ignored, not an error.
type alpacaSmokeYAMLCredential struct {
	APIKey    string `yaml:"api_key"`
	SecretKey string `yaml:"secret_key"`
}

// loadAlpacaSmokeCredentials resolves this file's credential pair:
// from alpacaSmokeCredentialFile if it exists and supplies both
// fields, otherwise from the alpacaSmokeKeyID/SecretKey constants. ok
// is false (with no error) when neither source supplies a complete
// pair — the expected, common case for a fresh checkout.
func loadAlpacaSmokeCredentials(t *testing.T) (keyID, secretKey string, ok bool) {
	t.Helper()
	if alpacaSmokeCredentialFile != "" {
		path := alpacaSmokeCredentialFile
		if rest, cut := strings.CutPrefix(path, "~/"); cut {
			home, err := os.UserHomeDir()
			require.NoError(t, err)
			path = filepath.Join(home, rest)
		}
		data, err := os.ReadFile(path)
		switch {
		case err == nil:
			var cred alpacaSmokeYAMLCredential
			require.NoError(t, yaml.Unmarshal(data, &cred), "parse %s", path)
			if cred.APIKey != "" && cred.SecretKey != "" {
				return cred.APIKey, cred.SecretKey, true
			}
		case !os.IsNotExist(err):
			t.Fatalf("read %s: %v", path, err)
		}
	}
	if alpacaSmokeKeyID != "" && alpacaSmokeSecretKey != "" {
		return alpacaSmokeKeyID, alpacaSmokeSecretKey, true
	}
	return "", "", false
}

// alpacaSmokeCredential structurally satisfies
// marketdata.Config.AlpacaCredential (alpaca.CredentialProvider's
// Credentials(ctx) (string, string, error) method) without importing
// the internal package that defines the interface — see this file's
// own "No Alpaca SDK or provider-native types escape" doc section.
type alpacaSmokeCredential struct{ key, secret string }

func (c alpacaSmokeCredential) Credentials(context.Context) (string, string, error) {
	return c.key, c.secret, nil
}

// smokeSPYListing/smokeAAPLListing mirror alpacaSPYListing/
// alpacaAAPLListing (alpaca_integration_test.go) exactly; this file
// needs its own copies because it registers them into resolvers built
// fresh per test against a real Manager, not the shared offline
// fixture helpers.
func smokeSPYListing(t *testing.T) instrument.Listing {
	t.Helper()
	inst, err := instrument.NewETF("ARCA", "SPY")
	require.NoError(t, err)
	spec, err := instrument.NewSpec(
		num.MustParsePrice("0.01"), num.MustParseQuantity("1"), num.MustParseRate("1"), num.MustParseCurrency("USD"),
	)
	require.NoError(t, err)
	listing, err := instrument.NewListing(instrument.ListingParams{
		Instrument: inst, Provider: "alpaca", Venue: "ARCA", Symbol: "SPY", Spec: spec, Tradable: true,
	})
	require.NoError(t, err)
	return listing
}

func smokeAAPLListing(t *testing.T) instrument.Listing {
	t.Helper()
	inst, err := instrument.NewEquity("NASDAQ", "AAPL")
	require.NoError(t, err)
	spec, err := instrument.NewSpec(
		num.MustParsePrice("0.01"), num.MustParseQuantity("1"), num.MustParseRate("1"), num.MustParseCurrency("USD"),
	)
	require.NoError(t, err)
	listing, err := instrument.NewListing(instrument.ListingParams{
		Instrument: inst, Provider: "alpaca", Venue: "NASDAQ", Symbol: "AAPL", Spec: spec, Tradable: true,
	})
	require.NoError(t, err)
	return listing
}

// newSmokeAlpacaManager returns a *Manager wired against the real
// Alpaca Market Data API with both SPY and AAPL registered, rooted at
// fresh temp directories. Skips the calling test (not a failure) when
// loadAlpacaSmokeCredentials finds no usable credential pair — the
// expected, common case for a fresh checkout — so every test in this
// file gets that behavior from one place rather than repeating it.
func newSmokeAlpacaManager(t *testing.T) *Manager {
	t.Helper()
	keyID, secretKey, ok := loadAlpacaSmokeCredentials(t)
	if !ok {
		t.Skipf("no Alpaca paper credentials configured: neither %s nor alpacaSmokeKeyID/SecretKey supplied a complete pair; point one at a real Alpaca paper credential pair to run this test", alpacaSmokeCredentialFile)
	}
	r := instrument.NewMemoryResolver()
	require.NoError(t, r.Register(smokeSPYListing(t)))
	require.NoError(t, r.Register(smokeAAPLListing(t)))
	mgr, err := New(Config{
		Clock:            clock.Real{},
		StoreRoot:        t.TempDir(),
		RawRoot:          t.TempDir(),
		Resolver:         r,
		ProviderName:     "alpaca",
		Calendar:         NewUSEquityCalendar(StandardUSEquityHolidays(testUSEquityCalendarYears()...)),
		AlpacaCredential: alpacaSmokeCredential{keyID, secretKey},
		AlpacaBaseURL:    alpacaSmokeBaseURL,
	})
	require.NoError(t, err)
	return mgr
}

// smokeRecentRange returns a bounded, deterministic-enough-for-a-smoke-test
// D1 range: a window of daysBack calendar days, ending marginDays before
// "now" — comfortably clear of any still-forming trading day, matching
// TestSmokeFetchBars's own safety margin.
func smokeRecentRange(t *testing.T, daysBack, marginDays int) TimeRange {
	t.Helper()
	to := time.Now().UTC().AddDate(0, 0, -marginDays)
	to = time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, time.UTC)
	from := to.AddDate(0, 0, -daysBack)
	span, err := NewTimeRange(from, to)
	require.NoError(t, err)
	return span
}

// readAllBars drains reader into a slice, failing the test on any
// error other than io.EOF.
func readAllBars(t *testing.T, ctx context.Context, reader *BarReader) []Bar {
	t.Helper()
	defer func() { _ = reader.Close() }()
	var bars []Bar
	for {
		b, err := reader.Next(ctx)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("Bars.Next: %v", err)
		}
		bars = append(bars, b)
	}
	return bars
}

// assertBarsWellFormed checks the AC-required shape of a full Bars
// read: chronological order, uniqueness by Time, and every individual
// Bar valid under Bar.Validate — exercising issue #72's own canonical
// invariant against real, live-fetched data rather than only fixtures.
func assertBarsWellFormed(t *testing.T, bars []Bar) {
	t.Helper()
	require.NotEmpty(t, bars, "expected at least one real trading day in range")
	seen := make(map[time.Time]bool, len(bars))
	for i, b := range bars {
		assert.NoErrorf(t, b.Validate(), "bar %d (%s) failed Bar.Validate", i, b.Time)
		assert.Falsef(t, seen[b.Time], "duplicate bar Time %s", b.Time)
		seen[b.Time] = true
		if i > 0 {
			assert.Truef(t, b.Time.After(bars[i-1].Time), "bar %d (%s) is not after bar %d (%s): not chronologically ordered", i, b.Time, i-1, bars[i-1].Time)
		}
	}
}

// TestSmokeAlpacaEndToEndSPY is EQ-13's central SPY acceptance
// criterion: retrieve -> canonicalize -> persist -> Manager read
// against the real Alpaca Market Data API, plus the idempotence and
// incremental-extension criteria.
func TestSmokeAlpacaEndToEndSPY(t *testing.T) {
	ctx := context.Background()
	mgr := newSmokeAlpacaManager(t)
	spy := alpacaSmokeSPYID(t)

	span := smokeRecentRange(t, 20, 5)
	query := BarQuery{Instrument: spy, Interval: D1, Range: span}

	buildResult := syncThenBuild(t, ctx, mgr, query)
	require.NotEmpty(t, buildResult.Published)
	for _, pr := range buildResult.Published {
		assert.Equal(t, "alpaca", pr.Manifest.Provider)
		t.Logf("published %s %s: %d bars, feed=%s, adjustment=%s, calendar=%s",
			pr.Manifest.Instrument, pr.Manifest.Interval, pr.BarCount, pr.Manifest.Feed, pr.Manifest.AdjustmentPolicy, pr.Manifest.CalendarVersion)
	}

	firstBars := readAllBars(t, ctx, mustBars(t, ctx, mgr, query))
	assertBarsWellFormed(t, firstBars)
	t.Logf("SPY: %d real trading days from %s to %s", len(firstBars), span.Start().Format("2006-01-02"), span.End().Format("2006-01-02"))

	cov, err := mgr.Coverage(ctx, query)
	require.NoError(t, err)
	assert.Empty(t, cov.Gaps, "no real coverage gaps expected in a plain recent SPY range")
	t.Logf("coverage: %d partition(s), %d gap(s)", len(cov.Partitions), len(cov.Gaps))

	// Idempotence (AC): re-run the identical sync/build; must produce
	// the same bars, not duplicate or alter them.
	syncThenBuild(t, ctx, mgr, query)
	secondBars := readAllBars(t, ctx, mustBars(t, ctx, mgr, query))
	assertBarsWellFormed(t, secondBars)
	require.Equal(t, len(firstBars), len(secondBars), "re-running the identical sync must not add or drop bars")
	for i := range firstBars {
		assert.True(t, firstBars[i].Time.Equal(secondBars[i].Time))
		assert.Equal(t, firstBars[i].Close.String(), secondBars[i].Close.String(), "idempotent re-run must not change an already-settled bar's Close")
	}

	// Incremental extension (AC): extend the range forward by a few
	// more real trading days and re-sync. Every date already covered
	// by span must still be present afterward (extension augments, it
	// does not replace); values may legitimately be corrected by the
	// provider (rare, and exactly what issue #325/#329's revision
	// detection exists for) so this logs rather than hard-asserts
	// value stability beyond that already-proven mechanism.
	extended, err := NewTimeRange(span.Start(), span.End().AddDate(0, 0, 3))
	require.NoError(t, err)
	extQuery := BarQuery{Instrument: spy, Interval: D1, Range: extended}
	syncThenBuild(t, ctx, mgr, extQuery)
	extendedBars := readAllBars(t, ctx, mustBars(t, ctx, mgr, extQuery))
	assertBarsWellFormed(t, extendedBars)
	assert.GreaterOrEqualf(t, len(extendedBars), len(firstBars),
		"extending the range must not lose previously-covered trading days, had %d now %d", len(firstBars), len(extendedBars))

	extendedByTime := make(map[time.Time]Bar, len(extendedBars))
	for _, b := range extendedBars {
		extendedByTime[b.Time] = b
	}
	var revisedCount int
	for _, b := range firstBars {
		eb, ok := extendedByTime[b.Time]
		assert.Truef(t, ok, "date %s from the original range is missing after extension — extension must augment, not replace", b.Time)
		if ok && eb.Close.String() != b.Close.String() {
			revisedCount++
			t.Logf("NOTE: %s Close changed from %s to %s between syncs (legitimate provider-side correction, see #325/#329)", b.Time, b.Close, eb.Close)
		}
	}
	t.Logf("SPY extension: %d -> %d bars, %d legitimately revised", len(firstBars), len(extendedBars), revisedCount)
}

// TestSmokeAlpacaEndToEndAAPL is EQ-13's AAPL retrieve/canonicalize/
// persist/Manager-read acceptance criterion, over a recent range. It
// confirms the Manifest metadata label (AdjustmentSplitAdjusted) is
// recorded against real data, but a recent range with no split
// boundary in it cannot demonstrate that adjustment semantics actually
// affect or correctly normalize returned history — that is
// TestSmokeAlpacaAAPLSplitAdjustment's own, separate job (PR #332
// review).
func TestSmokeAlpacaEndToEndAAPL(t *testing.T) {
	ctx := context.Background()
	mgr := newSmokeAlpacaManager(t)
	aapl := alpacaSmokeAAPLID(t)

	span := smokeRecentRange(t, 15, 5)
	query := BarQuery{Instrument: aapl, Interval: D1, Range: span}

	buildResult := syncThenBuild(t, ctx, mgr, query)
	require.NotEmpty(t, buildResult.Published)
	for _, pr := range buildResult.Published {
		assert.Equal(t, AdjustmentSplitAdjusted, pr.Manifest.AdjustmentPolicy, "AAPL bars must be recorded as split-adjusted")
		t.Logf("published %s %s: %d bars, feed=%s, adjustment=%s", pr.Manifest.Instrument, pr.Manifest.Interval, pr.BarCount, pr.Manifest.Feed, pr.Manifest.AdjustmentPolicy)
	}

	bars := readAllBars(t, ctx, mustBars(t, ctx, mgr, query))
	assertBarsWellFormed(t, bars)
	t.Logf("AAPL: %d real trading days from %s to %s", len(bars), span.Start().Format("2006-01-02"), span.End().Format("2006-01-02"))
}

// TestSmokeAlpacaAAPLSplitAdjustment is issue #326 (EQ-13)'s AAPL
// adjustment-sensitive acceptance criterion, addressed directly (PR
// #332 review): a small, bounded, real historical range spanning
// AAPL's real 2020-08-31 4-for-1 split — the same reference date
// stooq_aapl_fullarchive_test.go's own aaplSplitDates uses, so this
// result is directly comparable to that offline fixture's — proving
// live that Alpaca's own adjustment normalizes the split rather than
// merely labeling the Manifest correctly. The close-to-close ratio
// across the split boundary must stay near 1: split-adjusted data
// absorbs a real split into ordinary day-to-day movement, where
// raw/unadjusted data would show a discontinuous ~4x (or ~0.25x,
// depending on direction) jump.
func TestSmokeAlpacaAAPLSplitAdjustment(t *testing.T) {
	ctx := context.Background()
	mgr := newSmokeAlpacaManager(t)
	aapl := alpacaSmokeAAPLID(t)

	// 2020-08-28 (last trading day before the split) through
	// 2020-09-01 (a couple of trading days after), well in the past —
	// no "still forming session" concern applies to a fixed historical
	// range the way it does to smokeRecentRange's "now"-relative ones.
	span, err := NewTimeRange(
		time.Date(2020, 8, 26, 0, 0, 0, 0, time.UTC),
		time.Date(2020, 9, 2, 0, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	query := BarQuery{Instrument: aapl, Interval: D1, Range: span}

	buildResult := syncThenBuild(t, ctx, mgr, query)
	require.NotEmpty(t, buildResult.Published)
	for _, pr := range buildResult.Published {
		assert.Equal(t, AdjustmentSplitAdjusted, pr.Manifest.AdjustmentPolicy)
	}

	bars := readAllBars(t, ctx, mustBars(t, ctx, mgr, query))
	assertBarsWellFormed(t, bars)

	byDate := make(map[string]Bar, len(bars))
	for _, b := range bars {
		byDate[b.Time.Format("2006-01-02")] = b
	}
	before, ok := byDate["2020-08-28"]
	require.True(t, ok, "missing bar for 2020-08-28 (last trading day before AAPL's real 4-for-1 split)")
	after, ok := byDate["2020-08-31"]
	require.True(t, ok, "missing bar for 2020-08-31 (AAPL's real 4-for-1 split effective date)")

	ratio := after.Close.Float64() / before.Close.Float64()
	t.Logf("AAPL 2020 4-for-1 split: close %s (2020-08-28) -> close %s (2020-08-31), ratio=%.4f", before.Close, after.Close, ratio)
	// Mirrors stooq_aapl_fullarchive_test.go's own aaplSplitDates
	// tolerance reasoning exactly: unadjusted raw data would show ratio
	// near 1/4 = 0.25 (a real 4-for-1 split divides the pre-split price
	// by 4); split-adjusted data keeps ordinary day-to-day movement,
	// comfortably within 50% either way. The two are not remotely close
	// to each other, so a generous tolerance here still cleanly
	// distinguishes them.
	assert.InDeltaf(t, 1.0, ratio, 0.5,
		"close-to-close ratio across AAPL's real 2020-08-31 split = %.4f, want ~1 (split-adjusted); a raw/unadjusted series would show ~0.25 here", ratio)
}

// TestSmokeAlpacaVsStooqComparison is EQ-13's cross-provider
// comparison acceptance criterion: a small D1 range covered by both a
// live Alpaca sync and the existing real local Stooq archive, compared
// with a documented tolerance rather than byte equality — Alpaca's IEX
// feed and Stooq's own upstream source are not guaranteed to close a
// millisecond-identical price, and both providers being independently
// split-adjusted is exactly what a close, not identical, comparison is
// meant to demonstrate.
func TestSmokeAlpacaVsStooqComparison(t *testing.T) {
	if stooqSmokeSPYCSVPath == "" {
		t.Skip("stooqSmokeSPYCSVPath is empty; edit the constant in this file to point at a real local Stooq SPY CSV export (for example /srv/trading/data/raw/stooq/spy_us_d.csv) to run this test")
	}
	if info, err := os.Stat(stooqSmokeSPYCSVPath); err != nil || info.IsDir() {
		t.Skipf("stooqSmokeSPYCSVPath %q is not a readable file: %v", stooqSmokeSPYCSVPath, err)
	}

	ctx := context.Background()

	// Live Alpaca leg.
	alpacaMgr := newSmokeAlpacaManager(t)
	spy := alpacaSmokeSPYID(t)
	span := smokeRecentRange(t, 10, 5)
	query := BarQuery{Instrument: spy, Interval: D1, Range: span}
	syncThenBuild(t, ctx, alpacaMgr, query)
	alpacaBars := readAllBars(t, ctx, mustBars(t, ctx, alpacaMgr, query))
	require.NotEmpty(t, alpacaBars, "expected real recent SPY trading days from Alpaca")

	// Local real Stooq leg — same instrument.ID (spyID/alpacaSmokeSPYID
	// both construct ETF("ARCA","SPY"), which is provider-independent
	// identity per ADR-003), different provider/store, imported from
	// the real local archive already used by stooq_fullarchive_test.go.
	stooqRawRoot := t.TempDir()
	result, err := stooq.Import(ctx, stooqSmokeSPYCSVPath, stooqRawRoot, "SPY")
	require.NoError(t, err)
	t.Logf("imported %d Stooq rows through %s", result.RowsImported, result.LastDate.Format("2006-01-02"))

	stooqResolver := instrument.NewMemoryResolver()
	require.NoError(t, stooqResolver.Register(spyListing(t)))
	stooqMgr, err := New(Config{
		Clock: clock.Real{}, StoreRoot: t.TempDir(), RawRoot: stooqRawRoot,
		Resolver: stooqResolver, ProviderName: "stooq",
		Calendar: NewUSEquityCalendar(StandardUSEquityHolidays(testUSEquityCalendarYears()...)),
	})
	require.NoError(t, err)
	stooqPlan, err := stooqMgr.Plan(ctx, query)
	require.NoError(t, err)
	_, err = stooqMgr.Build(ctx, stooqPlan)
	require.NoError(t, err)
	stooqBars := readAllBars(t, ctx, mustBars(t, ctx, stooqMgr, query))

	stooqByDate := make(map[string]Bar, len(stooqBars))
	for _, b := range stooqBars {
		stooqByDate[b.Time.Format("2006-01-02")] = b
	}

	// Comparison tolerance: 1% of Close. Documented rather than
	// discovered — Alpaca IEX and Stooq's own upstream source can
	// legitimately disagree slightly; this is a sanity comparison, not
	// a claim the two are the same feed.
	const tolerance = 0.01
	var compared int
	for _, ab := range alpacaBars {
		date := ab.Time.Format("2006-01-02")
		sb, ok := stooqByDate[date]
		if !ok {
			t.Logf("NOTE: %s present in Alpaca but not in the local Stooq archive's covered range — skipped", date)
			continue
		}
		compared++
		diff := closeDiffRatio(t, ab.Close, sb.Close)
		t.Logf("%s: alpaca close=%s stooq close=%s diff=%.4f%%", date, ab.Close, sb.Close, diff*100)
		assert.LessOrEqualf(t, diff, tolerance, "%s: Alpaca/Stooq Close differ by more than %.0f%% (alpaca=%s stooq=%s)", date, tolerance*100, ab.Close, sb.Close)
	}
	assert.Greaterf(t, compared, 0, "expected at least one overlapping trading day between the live Alpaca range and the local Stooq archive")
}

// closeDiffRatio returns |a-b|/b as a float64, for the comparison
// tolerance check only — analytical, not an authoritative value, so
// float64 here does not conflict with ADR-004's exact-value rule for
// order/accounting values. Uses Price.Float64() directly, not a
// String()+strconv.ParseFloat round-trip: ADR-045 documents that
// round-trip as a smell, and Price.Float64() already handles the
// whole/fractional split needed to stay exact within float64's 53-bit
// range (PR #332 review). b (the comparison baseline) must be
// non-zero — a real SPY/AAPL close is never legitimately zero, so a
// zero baseline is a real data problem to fail loudly on, not a
// silent 0% difference.
func closeDiffRatio(t *testing.T, a, b num.Price) float64 {
	t.Helper()
	bf := b.Float64()
	require.NotZero(t, bf, "comparison baseline Close must not be zero")
	diff := a.Float64() - bf
	if diff < 0 {
		diff = -diff
	}
	return diff / bf
}

// syncThenBuild is the two-phase acquire-then-canonicalize flow every
// test in this file needs against a store with no existing raw data:
// Sync executes whatever ActionDownloadRaw entries the first Plan
// contains (Build silently skips those — downloading is Sync's job,
// not Build's, per Manager.Build's own action-kind switch), then a
// second Plan against the now-populated raw store returns
// ActionNormalizeCanonical entries for Build to actually publish.
// Returns the BuildResult so callers can inspect Published/Skipped.
func syncThenBuild(t *testing.T, ctx context.Context, mgr *Manager, query BarQuery) BuildResult {
	t.Helper()
	plan, err := mgr.Plan(ctx, query)
	require.NoError(t, err)
	syncResult, err := mgr.Sync(ctx, plan)
	require.NoError(t, err, "a decoding/auth error here means the live pipeline's assumed shape needs correcting against the real API")
	for _, d := range syncResult.Downloaded {
		t.Logf("synced %s %s %04d-%02d: %d record(s) written, %d revised",
			d.Action.Instrument, d.Action.Interval, d.Action.Year, int(d.Action.Month), d.RecordsWritten, d.RecordsRevised)
	}

	plan2, err := mgr.Plan(ctx, query)
	require.NoError(t, err)
	buildResult, err := mgr.Build(ctx, plan2)
	require.NoError(t, err)
	for _, sk := range buildResult.Skipped {
		t.Logf("build skipped %s %s %04d-%02d: %s", sk.Action.Instrument, sk.Action.Interval, sk.Action.Year, int(sk.Action.Month), sk.Reason)
	}
	return buildResult
}

// mustBars is a small ctx/require-wiring convenience so every call
// site above reads as one line rather than repeating error handling.
func mustBars(t *testing.T, ctx context.Context, mgr *Manager, query BarQuery) *BarReader {
	t.Helper()
	reader, err := mgr.Bars(ctx, query)
	require.NoError(t, err)
	return reader
}

// alpacaSmokeSPYID/alpacaSmokeAAPLID mirror alpacaSPYID/alpacaAAPLID
// (alpaca_integration_test.go); this file needs its own copies for the
// same reason smokeSPYListing/smokeAAPLListing do.
func alpacaSmokeSPYID(t *testing.T) instrument.ID {
	t.Helper()
	inst, err := instrument.NewETF("ARCA", "SPY")
	require.NoError(t, err)
	return inst.ID()
}

func alpacaSmokeAAPLID(t *testing.T) instrument.ID {
	t.Helper()
	inst, err := instrument.NewEquity("NASDAQ", "AAPL")
	require.NoError(t, err)
	return inst.ID()
}
