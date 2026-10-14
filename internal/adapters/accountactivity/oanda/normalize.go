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
type tradeRef struct {
	TradeID string `json:"tradeID"`
}

type txWire struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Time       string `json:"time"`
	OrderID    string `json:"orderID"`
	Instrument string `json:"instrument"`

	Units string `json:"units"`
	Price string `json:"price"`

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

// instrumentOrZero maps an OANDA instrument name (EUR_USD) to Trader's ID,
// or the zero ID when it is not a currency pair Trader can identify.
func instrumentOrZero(name string) instrument.ID {
	base, quote, ok := strings.Cut(name, "_")
	if !ok {
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
	p, err := num.ParsePrice(w.Price)
	if err != nil {
		return fmt.Errorf("price %q: %w", w.Price, err)
	}
	if a.Price, err = accountactivity.ReportedFrom(p, "price"); err != nil {
		return err
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
		{&a.SpreadCost, w.HalfSpreadCost, "halfSpreadCost", -1},
	} {
		if *f.dst, err = reportedMoney(f.src, f.field, f.sign, cur); err != nil {
			return err
		}
	}

	if w.TradeOpened != nil {
		a.TradeLinks = append(a.TradeLinks, accountactivity.TradeLink{BrokerTradeID: w.TradeOpened.TradeID, Role: accountactivity.TradeRoleOpened})
	}
	if w.TradeReduced != nil {
		a.TradeLinks = append(a.TradeLinks, accountactivity.TradeLink{BrokerTradeID: w.TradeReduced.TradeID, Role: accountactivity.TradeRoleReduced})
	}
	for _, c := range w.TradesClosed {
		a.TradeLinks = append(a.TradeLinks, accountactivity.TradeLink{BrokerTradeID: c.TradeID, Role: accountactivity.TradeRoleClosed})
	}
	return nil
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
