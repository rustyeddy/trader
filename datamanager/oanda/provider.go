// Package oanda implements the datamanager.CandleProvider interface for the
// OANDA REST candle API. Unlike datamanager/dukascopy, it fetches finished
// candles directly rather than raw ticks for local aggregation, and it
// needs runtime credentials — so it is not self-registered from init() the
// way Dukascopy is. Callers construct one with New and pass it explicitly
// to DataManager sync calls.
package oanda

import (
	"context"
	"fmt"
	"strings"
	"time"

	oandaclient "github.com/rustyeddy/trader/brokers/oanda"
	"github.com/rustyeddy/trader/datamanager"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
)

// SourceName is the canonical name under which this provider identifies
// itself, matching market.SourceOanda.
const SourceName = market.SourceOanda

// Provider fetches finished candles directly from the OANDA REST API.
type Provider struct {
	client *oandaclient.Client
}

// New returns an OANDA CandleProvider backed by client.
func New(client *oandaclient.Client) *Provider {
	return &Provider{client: client}
}

func (p *Provider) Name() string { return SourceName }

// FetchCandleMonth fetches one calendar month of candles for instrument
// (OANDA wire format, e.g. "EUR_USD") at the given timeframe, converting to
// the trader canonical (bid OHLC + computed spread) representation while
// also preserving the raw bid+ask rows for optional archival.
func (p *Provider) FetchCandleMonth(ctx context.Context, instrument string, tf types.Timeframe, monthStart time.Time) (*datamanager.CandleMonth, error) {
	if p.client == nil {
		return nil, fmt.Errorf("oanda candle provider: not configured")
	}

	monthStart = monthStart.UTC()
	monthEnd := monthStart.AddDate(0, 1, 0)

	raw, err := p.client.FetchCandles(ctx, oandaclient.FetchCandlesOptions{
		Instrument:  instrument,
		Granularity: toOandaGranularity(tf),
		From:        monthStart,
		To:          monthEnd,
	})
	if err != nil {
		return nil, fmt.Errorf("fetch %s %s: %w", instrument, monthStart.Format("2006-01"), err)
	}

	stepSec := int64(tf)
	slotCount := int(monthEnd.Sub(monthStart).Seconds() / float64(stepSec))
	candles := make([]market.CandleTime, slotCount)
	rows := make([]datamanager.RawCandleRow, 0, len(raw))

	for _, oc := range raw {
		rows = append(rows, datamanager.RawCandleRow{
			Time:     oc.Time,
			BidOpen:  oc.BidOpen,
			BidHigh:  oc.BidHigh,
			BidLow:   oc.BidLow,
			BidClose: oc.BidClose,
			AskOpen:  oc.AskOpen,
			AskHigh:  oc.AskHigh,
			AskLow:   oc.AskLow,
			AskClose: oc.AskClose,
			Volume:   oc.Volume,
			Complete: oc.Complete,
		})

		if oc.BidClose == 0 && oc.AskClose == 0 {
			continue
		}
		if oc.Time.Before(monthStart) {
			// Guard against Go's integer division truncating toward zero: a
			// candle timestamped a few hours before the month boundary (e.g.
			// OANDA's daily candles open at 21:00 UTC the previous day) has a
			// small negative delta that truncates to index 0 instead of
			// flooring to -1, silently duplicating it into this month's slot 0.
			continue
		}
		idx := int(oc.Time.Unix()-monthStart.Unix()) / int(stepSec)
		if idx >= slotCount {
			continue
		}

		spreads := [4]float64{
			oc.AskOpen - oc.BidOpen,
			oc.AskHigh - oc.BidHigh,
			oc.AskLow - oc.BidLow,
			oc.AskClose - oc.BidClose,
		}
		var sum, max float64
		for _, sp := range spreads {
			sum += sp
			if sp > max {
				max = sp
			}
		}
		candles[idx] = market.CandleTime{
			Candle: market.Candle{
				Open:      types.PriceFromFloat(oc.BidOpen),
				High:      types.PriceFromFloat(oc.BidHigh),
				Low:       types.PriceFromFloat(oc.BidLow),
				Close:     types.PriceFromFloat(oc.BidClose),
				AvgSpread: types.PriceFromFloat(sum / 4),
				MaxSpread: types.PriceFromFloat(max),
				Ticks:     int32(oc.Volume),
			},
			Timestamp: types.FromTime(oc.Time.UTC()),
		}
	}

	return &datamanager.CandleMonth{Candles: candles, Raw: rows}, nil
}

// toOandaGranularity converts a trader timeframe to the OANDA API
// granularity value. OANDA uses "D" not "D1".
func toOandaGranularity(tf types.Timeframe) string {
	if tf == types.D1 {
		return "D"
	}
	return strings.ToUpper(tf.String())
}
