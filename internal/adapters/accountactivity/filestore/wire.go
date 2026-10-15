package filestore

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/internal/account"
	"github.com/rustyeddy/trader/internal/accountactivity"
	"github.com/rustyeddy/trader/internal/id"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

// Explicit wire structs, never reflection over the domain type: Value's
// fields are unexported, so a bare json.Marshal would silently drop the
// provenance this ledger exists to keep (the same discipline as the journal
// adapter's wire.go).

// schemaVersion 2 is the canonical v1 execution representation (#511):
// per-trade effects, fullVWAP and the quote snapshot. Schema 1 was the
// interim #510 shape (trade effects with no detail), never exposed by a
// shipped importer; a ledger written in it is rebuilt by re-importing, not
// migrated in place.
const schemaVersion = 2

// errOldSchema explains how to recover from a ledger in an older schema.
func schemaError(got int) error {
	if got < schemaVersion {
		return fmt.Errorf("schema %d is from an interim development format and is not read; delete the stream directory and re-import (want schema %d)", got, schemaVersion)
	}
	return fmt.Errorf("unsupported schema %d (want %d)", got, schemaVersion)
}

// valueWire's Value is a pointer so presence is explicit: a value object
// with no "value" (or a null one) is corruption, not a known zero, while an
// explicitly encoded zero is a pointer to zero and stays valid.
type valueWire[T any] struct {
	Value  *T     `json:"value"`
	Basis  string `json:"basis"`
	Source string `json:"source,omitempty"`
}

func toValueWire[T any](v accountactivity.Value[T]) *valueWire[T] {
	got, ok := v.Get()
	if !ok {
		return nil
	}
	return &valueWire[T]{Value: &got, Basis: v.Basis().String(), Source: v.Source()}
}

func fromValueWire[T any](w *valueWire[T]) (accountactivity.Value[T], error) {
	if w == nil {
		return accountactivity.Value[T]{}, nil
	}
	if w.Value == nil {
		return accountactivity.Value[T]{}, fmt.Errorf("value object has no value")
	}
	switch w.Basis {
	case "reported":
		if w.Source == "" {
			return accountactivity.Reported(*w.Value), nil
		}
		return accountactivity.ReportedFrom(*w.Value, w.Source)
	case "derived":
		return accountactivity.Derived(*w.Value, w.Source)
	default:
		return accountactivity.Value[T]{}, fmt.Errorf("value basis %q is not reported or derived", w.Basis)
	}
}

type tradeEffectWire struct {
	BrokerTradeID          string                   `json:"broker_trade_id"`
	Role                   string                   `json:"role"`
	Units                  *valueWire[num.Quantity] `json:"units,omitempty"`
	Price                  *valueWire[num.Price]    `json:"price,omitempty"`
	RealizedPL             *valueWire[num.Money]    `json:"realized_pl,omitempty"`
	Financing              *valueWire[num.Money]    `json:"financing,omitempty"`
	SpreadCost             *valueWire[num.Money]    `json:"spread_cost,omitempty"`
	GuaranteedExecutionFee *valueWire[num.Money]    `json:"guaranteed_execution_fee,omitempty"`
}

// priceLevelWire's fields are pointers so a missing price or liquidity is
// corruption, not a level of zero.
type priceLevelWire struct {
	Price     *num.Price    `json:"price"`
	Liquidity *num.Quantity `json:"liquidity"`
}

func toLevelWire(l accountactivity.PriceLevel) priceLevelWire {
	return priceLevelWire{Price: &l.Price, Liquidity: &l.Liquidity}
}

func fromLevelWire(side string, l priceLevelWire) (accountactivity.PriceLevel, error) {
	if l.Price == nil || l.Liquidity == nil {
		return accountactivity.PriceLevel{}, fmt.Errorf("%s level is missing its price or liquidity", side)
	}
	return accountactivity.PriceLevel{Price: *l.Price, Liquidity: *l.Liquidity}, nil
}

// quoteWire is the payload of an execution quote value; presence of the
// closeout prices is explicit like every other value.
type quoteWire struct {
	At          *time.Time            `json:"at,omitempty"`
	Bids        []priceLevelWire      `json:"bids,omitempty"`
	Asks        []priceLevelWire      `json:"asks,omitempty"`
	CloseoutBid *valueWire[num.Price] `json:"closeout_bid,omitempty"`
	CloseoutAsk *valueWire[num.Price] `json:"closeout_ask,omitempty"`
}

