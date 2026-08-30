package jsonl

import (
	"fmt"
	"strings"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/journal"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/rustyeddy/trader/risk"
)

// This file inverts wire.go: reconstructing journal.Entry (and its
// nested domain payloads) from a decoded entryWire.
//
// # Listing reconstruction is FX-only for now
//
// instrument.Listing's only public constructor, NewListing, requires a
// full instrument.Instrument, not merely its ID — since Instrument's
// identity is derived deterministically from its own Kind-specific
// fields (currency pair base/quote, equity exchange/ticker, and so
// on), reconstructing an arbitrary Instrument purely from its
// serialized instrument.ID string would need a per-Kind parser for
// instrument.ID.String()'s own text form. That form is documented
// (instrument.ID.String()'s own doc comment shows "fx:EUR/USD" as an
// example), so parsing the "fx:" case specifically is not reading an
// undocumented internal format — but every other Kind's text shape is
// not spelled out the same way, and every fixture and test in this
// codebase through M5 uses currency pairs exclusively. fromListingWire
// therefore supports "fx:" reconstruction now and returns
// ErrCorruptEntry for any other prefix, rather than silently guessing
// at a Kind-specific parse this package hasn't implemented — a
// documented v0 limitation to lift when a non-FX instrument kind
// actually needs journal read-back.
func fromListingWire(w listingWire) (instrument.Listing, error) {
	base, quote, err := parseFXPair(w.InstrumentID)
	if err != nil {
		return instrument.Listing{}, err
	}
	inst, err := instrument.NewCurrencyPair(base, quote)
	if err != nil {
		return instrument.Listing{}, fmt.Errorf("%w: reconstructing instrument for %s: %v", ErrCorruptEntry, w.InstrumentID, err)
	}
	spec, err := instrument.NewSpec(w.Spec.TickSize, w.Spec.QuantityIncrement, w.Spec.Multiplier, w.Spec.SettlementCurrency)
	if err != nil {
		return instrument.Listing{}, fmt.Errorf("%w: reconstructing spec for %s: %v", ErrCorruptEntry, w.InstrumentID, err)
	}
	listing, err := instrument.NewListing(instrument.ListingParams{
		Instrument: inst,
		Provider:   w.Provider,
		Venue:      w.Venue,
		Symbol:     w.Symbol,
		Spec:       spec,
		Tradable:   w.Tradable,
	})
	if err != nil {
		return instrument.Listing{}, fmt.Errorf("%w: reconstructing listing for %s: %v", ErrCorruptEntry, w.InstrumentID, err)
	}
	if listing.InstrumentID().String() != w.InstrumentID {
		return instrument.Listing{}, fmt.Errorf("%w: reconstructed instrument id %s does not match recorded %s", ErrCorruptEntry, listing.InstrumentID(), w.InstrumentID)
	}
	return listing, nil
}

// parseFXPair parses "fx:BASE/QUOTE" into its two currencies.
func parseFXPair(rawID string) (base, quote num.Currency, err error) {
	const prefix = "fx:"
	if !strings.HasPrefix(rawID, prefix) {
		return num.Currency{}, num.Currency{}, fmt.Errorf("%w: instrument id %q is not a currency pair (only fx: reconstruction is supported)", ErrCorruptEntry, rawID)
	}
	rest := strings.TrimPrefix(rawID, prefix)
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) != 2 {
		return num.Currency{}, num.Currency{}, fmt.Errorf("%w: malformed currency pair id %q", ErrCorruptEntry, rawID)
	}
	base, err = num.ParseCurrency(parts[0])
	if err != nil {
		return num.Currency{}, num.Currency{}, fmt.Errorf("%w: parsing base currency in %q: %v", ErrCorruptEntry, rawID, err)
	}
	quote, err = num.ParseCurrency(parts[1])
	if err != nil {
		return num.Currency{}, num.Currency{}, fmt.Errorf("%w: parsing quote currency in %q: %v", ErrCorruptEntry, rawID, err)
	}
	return base, quote, nil
}

