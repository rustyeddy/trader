package oanda

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/internal/accountactivity"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

// Money, units and prices are decoded as strings and parsed by num, so no
// provider value passes through binary floating point. Optional amounts are
// pointers: absent means unknown, "0.0000" means a reported zero.
//
// tradeRef is the part of tradeOpened, tradeReduced and each tradesClosed
// entry the importer keeps. OANDA documents their prices as the exact,
// official execution prices.
type tradeRef struct {
	TradeID                string  `json:"tradeID"`
	Units                  *string `json:"units"`
	Price                  *string `json:"price"`
	RealizedPL             *string `json:"realizedPL"`
	Financing              *string `json:"financing"`
	GuaranteedExecutionFee *string `json:"guaranteedExecutionFee"`
	HalfSpreadCost         *string `json:"halfSpreadCost"`
}

// bucketWire's fields are pointers: a rung with no price or no liquidity is
// malformed, not a rung of zero.
type bucketWire struct {
	Price     *string      `json:"price"`
	Liquidity *json.Number `json:"liquidity"`
}

// fullPriceWire is OANDA's ClientPrice snapshot. Deprecated status and
// conversion fields are not carried. Every field is required when a
// fullPrice is present; presence is tracked so that missing is never taken
// for explicitly empty (a ladder may legitimately be an empty array).
type fullPriceWire struct {
	Time        *string       `json:"time"`
	Bids        *[]bucketWire `json:"bids"`
	Asks        *[]bucketWire `json:"asks"`
	CloseoutBid *string       `json:"closeoutBid"`
	CloseoutAsk *string       `json:"closeoutAsk"`
}

type txWire struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Time       string `json:"time"`
	BatchID    string `json:"batchID"`
	OrderID    string `json:"orderID"`
	Instrument string `json:"instrument"`

	Units string `json:"units"`
	// Price is the deprecated aggregate top-level price. It is kept as a
	// non-authoritative fact; the per-trade prices are the exact ones.
	Price string `json:"price"`

	// FullVWAP is the price the units should have been filled at absent
	// guaranteed-price execution: a broker reference, not the actual price.
	FullVWAP  *string        `json:"fullVWAP"`
	FullPrice *fullPriceWire `json:"fullPrice"`

	PL                     *string `json:"pl"`
	Financing              *string `json:"financing"`
	Commission             *string `json:"commission"`
	GuaranteedExecutionFee *string `json:"guaranteedExecutionFee"`
	HalfSpreadCost         *string `json:"halfSpreadCost"`

	TradeOpened  *tradeRef  `json:"tradeOpened"`
	TradeReduced *tradeRef  `json:"tradeReduced"`
	TradesClosed []tradeRef `json:"tradesClosed"`

	Amount             *string `json:"amount"`
	DividendAdjustment *string `json:"dividendAdjustment"`
}

// orderTypes are the transactions that create an order; their own ID is the
// broker order ID.
var orderTypes = map[string]bool{
	"MARKET_ORDER": true, "LIMIT_ORDER": true, "STOP_ORDER": true,
	"MARKET_IF_TOUCHED_ORDER": true, "TAKE_PROFIT_ORDER": true, "STOP_LOSS_ORDER": true,
	"GUARANTEED_STOP_LOSS_ORDER": true, "TRAILING_STOP_LOSS_ORDER": true, "FIXED_PRICE_ORDER": true,
}

// normalize converts one raw OANDA transaction into an Activity for stream,
// with money in the account currency cur. ImportedAt is left for the sync
// loop. A recognized transaction that cannot be parsed is an error; an
// unrecognized one becomes KindOther.
func normalize(raw json.RawMessage, stream accountactivity.SyncKey, cur num.Currency) (accountactivity.Activity, error) {
	var w txWire
	if err := json.Unmarshal(raw, &w); err != nil {
		return accountactivity.Activity{}, fmt.Errorf("%w: transaction: %v", ErrBadResponse, err)
	}
	if w.ID == "" || w.Type == "" {
		return accountactivity.Activity{}, fmt.Errorf("%w: transaction without id or type", ErrBadResponse)
	}
	occurred, err := time.Parse(time.RFC3339Nano, w.Time)
	if err != nil {
		return accountactivity.Activity{}, fmt.Errorf("transaction %s: time %q is not RFC3339: %w", w.ID, w.Time, err)
	}

	a := accountactivity.Activity{
		Provider:           stream.Provider,
		Environment:        stream.Environment,
		ProviderAccountID:  stream.ProviderAccountID,
		ProviderActivityID: w.ID,
		ProviderType:       w.Type,
		OccurredAt:         occurred.UTC(),
		BrokerOrderID:      w.OrderID,
		BrokerBatchID:      w.BatchID,
		ProviderInstrument: w.Instrument,
	}
	if orderTypes[w.Type] && a.BrokerOrderID == "" {
		a.BrokerOrderID = w.ID
	}

	var norm func() error
	switch w.Type {
	case "ORDER_FILL":
		norm = func() error { return fillFields(&a, w, cur) }
	case "DAILY_FINANCING":
		norm = func() error { return financingFields(&a, w, cur) }
	case "TRANSFER_FUNDS":
		norm = func() error { return transferFields(&a, w, cur) }
	case "DIVIDEND_ADJUSTMENT":
		norm = func() error { return dividendFields(&a, w, cur) }
	default:
		a.Kind = accountactivity.KindOther
		a.Instrument = instrumentOrZero(w.Instrument)
	}
	if norm != nil {
		if err := norm(); err != nil {
			return accountactivity.Activity{}, fmt.Errorf("transaction %s (%s): %w", w.ID, w.Type, err)
		}
	}
	return a, nil
}

