package backtest

// This file is intentionally package backtest (internal), not
// backtest_test: it needs direct access to this package's own
// unexported composition-root helpers (environmentFactory,
// nextBarOpenPriceSource) to drive strategy/smatrend through
// service/backtest exactly the way run.go itself does for the
// --config/EMA-crossover path, so the in-tree side of this comparison
// is built the identical way a real "trader backtest run" invocation
// would build it — not a simplified stand-in.
//
// This is issue #384's own milestone-completion gate, superseding
// #383's narrower trades/account-only comparison with the full
// "compare at minimum" list that issue asks for: strategy descriptor/
// data requirements, emitted intent semantics and ordering,
// correlation relationships, orders/fills, closed/open trades, final
// equity, the equity curve, signal/journal records, and manifest
// identity — all driven through the real service/backtest runtime
// (never a private shortcut). Manifest.StrategyParameters is
// normalized and compared (smaLongHoldSemanticIdentity, below) rather
// than excluded, since it is exactly the "manifest strategy/config
// identity" issue #384 asks for; only each side's own separate
// implementation identity (Descriptor.Name/Version, Manifest.
// StrategyName, and journal.Signal.Strategy) is intentionally
// excluded, named explicitly wherever it is, not silently ignored.
//
// Reused technique: backtest/determinism_test.go's own idNormalizer
// (issue #223) proved that two independently-seeded runs of the
// *same* strategy produce the identical *causal shape* despite
// entirely different literal ULIDs. The identical technique applies
// unchanged to two *different* Strategy implementations of the same
// trading logic: neither run's IDs are ever expected to be literally
// equal, only their relative shape. idNormalizer/compareTrades below
// are close ports of that file's own versions (this package cannot
// import backtest_test's unexported helpers, so they are duplicated,
// not shared, matching strategysdk's own "each side of a boundary
// owns its own half of the translation logic" precedent elsewhere in
// this codebase).

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/adapters/strategy/external"
	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/journal"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/rustyeddy/trader/risk"
	svcbacktest "github.com/rustyeddy/trader/service/backtest"
	"github.com/rustyeddy/trader/strategy"
	"github.com/rustyeddy/trader/strategy/smatrend"
)

// buildSMALongHoldBinary builds examples/sma-long-hold into t's own
// t.TempDir() (automatically removed once t ends) — this file has its
// own build step rather than reusing external_strategy_test.go's
// TestMain (package backtest_test, a different Go package sharing
// this directory) since a test binary may define only one TestMain
// across every _test.go file in a directory, internal and external
// test packages alike.
func buildSMALongHoldBinary(t *testing.T) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "sma-long-hold")
	build := exec.Command("go", "build", "-o", out, "github.com/rustyeddy/trader/examples/sma-long-hold")
	build.Stdout = os.Stdout
	build.Stderr = os.Stderr
	require.NoError(t, build.Run())
	return out
}

// smaLongHoldEquivalenceListing mirrors run_test.go's own
// newGappedFixtureManager pattern: a real registered EUR/USD listing
// on both the "oanda" (historical) and "sim" (broker/fill) providers,
// exactly the two-resolver split run.go's own resolveInstrumentSet
// maintains for every real CLI invocation.
func smaLongHoldEquivalenceListing(t *testing.T, provider string) instrument.Listing {
	t.Helper()
	eurusd, err := instrument.NewCurrencyPair(num.MustParseCurrency("EUR"), num.MustParseCurrency("USD"))
	require.NoError(t, err)
	spec, err := instrument.NewSpec(
		num.MustParsePrice("0.00001"),
		num.MustParseQuantity("1"),
		num.MustParseRate("1"),
		num.MustParseCurrency("USD"),
	)
	require.NoError(t, err)
	listing, err := instrument.NewListing(instrument.ListingParams{
		Instrument: eurusd,
		Provider:   provider,
		Symbol:     "EURUSD",
		Spec:       spec,
		Tradable:   true,
	})
	require.NoError(t, err)
	return listing
}

// smaLongHoldExitRule/smaLongHoldReEntryRule/smaLongHoldInitialEntryMode
// name the exact rule identities this scenario holds constant on both
// sides — smatrend.DefaultExitRuleName/DefaultReEntryRuleName's own
// literal values, plus InitialEntryModeName's identical default
// (smatrend exports no matching InitialEntryModeName constant to
// import; this literal must be kept in sync with that package's own
// DefaultInitialEntryModeName by hand).
const (
	smaLongHoldExitRule         = "trailing-stop"
	smaLongHoldReEntryRule      = "fresh-cross"
	smaLongHoldInitialEntryMode = "fresh-cross"
)

// smaLongHoldSemanticIdentity is the normalized, cross-implementation
// strategy configuration issue #384 asks be compared as "manifest
// strategy/config identity" (review finding: an earlier version of
// this test excluded StrategyParameters from comparison entirely,
// rather than normalizing it) — the actual tuning knobs governing
// trading behavior, independent of each side's own separate
// Descriptor.Name/Version implementation identity (see this file's
// own doc comment for why those remain excluded, not normalized: they
// identify *which code ran*, not *what it was configured to do*).
//
// JSON tags deliberately match smatrend.Config's own tags exactly, so
// json.Unmarshal(inTree.resp.Manifest.StrategyParameters(), ...)
// decodes smatrend's real Config directly into this narrower shape
// (SMAPeriod/TrailingStopPercent/ExitRuleName/ReEntryRuleName/
// InitialEntryModeName is a superset of this type's own fields). The
// external run's own RunRequest.StrategyParameters (below) is built
// directly as this same type, so both manifests decode into it
// symmetrically — this is a deliberate divergence from run.go's own
// real --strategy-exec convention (which records launch metadata —
// exec/args/config path — as StrategyParameters, not the guest's
// decoded internal config, since the host never learns that value
// over the wire): this dedicated equivalence test needs the actual
// semantic configuration to assert against, which the guest process
// here happens to know precisely because this test wrote its own
// config file for it moments before launch.
type smaLongHoldSemanticIdentity struct {
	SMAPeriod            int    `json:"sma_period"`
	TrailingStopPercent  string `json:"trailing_stop_percent"`
	ExitRuleName         string `json:"exit_rule"`
	ReEntryRuleName      string `json:"reentry_rule"`
	InitialEntryModeName string `json:"initial_entry_mode"`
}