func fromIntentWire(w intentWire) (order.Intent, error) {
	kind, err := parseIntentKind(w.Kind)
	if err != nil {
		return order.Intent{}, err
	}
	side, err := parseSide(w.Side)
	if err != nil {
		return order.Intent{}, err
	}
	return order.Intent{
		IntentID:   w.IntentID,
		Kind:       kind,
		Instrument: w.Instrument,
		Side:       side,
		Quantity:   w.Quantity,
		StopPrice:  w.StopPrice,
		Metadata:   w.Metadata,
	}, nil
}

func fromProposalWire(w proposalWire) (order.Proposal, error) {
	listing, err := fromListingWire(w.Listing)
	if err != nil {
		return order.Proposal{}, err
	}
	side, err := parseSide(w.Side)
	if err != nil {
		return order.Proposal{}, err
	}
	typ, err := parseType(w.Type)
	if err != nil {
		return order.Proposal{}, err
	}
	tif, err := parseTimeInForce(w.TimeInForce)
	if err != nil {
		return order.Proposal{}, err
	}
	return order.Proposal{
		Listing:     listing,
		AccountID:   w.AccountID,
		Side:        side,
		Type:        typ,
		TimeInForce: tif,
		Quantity:    w.Quantity,
		LimitPrice:  w.LimitPrice,
		StopPrice:   w.StopPrice,
		ReduceOnly:  w.ReduceOnly,
		Metadata:    w.Metadata,
	}, nil
}

func fromDecisionWire(w decisionWire) risk.Decision {
	d := risk.Decision{Allowed: w.Allowed}
	for _, v := range w.Violations {
		d.Violations = append(d.Violations, risk.Violation{Rule: v.Rule, Message: v.Message, Measured: v.Measured, Limit: v.Limit})
	}
	for _, ww := range w.Warnings {
		d.Warnings = append(d.Warnings, risk.Warning{Rule: ww.Rule, Message: ww.Message})
	}
	for _, rr := range w.RuleResults {
		res := risk.RuleResult{Rule: rr.Rule}
		for _, v := range rr.Violations {
			res.Violations = append(res.Violations, risk.Violation{Rule: v.Rule, Message: v.Message, Measured: v.Measured, Limit: v.Limit})
		}
		for _, ww := range rr.Warnings {
			res.Warnings = append(res.Warnings, risk.Warning{Rule: ww.Rule, Message: ww.Message})
		}
		d.RuleResults = append(d.RuleResults, res)
	}
	return d
}

func fromRequestWire(w requestWire) (order.Request, error) {
	proposal, err := fromProposalWire(w.Proposal)
	if err != nil {
		return order.Request{}, err
	}
	return order.Request{Proposal: proposal, OrderID: w.OrderID}, nil
}

func fromOrderWire(w orderWire) (order.Order, error) {
	req, err := fromRequestWire(w.Request)
	if err != nil {
		return order.Order{}, err
	}
	status, err := parseStatus(w.Status)
	if err != nil {
		return order.Order{}, err
	}
	o := order.Order{
		Request:              req,
		BrokerOrderID:        w.BrokerOrderID,
		AcceptedQuantity:     w.AcceptedQuantity,
		AcceptedLimitPrice:   w.AcceptedLimitPrice,
		AcceptedStopPrice:    w.AcceptedStopPrice,
		Status:               status,
		FilledQuantity:       w.FilledQuantity,
		AvgFillPrice:         w.AvgFillPrice,
		AppliedFillIDs:       w.AppliedFillIDs,
		AppliedBrokerFillIDs: w.AppliedBrokerFillIDs,
		UpdatedAt:            w.UpdatedAt,
	}
	if w.Rejection != nil {
		reason, err := parseRejectReason(w.Rejection.Reason)
		if err != nil {
			return order.Order{}, err
		}
		o.Rejection = &order.Rejection{Reason: reason, Detail: w.Rejection.Detail, BrokerCode: w.Rejection.BrokerCode}
	}
	return o, nil
}

func fromFillWire(w fillWire) (order.Fill, error) {
	listing, err := fromListingWire(w.Listing)
	if err != nil {
		return order.Fill{}, err
	}
	side, err := parseSide(w.Side)
	if err != nil {
		return order.Fill{}, err
	}
	return order.Fill{
		FillID:        w.FillID,
		OrderID:       w.OrderID,
		BrokerOrderID: w.BrokerOrderID,
		BrokerFillID:  w.BrokerFillID,
		AccountID:     w.AccountID,
		Listing:       listing,
		Side:          side,
		Price:         w.Price,
		Quantity:      w.Quantity,
		Commission:    w.Commission,
		Timestamp:     w.Timestamp,
		Metadata:      w.Metadata,
	}, nil
}