// fxCurrencies are the fiat currency codes OANDA quotes forex pairs in.
// num.ParseCurrency only checks that a code looks like one (three to five
// capitals), so XAU, WTICO or SPX500 would pass it; this list is what
// separates a currency pair from a metal, commodity or index CFD.
var fxCurrencies = map[string]bool{
	"AUD": true, "CAD": true, "CHF": true, "CNH": true, "CZK": true, "DKK": true,
	"EUR": true, "GBP": true, "HKD": true, "HUF": true, "JPY": true, "MXN": true,
	"NOK": true, "NZD": true, "PLN": true, "SEK": true, "SGD": true, "THB": true,
	"TRY": true, "USD": true, "ZAR": true,
}

// instrumentOrZero maps an OANDA instrument name (EUR_USD) to Trader's ID,
// or the zero ID when it is not a forex pair of known fiat currencies (a
// metal, commodity, index or bond CFD). The provider-native name is kept in
// Activity.ProviderInstrument either way.
func instrumentOrZero(name string) instrument.ID {
	base, quote, ok := strings.Cut(name, "_")
	if !ok || !fxCurrencies[base] || !fxCurrencies[quote] {
		return instrument.ID{}
	}
	b, err1 := num.ParseCurrency(base)
	q, err2 := num.ParseCurrency(quote)
	if err1 != nil || err2 != nil {
		return instrument.ID{}
	}
	return instrument.CurrencyPairID(b, q)
}

// reportedMoney parses an optional OANDA amount into a reported Money value
// named by its provider field. sign is -1 for fields OANDA reports as a
// positive cost (they reduce the balance), which Trader holds as negative
// account-centric money (ADR-079), and +1 for fields already signed.
func reportedMoney(p *string, field string, sign int, cur num.Currency) (accountactivity.Value[num.Money], error) {
	if p == nil {
		return accountactivity.Value[num.Money]{}, nil
	}
	m, err := num.ParseMoney(*p, cur)
	if err != nil {
		return accountactivity.Value[num.Money]{}, fmt.Errorf("%s: %w", field, err)
	}
	if sign < 0 {
		if m, err = m.Neg(); err != nil {
			return accountactivity.Value[num.Money]{}, fmt.Errorf("%s: %w", field, err)
		}
	}
	return accountactivity.ReportedFrom(m, field)
}

