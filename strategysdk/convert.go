package strategysdk

import (
	"fmt"
	"time"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/order"
	v1 "github.com/rustyeddy/trader/protocol/strategy/v1"
)

// fromWireBar reconstructs a marketdata.Bar from a received *v1.Bar
// (BarEvent.bar, GetHistoryBarsResponse.bars). time_unix_nanos is
// carried verbatim, never reconstructed from array position or
// fixed-duration arithmetic — marketdata.Bar's own documented
// invariant.
func fromWireBar(w *v1.Bar) (marketdata.Bar, error) {
	if w == nil {
		return marketdata.Bar{}, fmt.Errorf("%w: bar must be set", ErrInvalidWireValue)
	}
	open, err := parsePrice("open", w.GetOpen())
	if err != nil {
		return marketdata.Bar{}, err
	}
	high, err := parsePrice("high", w.GetHigh())
	if err != nil {
		return marketdata.Bar{}, err
	}
	low, err := parsePrice("low", w.GetLow())
	if err != nil {
		return marketdata.Bar{}, err
	}
	closePrice, err := parsePrice("close", w.GetClose())
	if err != nil {
		return marketdata.Bar{}, err
	}
	avgSpread, err := parsePrice("avg_spread", w.GetAvgSpread())
	if err != nil {
		return marketdata.Bar{}, err
	}
	maxSpread, err := parsePrice("max_spread", w.GetMaxSpread())
	if err != nil {
		return marketdata.Bar{}, err
	}
	if w.GetTicks() < 0 {
		return marketdata.Bar{}, fmt.Errorf("%w: ticks must not be negative", ErrInvalidWireValue)
	}

	return marketdata.Bar{
		Time:      time.Unix(0, w.GetTimeUnixNanos()).UTC(),
		Open:      open,
		High:      high,
		Low:       low,
		Close:     closePrice,
		AvgSpread: avgSpread,
		MaxSpread: maxSpread,
		Ticks:     w.GetTicks(),
	}, nil
}

// fromWireBars reconstructs bars in the order given — oldest-first,
// matching GetHistoryBarsResponse.bars' own documented ordering.
func fromWireBars(ws []*v1.Bar) ([]marketdata.Bar, error) {
	out := make([]marketdata.Bar, len(ws))
	for i, w := range ws {
		b, err := fromWireBar(w)
		if err != nil {
			return nil, fmt.Errorf("bar %d: %w", i, err)
		}
		out[i] = b
	}
	return out, nil
}

// fromWireBarEvent reconstructs a BarEvent from a received *v1.BarEvent.
func fromWireBarEvent(w *v1.BarEvent) (BarEvent, error) {
	if w == nil {
		return BarEvent{}, fmt.Errorf("%w: bar event must be set", ErrInvalidWireValue)
	}
	instID, err := parseInstrumentID(w.GetInstrumentId())
	if err != nil {
		return BarEvent{}, err
	}
	interval, err := fromWireInterval(w.GetInterval())
	if err != nil {
		return BarEvent{}, err
	}
	bar, err := fromWireBar(w.GetBar())
	if err != nil {
		return BarEvent{}, err
	}
	return BarEvent{Instrument: instID, Interval: interval, Bar: bar}, nil
}

// fromWirePositionSnapshot reconstructs a PositionSnapshot from a
// received *v1.PositionSnapshot.
func fromWirePositionSnapshot(w *v1.PositionSnapshot) (PositionSnapshot, error) {
	if w == nil {
		return PositionSnapshot{}, fmt.Errorf("%w: position snapshot must be set", ErrInvalidWireValue)
	}
	instID, err := parseInstrumentID(w.GetInstrumentId())
	if err != nil {
		return PositionSnapshot{}, err
	}
	side, err := fromWirePositionSide(w.GetSide())
	if err != nil {
		return PositionSnapshot{}, err
	}
	avgPrice, err := parseOptionalPrice("avg_price", w.GetAvgPrice())
	if err != nil {
		return PositionSnapshot{}, err
	}
	return PositionSnapshot{Instrument: instID, Side: side, AvgPrice: avgPrice}, nil
}

// fromWireAccountSnapshot reconstructs an AccountSnapshot from a
// received *v1.AccountSnapshot.
func fromWireAccountSnapshot(w *v1.AccountSnapshot) (AccountSnapshot, error) {
	if w == nil {
		return AccountSnapshot{}, fmt.Errorf("%w: account snapshot must be set", ErrInvalidWireValue)
	}
	positions := make([]PositionSnapshot, len(w.GetPositions()))
	for i, wp := range w.GetPositions() {
		p, err := fromWirePositionSnapshot(wp)
		if err != nil {
			return AccountSnapshot{}, fmt.Errorf("position %d: %w", i, err)
		}
		positions[i] = p
	}
	return AccountSnapshot{
		AccountID: w.GetAccountId(),
		Currency:  w.GetCurrency(),
		AsOf:      time.Unix(0, w.GetAsOfUnixNanos()).UTC(),
		Positions: positions,
	}, nil
}