func fromPositionWire(w positionWire) (order.Position, error) {
	listing, err := fromListingWire(w.Listing)
	if err != nil {
		return order.Position{}, err
	}
	side, err := parsePositionSide(w.Side)
	if err != nil {
		return order.Position{}, err
	}
	return order.Position{AccountID: w.AccountID, Listing: listing, Side: side, Quantity: w.Quantity, AvgPrice: w.AvgPrice}, nil
}

func fromAccountWire(w accountWire) (account.Snapshot, error) {
	positions := make([]order.Position, 0, len(w.Positions))
	for _, pw := range w.Positions {
		p, err := fromPositionWire(pw)
		if err != nil {
			return account.Snapshot{}, err
		}
		positions = append(positions, p)
	}
	openOrders := make([]order.Order, 0, len(w.OpenOrders))
	for _, ow := range w.OpenOrders {
		o, err := fromOrderWire(ow)
		if err != nil {
			return account.Snapshot{}, err
		}
		openOrders = append(openOrders, o)
	}
	snap, err := account.NewSnapshot(account.SnapshotParams{
		AccountID:       w.AccountID,
		Broker:          w.Broker,
		Currency:        w.Currency,
		AsOf:            w.AsOf,
		Cursor:          w.Cursor,
		CashBalances:    w.CashBalances,
		Equity:          w.Equity,
		BuyingPower:     w.BuyingPower,
		MarginUsed:      w.MarginUsed,
		MarginAvailable: w.MarginAvailable,
		RealizedPnL:     w.RealizedPnL,
		UnrealizedPnL:   w.UnrealizedPnL,
		Fees:            w.Fees,
		Financing:       w.Financing,
		Positions:       positions,
		OpenOrders:      openOrders,
	})
	if err != nil {
		return account.Snapshot{}, fmt.Errorf("%w: reconstructing account snapshot: %v", ErrCorruptEntry, err)
	}
	return snap, nil
}

func fromStatusWire(w statusWire) (broker.Status, error) {
	state, err := parseAccountStatus(w.State)
	if err != nil {
		return broker.Status{}, err
	}
	return broker.Status{State: state, BrokerCode: w.BrokerCode, Message: w.Message}, nil
}

func fromTradeWire(w tradeWire) (order.Trade, error) {
	listing, err := fromListingWire(w.Listing)
	if err != nil {
		return order.Trade{}, err
	}
	side, err := parsePositionSide(w.Side)
	if err != nil {
		return order.Trade{}, err
	}
	return order.Trade{
		AccountID:    w.AccountID,
		Listing:      listing,
		Side:         side,
		EntryFillIDs: w.EntryFillIDs,
		ExitFillIDs:  w.ExitFillIDs,
		OpenedAt:     w.OpenedAt,
		ClosedAt:     w.ClosedAt,
		RealizedPnL:  w.RealizedPnL,
		Costs:        w.Costs,
	}, nil
}

