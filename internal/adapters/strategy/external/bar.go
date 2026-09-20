package external

import (
	"fmt"

	"github.com/rustyeddy/trader/internal/account"
	"github.com/rustyeddy/trader/internal/strategy"
	"github.com/rustyeddy/trader/marketdata"
	v1 "github.com/rustyeddy/trader/protocol/strategy/v1"
)

// ToWireBar converts a canonical marketdata.Bar to its v1 wire form.
// b.Time is carried verbatim as UTC nanoseconds since the Unix epoch
// (marketdata.Bar's own documented invariant against reconstructing
// time from array position or fixed-duration arithmetic — this
// preserves that by never reconstructing it either).
func ToWireBar(b marketdata.Bar) *v1.Bar {
	return &v1.Bar{
		TimeUnixNanos: b.Time.UTC().UnixNano(),
		Open:          b.Open.String(),
		High:          b.High.String(),
		Low:           b.Low.String(),
		Close:         b.Close.String(),
		AvgSpread:     b.AvgSpread.String(),
		MaxSpread:     b.MaxSpread.String(),
		Ticks:         b.Ticks,
	}
}

// ToWireBars converts bars in order, oldest-first as given — the
// GetHistoryBarsResponse.bars ordering the wire schema itself
// documents.
func ToWireBars(bars []marketdata.Bar) []*v1.Bar {
	out := make([]*v1.Bar, len(bars))
	for i, b := range bars {
		out[i] = ToWireBar(b)
	}
	return out
}

// ToWireBarEvent builds the *v1.BarEvent the host writes down the Run
// stream for one strategy.BarEvent, at sequence, carrying acct as the
// inline account snapshot ADR-062's View design requires — the host
// must have already computed acct as of the identical no-lookahead
// cutoff its own frozen View enforces for an in-process strategy
// before calling this.
func ToWireBarEvent(sequence uint64, event strategy.BarEvent, acct account.Snapshot) (*v1.BarEvent, error) {
	interval, err := ToWireInterval(event.Interval)
	if err != nil {
		return nil, fmt.Errorf("external: bar event: %w", err)
	}
	wireAcct, err := ToWireAccountSnapshot(acct)
	if err != nil {
		return nil, fmt.Errorf("external: bar event: %w", err)
	}
	return &v1.BarEvent{
		Sequence:     sequence,
		InstrumentId: event.Instrument.String(),
		Interval:     interval,
		Bar:          ToWireBar(event.Bar),
		Account:      wireAcct,
	}, nil
}
