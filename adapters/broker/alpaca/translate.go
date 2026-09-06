package alpaca

import (
	"fmt"
	"time"

	"github.com/rustyeddy/trader/account"
	brokerpkg "github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

// This file is the single seam translating between Alpaca's wire
// vocabulary and Trader's canonical order/account types. Every other
// file in this package either produces wire values (client.go) or
// consumes canonical ones (broker.go, account.go, event_reader.go); no
// other file interprets a wire string or builds a canonical value
// directly.

// sideToWire/sideFromWire translate order.Side.
func sideToWire(s order.Side) (string, error) {
	switch s {
	case order.Buy:
		return "buy", nil
	case order.Sell:
		return "sell", nil
	default:
		return "", fmt.Errorf("%w: side %v", brokerpkg.ErrUnsupported, s)
	}
}

func sideFromWire(s string) order.Side {
	switch s {
	case "buy":
		return order.Buy
	case "sell":
		return order.Sell
	default:
		return 0
	}
}

// typeToWire/typeFromWire translate order.Type. Only Market is
// supported for submission in Phase 1 (see the package doc comment);
// typeFromWire recognizes every value Alpaca may report on an existing
// order regardless, so an order this adapter did not itself submit
// (for example, one placed outside Trader) can still be translated for
// Snapshot/Events reporting.
func typeToWire(t order.Type) (string, error) {
	if t != order.Market {
		return "", fmt.Errorf("%w: order type %v is not supported by this adapter", brokerpkg.ErrUnsupported, t)
	}
	return "market", nil
}

func typeFromWire(s string) order.Type {
	switch s {
	case "market":
		return order.Market
	case "limit":
		return order.Limit
	case "stop":
		return order.Stop
	case "stop_limit":
		return order.StopLimit
	default:
		return 0
	}
}

// tifToWire/tifFromWire translate order.TimeInForce.
func tifToWire(f order.TimeInForce) (string, error) {
	switch f {
	case order.GTC:
		return "gtc", nil
	case order.DAY:
		return "day", nil
	case order.IOC:
		return "ioc", nil
	case order.FOK:
		return "fok", nil
	default:
		return "", fmt.Errorf("%w: time in force %v", brokerpkg.ErrUnsupported, f)
	}
}

func tifFromWire(s string) order.TimeInForce {
	switch s {
	case "gtc":
		return order.GTC
	case "day":
		return order.DAY
	case "ioc":
		return order.IOC
	case "fok":
		return order.FOK
	default:
		return 0
	}
}

// statusFromWire translates Alpaca's real, documented order status
// enum to order.Status. This mapping is the one part of this file
// genuinely unverified against a live account (see wireshape.go);
// EQ-09's end-to-end smoke test is where it gets its first real-world
// check. Any status not listed here — including one Alpaca adds in the
// future — reports order.StatusUnknown, which is a legal, storable
// value (order.Status's own zero-value convention) rather than an
// error, so an unrecognized status is reported, not dropped.
//
// Alpaca's own "pending_replace" and terminal "replaced" are two
// distinct statuses, and must not share one mapping (PR #313 review):
// "pending_replace" means a replace request is outstanding and
// genuinely belongs to order.StatusPendingReplace, while "replaced"
// means this specific order is done — permanently superseded by
// whatever new order Alpaca's replace created (see Replace's own doc
// comment in cancel_replace.go on that new order possibly carrying a
// different id). order.Status has no dedicated terminal "replaced"
// value of its own; introducing one is a cross-cutting ADR-018 change
// (order.Status's valid()/requiresAcceptance/precludesAcceptance and
// every existing consumer) out of this adapter's own scope. Mapping
// "replaced" back to StatusPendingReplace was the actual bug this
// comment replaces: it made a finished lifecycle transition look
// perpetually pending, and separately caused wireOrderToOrder to
// synthesize a spurious PendingCommandID for it. StatusCanceled is the
// closest existing terminal meaning — this specific order's own life
// ended, intentionally, without filling — and is used here instead,
// documented explicitly as an approximation pending a possible future
// order.StatusReplaced addition.
func statusFromWire(s string) order.Status {
	switch s {
	case "new", "accepted", "pending_new", "accepted_for_bidding":
		return order.StatusWorking
	case "partially_filled":
		return order.StatusPartiallyFilled
	case "filled":
		return order.StatusFilled
	case "pending_cancel":
		return order.StatusPendingCancel
	case "canceled", "replaced":
		return order.StatusCanceled
	case "pending_replace":
		return order.StatusPendingReplace
	case "rejected":
		return order.StatusRejected
	case "expired", "done_for_day":
		return order.StatusExpired
	default:
		return order.StatusUnknown
	}
}

// requestToWireSubmit translates a validated order.Request into
// SubmitOrder's request body. req.OrderID becomes client_order_id,
// which is also this adapter's sole idempotency mechanism for
// submission (see the package doc comment) — Alpaca resubmitting the
// same client_order_id is expected to return the existing order rather
// than create a duplicate.
func requestToWireSubmit(req order.Request) (wireOrderSubmit, error) {
	side, err := sideToWire(req.Side)
	if err != nil {
		return wireOrderSubmit{}, err
	}
	typ, err := typeToWire(req.Type)
	if err != nil {
		return wireOrderSubmit{}, err
	}
	tif, err := tifToWire(req.TimeInForce)
	if err != nil {
		return wireOrderSubmit{}, err
	}
	return wireOrderSubmit{
		Symbol:        req.Listing.Symbol(),
		Qty:           req.Quantity.String(),
		Side:          side,
		Type:          typ,
		TimeInForce:   tif,
		ClientOrderID: req.OrderID.String(),
	}, nil
}

// resolveListing resolves symbol to a Listing via resolver, using
// providerName as the resolution provider — see accountHandle's own doc
// comment for why this is the same "alpaca" provider tag the equity
// Market Data provider (issue #297) already registers Listings under.
func resolveListing(resolver instrument.Resolver, providerName, symbol string) (instrument.Listing, error) {
	listing, err := resolver.ResolveSymbol(providerName, "", symbol)
	if err != nil {
		return instrument.Listing{}, fmt.Errorf("alpaca: resolve listing for %s: %w", symbol, err)
	}
	return listing, nil
}

// wireOrderToOrder translates one wireOrder into a canonical order.Order,
// reconstructing its originating Request from the wire fields (Trader's
// OrderID from client_order_id, Listing from resolver, Side/Type/
// TimeInForce/prices from their wire counterparts). accountID is the
// Trader account this order belongs to — Alpaca's own order response
// carries no Trader account identity, since one Client/Credential pair
// authenticates as exactly one Alpaca account (see config.go).
//
// ids is consulted only when w.Status translates to StatusPendingCancel
// or StatusPendingReplace: order.NewOrder requires a non-zero
// PendingCommandID in that state (ADR-018), correlating to the
// originating cancel/replace command's own EventID. Alpaca's wire order
// carries no such identity at all — a pending cancel/replace observed
// through Snapshot or the polling EventReader may not even be one this
// process itself issued (an operator could cancel through Alpaca's own
// UI). This adapter therefore synthesizes a fresh, adapter-local
// EventID purely to satisfy order.Order's structural invariant; it does
// not correlate to any real command and must not be treated as one — a
// known, documented Phase 1 limitation (see the package doc comment).
func wireOrderToOrder(w wireOrder, resolver instrument.Resolver, providerName string, accountID id.AccountID, ids *id.Generator) (order.Order, error) {
	orderID, err := id.ParseOrderID(w.ClientOrderID)
	if err != nil {
		return order.Order{}, fmt.Errorf("alpaca: parse client_order_id %q: %w", w.ClientOrderID, err)
	}
	listing, err := resolveListing(resolver, providerName, w.Symbol)
	if err != nil {
		return order.Order{}, err
	}
	side := sideFromWire(w.Side)
	typ := typeFromWire(w.Type)
	tif := tifFromWire(w.TimeInForce)

	limitPrice, err := optionalPrice(w.LimitPrice)
	if err != nil {
		return order.Order{}, fmt.Errorf("alpaca: parse limit_price: %w", err)
	}
	stopPrice, err := optionalPrice(w.StopPrice)
	if err != nil {
		return order.Order{}, fmt.Errorf("alpaca: parse stop_price: %w", err)
	}

	qty, err := num.ParseQuantity(w.Qty)
	if err != nil {
		return order.Order{}, fmt.Errorf("alpaca: parse qty %q: %w", w.Qty, err)
	}
	proposal := order.Proposal{
		Listing:     listing,
		AccountID:   accountID,
		Side:        side,
		Type:        typ,
		TimeInForce: tif,
		Quantity:    qty,
		LimitPrice:  limitPrice,
		StopPrice:   stopPrice,
	}
	req, err := order.NewRequest(proposal, orderID)
	if err != nil {
		return order.Order{}, fmt.Errorf("alpaca: reconstruct request: %w", err)
	}

	status := statusFromWire(w.Status)

	o := order.Order{
		Request:       req,
		BrokerOrderID: w.ID,
		Status:        status,
	}
	if status == order.StatusPendingCancel || status == order.StatusPendingReplace {
		pendingID, err := id.GenerateEventID(ids)
		if err != nil {
			return order.Order{}, fmt.Errorf("alpaca: synthesize pending command id: %w", err)
		}
		o.PendingCommandID = pendingID
	}
	if requiresAcceptedQuantity(status) {
		acceptedQty := qty
		o.AcceptedQuantity = &acceptedQty
		o.AcceptedLimitPrice = limitPrice
		o.AcceptedStopPrice = stopPrice

		filledQty, err := num.ParseQuantity(orDefault(w.FilledQty, "0"))
		if err != nil {
			return order.Order{}, fmt.Errorf("alpaca: parse filled_qty %q: %w", w.FilledQty, err)
		}
		o.FilledQuantity = filledQty

		// AvgFillPrice is reserved by order.Order for a quantity-weighted
		// average fill price; order.NewOrder places no constraint on it
		// (unlike ApplyFill, which deliberately clears it — see its own
		// doc comment). Alpaca reports exactly this figure directly
		// (filled_avg_price), so this is a genuine, direct translation,
		// not a computed approximation — and it is what
		// emitFillIfIncreased (event_reader.go) uses as a synthesized
		// fill's price.
		avgFillPrice, err := optionalPrice(w.FilledAvgPrice)
		if err != nil {
			return order.Order{}, fmt.Errorf("alpaca: parse filled_avg_price: %w", err)
		}
		o.AvgFillPrice = avgFillPrice
	}
	if status == order.StatusRejected {
		o.Rejection = &order.Rejection{Reason: order.ReasonUnknown, Detail: "alpaca reported status \"" + w.Status + "\""}
	}

	built, err := order.NewOrder(o)
	if err != nil {
		return order.Order{}, fmt.Errorf("alpaca: build order: %w", err)
	}
	return built, nil
}

// requiresAcceptedQuantity reports whether an order.Order translated
// from a wire order should carry a non-nil AcceptedQuantity. Every
// order this package ever translates already exists on Alpaca's side
// (it was submitted, fetched, or listed) — Alpaca never reports an
// order before accepting it, even "new" already means accepted-and-
// working — so the only status that precludes acceptance is an outright
// rejection. Mirrors order.Status's own unexported precludesAcceptance
// classification (StatusPendingSubmit, StatusRejected), reimplemented
// here since that method is unexported to the order package.
func requiresAcceptedQuantity(s order.Status) bool {
	switch s {
	case order.StatusPendingSubmit, order.StatusRejected:
		return false
	default:
		return true
	}
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func optionalPrice(s *string) (*num.Price, error) {
	if s == nil || *s == "" {
		return nil, nil
	}
	p, err := num.ParsePrice(*s)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// wirePositionToPosition translates one wirePosition into a canonical
// order.Position.
func wirePositionToPosition(w wirePosition, resolver instrument.Resolver, providerName string, accountID id.AccountID) (order.Position, error) {
	listing, err := resolveListing(resolver, providerName, w.Symbol)
	if err != nil {
		return order.Position{}, err
	}
	var side order.PositionSide
	switch w.Side {
	case "long":
		side = order.Long
	case "short":
		side = order.Short
	default:
		return order.Position{}, fmt.Errorf("alpaca: unrecognized position side %q for %s", w.Side, w.Symbol)
	}
	qty, err := num.ParseQuantity(w.Qty)
	if err != nil {
		return order.Position{}, fmt.Errorf("alpaca: parse position qty %q: %w", w.Qty, err)
	}
	avgPrice, err := num.ParsePrice(w.AvgEntryPrice)
	if err != nil {
		return order.Position{}, fmt.Errorf("alpaca: parse avg_entry_price %q: %w", w.AvgEntryPrice, err)
	}
	pos, err := order.NewPosition(order.Position{
		AccountID: accountID,
		Listing:   listing,
		Side:      side,
		Quantity:  qty,
		AvgPrice:  &avgPrice,
	})
	if err != nil {
		return order.Position{}, fmt.Errorf("alpaca: build position for %s: %w", w.Symbol, err)
	}
	return pos, nil
}

// wireAccountToSnapshotParams translates wireAccount plus already-
// translated positions/open orders into account.SnapshotParams.
//
// RealizedPnL is always zero: Alpaca's account/positions endpoints
// report no lifetime realized PnL figure, and computing one would
// require the portfolio-history/activities endpoints this package's
// Phase 1 scope deliberately excludes (see the package doc comment).
// UnrealizedPnL is supplied by the caller as unrealizedPnL — the sum of
// every position's reported unrealized_pl (a field Alpaca reports
// directly on wirePosition, not something order.Position itself carries,
// so the caller sums it from the raw wire positions before this
// function ever sees the translated order.Position values).
// MarginUsed/MarginAvailable are approximated from Alpaca's
// initial_margin/buying_power fields — Alpaca's real margin model does
// not map one-to-one onto Trader's MarginUsed/MarginAvailable
// vocabulary, the same "M3 placeholder, not a claim of real
// margin/leverage semantics" caveat adapters/broker/sim's own Snapshot
// construction already documents.
func wireAccountToSnapshotParams(w wireAccount, positions []order.Position, openOrders []order.Order, unrealizedPnL num.Money, accountID id.AccountID, broker string, asOf time.Time) (account.SnapshotParams, error) {
	currency, err := num.ParseCurrency(w.Currency)
	if err != nil {
		return account.SnapshotParams{}, fmt.Errorf("alpaca: parse account currency %q: %w", w.Currency, err)
	}
	cash, err := num.ParseMoney(w.Cash, currency)
	if err != nil {
		return account.SnapshotParams{}, fmt.Errorf("alpaca: parse cash %q: %w", w.Cash, err)
	}
	equity, err := num.ParseMoney(w.Equity, currency)
	if err != nil {
		return account.SnapshotParams{}, fmt.Errorf("alpaca: parse equity %q: %w", w.Equity, err)
	}
	buyingPower, err := num.ParseMoney(w.BuyingPower, currency)
	if err != nil {
		return account.SnapshotParams{}, fmt.Errorf("alpaca: parse buying_power %q: %w", w.BuyingPower, err)
	}
	marginUsed, err := num.ParseMoney(orDefault(w.InitialMargin, "0"), currency)
	if err != nil {
		return account.SnapshotParams{}, fmt.Errorf("alpaca: parse initial_margin %q: %w", w.InitialMargin, err)
	}
	zero, err := num.ParseMoney("0", currency)
	if err != nil {
		return account.SnapshotParams{}, fmt.Errorf("alpaca: zero money: %w", err)
	}

	return account.SnapshotParams{
		AccountID:       accountID,
		Broker:          broker,
		Currency:        currency,
		AsOf:            asOf,
		CashBalances:    []num.Money{cash},
		Equity:          equity,
		BuyingPower:     buyingPower,
		MarginUsed:      marginUsed,
		MarginAvailable: buyingPower,
		RealizedPnL:     zero,
		UnrealizedPnL:   unrealizedPnL,
		Fees:            zero,
		Financing:       zero,
		Positions:       positions,
		OpenOrders:      openOrders,
	}, nil
}
