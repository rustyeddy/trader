//go:build alpacasmoke

package eq09

// This file is EQ-09's (#302) opt-in, real-credential end-to-end smoke
// test: SPY, real canonical market data, and a real Alpaca paper
// account, carried through Trader's normal execution/risk/pipeline
// path — see the package doc comment for what "normal path" means
// here and what is deliberately out of scope.
//
// # Why this reuses "alpacasmoke" rather than a new tag
//
// This is the third opt-in real-Alpaca-credential test in this
// codebase (after marketdata/internal/provider/alpaca's own Market
// Data smoke test, issue #297, and adapters/broker/alpaca's own
// Trading API smoke test, issue #301). All three share one build tag
// deliberately: an operator enables real Alpaca network access once
// and gets every opt-in smoke path, rather than juggling several
// nearly-identical tags for the same purpose.
//
// # Prerequisites
//
// 1. An Alpaca account with paper trading enabled (the default; no
//    funding step — the paper balance is synthetic and pre-seeded).
// 2. A paper-account API key ID/secret key pair, generated from
//    Alpaca's dashboard. The SAME pair authenticates both the Trading
//    API (order/account) and the Market Data API (bars) used here.
// 3. Edit the local constants below (never commit the edit — the
//    identical discipline marketdata/internal/provider/alpaca's own
//    smoke_test.go and adapters/broker/alpaca's own smoke_test.go
//    already establish, and for the identical reason:
//    config/arch_test.go's TestDomainPackagesDoNotReadEnvOrFlags
//    forbids os.Getenv outside config/, cmd/, or test/, and this
//    package is none of those).
// 4. Run during a real NYSE regular trading session (9:30-16:00
//    America/New_York, a trading day) — this test submits a real
//    market order and waits for a real fill; outside regular hours
//    Alpaca queues rather than rejects a market order, which would
//    make this test hang waiting for a fill that will not arrive
//    within its own timeout. The test skips (does not fail) outside
//    those hours, checked directly against marketdata.USEquityCalendar
//    rather than assumed.
//
//	go test -tags alpacasmoke ./examples/eq09/... \
//	    -run TestSmokeSPYPaperRoundTrip -v
//
// See docs/research/eq-09-spy-paper-smoke.org for the full documented
// procedure and a recorded real run's results.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/rustyeddy/trader/adapters/broker/alpaca"
	brokerpkg "github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/clock"
	execpkg "github.com/rustyeddy/trader/execution"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/rustyeddy/trader/pipeline"
	"github.com/rustyeddy/trader/risk"
	svcexecution "github.com/rustyeddy/trader/service/execution"
	mdsvc "github.com/rustyeddy/trader/service/marketdata"
)

// alpacaEQ09CredentialFile names a local YAML file this test reads its
// credential pair from at run time — api_key/secret_key fields, the
// same names Alpaca's own tooling commonly uses for a profile file —
// rather than requiring an operator to paste a real secret into this
// tracked source file at all. This is the safer alternative the other
// "alpacasmoke" files' own doc comments considered and set aside only
// because config/arch_test.go's TestDomainPackagesDoNotReadEnvOrFlags
// forbids os.Getenv/os.LookupEnv/os.Environ specifically — it does not
// restrict reading an explicit local file path, so no architectural
// exemption is needed here. "~/" is expanded against the current
// user's home directory. Point this at your own local profile path (or
// clear it) without ever committing that edit; a missing file is not
// an error — see loadAlpacaEQ09Credentials.
const alpacaEQ09CredentialFile = "~/.config/alpaca/profiles/paper.yaml"

// alpacaEQ09KeyID/SecretKey are the fallback credential pair, used only
// when alpacaEQ09CredentialFile is empty or does not resolve to both
// fields — the same "edit locally, never commit" constants every other
// "alpacasmoke" file already establishes.
const alpacaEQ09KeyID = ""
const alpacaEQ09SecretKey = ""

// alpacaYAMLCredential is alpacaEQ09CredentialFile's expected shape.
// Fields beyond these two (for example an OAuth access_token, a
// different auth mechanism this adapter does not implement) are
// ignored, not an error.
type alpacaYAMLCredential struct {
	APIKey    string `yaml:"api_key"`
	SecretKey string `yaml:"secret_key"`
}

