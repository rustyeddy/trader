package datamanager

import (
	"context"
	"fmt"
	"time"

	"github.com/rustyeddy/trader/market"
)

// CandleSyncResult summarizes a FetchCandleMonths run.
type CandleSyncResult struct {
	MonthsProcessed int
	CandlesWritten  int
}

// CandleSyncProgress is reported once per month during FetchCandleMonths.
type CandleSyncProgress struct {
	Instrument     string
	Timeframe      market.Timeframe
	MonthStart     time.Time
	MonthEnd       time.Time
	CandlesWritten int
}

// FetchCandleMonths fetches and writes canonical (and optionally raw)
// candles for instrument/tf from a candle-native provider (e.g. OANDA) for
// every calendar month overlapping [from,to), unconditionally overwriting
// each month. This is DataManager's single acquisition path for
// candle-native sources — the sole place OANDA candle downloads happen.
//
// instrument is passed through to the provider as-is (OANDA wire format,
// e.g. "EUR_USD"); it is normalized (underscores stripped) for store keys.
func (dm *DataManager) FetchCandleMonths(ctx context.Context, provider CandleProvider, instrument string, tf market.Timeframe, from, to time.Time, rawDir string, onProgress func(CandleSyncProgress)) (*CandleSyncResult, error) {
	if provider == nil {
		return nil, fmt.Errorf("nil candle provider")
	}
	if instrument == "" {
		return nil, fmt.Errorf("missing instrument")
	}
	if tf <= 0 {
		return nil, fmt.Errorf("invalid timeframe %v", tf)
	}
	if from.IsZero() || to.IsZero() {
		return nil, fmt.Errorf("from and to are required")
	}
	if !from.Before(to) {
		return nil, fmt.Errorf("from must be before to")
	}

	normInst := market.NormalizeInstrument(instrument)
	source := provider.Name()

	cursor := time.Date(from.Year(), from.Month(), 1, 0, 0, 0, 0, time.UTC)
	endMonth := time.Date(to.Year(), to.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 1, 0)

	result := &CandleSyncResult{}
	for cursor.Before(endMonth) {
		if err := ctx.Err(); err != nil {
			return result, err
		}

		monthStart := cursor
		monthEnd := cursor.AddDate(0, 1, 0)

		month, err := provider.FetchCandleMonth(ctx, instrument, tf, monthStart)
		if err != nil {
			return result, fmt.Errorf("fetch %s: %w", monthStart.Format("2006-01"), err)
		}

		nonZero := 0
		for i := range month.Candles {
			if !month.Candles[i].IsZero() {
				nonZero++
			}
		}

		if err := getStore().WriteMonthlyCandles(source, normInst, tf, monthStart, month.Candles); err != nil {
			return result, fmt.Errorf("write %s: %w", monthStart.Format("2006-01"), err)
		}

		if rawDir != "" && len(month.Raw) > 0 {
			key := Key{
				Kind:       KindCandle,
				Source:     source,
				Instrument: normInst,
				TF:         tf,
				Year:       cursor.Year(),
				Month:      int(cursor.Month()),
			}
			if err := writeRawMonth(rawDir, key, monthStart, month.Raw); err != nil {
				return result, fmt.Errorf("write raw %s: %w", monthStart.Format("2006-01"), err)
			}
		}

		result.MonthsProcessed++
		result.CandlesWritten += nonZero
		if onProgress != nil {
			onProgress(CandleSyncProgress{
				Instrument:     normInst,
				Timeframe:      tf,
				MonthStart:     monthStart,
				MonthEnd:       monthEnd,
				CandlesWritten: nonZero,
			})
		}

		cursor = cursor.AddDate(0, 1, 0)
	}

	return result, nil
}

