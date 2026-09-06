//go:build alpacasmoke

package alpaca

// This file is EQ-08's opt-in, real-credential integration/smoke path,
// exercising this adapter against a real Alpaca paper account —
// distinct from marketdata/internal/provider/alpaca/smoke_test.go's
// own "alpacasmoke"-tagged test, which exercises the unrelated Market
// Data API. Both share the one "alpacasmoke" build tag deliberately
// (see that file's own doc comment for why a second tag was not
// introduced): an operator enables real Alpaca network access once,
// and gets both smoke paths.
//
// This package is not under marketdata/internal/, but the same
// config/arch_test.go "no os.Getenv outside config/cmd/test" rule this
// repository mechanically enforces still applies here (that check is
// package-boundary-based, not internal/-scoped), so this file follows
// the identical local-uncommitted-constant pattern, never environment
// variables.
//
// To run this test against a real Alpaca paper account, edit the
// constants below locally (never commit the edit) and run:
//
//	go test -tags alpacasmoke ./adapters/broker/alpaca/... \
//	    -run TestSmokeBrokerRoundTrip -v
//
// A failure decoding Alpaca's response is the expected signal that
// wireshape.go's assumed field names need correcting against the real
// API (see doc.go); it is not evidence that client.go's request/retry
// logic, or the translate.go mapping tables, are wrong.

import (
	"context"
	"testing"
	"time"

	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// alpacaBrokerSmokeKeyID/SecretKey name the real Alpaca paper-account
// credential pair TestSmokeBrokerRoundTrip uses. They ship empty for
// the same reason marketdata's own alpacaSmokeKeyID does: an operator
// wanting to run this test edits these constants locally. The test
// skips whenever either is empty, which is always true for a fresh
// checkout.
const alpacaBrokerSmokeKeyID = ""
const alpacaBrokerSmokeSecretKey = ""

// TestSmokeBrokerRoundTrip connects to a real Alpaca paper account,
// fetches its account snapshot, submits a one-share AAPL market buy,
// observes its resulting order-accepted event, and cancels it
// immediately (accepting whatever outcome Alpaca reports — the order
// may already have filled before the cancel reaches it, which is
// itself a legitimate, informative smoke result, not a test failure).
// This deliberately does not attempt to prove a completed round-trip
// fill/flatten cycle; EQ-09 (#302) owns the full end-to-end strategy
// smoke test built on top of this adapter.
func TestSmokeBrokerRoundTrip(t *testing.T) {
	if alpacaBrokerSmokeKeyID == "" || alpacaBrokerSmokeSecretKey == "" {
		t.Skip("alpacaBrokerSmokeKeyID/alpacaBrokerSmokeSecretKey are empty; edit the constants in this file to point at a real Alpaca paper credential pair to run this test")
	}

	client, err := NewClient(ClientConfig{
		BaseURL:    DefaultPaperBaseURL,
		Credential: StaticCredential{KeyID: alpacaBrokerSmokeKeyID, SecretKey: alpacaBrokerSmokeSecretKey},
	})
	require.NoError(t, err)

	inst, err := instrument.NewEquity("NASDAQ", "AAPL")
	require.NoError(t, err)
	spec, err := instrument.NewSpec(
		num.MustParsePrice("0.01"), num.MustParseQuantity("1"), num.MustParseRate("1"), num.MustParseCurrency("USD"),
	)
	require.NoError(t, err)
	listing, err := instrument.NewListing(instrument.ListingParams{
		Instrument: inst, Provider: "alpaca", Symbol: "AAPL", Spec: spec, Tradable: true,
	})
	require.NoError(t, err)
	resolver := instrument.NewMemoryResolver()
	require.NoError(t, resolver.Register(listing))

	accountID := id.MustParseAccountID("acc_01ARZ3NDEKTSV4RRFFQ69G5FAV")
	deps := Deps{
		Clock:    clock.Real{},
		IDs:      id.NewGenerator(clock.Real{}, id.Random{}),
		Resolver: resolver,
	}
	broker, err := NewBroker("alpaca", client, deps, AccountConfig{AccountID: accountID})
	require.NoError(t, err)
	defer broker.Close()

	acc, err := broker.OpenAccount(context.Background(), accountID)
	require.NoError(t, err)

	snap, err := acc.Snapshot(context.Background())
	require.NoError(t, err, "a decoding error here means wireshape.go's assumed account/position shape needs correcting against the real API")
	t.Logf("real paper account equity: %s", snap.Equity())

	orderID, err := id.GenerateOrderID(deps.IDs)
	require.NoError(t, err)
	req, err := order.NewRequest(order.Proposal{
		Listing:     listing,
		AccountID:   accountID,
		Side:        order.Buy,
		Type:        order.Market,
		TimeInForce: order.DAY,
		Quantity:    num.MustParseQuantity("1"),
	}, orderID)
	require.NoError(t, err)

	o, err := acc.Submit(context.Background(), req)
	require.NoError(t, err, "a decoding error here means wireshape.go's assumed order shape needs correcting against the real API")
	t.Logf("submitted order %s, status %s", o.BrokerOrderID, o.Status)
	assert.NotEmpty(t, o.BrokerOrderID)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := acc.Cancel(ctx, order.CancelRequest{
		OrderID:  orderID,
		Metadata: id.Metadata{EventID: id.MustParseEventID("evt_01ARZ3NDEKTSV4RRFFQ69G5FAV")},
	})
	require.NoError(t, err)
	t.Logf("cancel result status: %s", result.Status)
}