func toQuoteWire(q accountactivity.ExecutionQuote) quoteWire {
	w := quoteWire{CloseoutBid: toValueWire(q.CloseoutBid), CloseoutAsk: toValueWire(q.CloseoutAsk)}
	if !q.At.IsZero() {
		at := q.At.UTC()
		w.At = &at
	}
	for _, l := range q.Bids {
		w.Bids = append(w.Bids, toLevelWire(l))
	}
	for _, l := range q.Asks {
		w.Asks = append(w.Asks, toLevelWire(l))
	}
	return w
}

func fromQuoteWire(w quoteWire) (accountactivity.ExecutionQuote, error) {
	var q accountactivity.ExecutionQuote
	var err error
	if w.At != nil {
		q.At = w.At.UTC()
	}
	for _, l := range w.Bids {
		lv, err := fromLevelWire("bid", l)
		if err != nil {
			return q, err
		}
		q.Bids = append(q.Bids, lv)
	}
	for _, l := range w.Asks {
		lv, err := fromLevelWire("ask", l)
		if err != nil {
			return q, err
		}
		q.Asks = append(q.Asks, lv)
	}
	if q.CloseoutBid, err = fromValueWire(w.CloseoutBid); err != nil {
		return q, fmt.Errorf("closeout bid: %w", err)
	}
	if q.CloseoutAsk, err = fromValueWire(w.CloseoutAsk); err != nil {
		return q, fmt.Errorf("closeout ask: %w", err)
	}
	return q, nil
}

type accountRefWire struct {
	AccountID string `json:"account_id"`
	Broker    string `json:"broker"`
}

type activityWire struct {
	Schema             int    `json:"schema"`
	Provider           string `json:"provider"`
	Environment        string `json:"environment"`
	ProviderAccountID  string `json:"provider_account_id"`
	ProviderActivityID string `json:"provider_activity_id"`
	ProviderType       string `json:"provider_type"`
	Kind               string `json:"kind"`

	Account *accountRefWire `json:"account,omitempty"`

	OccurredAt time.Time `json:"occurred_at"`
	ImportedAt time.Time `json:"imported_at"`

	Instrument         string            `json:"instrument,omitempty"`
	Side               string            `json:"side,omitempty"`
	BrokerOrderID      string            `json:"broker_order_id,omitempty"`
	BrokerBatchID      string            `json:"broker_batch_id,omitempty"`
	ProviderInstrument string            `json:"provider_instrument,omitempty"`
	TradeEffects       []tradeEffectWire `json:"trade_effects,omitempty"`

	Quantity     *valueWire[num.Quantity] `json:"quantity,omitempty"`
	Price        *valueWire[num.Price]    `json:"price,omitempty"`
	FullVWAP     *valueWire[num.Price]    `json:"full_vwap,omitempty"`
	Quote        *valueWire[quoteWire]    `json:"execution_quote,omitempty"`
	RealizedPL   *valueWire[num.Money]    `json:"realized_pl,omitempty"`
	SpreadCost   *valueWire[num.Money]    `json:"spread_cost,omitempty"`
	Commission   *valueWire[num.Money]    `json:"commission,omitempty"`
	Financing    *valueWire[num.Money]    `json:"financing,omitempty"`
	OtherFees    *valueWire[num.Money]    `json:"other_fees,omitempty"`
	Cash         *valueWire[num.Money]    `json:"cash,omitempty"`
	Benchmark    *valueWire[num.Price]    `json:"benchmark,omitempty"`
	Slippage     *valueWire[num.Rate]     `json:"slippage,omitempty"`
	SlippageCost *valueWire[num.Money]    `json:"slippage_cost,omitempty"`
}

var kindByName = map[string]accountactivity.Kind{}

func init() {
	for k := accountactivity.KindFill; k.Valid(); k++ {
		kindByName[k.String()] = k
	}
}