// fromWireFillEvent reconstructs a FillEvent from a received
// *v1.FillEvent.
func fromWireFillEvent(w *v1.FillEvent) (FillEvent, error) {
	if w == nil {
		return FillEvent{}, fmt.Errorf("%w: fill event must be set", ErrInvalidWireValue)
	}
	instID, err := parseInstrumentID(w.GetInstrumentId())
	if err != nil {
		return FillEvent{}, err
	}
	side, err := fromWireSide(w.GetSide())
	if err != nil {
		return FillEvent{}, err
	}
	price, err := parsePrice("price", w.GetPrice())
	if err != nil {
		return FillEvent{}, err
	}
	quantity, err := parseQuantity("quantity", w.GetQuantity())
	if err != nil {
		return FillEvent{}, err
	}
	return FillEvent{
		OrderID:       w.GetOrderId(),
		Instrument:    instID,
		Side:          side,
		Price:         price,
		Quantity:      quantity,
		CorrelationID: w.GetCorrelationId(),
		CausationID:   w.GetCausationId(),
	}, nil
}

// toWireDataRequirement converts a DataRequirement to its v1 wire form.
func toWireDataRequirement(r DataRequirement) (*v1.DataRequirement, error) {
	if r.WarmupBars < 0 {
		return nil, fmt.Errorf("%w: warmup_bars must not be negative", ErrInvalidWireValue)
	}
	interval, err := toWireInterval(r.Interval)
	if err != nil {
		return nil, err
	}
	return &v1.DataRequirement{
		InstrumentId: r.Instrument.String(),
		Interval:     interval,
		WarmupBars:   int32(r.WarmupBars),
	}, nil
}

// toWireDescriptor converts a Descriptor to its v1 wire form, carried
// in HandshakeRequest.
func toWireDescriptor(d Descriptor) (*v1.StrategyDescriptor, error) {
	reqs := make([]*v1.DataRequirement, len(d.Requirements))
	for i, r := range d.Requirements {
		wr, err := toWireDataRequirement(r)
		if err != nil {
			return nil, fmt.Errorf("requirement %d: %w", i, err)
		}
		reqs[i] = wr
	}
	return &v1.StrategyDescriptor{
		Name:         d.Name,
		Version:      d.Version,
		Requirements: reqs,
	}, nil
}

// toWireDescribedIntent converts a DescribedIntent to its v1 wire
// form, sent in OnBarResponse.intents. It enforces exactly the
// per-Kind required/forbidden field contract order.NewIntent itself
// enforces host-side (order/intent.go's own "Field requirements are
// per Kind, not uniform" table), so a malformed DescribedIntent fails
// here, in the guest's own process, rather than crossing the wire
// only to be rejected by the host.
func toWireDescribedIntent(d DescribedIntent) (*v1.DescribedIntent, error) {
	requireSide := d.Kind == order.IntentEnter || d.Kind == order.IntentTargetExposure || d.Kind == order.IntentEnterWithStop
	requireQuantity := d.Kind == order.IntentTargetExposure
	requireStopPrice := d.Kind == order.IntentAdjustStop || d.Kind == order.IntentEnterWithStop

	if requireSide && d.Side == 0 {
		return nil, fmt.Errorf("%w: side is required for intent kind %v", ErrInvalidWireValue, d.Kind)
	}
	if !requireSide && d.Side != 0 {
		return nil, fmt.Errorf("%w: side must not be set for intent kind %v", ErrInvalidWireValue, d.Kind)
	}
	if requireQuantity && d.Quantity == nil {
		return nil, fmt.Errorf("%w: quantity is required for intent kind %v", ErrInvalidWireValue, d.Kind)
	}
	if !requireQuantity && d.Quantity != nil {
		return nil, fmt.Errorf("%w: quantity must not be set for intent kind %v", ErrInvalidWireValue, d.Kind)
	}
	if requireStopPrice && d.StopPrice == nil {
		return nil, fmt.Errorf("%w: stop_price is required for intent kind %v", ErrInvalidWireValue, d.Kind)
	}
	if !requireStopPrice && d.StopPrice != nil {
		return nil, fmt.Errorf("%w: stop_price must not be set for intent kind %v", ErrInvalidWireValue, d.Kind)
	}

	kind, err := toWireIntentKind(d.Kind)
	if err != nil {
		return nil, err
	}

	var side v1.Side
	if requireSide {
		side, err = toWireSide(d.Side)
		if err != nil {
			return nil, err
		}
	}

	if d.Instrument.IsZero() {
		return nil, fmt.Errorf("%w: instrument must be set", ErrInvalidWireValue)
	}

	return &v1.DescribedIntent{
		Kind:             kind,
		InstrumentId:     d.Instrument.String(),
		Side:             side,
		Quantity:         quantityOrEmpty(d.Quantity),
		StopPrice:        priceOrEmpty(d.StopPrice),
		CorrelationToken: d.CorrelationToken,
	}, nil
}

// toWireDescribedSignal converts a DescribedSignal to its v1 wire
// form, sent in OnBarResponse.signals.
func toWireDescribedSignal(s DescribedSignal) *v1.DescribedSignal {
	return &v1.DescribedSignal{
		Strategy:         s.Strategy,
		Values:           s.Values,
		CorrelationToken: s.CorrelationToken,
	}
}