// loadAlpacaEQ09Credentials resolves this test's credential pair: from
// alpacaEQ09CredentialFile if it exists and supplies both fields,
// otherwise from the alpacaEQ09KeyID/SecretKey constants. ok is false
// (with no error) when neither source supplies a complete pair, which
// is the expected, common case for a fresh checkout.
func loadAlpacaEQ09Credentials(t *testing.T) (keyID, secretKey string, ok bool) {
	t.Helper()
	if alpacaEQ09CredentialFile != "" {
		path := alpacaEQ09CredentialFile
		if rest, cut := strings.CutPrefix(path, "~/"); cut {
			home, err := os.UserHomeDir()
			require.NoError(t, err)
			path = filepath.Join(home, rest)
		}
		data, err := os.ReadFile(path)
		switch {
		case err == nil:
			var cred alpacaYAMLCredential
			require.NoError(t, yaml.Unmarshal(data, &cred), "parse %s", path)
			if cred.APIKey != "" && cred.SecretKey != "" {
				return cred.APIKey, cred.SecretKey, true
			}
		case !os.IsNotExist(err):
			t.Fatalf("read %s: %v", path, err)
		}
	}
	if alpacaEQ09KeyID != "" && alpacaEQ09SecretKey != "" {
		return alpacaEQ09KeyID, alpacaEQ09SecretKey, true
	}
	return "", "", false
}

// alpacaEQ09AccountID is Trader's own, fixed identity for the one
// Alpaca paper account this test addresses (alpaca.AccountConfig's own
// doc comment: this names, but does not create, the account the
// credential pair already authenticates as). Fixed rather than
// generated fresh per run, so repeated manual runs address the same
// Trader-side identity and any position left over from an interrupted
// prior run (see flattenExistingSPYPosition) is visible and
// recoverable across runs, not silently orphaned under a new identity
// every time.
var alpacaEQ09AccountID = id.MustParseAccountID("acc_01M1WJBKGPYKFCX4ATF8QQD0TZ")

// maxSmokeQuantity bounds this test's real position size (issue #302's
// own "deliberately small paper order" instruction) — enforced through
// a real risk.Rule, not merely a small requested size the test happens
// to ask for.
var maxSmokeQuantity = num.MustParseQuantity("2")