func toWire(a accountactivity.Activity) activityWire {
	w := activityWire{
		Schema:             schemaVersion,
		Provider:           a.Provider,
		Environment:        a.Environment.String(),
		ProviderAccountID:  a.ProviderAccountID,
		ProviderActivityID: a.ProviderActivityID,
		ProviderType:       a.ProviderType,
		Kind:               a.Kind.String(),
		OccurredAt:         a.OccurredAt.UTC(),
		ImportedAt:         a.ImportedAt.UTC(),
		Instrument:         a.Instrument.String(),
		BrokerOrderID:      a.BrokerOrderID,
		BrokerBatchID:      a.BrokerBatchID,
		ProviderInstrument: a.ProviderInstrument,
		Quantity:           toValueWire(a.Quantity),
		Price:              toValueWire(a.Price),
		FullVWAP:           toValueWire(a.FullVWAP),
		RealizedPL:         toValueWire(a.RealizedPL),
		SpreadCost:         toValueWire(a.SpreadCost),
		Commission:         toValueWire(a.Commission),
		Financing:          toValueWire(a.Financing),
		OtherFees:          toValueWire(a.OtherFees),
		Cash:               toValueWire(a.Cash),
		Benchmark:          toValueWire(a.Benchmark),
		Slippage:           toValueWire(a.Slippage),
		SlippageCost:       toValueWire(a.SlippageCost),
	}
	if q, ok := a.ExecutionQuote.Get(); ok {
		w.Quote = &valueWire[quoteWire]{Value: ptr(toQuoteWire(q)), Basis: a.ExecutionQuote.Basis().String(), Source: a.ExecutionQuote.Source()}
	}
	if a.Side.Valid() {
		w.Side = a.Side.String()
	}
	if a.Account != (account.Reference{}) {
		w.Account = &accountRefWire{AccountID: a.Account.AccountID.String(), Broker: a.Account.Broker}
	}
	for _, e := range a.TradeEffects {
		w.TradeEffects = append(w.TradeEffects, tradeEffectWire{
			BrokerTradeID: e.BrokerTradeID, Role: e.Role.String(),
			Units: toValueWire(e.Units), Price: toValueWire(e.Price),
			RealizedPL: toValueWire(e.RealizedPL), Financing: toValueWire(e.Financing),
			SpreadCost: toValueWire(e.SpreadCost), GuaranteedExecutionFee: toValueWire(e.GuaranteedExecutionFee),
		})
	}
	return w
}

var roleByName = map[string]accountactivity.TradeRole{
	"opened": accountactivity.TradeRoleOpened, "reduced": accountactivity.TradeRoleReduced, "closed": accountactivity.TradeRoleClosed,
}

func fromWire(w activityWire) (accountactivity.Activity, error) {
	if w.Schema != schemaVersion {
		return accountactivity.Activity{}, schemaError(w.Schema)
	}
	a := accountactivity.Activity{
		Provider:           w.Provider,
		ProviderAccountID:  w.ProviderAccountID,
		ProviderActivityID: w.ProviderActivityID,
		ProviderType:       w.ProviderType,
		OccurredAt:         w.OccurredAt,
		ImportedAt:         w.ImportedAt,
		BrokerOrderID:      w.BrokerOrderID,
		BrokerBatchID:      w.BrokerBatchID,
		ProviderInstrument: w.ProviderInstrument,
		Instrument:         instrument.ID{},
	}
	switch w.Environment {
	case "practice":
		a.Environment = accountactivity.EnvironmentPractice
	case "live":
		a.Environment = accountactivity.EnvironmentLive
	default:
		return a, fmt.Errorf("environment %q is not practice or live", w.Environment)
	}
	kind, ok := kindByName[w.Kind]
	if !ok {
		return a, fmt.Errorf("kind %q is unknown", w.Kind)
	}
	a.Kind = kind

	if w.Instrument != "" {
		if err := json.Unmarshal([]byte(fmt.Sprintf("%q", w.Instrument)), &a.Instrument); err != nil {
			return a, fmt.Errorf("instrument: %w", err)
		}
	}
	switch w.Side {
	case "":
	case "buy":
		a.Side = order.Buy
	case "sell":
		a.Side = order.Sell
	default:
		return a, fmt.Errorf("side %q is not buy or sell", w.Side)
	}
	if w.Account != nil {
		aid, err := id.ParseAccountID(w.Account.AccountID)
		if err != nil {
			return a, fmt.Errorf("account id: %w", err)
		}
		a.Account = account.Reference{AccountID: aid, Broker: w.Account.Broker}
	}
	for _, l := range w.TradeEffects {
		role, ok := roleByName[l.Role]
		if !ok {
			return a, fmt.Errorf("trade effect role %q is unknown", l.Role)
		}
		e := accountactivity.TradeEffect{BrokerTradeID: l.BrokerTradeID, Role: role}
		var err error
		if e.Units, err = fromValueWire(l.Units); err != nil {
			return a, fmt.Errorf("trade effect %s units: %w", l.BrokerTradeID, err)
		}
		if e.Price, err = fromValueWire(l.Price); err != nil {
			return a, fmt.Errorf("trade effect %s price: %w", l.BrokerTradeID, err)
		}
		for _, f := range []struct {
			name string
			src  *valueWire[num.Money]
			dst  *accountactivity.Value[num.Money]
		}{
			{"realized_pl", l.RealizedPL, &e.RealizedPL}, {"financing", l.Financing, &e.Financing},
			{"spread_cost", l.SpreadCost, &e.SpreadCost}, {"guaranteed_execution_fee", l.GuaranteedExecutionFee, &e.GuaranteedExecutionFee},
		} {
			if *f.dst, err = fromValueWire(f.src); err != nil {
				return a, fmt.Errorf("trade effect %s %s: %w", l.BrokerTradeID, f.name, err)
			}
		}
		a.TradeEffects = append(a.TradeEffects, e)
	}

	var err error
	if a.Quantity, err = fromValueWire(w.Quantity); err != nil {
		return a, fmt.Errorf("quantity: %w", err)
	}
	if a.Price, err = fromValueWire(w.Price); err != nil {
		return a, fmt.Errorf("price: %w", err)
	}
	if a.FullVWAP, err = fromValueWire(w.FullVWAP); err != nil {
		return a, fmt.Errorf("full_vwap: %w", err)
	}
	if w.Quote != nil {
		if w.Quote.Value == nil {
			return a, fmt.Errorf("execution_quote: value object has no value")
		}
		q, err := fromQuoteWire(*w.Quote.Value)
		if err != nil {
			return a, fmt.Errorf("execution_quote: %w", err)
		}
		switch w.Quote.Basis {
		case "reported":
			if w.Quote.Source == "" {
				a.ExecutionQuote = accountactivity.Reported(q)
			} else if a.ExecutionQuote, err = accountactivity.ReportedFrom(q, w.Quote.Source); err != nil {
				return a, fmt.Errorf("execution_quote: %w", err)
			}
		case "derived":
			if a.ExecutionQuote, err = accountactivity.Derived(q, w.Quote.Source); err != nil {
				return a, fmt.Errorf("execution_quote: %w", err)
			}
		default:
			return a, fmt.Errorf("execution_quote: basis %q is not reported or derived", w.Quote.Basis)
		}
	}
	for _, f := range []struct {
		name string
		src  *valueWire[num.Money]
		dst  *accountactivity.Value[num.Money]
	}{
		{"realized_pl", w.RealizedPL, &a.RealizedPL}, {"spread_cost", w.SpreadCost, &a.SpreadCost},
		{"commission", w.Commission, &a.Commission}, {"financing", w.Financing, &a.Financing},
		{"other_fees", w.OtherFees, &a.OtherFees}, {"cash", w.Cash, &a.Cash},
		{"slippage_cost", w.SlippageCost, &a.SlippageCost},
	} {
		if *f.dst, err = fromValueWire(f.src); err != nil {
			return a, fmt.Errorf("%s: %w", f.name, err)
		}
	}
	if a.Benchmark, err = fromValueWire(w.Benchmark); err != nil {
		return a, fmt.Errorf("benchmark: %w", err)
	}
	if a.Slippage, err = fromValueWire(w.Slippage); err != nil {
		return a, fmt.Errorf("slippage: %w", err)
	}
	return a, nil
}