// smaLongHoldEquivalenceParams is the one configuration both sides of
// this equivalence comparison must agree on byte-for-byte: SMA
// period, trailing stop percent, instrument, and interval.
type smaLongHoldEquivalenceParams struct {
	smaPeriod           int
	trailingStopPercent string
	span                marketdata.TimeRange
	startingCapital     num.Money
	riskFraction        num.Rate
	adverseDistance     num.Price
}

// defaultSMALongHoldEquivalenceParams uses a small, deliberately
// engineered fixture (testdata/raw/oanda/EURUSD/2024/06) rather than
// the repo's other, organically-sourced EURUSD fixtures: real OANDA
// data from those months happens to stay continuously above its own
// short-period SMA once warmed up (a strong, smooth uptrend), so
// "fresh-cross" — the exact default this comparison must exercise —
// never actually fires and this test would trivially compare two
// empty runs. This fixture instead engineers a real below-SMA warm-up,
// a sharp cross-above (bar 9) triggering entry, three bars of rising
// highs ratcheting the trailing stop upward (bars 10-12), then a sharp
// reversal (bar 13) whose Low breaches the ratcheted stop, closing the
// position via the real broker-side resting-order mechanism (ADR-026)
// — a genuine, non-trivial entry/ratchet/stop-out episode.
func defaultSMALongHoldEquivalenceParams(t *testing.T) smaLongHoldEquivalenceParams {
	t.Helper()
	span, err := marketdata.NewTimeRange(
		time.Date(2024, time.June, 3, 0, 0, 0, 0, time.UTC),
		time.Date(2024, time.June, 3, 16, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	return smaLongHoldEquivalenceParams{
		smaPeriod:           5,
		trailingStopPercent: "0.02",
		span:                span,
		startingCapital:     num.MustParseMoney("10000", num.MustParseCurrency("USD")),
		riskFraction:        num.MustParseRate("0.10"),
		adverseDistance:     num.MustParsePrice("0.01000"),
	}
}

// capturingRecorder is an in-memory journal.Recorder collecting every
// Record it receives, in call order — the same fixture backtest/
// determinism_test.go's own capturingRecorder is, duplicated here
// (see this file's own doc comment for why).
type capturingRecorder struct {
	records []journal.Record
}

func (r *capturingRecorder) Record(_ context.Context, rec journal.Record) error {
	r.records = append(r.records, rec)
	return nil
}

func (r *capturingRecorder) Close() error { return nil }

// idNormalizer maps every opaque, run-local identifier it sees (in
// first-seen order, scoped to one run) to a stable "<kind>-<n>" token
// — see backtest/determinism_test.go's own identical type for the
// full rationale. Applied here across two *different* Strategy
// implementations rather than two seeds of the same one: the claim
// being tested is still "same causal graph," which literal ID
// equality was never expected to prove either way.
type idNormalizer struct {
	tokens map[string]string
	next   map[string]int
}

func newIDNormalizer() *idNormalizer {
	return &idNormalizer{tokens: map[string]string{}, next: map[string]int{}}
}

func (n *idNormalizer) token(kind, raw string) string {
	if raw == "" {
		return ""
	}
	if tok, ok := n.tokens[kind+":"+raw]; ok {
		return tok
	}
	n.next[kind]++
	tok := kind + "-" + strconvItoa(n.next[kind])
	n.tokens[kind+":"+raw] = tok
	return tok
}

// strconvItoa avoids importing strconv solely for this; every count
// here is small (well under 100 records per test run) — matching
// backtest/determinism_test.go's own identical itoa helper.
func strconvItoa(i int) string {
	digits := "0123456789"
	if i == 0 {
		return "0"
	}
	var buf []byte
	for i > 0 {
		buf = append([]byte{digits[i%10]}, buf...)
		i /= 10
	}
	return string(buf)
}

func (n *idNormalizer) run(v id.RunID) string       { return n.token("run", v.String()) }
func (n *idNormalizer) intent(v id.IntentID) string { return n.token("intent", v.String()) }
func (n *idNormalizer) event(v id.EventID) string   { return n.token("event", v.String()) }
func (n *idNormalizer) correlation(v id.CorrelationID) string {
	return n.token("correlation", v.String())
}
func (n *idNormalizer) account(v id.AccountID) string { return n.token("account", v.String()) }
func (n *idNormalizer) order(v id.OrderID) string     { return n.token("order", v.String()) }
func (n *idNormalizer) fill(v id.FillID) string       { return n.token("fill", v.String()) }

// brokerOrderID normalizes sim's own "sim-<OrderID>" BrokerOrderID
// convention, matching backtest/determinism_test.go's own identical
// helper.
func (n *idNormalizer) brokerOrderID(v string) string {
	const prefix = "sim-"
	if len(v) <= len(prefix) || v[:len(prefix)] != prefix {
		return n.token("broker-order-raw", v)
	}
	return prefix + n.token("order", v[len(prefix):])
}

func (n *idNormalizer) metadata(m id.Metadata) (event, correlation, causation string) {
	event = n.event(m.EventID)
	correlation = n.correlation(m.CorrelationID)
	if !m.CausationID.IsZero() {
		causation = n.event(m.CausationID)
	}
	return
}

// smaLongHoldRun is one side's complete observable output: the
// service response, this side's own Descriptor (captured before
// Start, the same shape both implementations expose since
// ExternalStrategyAdapter itself implements strategy.Strategy), and
// the captured journal alongside a fresh idNormalizer for it.
type smaLongHoldRun struct {
	resp       svcbacktest.RunResponse
	descriptor strategy.Descriptor
	records    []journal.Record
	norm       *idNormalizer
}

// runInTreeSMATrend drives strategy/smatrend through service/backtest
// exactly like run.go's own --config path does, configured to
// smatrend's own EQS-01 defaults (trailing-stop/fresh-cross/
// fresh-cross) — the same defaults examples/sma-long-hold reproduces.
func runInTreeSMATrend(t *testing.T, manager *marketdata.Manager, simResolver instrument.Resolver, simListing instrument.Listing, p smaLongHoldEquivalenceParams) smaLongHoldRun {
	t.Helper()
	ctx := t.Context()

	interval := marketdata.H1
	trailingStopPercent, err := num.ParseRate(p.trailingStopPercent)
	require.NoError(t, err)

	// ExitRuleName/ReEntryRuleName/InitialEntryModeName are set
	// explicitly to their own default values here (review finding),
	// not left empty: Config.exitRuleName()/reEntryRuleName()/
	// initialEntryModeName() resolve an empty field to
	// DefaultExitRuleName/DefaultReEntryRuleName/
	// DefaultInitialEntryModeName at read time, but strat.Config()
	// below returns the raw, unresolved Config struct — leaving these
	// empty would record an empty string in the manifest's own
	// StrategyParameters, not the actual resolved semantic identity
	// this test's own "manifest strategy/config identity" comparison
	// needs to assert against.
	strat, err := smatrend.New(simListing.InstrumentID(), interval, smatrend.Config{
		SMAPeriod:            p.smaPeriod,
		TrailingStopPercent:  trailingStopPercent,
		ExitRuleName:         smaLongHoldExitRule,
		ReEntryRuleName:      smaLongHoldReEntryRule,
		InitialEntryModeName: smaLongHoldInitialEntryMode,
	})
	require.NoError(t, err)
	descriptor := strat.Describe()

	src := newNextBarOpenPriceSource()
	require.NoError(t, src.load(ctx, manager, simListing.Symbol(), marketdata.BarQuery{
		Instrument: simListing.InstrumentID(), Interval: interval, Range: p.span,
	}))

	rec := &capturingRecorder{}
	factory := environmentFactory{prices: src, journal: rec}
	svc, err := svcbacktest.New(manager, simResolver, factory, nil)
	require.NoError(t, err)

	resp, err := svc.Run(ctx, svcbacktest.RunRequest{
		Strategy:           strat,
		StrategyParameters: strat.Config(),
		Span:               p.span,
		StartingCapital:    p.startingCapital,
		RiskFraction:       p.riskFraction,
		AdverseDistance:    p.adverseDistance,
	})
	require.NoError(t, err)
	return smaLongHoldRun{resp: resp, descriptor: descriptor, records: rec.records, norm: newIDNormalizer()}
}

// runExternalSMALongHold drives examples/sma-long-hold through the
// identical service/backtest composition, but launched as a real
// out-of-process guest via adapters/strategy/external.Launch — the
// same construction run.go's own --strategy-exec branch performs,
// built directly here (rather than through the CLI's own text/JSON
// rendering) so its RunResponse can be compared field-for-field
// against runInTreeSMATrend's, with no serialization round trip in
// between.
func runExternalSMALongHold(t *testing.T, manager *marketdata.Manager, simResolver instrument.Resolver, simListing instrument.Listing, p smaLongHoldEquivalenceParams) smaLongHoldRun {
	t.Helper()
	ctx := t.Context()

	interval := marketdata.H1

	configPath := filepath.Join(t.TempDir(), "sma-long-hold.json")
	configJSON, err := json.Marshal(map[string]any{
		"base": "EUR", "quote": "USD",
		"interval_unit": "hour", "interval_count": 1,
		"sma_period":            p.smaPeriod,
		"trailing_stop_percent": p.trailingStopPercent,
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(configPath, configJSON, 0o600))

	process, err := external.Launch(ctx, external.LaunchConfig{
		Command: buildSMALongHoldBinary(t),
		Env:     append(os.Environ(), "TRADER_STRATEGY_CONFIG="+configPath),
	})
	require.NoError(t, err)
	// context.Background(), not t.Context(): Process.Stop's own
	// SIGTERM-then-grace-then-SIGKILL shutdown should run to
	// completion on its own configured grace period, not be coupled to
	// whatever timing t.Context()'s own cancellation happens to have
	// relative to t.Cleanup's own invocation.
	t.Cleanup(func() { _ = process.Stop(context.Background()) })

	descriptor := process.Strategy().Describe()

	src := newNextBarOpenPriceSource()
	require.NoError(t, src.load(ctx, manager, simListing.Symbol(), marketdata.BarQuery{
		Instrument: simListing.InstrumentID(), Interval: interval, Range: p.span,
	}))

	rec := &capturingRecorder{}
	factory := environmentFactory{prices: src, journal: rec}
	svc, err := svcbacktest.New(manager, simResolver, factory, nil)
	require.NoError(t, err)

	resp, err := svc.Run(ctx, svcbacktest.RunRequest{
		Strategy: process.Strategy(),
		// See smaLongHoldSemanticIdentity's own doc comment for why
		// this is the guest's actual decoded semantic configuration,
		// not run.go's own real --strategy-exec launch-metadata
		// convention.
		StrategyParameters: smaLongHoldSemanticIdentity{
			SMAPeriod:            p.smaPeriod,
			TrailingStopPercent:  p.trailingStopPercent,
			ExitRuleName:         smaLongHoldExitRule,
			ReEntryRuleName:      smaLongHoldReEntryRule,
			InitialEntryModeName: smaLongHoldInitialEntryMode,
		},
		Span:            p.span,
		StartingCapital: p.startingCapital,
		RiskFraction:    p.riskFraction,
		AdverseDistance: p.adverseDistance,
	})
	require.NoError(t, err)

	// A normal-completion SessionEnd before this run's own process.Stop
	// (deferred above) fires — mirroring run.go's own --strategy-exec
	// two-phase shutdown — so the external process's own logs read as
	// a clean exit, not a SIGTERM interrupting an active Run stream.
	if closer, ok := process.Strategy().(interface{ Close(context.Context) error }); ok {
		_ = closer.Close(context.Background())
	}

	return smaLongHoldRun{resp: resp, descriptor: descriptor, records: rec.records, norm: newIDNormalizer()}
}

// tradeKey is a trade's own economically meaningful content, with
// every identifier (AccountID, FillIDs) deliberately excluded — the
// in-tree and external runs use independently generated IDs and are
// never expected to agree on those, only on what actually happened.
type tradeKey struct {
	Instrument  string
	Side        order.PositionSide
	OpenedAt    time.Time
	ClosedAt    time.Time
	RealizedPnL string
	Costs       string
}

func tradeKeys(trades []order.Trade) []tradeKey {
	keys := make([]tradeKey, len(trades))
	for i, tr := range trades {
		keys[i] = tradeKey{
			Instrument:  tr.Listing.InstrumentID().String(),
			Side:        tr.Side,
			OpenedAt:    tr.OpenedAt.UTC(),
			ClosedAt:    tr.ClosedAt.UTC(),
			RealizedPnL: tr.RealizedPnL.String(),
			Costs:       tr.Costs.String(),
		}
	}
	return keys
}

// positionKey is one open account.Position's own economically
// meaningful content — instrument, side, quantity, and average price.
type positionKey struct {
	Instrument string
	Side       order.PositionSide
	Quantity   string
	AvgPrice   string
}

func positionKeys(positions []order.Position) []positionKey {
	keys := make([]positionKey, len(positions))
	for i, p := range positions {
		var avgPrice string
		if p.AvgPrice != nil {
			avgPrice = p.AvgPrice.String()
		}
		keys[i] = positionKey{
			Instrument: p.Listing.InstrumentID().String(),
			Side:       p.Side,
			Quantity:   p.Quantity.String(),
			AvgPrice:   avgPrice,
		}
	}
	return keys
}

// requiredKinds mirrors backtest/determinism_test.go's own identical
// list (minus KindAccount, unreachable from any sim.Broker-backed run
// today — see that file's own doc comment) plus KindSignal, which
// this scenario's own engineered episode is specifically built to
// produce from both sides (an enter-long and at least one
// adjust-stop, each recording decision evidence) — issue #384's own
// "signal/journal records where deterministic" bullet.
var requiredKinds = []journal.Kind{
	journal.KindRunStarted,
	journal.KindIntent,
	journal.KindProposal,
	journal.KindDecision,
	journal.KindRequest,
	journal.KindReplaceRequest,
	journal.KindOrder,
	journal.KindFill,
	journal.KindTrade,
	journal.KindSignal,
	journal.KindRunCompleted,
}

func assertRequiredKindsPresent(t *testing.T, records []journal.Record) {
	t.Helper()
	seen := map[journal.Kind]bool{}
	for _, rec := range records {
		seen[rec.Kind] = true
	}
	for _, k := range requiredKinds {
		assert.Truef(t, seen[k], "expected at least one %s record in the journal, found none — this equivalence gate cannot protect a kind it never observes", k)
	}
}

// compareRecordSemantics compares r1/r2 (already known to share a
// Kind) field by field, normalizing every opaque ID through each
// run's own idNormalizer first — never comparing raw ULIDs, which are
// never expected to be literally equal between two independent
// implementations/runs. Started as a full copy of backtest/
// determinism_test.go's own current function of the same name (see
// this file's own doc comment for why it is duplicated, not shared),
// then extended further (review finding, second round): Intent's own
// Quantity/StopPrice/Metadata, Proposal's own LimitPrice/StopPrice/
// ReduceOnly/Metadata, and Request's full embedded Proposal semantics
// were compared even more narrowly than the source function itself —
// this gate now compares strictly more than that source, factored
// through compareMetadata/compareIntent/compareProposal/
// compareRequest so Proposal/Request/Order's shared embedded-Proposal
// semantics are checked identically everywhere they appear, rather
// than three independent, driftable hand-copies of the same fields.
// Two additions beyond backtest/determinism_test.go's own kinds:
// journal.KindReplaceRequest (this scenario's three ratcheting
// AdjustStop intents all take that path, ADR-054) and
// journal.KindSignal (decision evidence, unique to this gate — see
// its own case below for why Strategy specifically is excluded). One
// deliberate omission, matching the source function exactly:
// journal.KindAccount, unreachable from any sim.Broker-backed run
// today.
func compareRecordSemantics(t *testing.T, i int, r1, r2 journal.Record, n1, n2 *idNormalizer) {
	t.Helper()

	compareMetadata(t, i, "record", r1.Metadata, r2.Metadata, n1, n2)

	switch r1.Kind {
	case journal.KindRunStarted:
		assert.Equalf(t, n1.run(r1.RunStarted.RunID), n2.run(r2.RunStarted.RunID), "record[%d]/run_started: run id shape mismatch", i)
	case journal.KindIntent:
		compareIntent(t, i, "intent", *r1.Intent, *r2.Intent, n1, n2)
	case journal.KindProposal:
		compareProposal(t, i, "proposal", *r1.Proposal, *r2.Proposal, n1, n2)
	case journal.KindDecision:
		d1, d2 := r1.Decision, r2.Decision
		assert.Equalf(t, d1.Allowed, d2.Allowed, "record[%d]/decision: allowed mismatch", i)
		compareViolations(t, i, "decision", d1.Violations, d2.Violations)
		compareWarnings(t, i, "decision", d1.Warnings, d2.Warnings)
		require.Equalf(t, len(d1.RuleResults), len(d2.RuleResults), "record[%d]/decision: rule result count mismatch", i)
		for j := range d1.RuleResults {
			rr1, rr2 := d1.RuleResults[j], d2.RuleResults[j]
			assert.Equalf(t, rr1.Rule, rr2.Rule, "record[%d]/decision: rule_results[%d] name mismatch", i, j)
			compareViolations(t, i, fmt.Sprintf("decision.rule_results[%d]", j), rr1.Violations, rr2.Violations)
			compareWarnings(t, i, fmt.Sprintf("decision.rule_results[%d]", j), rr1.Warnings, rr2.Warnings)
		}
	case journal.KindRequest:
		compareRequest(t, i, "request", *r1.Request, *r2.Request, n1, n2)
	case journal.KindReplaceRequest:
		rr1, rr2 := r1.ReplaceRequest, r2.ReplaceRequest
		assert.Equalf(t, n1.order(rr1.OrderID), n2.order(rr2.OrderID), "record[%d]/replace_request: order id shape mismatch", i)
		comparePrice(t, i, "replace_request.new_stop_price", rr1.NewStopPrice, rr2.NewStopPrice)
		comparePrice(t, i, "replace_request.new_limit_price", rr1.NewLimitPrice, rr2.NewLimitPrice)
		if rr1.NewQuantity != nil && rr2.NewQuantity != nil {
			assert.Truef(t, rr1.NewQuantity.Equal(*rr2.NewQuantity), "record[%d]/replace_request: new quantity mismatch", i)
		} else {
			assert.Equalf(t, rr1.NewQuantity == nil, rr2.NewQuantity == nil, "record[%d]/replace_request: new quantity nilness mismatch", i)
		}
	case journal.KindOrder:
		o1, o2 := r1.Order, r2.Order
		// The embedded Request carries the order's own full Proposal
		// semantics (type/TIF/limit/stop/reduce-only/account/metadata)
		// — compared here via the identical compareRequest helper
		// KindRequest itself uses (review finding: an earlier version
		// represented the embedded Request by OrderID alone, so a
		// divergence there could hide behind an earlier, separately-
		// passing KindRequest record for the same order).
		compareRequest(t, i, "order.request", o1.Request, o2.Request, n1, n2)
		assert.Equalf(t, o1.Status, o2.Status, "record[%d]/order: status mismatch", i)
		assert.Equalf(t, n1.brokerOrderID(o1.BrokerOrderID), n2.brokerOrderID(o2.BrokerOrderID), "record[%d]/order: broker order id shape mismatch", i)
		comparePrice(t, i, "order.accepted_limit_price", o1.AcceptedLimitPrice, o2.AcceptedLimitPrice)
		comparePrice(t, i, "order.accepted_stop_price", o1.AcceptedStopPrice, o2.AcceptedStopPrice)
		assert.Truef(t, o1.FilledQuantity.Equal(o2.FilledQuantity), "record[%d]/order: filled quantity mismatch: got %s want %s", i, o2.FilledQuantity, o1.FilledQuantity)
		if o1.AcceptedQuantity != nil && o2.AcceptedQuantity != nil {
			assert.Truef(t, o1.AcceptedQuantity.Equal(*o2.AcceptedQuantity), "record[%d]/order: accepted quantity mismatch", i)
		} else {
			assert.Equalf(t, o1.AcceptedQuantity == nil, o2.AcceptedQuantity == nil, "record[%d]/order: accepted quantity nilness mismatch", i)
		}
	case journal.KindFill:
		f1, f2 := r1.Fill, r2.Fill
		assert.Truef(t, f1.Listing.InstrumentID().Equal(f2.Listing.InstrumentID()), "record[%d]/fill: instrument mismatch", i)
		assert.Equalf(t, f1.Side, f2.Side, "record[%d]/fill: side mismatch", i)
		assert.Truef(t, f1.Price.Equal(f2.Price), "record[%d]/fill: price mismatch: got %s want %s", i, f2.Price, f1.Price)
		assert.Truef(t, f1.Quantity.Equal(f2.Quantity), "record[%d]/fill: quantity mismatch", i)
		assert.Truef(t, f1.Timestamp.Equal(f2.Timestamp), "record[%d]/fill: timestamp mismatch: got %s want %s", i, f2.Timestamp, f1.Timestamp)
		compareMoney(t, i, "fill.commission", f1.Commission, f2.Commission)
		assert.Equalf(t, n1.fill(f1.FillID), n2.fill(f2.FillID), "record[%d]/fill: fill id shape mismatch", i)
		assert.Equalf(t, n1.order(f1.OrderID), n2.order(f2.OrderID), "record[%d]/fill: order id shape mismatch", i)
		assert.Equalf(t, n1.brokerOrderID(f1.BrokerOrderID), n2.brokerOrderID(f2.BrokerOrderID), "record[%d]/fill: broker order id shape mismatch", i)
		// BrokerFillID: adapters/broker/sim never populates order.Fill.
		// BrokerFillID (its own NewFill call omits the field, staying
		// permanently ""), so both sides always compare "" == "" today
		// — a plain equality check, not a normalizer, since there is
		// no non-empty broker-assigned value to normalize yet. Kept as
		// an explicit check (rather than omitted) so this comparison
		// does not silently stop protecting the field the day sim
		// starts populating it (review finding).
		assert.Equalf(t, f1.BrokerFillID, f2.BrokerFillID, "record[%d]/fill: broker fill id mismatch", i)
		assert.Equalf(t, n1.account(f1.AccountID), n2.account(f2.AccountID), "record[%d]/fill: account id shape mismatch", i)
		compareMetadata(t, i, "fill", f1.Metadata, f2.Metadata, n1, n2)
	case journal.KindTrade:
		compareTrades(t, i, *r1.Trade, *r2.Trade, n1, n2)
	case journal.KindSignal:
		// Strategy is deliberately excluded: it is each side's own
		// Descriptor.Name ("sma-trend" in-tree, "sma-long-hold"
		// external) — an intentional identity difference, documented
		// on TestSMALongHold_EquivalentToInTreeSMATrendDefaultConfig's
		// own doc comment, not a divergence to catch. Values is the
		// actual decision-evidence content and is compared byte for
		// byte — examples/sma-long-hold's own signal method mirrors
		// strategy/smatrend.recordSignal's Values map shape and keys
		// exactly for this reason.
		s1, s2 := r1.Signal, r2.Signal
		assert.Equalf(t, s1.Values, s2.Values, "record[%d]/signal: values mismatch", i)
	case journal.KindRunCompleted:
		assert.Equalf(t, r1.RunCompleted.EntryCount, r2.RunCompleted.EntryCount, "record[%d]/run_completed: entry count mismatch", i)
	default:
		t.Fatalf("record[%d]: unhandled journal.Kind %s in this equivalence gate — add a semantic comparison case for it", i, r1.Kind)
	}
}

// compareViolations compares two []risk.Violation slices field-by-field
// with a named index/field on mismatch, matching backtest/
// determinism_test.go's own identical helper.
func compareViolations(t *testing.T, i int, label string, v1, v2 []risk.Violation) {
	t.Helper()
	require.Equalf(t, len(v1), len(v2), "record[%d]/%s: violation count mismatch", i, label)
	for j := range v1 {
		assert.Equalf(t, v1[j].Rule, v2[j].Rule, "record[%d]/%s: violations[%d].rule mismatch", i, label, j)
		assert.Equalf(t, v1[j].Message, v2[j].Message, "record[%d]/%s: violations[%d].message mismatch", i, label, j)
		assert.Equalf(t, v1[j].Measured, v2[j].Measured, "record[%d]/%s: violations[%d].measured mismatch", i, label, j)
		assert.Equalf(t, v1[j].Limit, v2[j].Limit, "record[%d]/%s: violations[%d].limit mismatch", i, label, j)
	}
}

// compareWarnings compares two []risk.Warning slices field-by-field
// with a named index/field on mismatch, matching backtest/
// determinism_test.go's own identical helper.
func compareWarnings(t *testing.T, i int, label string, w1, w2 []risk.Warning) {
	t.Helper()
	require.Equalf(t, len(w1), len(w2), "record[%d]/%s: warning count mismatch", i, label)
	for j := range w1 {
		assert.Equalf(t, w1[j].Rule, w2[j].Rule, "record[%d]/%s: warnings[%d].rule mismatch", i, label, j)
		assert.Equalf(t, w1[j].Message, w2[j].Message, "record[%d]/%s: warnings[%d].message mismatch", i, label, j)
	}
}

// compareMetadata compares two id.Metadata values — every opaque ID
// normalized through each run's own idNormalizer, timestamps compared
// directly — factored out (review finding) so the top-level per-
// record Metadata check and every embedded Metadata field (Intent,
// Proposal, Fill) use the identical comparison rather than three
// independent hand-copies.
func compareMetadata(t *testing.T, i int, label string, m1, m2 id.Metadata, n1, n2 *idNormalizer) {
	t.Helper()
	e1, c1, cause1 := n1.metadata(m1)
	e2, c2, cause2 := n2.metadata(m2)
	assert.Equalf(t, e1, e2, "record[%d]/%s: metadata event id shape mismatch", i, label)
	assert.Equalf(t, c1, c2, "record[%d]/%s: metadata correlation id shape mismatch", i, label)
	assert.Equalf(t, cause1, cause2, "record[%d]/%s: metadata causation id shape mismatch", i, label)
	assert.Truef(t, m1.Timestamp.Equal(m2.Timestamp), "record[%d]/%s: metadata timestamp mismatch: got %s want %s", i, label, m2.Timestamp, m1.Timestamp)
}

// compareIntent compares two order.Intent values field by field
// (review finding: an earlier version omitted Quantity, StopPrice,
// and the intent's own Metadata — this scenario's every AdjustStop
// intent carries a non-nil StopPrice, so omitting it let two
// implementations choose different stop prices at the intent stage
// and still pass).
func compareIntent(t *testing.T, i int, label string, in1, in2 order.Intent, n1, n2 *idNormalizer) {
	t.Helper()
	assert.Equalf(t, in1.Kind, in2.Kind, "record[%d]/%s: kind mismatch", i, label)
	assert.Truef(t, in1.Instrument.Equal(in2.Instrument), "record[%d]/%s: instrument mismatch", i, label)
	assert.Equalf(t, in1.Side, in2.Side, "record[%d]/%s: side mismatch", i, label)
	if in1.Quantity != nil && in2.Quantity != nil {
		assert.Truef(t, in1.Quantity.Equal(*in2.Quantity), "record[%d]/%s: quantity mismatch", i, label)
	} else {
		assert.Equalf(t, in1.Quantity == nil, in2.Quantity == nil, "record[%d]/%s: quantity nilness mismatch", i, label)
	}
	comparePrice(t, i, label+".stop_price", in1.StopPrice, in2.StopPrice)
	assert.Equalf(t, n1.intent(in1.IntentID), n2.intent(in2.IntentID), "record[%d]/%s: intent id shape mismatch", i, label)
	compareMetadata(t, i, label+".metadata", in1.Metadata, in2.Metadata, n1, n2)
}

// compareProposal compares two order.Proposal values field by field
// (review finding: an earlier version omitted LimitPrice, StopPrice,
// ReduceOnly, and Metadata — execution-planning semantics a diverging
// proposal should fail on immediately, at the record where the
// divergence actually occurred).
func compareProposal(t *testing.T, i int, label string, p1, p2 order.Proposal, n1, n2 *idNormalizer) {
	t.Helper()
	assert.Truef(t, p1.Listing.InstrumentID().Equal(p2.Listing.InstrumentID()), "record[%d]/%s: instrument mismatch", i, label)
	assert.Equalf(t, p1.Side, p2.Side, "record[%d]/%s: side mismatch", i, label)
	assert.Equalf(t, p1.Type, p2.Type, "record[%d]/%s: type mismatch", i, label)
	assert.Equalf(t, p1.TimeInForce, p2.TimeInForce, "record[%d]/%s: time in force mismatch", i, label)
	assert.Truef(t, p1.Quantity.Equal(p2.Quantity), "record[%d]/%s: quantity mismatch: got %s want %s", i, label, p2.Quantity, p1.Quantity)
	comparePrice(t, i, label+".limit_price", p1.LimitPrice, p2.LimitPrice)
	comparePrice(t, i, label+".stop_price", p1.StopPrice, p2.StopPrice)
	assert.Equalf(t, p1.ReduceOnly, p2.ReduceOnly, "record[%d]/%s: reduce_only mismatch", i, label)
	assert.Equalf(t, n1.account(p1.AccountID), n2.account(p2.AccountID), "record[%d]/%s: account id shape mismatch", i, label)
	compareMetadata(t, i, label+".metadata", p1.Metadata, p2.Metadata, n1, n2)
}

// compareRequest compares two order.Request values: its own OrderID,
// plus its full embedded Proposal via compareProposal (review
// finding: an earlier version represented the embedded Proposal by
// instrument/side/quantity alone, so a Type/TIF/limit/stop/reduce-
// only/account/metadata divergence could hide behind an earlier,
// separately-passing KindProposal record for the same decision).
// KindOrder's own case reuses this identical helper for its embedded
// Request, rather than a fourth independent hand-copy.
func compareRequest(t *testing.T, i int, label string, req1, req2 order.Request, n1, n2 *idNormalizer) {
	t.Helper()
	compareProposal(t, i, label, req1.Proposal, req2.Proposal, n1, n2)
	assert.Equalf(t, n1.order(req1.OrderID), n2.order(req2.OrderID), "record[%d]/%s: order id shape mismatch", i, label)
}

func comparePrice(t *testing.T, i int, label string, p1, p2 *num.Price) {
	t.Helper()
	if p1 == nil || p2 == nil {
		assert.Equalf(t, p1 == nil, p2 == nil, "record[%d]/%s: nilness mismatch", i, label)
		return
	}
	assert.Truef(t, p1.Equal(*p2), "record[%d]/%s: mismatch: got %s want %s", i, label, *p2, *p1)
}

func compareMoney(t *testing.T, i int, label string, m1, m2 *num.Money) {
	t.Helper()
	if m1 == nil || m2 == nil {
		assert.Equalf(t, m1 == nil, m2 == nil, "record[%d]/%s: nilness mismatch", i, label)
		return
	}
	assert.Truef(t, m1.Equal(*m2), "record[%d]/%s: mismatch: got %s want %s", i, label, *m2, *m1)
}

// compareTrades compares two order.Trade values (either two closed
// trades at the same index, or the payload of two KindTrade journal
// records), normalizing AccountID/fill IDs and comparing every
// economically meaningful field directly.
func compareTrades(t *testing.T, i int, tr1, tr2 order.Trade, n1, n2 *idNormalizer) {
	t.Helper()
	assert.Truef(t, tr1.Listing.InstrumentID().Equal(tr2.Listing.InstrumentID()), "trade[%d]: instrument mismatch", i)
	assert.Equalf(t, tr1.Side, tr2.Side, "trade[%d]: side mismatch", i)
	assert.Truef(t, tr1.OpenedAt.Equal(tr2.OpenedAt), "trade[%d]: opened_at mismatch: got %s want %s", i, tr2.OpenedAt, tr1.OpenedAt)
	assert.Truef(t, tr1.ClosedAt.Equal(tr2.ClosedAt), "trade[%d]: closed_at mismatch: got %s want %s", i, tr2.ClosedAt, tr1.ClosedAt)
	assert.Truef(t, tr1.RealizedPnL.Equal(tr2.RealizedPnL), "trade[%d]: realized pnl mismatch: got %s want %s", i, tr2.RealizedPnL, tr1.RealizedPnL)
	assert.Truef(t, tr1.Costs.Equal(tr2.Costs), "trade[%d]: costs mismatch: got %s want %s", i, tr2.Costs, tr1.Costs)
	assert.Equalf(t, n1.account(tr1.AccountID), n2.account(tr2.AccountID), "trade[%d]: account id shape mismatch", i)

	require.Equalf(t, len(tr1.EntryFillIDs), len(tr2.EntryFillIDs), "trade[%d]: entry fill count mismatch", i)
	for j := range tr1.EntryFillIDs {
		assert.Equalf(t, n1.fill(tr1.EntryFillIDs[j]), n2.fill(tr2.EntryFillIDs[j]), "trade[%d]: entry_fill_ids[%d] shape mismatch", i, j)
	}
	require.Equalf(t, len(tr1.ExitFillIDs), len(tr2.ExitFillIDs), "trade[%d]: exit fill count mismatch", i)
	for j := range tr1.ExitFillIDs {
		assert.Equalf(t, n1.fill(tr1.ExitFillIDs[j]), n2.fill(tr2.ExitFillIDs[j]), "trade[%d]: exit_fill_ids[%d] shape mismatch", i, j)
	}
}

// TestSMALongHold_EquivalentToInTreeSMATrendDefaultConfig is issue
// #384's own milestone-completion gate: examples/sma-long-hold, driven
// as a real out-of-process guest through the normal backtest service/
// runtime (never a private shortcut — both sides call
// svcbacktest.Service.Run, the identical entry point run.go itself
// uses), produces the identical trading behavior as strategy/
// smatrend's own default configuration (trailing-stop/fresh-cross/
// fresh-cross) over identical canonical market data, held-constant
// sizing/risk/fill configuration, and independently-seeded clocks/IDs
// — proving Strategy Protocol v1 with a real, non-trivial strategy,
// not merely that a trivial guest can complete a Handshake.
//
// Fields intentionally different between the two runs, and therefore
// never compared for equality below: Descriptor.Name/Version (each
// side's own strategy identity — "sma-trend"/"v4" in-tree,
// "sma-long-hold"/"v1" external), the resulting Manifest.StrategyName,
// and journal.Signal.Strategy (ditto identity, recorded per signal).
// Manifest.StrategyParameters is normalized and compared, not
// excluded — see smaLongHoldSemanticIdentity's own doc comment. Every
// opaque identifier (RunID, AccountID, IntentID, OrderID, FillID,
// EventID/CorrelationID/CausationID) is normalized through
// idNormalizer before comparison, never compared as a literal ULID —
// see that type's own doc comment.
func TestSMALongHold_EquivalentToInTreeSMATrendDefaultConfig(t *testing.T) {
	params := defaultSMALongHoldEquivalenceParams(t)

	oandaResolver := instrument.NewMemoryResolver()
	require.NoError(t, oandaResolver.Register(smaLongHoldEquivalenceListing(t, "oanda")))
	simResolver := instrument.NewMemoryResolver()
	simListing := smaLongHoldEquivalenceListing(t, "sim")
	require.NoError(t, simResolver.Register(simListing))

	manager, err := marketdata.New(marketdata.Config{
		Clock:        clock.Real{},
		StoreRoot:    t.TempDir(),
		RawRoot:      "testdata/raw/oanda",
		Resolver:     oandaResolver,
		ProviderName: "oanda",
	})
	require.NoError(t, err)

	ctx := t.Context()
	plan, err := manager.Plan(ctx, marketdata.BarQuery{Instrument: simListing.InstrumentID(), Interval: marketdata.H1, Range: params.span})
	require.NoError(t, err)
	if len(plan.Actions) > 0 {
		_, err = manager.Build(ctx, plan)
		require.NoError(t, err)
	}

	inTree := runInTreeSMATrend(t, manager, simResolver, simListing, params)
	external := runExternalSMALongHold(t, manager, simResolver, simListing, params)

	t.Run("strategy descriptor / data requirements", func(t *testing.T) {
		// Name/Version deliberately not compared — see this test's own
		// doc comment.
		require.Equal(t, len(inTree.descriptor.Requirements), len(external.descriptor.Requirements), "requirement count mismatch")
		for i := range inTree.descriptor.Requirements {
			r1, r2 := inTree.descriptor.Requirements[i], external.descriptor.Requirements[i]
			assert.Truef(t, r1.Instrument.Equal(r2.Instrument), "requirements[%d]: instrument mismatch", i)
			assert.Equalf(t, r1.Interval, r2.Interval, "requirements[%d]: interval mismatch", i)
			assert.Equalf(t, r1.WarmupBars, r2.WarmupBars, "requirements[%d]: warmup bars mismatch", i)
		}
	})

	t.Run("manifest strategy/config identity", func(t *testing.T) {
		// StrategyName is each side's own Descriptor.Name (see this
		// test's own doc comment) — deliberately not compared. The
		// normalized semantic configuration StrategyParameters
		// actually encodes is compared directly, decoding both
		// manifests' own recorded StrategyParameters into the shared
		// smaLongHoldSemanticIdentity shape (review finding: an
		// earlier version of this test excluded StrategyParameters
		// from comparison entirely — a regression that dropped or
		// changed the external run's own recorded configuration would
		// have still passed).
		var s1, s2 smaLongHoldSemanticIdentity
		require.NoError(t, json.Unmarshal(inTree.resp.Manifest.StrategyParameters(), &s1))
		require.NoError(t, json.Unmarshal(external.resp.Manifest.StrategyParameters(), &s2))
		assert.Equal(t, s1, s2, "normalized strategy semantic identity must match between manifests")
		// ConfigDigest incorporates StrategyName (which legitimately
		// differs) and is therefore expected to differ too — not
		// compared.
	})

	t.Run("manifest universe and dataset", func(t *testing.T) {
		u1, u2 := inTree.resp.Manifest.Universe(), external.resp.Manifest.Universe()
		require.Equalf(t, len(u1), len(u2), "universe length mismatch")
		for i := range u1 {
			assert.Truef(t, u1[i].Instrument.Equal(u2[i].Instrument), "universe[%d]: instrument mismatch", i)
			assert.Equalf(t, u1[i].Interval, u2[i].Interval, "universe[%d]: interval mismatch", i)
		}

		d1, d2 := inTree.resp.Manifest.Dataset(), external.resp.Manifest.Dataset()
		require.Equalf(t, len(d1), len(d2), "dataset length mismatch")
		for i := range d1 {
			assert.Equalf(t, d1[i].Provider, d2[i].Provider, "dataset[%d]: provider mismatch", i)
			assert.Truef(t, d1[i].Instrument.Equal(d2[i].Instrument), "dataset[%d]: instrument mismatch", i)
			assert.Equalf(t, d1[i].Interval, d2[i].Interval, "dataset[%d]: interval mismatch", i)
			assert.Truef(t, d1[i].Span.Start().Equal(d2[i].Span.Start()), "dataset[%d]: span start mismatch: got %s want %s", i, d2[i].Span.Start(), d1[i].Span.Start())
			assert.Truef(t, d1[i].Span.End().Equal(d2[i].Span.End()), "dataset[%d]: span end mismatch: got %s want %s", i, d2[i].Span.End(), d1[i].Span.End())
			assert.Equalf(t, d1[i].Basis, d2[i].Basis, "dataset[%d]: basis mismatch", i)
			assert.Equalf(t, d1[i].RawFingerprint, d2[i].RawFingerprint, "dataset[%d]: raw fingerprint mismatch", i)
			assert.Equalf(t, d1[i].Revision(), d2[i].Revision(), "dataset[%d]: revision mismatch — both runs must read the identical canonical data", i)
		}
	})

	t.Run("journal contains every required event kind", func(t *testing.T) {
		assertRequiredKindsPresent(t, inTree.records)
		assertRequiredKindsPresent(t, external.records)
	})

	t.Run("journal: intent ordering, correlation, orders, fills, signals", func(t *testing.T) {
		require.Equal(t, len(inTree.records), len(external.records), "journal record count must match")
		for i := range inTree.records {
			r1, r2 := inTree.records[i], external.records[i]
			require.Equalf(t, r1.Kind, r2.Kind, "record %d: kind mismatch (first semantic divergence)", i)
			compareRecordSemantics(t, i, r1, r2, inTree.norm, external.norm)
		}
	})

	t.Run("closed and open trades", func(t *testing.T) {
		require.NotEmpty(t, inTree.resp.Trades, "the fixture/config must produce at least one closed trade for this comparison to be meaningful")
		assert.Equal(t, tradeKeys(inTree.resp.Trades), tradeKeys(external.resp.Trades),
			"closed trades must be identical between the in-tree and external runs")
		assert.Equal(t, tradeKeys(inTree.resp.OpenTrades), tradeKeys(external.resp.OpenTrades),
			"any still-open trade must be identical between the in-tree and external runs")
	})

	t.Run("equity curve", func(t *testing.T) {
		require.Equal(t, len(inTree.resp.EquityCurve), len(external.resp.EquityCurve), "equity curve length must match")
		for i := range inTree.resp.EquityCurve {
			p1, p2 := inTree.resp.EquityCurve[i], external.resp.EquityCurve[i]
			assert.Truef(t, p1.Timestamp.Equal(p2.Timestamp), "equity_curve[%d]: timestamp mismatch: got %s want %s", i, p2.Timestamp, p1.Timestamp)
			assert.Truef(t, p1.Equity.Equal(p2.Equity), "equity_curve[%d]: equity mismatch: got %s want %s", i, p2.Equity, p1.Equity)
		}
	})

	t.Run("final account state", func(t *testing.T) {
		assert.True(t, inTree.resp.Account.Equity().Equal(external.resp.Account.Equity()),
			"final account equity must match: in-tree %s, external %s", inTree.resp.Account.Equity(), external.resp.Account.Equity())
		assert.True(t, inTree.resp.Account.RealizedPnL().Equal(external.resp.Account.RealizedPnL()),
			"final realized PnL must match: in-tree %s, external %s", inTree.resp.Account.RealizedPnL(), external.resp.Account.RealizedPnL())
		assert.Equal(t, positionKeys(inTree.resp.Account.Positions()), positionKeys(external.resp.Account.Positions()),
			"final open positions must be identical, not merely the same count")
	})
}