func fillFields(a *accountactivity.Activity, w txWire, cur num.Currency) error {
	a.Instrument = instrumentOrZero(w.Instrument)
	if a.Instrument.IsZero() {
		// A fill in an instrument Trader cannot identify yet (an index or
		// metal CFD, say) is kept as an unmodelled event, never dropped and
		// never given an economics it cannot honestly claim.
		a.Kind = accountactivity.KindOther
		return nil
	}
	a.Kind = accountactivity.KindFill

	units := strings.TrimPrefix(w.Units, "+")
	switch {
	case strings.HasPrefix(units, "-"):
		a.Side = order.Sell
		units = units[1:]
	default:
		a.Side = order.Buy
	}
	q, err := num.ParseQuantity(units)
	if err != nil {
		return fmt.Errorf("units %q: %w", w.Units, err)
	}
	if q.IsZero() {
		return fmt.Errorf("units are zero")
	}
	a.Quantity, err = accountactivity.ReportedFrom(q, "units")
	if err != nil {
		return err
	}
	var p num.Price
	if w.Price != "" {
		if p, err = num.ParsePrice(w.Price); err != nil {
			return fmt.Errorf("price %q: %w", w.Price, err)
		}
		if a.Price, err = accountactivity.ReportedFrom(p, priceDeprecated); err != nil {
			return err
		}
	}
	if w.FullVWAP != nil {
		v, err := num.ParsePrice(*w.FullVWAP)
		if err != nil {
			return fmt.Errorf("fullVWAP %q: %w", *w.FullVWAP, err)
		}
		if a.FullVWAP, err = accountactivity.ReportedFrom(v, "fullVWAP"); err != nil {
			return err
		}
	}
	if w.FullPrice != nil {
		q, err := quoteFrom(*w.FullPrice)
		if err != nil {
			return fmt.Errorf("fullPrice: %w", err)
		}
		if a.ExecutionQuote, err = accountactivity.ReportedFrom(q, "fullPrice"); err != nil {
			return err
		}
	}

	for _, f := range []struct {
		dst   *accountactivity.Value[num.Money]
		src   *string
		field string
		sign  int
	}{
		{&a.RealizedPL, w.PL, "pl", +1},
		{&a.Financing, w.Financing, "financing", +1},
		{&a.Commission, w.Commission, "commission", -1},
		{&a.OtherFees, w.GuaranteedExecutionFee, "guaranteedExecutionFee", -1},
		{&a.SpreadCost, w.HalfSpreadCost, "halfSpreadCost", +1}, // OANDA signs it itself
	} {
		if *f.dst, err = reportedMoney(f.src, f.field, f.sign, cur); err != nil {
			return err
		}
	}

	if w.TradeOpened != nil {
		e, err := tradeEffect(*w.TradeOpened, accountactivity.TradeRoleOpened, "tradeOpened", cur)
		if err != nil {
			return err
		}
		a.TradeEffects = append(a.TradeEffects, e)
	}
	if w.TradeReduced != nil {
		e, err := tradeEffect(*w.TradeReduced, accountactivity.TradeRoleReduced, "tradeReduced", cur)
		if err != nil {
			return err
		}
		a.TradeEffects = append(a.TradeEffects, e)
	}
	for _, c := range w.TradesClosed {
		e, err := tradeEffect(c, accountactivity.TradeRoleClosed, "tradesClosed", cur)
		if err != nil {
			return err
		}
		a.TradeEffects = append(a.TradeEffects, e)
	}
	if !a.Price.Known() {
		priced := false
		for _, e := range a.TradeEffects {
			priced = priced || e.Price.Known()
		}
		if !priced {
			return fmt.Errorf("no execution price: neither price nor any per-trade price")
		}
	}
	return nil
}

// priceDeprecated is the recorded source of the top-level price, so no reader
// mistakes it for the authoritative per-trade figure.
const priceDeprecated = "price (deprecated aggregate)"

// tradeEffect converts one embedded trade record. Absent fields stay unknown.
// guaranteedExecutionFee is a positive cost in OANDA's data and is negated;
// halfSpreadCost keeps OANDA's sign; realizedPL and financing are signed.
func tradeEffect(t tradeRef, role accountactivity.TradeRole, section string, cur num.Currency) (accountactivity.TradeEffect, error) {
	e := accountactivity.TradeEffect{BrokerTradeID: t.TradeID, Role: role}
	if t.TradeID == "" {
		return e, fmt.Errorf("%s has no tradeID", section)
	}
	var err error
	if t.Units != nil {
		abs := strings.TrimPrefix(strings.TrimPrefix(*t.Units, "-"), "+")
		q, err := num.ParseQuantity(abs)
		if err != nil {
			return e, fmt.Errorf("%s %s units %q: %w", section, t.TradeID, *t.Units, err)
		}
		if e.Units, err = accountactivity.ReportedFrom(q, section+".units"); err != nil {
			return e, err
		}
	}
	if t.Price != nil {
		p, err := num.ParsePrice(*t.Price)
		if err != nil {
			return e, fmt.Errorf("%s %s price %q: %w", section, t.TradeID, *t.Price, err)
		}
		if e.Price, err = accountactivity.ReportedFrom(p, section+".price"); err != nil {
			return e, err
		}
	}
	for _, f := range []struct {
		dst   *accountactivity.Value[num.Money]
		src   *string
		field string
		sign  int
	}{
		{&e.RealizedPL, t.RealizedPL, "realizedPL", +1},
		{&e.Financing, t.Financing, "financing", +1},
		{&e.SpreadCost, t.HalfSpreadCost, "halfSpreadCost", +1},
		{&e.GuaranteedExecutionFee, t.GuaranteedExecutionFee, "guaranteedExecutionFee", -1},
	} {
		if *f.dst, err = reportedMoney(f.src, section+"."+f.field, f.sign, cur); err != nil {
			return e, fmt.Errorf("%s %s: %w", section, t.TradeID, err)
		}
	}
	return e, nil
}