type checkpointWire struct {
	Schema            int       `json:"schema"`
	Provider          string    `json:"provider"`
	Environment       string    `json:"environment"`
	ProviderAccountID string    `json:"provider_account_id"`
	Cursor            string    `json:"cursor"`
	UpdatedAt         time.Time `json:"updated_at"`
}

func toCheckpointWire(cp accountactivity.Checkpoint) checkpointWire {
	return checkpointWire{
		Schema:            schemaVersion,
		Provider:          cp.Sync.Provider,
		Environment:       cp.Sync.Environment.String(),
		ProviderAccountID: cp.Sync.ProviderAccountID,
		Cursor:            cp.Cursor,
		UpdatedAt:         cp.UpdatedAt.UTC(),
	}
}

func fromCheckpointWire(w checkpointWire) (accountactivity.Checkpoint, error) {
	if w.Schema != schemaVersion {
		return accountactivity.Checkpoint{}, schemaError(w.Schema)
	}
	var env accountactivity.Environment
	switch w.Environment {
	case "practice":
		env = accountactivity.EnvironmentPractice
	case "live":
		env = accountactivity.EnvironmentLive
	default:
		return accountactivity.Checkpoint{}, fmt.Errorf("environment %q is not practice or live", w.Environment)
	}
	cp := accountactivity.Checkpoint{
		Sync:      accountactivity.SyncKey{Provider: w.Provider, Environment: env, ProviderAccountID: w.ProviderAccountID},
		Cursor:    w.Cursor,
		UpdatedAt: w.UpdatedAt,
	}
	// The same rules Batch.Validate enforces on the way in.
	if err := cp.Sync.Validate(); err != nil {
		return accountactivity.Checkpoint{}, err
	}
	if cp.Cursor == "" {
		return accountactivity.Checkpoint{}, fmt.Errorf("cursor is empty")
	}
	if cp.UpdatedAt.IsZero() {
		return accountactivity.Checkpoint{}, fmt.Errorf("updated_at is not set")
	}
	return cp, nil
}

func ptr[T any](v T) *T { return &v }