func TestSmokeSPYPaperRoundTrip(t *testing.T) {
	keyID, secretKey, ok := loadAlpacaEQ09Credentials(t)
	if !ok {
		t.Skipf("no Alpaca paper credentials configured: neither %s nor alpacaEQ09KeyID/SecretKey supplied a complete pair; point one at a real Alpaca paper credential pair to run this test", alpacaEQ09CredentialFile)
	}
	ctx := context.Background()
	c := clock.Real{}
	ids := id.NewGenerator(c, id.Random{})

	// --- Step 1: resolve SPY as a canonical equity instrument ---
	//
	// One Listing, registered once, is used for both the market-data
	// read (step 2) and the broker leg (steps 3-6) — the same
	// instrument identity throughout, not two separately built
	// Listings that happen to look alike. Provider "alpaca" matches
	// alpaca.Broker.Name() exactly, which account.Snapshot's own
	// provider/broker case-insensitive-match requirement (ADR-019)
	// requires.
	resolver := instrument.NewMemoryResolver()
	spyID, err := mdsvc.RegisterETFInstrument(resolver, mdsvc.EquityRegistration{
		Provider: "alpaca",
		Exchange: "ARCA",
		Ticker:   "SPY",
		Currency: num.MustParseCurrency("USD"),
	})
	require.NoError(t, err)
	spyListing, err := resolver.ResolveSymbol("alpaca", "", "SPY")
	require.NoError(t, err)

	credential := alpaca.StaticCredential{KeyID: keyID, SecretKey: secretKey}

	// --- Step: is the market open right now? ---
	//
	// Answered directly against USEquityCalendar, independent of
	// whatever market data step 2 happens to have on hand (historical
	// data is available regardless of the current session state) — see
	// the package/file doc comments for why this test skips rather
	// than attempting a live order outside regular hours.
	//
	// A bare USEquityCalendarParams{} has no holiday data at all (its
	// own doc comment: "no built-in holiday data"), which would
	// misreport StatusOpen on a real NYSE holiday that falls on a
	// weekday — exactly the queued-order/hung-test failure mode this
	// gate exists to prevent (PR #314 review). StandardUSEquityHolidays
	// supplies the real, documented holiday/half-day set for the
	// current and next calendar year, so a run spanning a year
	// boundary is still covered.
	now := c.Now()
	cal := marketdata.NewUSEquityCalendar(marketdata.StandardUSEquityHolidays(now.Year(), now.Year()+1))
	if cal.Status(now) != marketdata.StatusOpen {
		t.Skip("NYSE regular session is not currently open; this smoke test only runs live during real market hours")
	}

	// --- Step 2: canonical equity market data through marketdata.Manager ---
	mgr, err := marketdata.New(marketdata.Config{
		Clock:            c,
		StoreRoot:        t.TempDir(),
		RawRoot:          t.TempDir(),
		Resolver:         resolver,
		ProviderName:     "alpaca",
		Calendar:         cal,
		AlpacaCredential: credential,
		AlpacaBaseURL:    "https://data.alpaca.markets",
	})
	require.NoError(t, err)
	mdService, err := mdsvc.New(mgr, nil)
	require.NoError(t, err)

	dataRange, err := marketdata.NewTimeRange(now.AddDate(0, 0, -10), now)
	require.NoError(t, err)
	datasetReq := mdsvc.DatasetRequest{Instrument: spyID, Interval: marketdata.D1, Range: dataRange}

	_, err = mdService.Update(ctx, mdsvc.UpdateRequest{DatasetRequest: datasetReq})
	require.NoError(t, err, "a decoding error here means the Alpaca Market Data provider's assumed wire shape needs correcting (see marketdata/internal/provider/alpaca/wireshape.go)")

	barsResp, err := mdService.Bars(ctx, mdsvc.BarsRequest{DatasetRequest: datasetReq})
	require.NoError(t, err)
	require.NotEmpty(t, barsResp.Bars, "expected at least one real SPY trading day in a 10-day recent window")
	for _, bar := range barsResp.Bars {
		assert.True(t, bar.Time.Equal(bar.Time.Truncate(24*time.Hour)), "D1 bar time must align to a midnight-UTC boundary, got %s", bar.Time)
		assert.False(t, bar.Open.IsZero() && bar.Close.IsZero(), "expected non-zero OHLC for %s", bar.Time)
	}
	t.Logf("fetched %d real SPY daily bars through marketdata.Manager", len(barsResp.Bars))

	// --- Compose the real broker and the normal execution/risk/pipeline path ---
	client, err := alpaca.NewClient(alpaca.ClientConfig{
		BaseURL:    alpaca.DefaultPaperBaseURL, // never a live/production base URL — see doc comment
		Credential: credential,
	})
	require.NoError(t, err)
	require.Equal(t, "https://paper-api.alpaca.markets", alpaca.DefaultPaperBaseURL, "sanity-pin: this constant must never silently change to a live endpoint")

	deps := alpaca.Deps{Clock: c, IDs: ids, Resolver: resolver}
	b, err := alpaca.NewBroker("alpaca", client, deps, alpaca.AccountConfig{AccountID: alpacaEQ09AccountID})
	require.NoError(t, err)
	defer func() { _ = b.Close() }()

	planner, err := execpkg.NewPlanner(execpkg.Deps{Clock: c, IDs: ids})
	require.NoError(t, err)

	// A real, configured M4 risk policy (not a synthetic always-allow
	// test double) enforcing this test's own "deliberately small"
	// requirement structurally, not merely by request size.
	maxQty, err := risk.NewMaxPositionQuantityRule(maxSmokeQuantity)
	require.NoError(t, err)
	engine, err := risk.NewEngine(maxQty)
	require.NoError(t, err)

	p, err := pipeline.NewPipeline(pipeline.Deps{
		Sizer:   risk.NewFixedFractionSizer(),
		Planner: planner,
		Engine:  engine,
		Broker:  b,
		IDs:     ids,
	})
	require.NoError(t, err)
	svc, err := svcexecution.New(b, p, nil)
	require.NoError(t, err)

	acc, err := b.OpenAccount(ctx, alpacaEQ09AccountID)
	require.NoError(t, err)

	// --- Clean slate: flatten any SPY position left over from an
	// interrupted prior manual run, so this run starts from a known
	// state and its own final flat-position assertion is meaningful. ---
	flattenSPYIfOpen(t, ctx, svc, acc, spyID, spyListing)

	reader, err := acc.Events(ctx, "")
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()

	buildIntent := func(kind order.IntentKind, side order.Side) order.Intent {
		intentID, err := id.GenerateIntentID(ids)
		require.NoError(t, err)
		eventID, err := id.GenerateEventID(ids)
		require.NoError(t, err)
		corrID, err := id.GenerateCorrelationID(ids)
		require.NoError(t, err)
		in, err := order.NewIntent(order.Intent{
			IntentID:   intentID,
			Kind:       kind,
			Instrument: spyID,
			Side:       side,
			Metadata:   id.Metadata{EventID: eventID, CorrelationID: corrID},
		})
		require.NoError(t, err)
		return in
	}

	// --- Steps 3-4: the normal decision/order pipeline, submitting a
	// deliberately small real paper Enter order ---
	//
	// RiskFraction is computed from the account's own real current
	// equity, not a fixed guess: risk.FixedFractionSizer sizes
	// Quantity = (Equity * RiskFraction) / AdverseDistance (times the
	// listing's multiplier, 1 for equities), so a fixed RiskFraction
	// combined with a real, unknown paper-account balance can size far
	// larger than intended — confirmed live: 0.001 against Alpaca's
	// default ~$100k paper balance and a $1.00 AdverseDistance sized
	// ~100 shares, which risk.MaxPositionQuantityRule correctly
	// rejected as exceeding maxSmokeQuantity (the system worked; the
	// test's own fixed parameters didn't account for the account's
	// real size). Deriving RiskFraction from Equity directly targets
	// targetShares regardless of the account's actual balance, with
	// maxSmokeQuantity remaining a real, independent backstop rather
	// than the primary sizing mechanism.
	preSnap, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	adverse := num.MustParsePrice("10.00") // ~1.3% of a ~$770 SPY share — a realistic short-term stop distance, not an arbitrary $1
	// targetShares matches maxSmokeQuantity itself, not 1: sizing rounds
	// the raw (equity*riskFraction)/adverseDistance quotient DOWN to the
	// nearest whole share, and a raw quotient computed to target exactly
	// 1 share can land fractionally under 1 (real account equity is
	// never a round number, e.g. $99,999.14) and round all the way down
	// to zero — confirmed live. Targeting the cap itself leaves margin
	// against that floor-to-zero edge case while risk.MaxPositionQuantityRule
	// still independently caps the result at maxSmokeQuantity regardless.
	targetShares := maxSmokeQuantity
	targetRiskMoney, err := adverse.MulQuantity(targetShares, preSnap.Currency())
	require.NoError(t, err)
	riskFraction, err := targetRiskMoney.Div(preSnap.Equity())
	require.NoError(t, err)
	t.Logf("account equity %s; targeting %s share(s) via risk fraction %s", preSnap.Equity(), targetShares, riskFraction)

	enterResp, err := svc.Submit(ctx, svcexecution.SubmitRequest{
		AccountID:       alpacaEQ09AccountID,
		Intent:          buildIntent(order.IntentEnter, order.Buy),
		Listing:         spyListing,
		RiskFraction:    riskFraction,
		AdverseDistance: &adverse,
	})
	if err != nil {
		require.ErrorIs(t, err, svcexecution.ErrRejected, "unexpected non-rejection error submitting the enter intent: %v", err)
	}
	require.True(t, enterResp.Decision.Allowed, "risk declined the enter intent: %+v", enterResp.Decision.Violations)
	require.NotNil(t, enterResp.Order.AcceptedQuantity)
	assert.True(t, enterResp.Order.AcceptedQuantity.Cmp(num.MustParseQuantity("0")) > 0, "expected a positive quantity")
	divisible, err := enterResp.Order.AcceptedQuantity.DivisibleBy(spyListing.Spec().QuantityIncrement())
	require.NoError(t, err)
	assert.True(t, divisible, "expected a whole-share quantity (a multiple of the listing's own quantity increment), got %s", enterResp.Order.AcceptedQuantity)
	assert.True(t, enterResp.Order.AcceptedQuantity.Cmp(maxSmokeQuantity) <= 0, "risk.MaxPositionQuantityRule should have capped the sized quantity")
	enterOrderID := enterResp.Order.Request.OrderID
	t.Logf("submitted enter order %s for %s shares of SPY, status %s", enterResp.Order.BrokerOrderID, enterResp.Order.AcceptedQuantity, enterResp.Order.Status)

	// --- Step 5: observe canonical order/fill state via Account.Events,
	// with a cancel-and-reconcile safety net mirroring
	// adapters/broker/alpaca/smoke_test.go's own pattern (cancel), but
	// never trusting the synchronous CancelResult alone (PR #314
	// review): Alpaca cancels asynchronously, so the order can still
	// race to a fill after cancellation is requested. ---
	if !waitForFill(t, ctx, reader, enterOrderID, 30*time.Second) {
		finalStatus := cancelAndAwaitTerminal(t, ctx, acc, reader, ids, enterOrderID, 30*time.Second)
		if finalStatus == order.StatusFilled {
			// The cancel raced with a real fill: a position now exists.
			// Flatten it through the same normal pipeline before this
			// run ends, so the paper account is never left with
			// unexpected exposure merely because the enter happened to
			// be slow.
			t.Log("enter order filled despite the cancel race; flattening the resulting position before skipping")
			flattenSPYIfOpen(t, ctx, svc, acc, spyID, spyListing)
			t.Skip("enter order was slow to fill and raced with cancellation; the resulting position was reconciled and flattened. This is an environment/timing skip, not an architecture failure.")
		}
		t.Skipf("enter order did not fill within the smoke test's timeout (final status %s after cancel); this is an environment/timing skip, not an architecture failure", finalStatus)
	}

	midSnap, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	assert.Equal(t, "USD", midSnap.Currency().String())
	t.Logf("account snapshot after fill: equity=%s cash=%s", midSnap.Equity(), midSnap.CashBalances())

	// --- Step 6: flatten the test position through the same normal
	// pipeline (order.IntentExit derives side/quantity from the current
	// position automatically) ---
	exitResp, err := svc.Submit(ctx, svcexecution.SubmitRequest{
		AccountID: alpacaEQ09AccountID,
		Intent:    buildIntent(order.IntentExit, 0),
		Listing:   spyListing,
	})
	require.NoError(t, err)
	require.True(t, exitResp.Decision.Allowed, "risk declined the exit intent: %+v", exitResp.Decision.Violations)
	exitOrderID := exitResp.Order.Request.OrderID
	t.Logf("submitted exit order %s, status %s", exitResp.Order.BrokerOrderID, exitResp.Order.Status)

	if !waitForFill(t, ctx, reader, exitOrderID, 30*time.Second) {
		finalStatus := cancelAndAwaitTerminal(t, ctx, acc, reader, ids, exitOrderID, 30*time.Second)
		if finalStatus != order.StatusFilled {
			t.Fatalf("exit order did not fill within the smoke test's timeout (final status %s after cancel); a real SPY position may remain open on the paper account and needs manual review", finalStatus)
		}
		// The cancel raced with a real fill: the flatten still
		// succeeded despite the timeout, so the run can proceed to its
		// own final flat-position assertion normally.
		t.Log("exit order filled despite the cancel race; the position was still successfully flattened")
	}

	finalSnap, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	assertFlatSPYPosition(t, finalSnap, spyListing)
	t.Logf("final account snapshot: equity=%s, SPY position flat", finalSnap.Equity())
}