// quoteFrom converts OANDA's fullPrice snapshot, keeping both ladders whole.
// A snapshot missing its time, either ladder (an explicit empty array is
// fine) or either closeout price is malformed provider data, not a partial
// quote: normalizing it would present an incomplete snapshot as the whole.
func quoteFrom(w fullPriceWire) (accountactivity.ExecutionQuote, error) {
	var q accountactivity.ExecutionQuote
	switch {
	case w.Time == nil:
		return q, fmt.Errorf("no time")
	case w.Bids == nil:
		return q, fmt.Errorf("no bids array")
	case w.Asks == nil:
		return q, fmt.Errorf("no asks array")
	case w.CloseoutBid == nil:
		return q, fmt.Errorf("no closeoutBid")
	case w.CloseoutAsk == nil:
		return q, fmt.Errorf("no closeoutAsk")
	}
	at, err := time.Parse(time.RFC3339Nano, *w.Time)
	if err != nil {
		return q, fmt.Errorf("time %q: %w", *w.Time, err)
	}
	q.At = at.UTC()

	levels := func(side string, in []bucketWire) ([]accountactivity.PriceLevel, error) {
		var out []accountactivity.PriceLevel
		for _, b := range in {
			if b.Price == nil || b.Liquidity == nil {
				return nil, fmt.Errorf("%s level is missing its price or liquidity", side)
			}
			p, err := num.ParsePrice(*b.Price)
			if err != nil {
				return nil, fmt.Errorf("%s price %q: %w", side, *b.Price, err)
			}
			l, err := num.ParseQuantity(b.Liquidity.String())
			if err != nil {
				return nil, fmt.Errorf("%s liquidity %q: %w", side, b.Liquidity.String(), err)
			}
			if p.IsZero() || l.IsZero() {
				return nil, fmt.Errorf("%s level needs a positive price and liquidity", side)
			}
			out = append(out, accountactivity.PriceLevel{Price: p, Liquidity: l})
		}
		return out, nil
	}
	if q.Bids, err = levels("bid", *w.Bids); err != nil {
		return q, err
	}
	if q.Asks, err = levels("ask", *w.Asks); err != nil {
		return q, err
	}
	for _, c := range []struct {
		dst   *accountactivity.Value[num.Price]
		src   string
		field string
	}{{&q.CloseoutBid, *w.CloseoutBid, "closeoutBid"}, {&q.CloseoutAsk, *w.CloseoutAsk, "closeoutAsk"}} {
		p, err := num.ParsePrice(c.src)
		if err != nil {
			return q, fmt.Errorf("%s %q: %w", c.field, c.src, err)
		}
		if *c.dst, err = accountactivity.ReportedFrom(p, "fullPrice."+c.field); err != nil {
			return q, err
		}
	}
	return q, nil
}

func financingFields(a *accountactivity.Activity, w txWire, cur num.Currency) error {
	a.Kind = accountactivity.KindFinancing
	if w.Financing == nil {
		return fmt.Errorf("no financing amount")
	}
	var err error
	a.Financing, err = reportedMoney(w.Financing, "financing", +1, cur)
	return err
}

func transferFields(a *accountactivity.Activity, w txWire, cur num.Currency) error {
	if w.Amount == nil {
		return fmt.Errorf("no amount")
	}
	cash, err := reportedMoney(w.Amount, "amount", +1, cur)
	if err != nil {
		return err
	}
	m, _ := cash.Get()
	zero, err := num.ParseMoney("0", cur)
	if err != nil {
		return err
	}
	sign, err := m.Cmp(zero)
	if err != nil {
		return err
	}
	switch {
	case sign < 0:
		a.Kind = accountactivity.KindWithdrawal
	case sign > 0:
		a.Kind = accountactivity.KindDeposit
	default:
		// A zero transfer moves nothing; keep it as an unmodelled event.
		a.Kind = accountactivity.KindOther
		return nil
	}
	a.Cash = cash
	return nil
}

func dividendFields(a *accountactivity.Activity, w txWire, cur num.Currency) error {
	a.Kind = accountactivity.KindDividend
	if w.DividendAdjustment == nil {
		return fmt.Errorf("no dividendAdjustment amount")
	}
	var err error
	a.Cash, err = reportedMoney(w.DividendAdjustment, "dividendAdjustment", +1, cur)
	a.Instrument = instrumentOrZero(w.Instrument)
	return err
}

// parseID parses an OANDA transaction ID or cursor.
func parseID(s string) (int64, error) {
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id < 0 {
		return 0, fmt.Errorf("transaction id %q is not a non-negative integer", s)
	}
	return id, nil
}