// LastCompleteDate returns the UTC calendar day of the most recent candle
// with real (non-gap) data for instrument/tf/source, by walking backward
// from the current month to find the newest month file that exists on disk
// and reading its last valid slot. It errors if no candle file exists at
// all, or if the newest existing file has no valid candles.
//
// This is the single gap-analysis/resume-cursor implementation for
// candle-native sources — callers (e.g. an update/catch-up download) use it
// instead of each re-implementing their own "what's the last good date"
// logic.
func (dm *DataManager) LastCompleteDate(instrument string, tf market.Timeframe, source string) (time.Time, error) {
	inst := market.NormalizeInstrument(instrument)
	source = normalizeSource(source)
	if source == "" {
		source = market.SourceOanda
	}

	s := getStore()
	now := time.Now().UTC()

	var found bool
	var key Key
	for year := now.Year(); year >= 2010 && !found; year-- {
		startMonth := 12
		if year == now.Year() {
			startMonth = int(now.Month())
		}
		for month := startMonth; month >= 1; month-- {
			k := Key{Instrument: inst, Source: source, Kind: KindCandle, TF: tf, Year: year, Month: month}
			exists, err := s.Exists(k)
			if err != nil {
				return time.Time{}, err
			}
			if exists {
				key = k
				found = true
				break
			}
		}
	}
	if !found {
		return time.Time{}, fmt.Errorf("no candle files found for %s/%s", instrument, tf)
	}

	cs, err := s.ReadCSV(key)
	if err != nil {
		return time.Time{}, err
	}

	t, ok := cs.LastValidTime()
	if !ok {
		return time.Time{}, fmt.Errorf("no non-zero candles in %s/%s %04d-%02d", instrument, tf, key.Year, key.Month)
	}
	return t, nil
}

// DeriveResult is returned by DeriveCanonicalFromRaw.
type DeriveResult struct {
	CandlesWritten int
	// MissingSlots is the count of market-hours slots the raw file had no data for.
	MissingSlots int
	// SampleMissing holds up to 10 RFC3339 timestamps of missing market-hours slots.
	SampleMissing []string
}

// DeriveCanonicalFromRaw reads a raw candle month CSV (bid+ask OHLC,
// written by FetchCandleMonths or an earlier download) from rawPath and
// (re)writes the canonical candle CSV for key. It also reports every
// expected market-hours slot the raw file had no data for, so gaps surface
// immediately rather than requiring a follow-up validate pass.
func (dm *DataManager) DeriveCanonicalFromRaw(ctx context.Context, rawPath string, key Key) (*DeriveResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	tf := key.TF
	monthStart := time.Date(key.Year, time.Month(key.Month), 1, 0, 0, 0, 0, time.UTC)
	monthEnd := monthStart.AddDate(0, 1, 0)
	stepSec := int64(tf)
	slotCount := int(monthEnd.Sub(monthStart).Seconds() / float64(stepSec))

	rows, err := readRawMonthRows(rawPath, monthStart, monthEnd)
	if err != nil {
		return nil, fmt.Errorf("read raw %s: %w", rawPath, err)
	}

	candles := make([]market.Candle, slotCount)
	filled := make([]bool, slotCount)

	for _, r := range rows {
		if !r.Complete {
			continue
		}
		if r.BidClose == 0 && r.AskClose == 0 {
			continue
		}

		idx := int((r.Time.Unix() - monthStart.Unix()) / stepSec)
		if idx < 0 || idx >= slotCount {
			continue
		}

		spreads := [4]float64{
			r.AskOpen - r.BidOpen,
			r.AskHigh - r.BidHigh,
			r.AskLow - r.BidLow,
			r.AskClose - r.BidClose,
		}
		var sumSpread, maxSpread float64
		for _, sp := range spreads {
			sumSpread += sp
			if sp > maxSpread {
				maxSpread = sp
			}
		}
		candles[idx] = market.Candle{
			Open:      market.PriceFromFloat(r.BidOpen),
			High:      market.PriceFromFloat(r.BidHigh),
			Low:       market.PriceFromFloat(r.BidLow),
			Close:     market.PriceFromFloat(r.BidClose),
			AvgSpread: market.PriceFromFloat(sumSpread / 4),
			MaxSpread: market.PriceFromFloat(maxSpread),
			Ticks:     int32(r.Volume),
		}
		filled[idx] = true
	}

	result := &DeriveResult{}
	step := time.Duration(stepSec) * time.Second
	for i := 0; i < slotCount; i++ {
		slotStart := monthStart.Add(time.Duration(i) * step)
		if !SlotMayHaveForexData(slotStart, slotStart.Add(step)) {
			continue
		}
		if filled[i] {
			result.CandlesWritten++
		} else {
			result.MissingSlots++
			if len(result.SampleMissing) < 10 {
				result.SampleMissing = append(result.SampleMissing, slotStart.UTC().Format(time.RFC3339))
			}
		}
	}

	if err := getStore().WriteMonthlyCandles(key.Source, key.Instrument, tf, monthStart, candles); err != nil {
		return nil, fmt.Errorf("write canonical %s: %w", rawPath, err)
	}
	return result, nil
}