// fromEntryWire reconstructs a journal.Entry from a decoded entryWire.
// It returns ErrCorruptEntry (possibly wrapping a more specific parse
// error) for any wire value that doesn't reconstruct into a valid
// domain payload — a malformed or hand-edited journal line surfaces
// here, not as a panic or a silently wrong value.
func fromEntryWire(w entryWire) (journal.Entry, error) {
	if w.Sequence == 0 {
		return journal.Entry{}, fmt.Errorf("%w: sequence must be at least 1, got 0", ErrCorruptEntry)
	}

	kind, err := parseKind(w.Kind)
	if err != nil {
		return journal.Entry{}, err
	}

	rec := journal.Record{RunID: w.RunID, Metadata: w.Metadata, Kind: kind}
	switch kind {
	case journal.KindRunStarted:
		if w.RunStarted == nil {
			return journal.Entry{}, fmt.Errorf("%w: kind run-started missing run_started payload", ErrCorruptEntry)
		}
		rec.RunStarted = &journal.RunStarted{RunID: w.RunStarted.RunID, Header: w.RunStarted.Header}
	case journal.KindIntent:
		if w.Intent == nil {
			return journal.Entry{}, fmt.Errorf("%w: kind intent missing intent payload", ErrCorruptEntry)
		}
		v, err := fromIntentWire(*w.Intent)
		if err != nil {
			return journal.Entry{}, err
		}
		rec.Intent = &v
	case journal.KindProposal:
		if w.Proposal == nil {
			return journal.Entry{}, fmt.Errorf("%w: kind proposal missing proposal payload", ErrCorruptEntry)
		}
		v, err := fromProposalWire(*w.Proposal)
		if err != nil {
			return journal.Entry{}, err
		}
		rec.Proposal = &v
	case journal.KindDecision:
		if w.Decision == nil {
			return journal.Entry{}, fmt.Errorf("%w: kind decision missing decision payload", ErrCorruptEntry)
		}
		v := fromDecisionWire(*w.Decision)
		rec.Decision = &v
	case journal.KindRequest:
		if w.Request == nil {
			return journal.Entry{}, fmt.Errorf("%w: kind request missing request payload", ErrCorruptEntry)
		}
		v, err := fromRequestWire(*w.Request)
		if err != nil {
			return journal.Entry{}, err
		}
		rec.Request = &v
	case journal.KindOrder:
		if w.Order == nil {
			return journal.Entry{}, fmt.Errorf("%w: kind order missing order payload", ErrCorruptEntry)
		}
		v, err := fromOrderWire(*w.Order)
		if err != nil {
			return journal.Entry{}, err
		}
		rec.Order = &v
	case journal.KindFill:
		if w.Fill == nil {
			return journal.Entry{}, fmt.Errorf("%w: kind fill missing fill payload", ErrCorruptEntry)
		}
		v, err := fromFillWire(*w.Fill)
		if err != nil {
			return journal.Entry{}, err
		}
		rec.Fill = &v
	case journal.KindAccount:
		if w.Account == nil {
			return journal.Entry{}, fmt.Errorf("%w: kind account missing account payload", ErrCorruptEntry)
		}
		v, err := fromAccountWire(*w.Account)
		if err != nil {
			return journal.Entry{}, err
		}
		rec.Account = &v
	case journal.KindStatus:
		if w.Status == nil {
			return journal.Entry{}, fmt.Errorf("%w: kind status missing status payload", ErrCorruptEntry)
		}
		v, err := fromStatusWire(*w.Status)
		if err != nil {
			return journal.Entry{}, err
		}
		rec.Status = &v
	case journal.KindTrade:
		if w.Trade == nil {
			return journal.Entry{}, fmt.Errorf("%w: kind trade missing trade payload", ErrCorruptEntry)
		}
		v, err := fromTradeWire(*w.Trade)
		if err != nil {
			return journal.Entry{}, err
		}
		rec.Trade = &v
	case journal.KindRunCompleted:
		if w.RunCompleted == nil {
			return journal.Entry{}, fmt.Errorf("%w: kind run-completed missing run_completed payload", ErrCorruptEntry)
		}
		rec.RunCompleted = &journal.RunCompleted{RunID: w.RunCompleted.RunID, EntryCount: w.RunCompleted.EntryCount}
	}

	validated, err := journal.NewRecord(rec)
	if err != nil {
		return journal.Entry{}, fmt.Errorf("%w: %v", ErrCorruptEntry, err)
	}
	return journal.Entry{Record: validated, Sequence: w.Sequence}, nil
}

// parseKind inverts Kind.String().
func parseKind(s string) (journal.Kind, error) {
	switch s {
	case "run-started":
		return journal.KindRunStarted, nil
	case "intent":
		return journal.KindIntent, nil
	case "proposal":
		return journal.KindProposal, nil
	case "decision":
		return journal.KindDecision, nil
	case "request":
		return journal.KindRequest, nil
	case "order":
		return journal.KindOrder, nil
	case "fill":
		return journal.KindFill, nil
	case "account":
		return journal.KindAccount, nil
	case "status":
		return journal.KindStatus, nil
	case "trade":
		return journal.KindTrade, nil
	case "run-completed":
		return journal.KindRunCompleted, nil
	default:
		return journal.KindUnknown, fmt.Errorf("%w: unrecognized kind %q", ErrCorruptEntry, s)
	}
}