// flattenSPYIfOpen submits an IntentExit through the normal pipeline
// if acc currently holds any SPY position, and waits for it to fill —
// step 6's own "close/cancel/flatten the test state as appropriate,"
// applied defensively at the start of a run too, so a prior
// interrupted manual run never silently accumulates real paper
// exposure across repeated invocations of this test.
func flattenSPYIfOpen(t *testing.T, ctx context.Context, svc *svcexecution.Service, acc brokerpkg.Account, spyID instrument.ID, spyListing instrument.Listing) {
	t.Helper()
	snap, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	if isFlatSPY(snap, spyListing) {
		return
	}
	t.Log("found an existing SPY position from a prior run; flattening before starting this run's own scenario")

	ids := id.NewGenerator(clock.Real{}, id.Random{})
	intentID, err := id.GenerateIntentID(ids)
	require.NoError(t, err)
	eventID, err := id.GenerateEventID(ids)
	require.NoError(t, err)
	corrID, err := id.GenerateCorrelationID(ids)
	require.NoError(t, err)
	exitIntent, err := order.NewIntent(order.Intent{
		IntentID: intentID, Kind: order.IntentExit, Instrument: spyID,
		Metadata: id.Metadata{EventID: eventID, CorrelationID: corrID},
	})
	require.NoError(t, err)

	resp, err := svc.Submit(ctx, svcexecution.SubmitRequest{AccountID: snap.AccountID(), Intent: exitIntent, Listing: spyListing})
	require.NoError(t, err)
	require.True(t, resp.Decision.Allowed)

	reader, err := acc.Events(ctx, "")
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()
	if waitForFill(t, ctx, reader, resp.Order.Request.OrderID, 30*time.Second) {
		return
	}

	// Mirrors the main scenario's own cancel-and-reconcile safety net
	// (PR #314 review): never trust the synchronous CancelResult alone
	// — Alpaca cancels asynchronously, so the order can still race to a
	// fill after cancellation is requested, and failing this test
	// without checking that first would misreport a successful flatten
	// as a failure.
	finalStatus := cancelAndAwaitTerminal(t, ctx, acc, reader, ids, resp.Order.Request.OrderID, 30*time.Second)
	if finalStatus != order.StatusFilled {
		t.Fatalf("failed to flatten a pre-existing SPY position before starting the smoke test (final status %s after cancel); the paper account needs manual review", finalStatus)
	}
	t.Log("flatten order filled despite the cancel race; the pre-existing position was still successfully closed")
}
