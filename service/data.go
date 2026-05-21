package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rustyeddy/trader"
	"github.com/rustyeddy/trader/brokers/oanda"
)

// DownloadOandaCandlesRequest parameterizes the candle download.
type DownloadOandaCandlesRequest struct {
	Instrument string    // OANDA format, e.g. "USD_JPY"
	Timeframe  string    // "M1", "H1", "D" — OANDA granularity
	From, To   time.Time // inclusive date range
	RawDir     string    // root for raw bid+ask preservation; "" = skip raw write

	// OnProgress (optional) is called once per month with a status line.
	// Useful for streaming output to a CLI or web UI.
	OnProgress func(line string)
}

// DownloadOandaCandlesResult summarizes one download run.
type DownloadOandaCandlesResult struct {
	MonthsProcessed int
	CandlesWritten  int
}

// DownloadOandaCandles fetches OANDA candles month-by-month, converts to
// the trader canonical (bid OHLC + computed spread) format, writes them
// to the store under source=oanda, and optionally preserves the raw
// bid+ask OHLC alongside.
//
// One CSV file is written per month per timeframe under
// <storeBaseDir>/oanda/<INSTR>/<YEAR>/<MM>/<INSTR>-<YEAR>-<MM>-<tf>.csv
// (matching the trader engine's expected layout).
func (s *Service) DownloadOandaCandles(ctx context.Context, req DownloadOandaCandlesRequest) (*DownloadOandaCandlesResult, error) {
	if req.Instrument == "" {
		return nil, fmt.Errorf("missing Instrument")
	}
	if req.Timeframe == "" {
		return nil, fmt.Errorf("missing Timeframe")
	}
	if req.From.IsZero() || req.To.IsZero() {
		return nil, fmt.Errorf("From and To are required")
	}
	if !req.From.Before(req.To) {
		return nil, fmt.Errorf("From must be before To")
	}

	tf, err := parseTraderTimeframe(req.Timeframe)
	if err != nil {
		return nil, err
	}
	tfStr := strings.ToUpper(strings.TrimSpace(req.Timeframe))

	store := trader.GetStore()
	instrTrader := strings.ReplaceAll(req.Instrument, "_", "")

	cursor := time.Date(req.From.Year(), req.From.Month(), 1, 0, 0, 0, 0, time.UTC)
	endMonth := time.Date(req.To.Year(), req.To.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 1, 0)

	result := &DownloadOandaCandlesResult{}

	for cursor.Before(endMonth) {
		monthStart := cursor
		monthEnd := cursor.AddDate(0, 1, 0)
		if monthStart.Before(req.From) {
			monthStart = req.From
		}
		if monthEnd.After(req.To.AddDate(0, 0, 1)) {
			monthEnd = req.To.AddDate(0, 0, 1)
		}

		candles, err := s.OANDA.FetchCandles(ctx, oanda.FetchCandlesOptions{
			Instrument:  req.Instrument,
			Granularity: tfStr,
			From:        monthStart,
			To:          monthEnd,
		})
		if err != nil {
			return result, fmt.Errorf("fetch %s: %w", monthStart.Format("2006-01"), err)
		}

		monthSlotStart := time.Date(cursor.Year(), cursor.Month(), 1, 0, 0, 0, 0, time.UTC)
		stepSec := int64(tf)
		slotCount := int(monthSlotStart.AddDate(0, 1, 0).Sub(monthSlotStart).Seconds() / float64(stepSec))

		traderCandles := make([]trader.Candle, slotCount)
		nonZero := 0
		for _, oc := range candles {
			if oc.BidClose == 0 && oc.AskClose == 0 {
				continue
			}
			idx := int(oc.Time.Unix()-monthSlotStart.Unix()) / int(stepSec)
			if idx < 0 || idx >= slotCount {
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
			traderCandles[idx] = trader.Candle{
				Open:      trader.PriceFromFloat(oc.BidOpen),
				High:      trader.PriceFromFloat(oc.BidHigh),
				Low:       trader.PriceFromFloat(oc.BidLow),
				Close:     trader.PriceFromFloat(oc.BidClose),
				AvgSpread: trader.PriceFromFloat(sum / 4),
				MaxSpread: trader.PriceFromFloat(max),
				Ticks:     int32(oc.Volume),
			}
			nonZero++
		}

		if err := store.WriteMonthlyCandles("oanda", instrTrader, tf, monthSlotStart, traderCandles); err != nil {
			return result, fmt.Errorf("write %s: %w", monthSlotStart.Format("2006-01"), err)
		}

		if req.RawDir != "" {
			if err := writeRawOandaMonth(req.RawDir, instrTrader, tfStr, monthSlotStart, candles); err != nil {
				return result, fmt.Errorf("write raw %s: %w", monthSlotStart.Format("2006-01"), err)
			}
		}

		result.MonthsProcessed++
		result.CandlesWritten += nonZero
		if req.OnProgress != nil {
			req.OnProgress(fmt.Sprintf("fetched %s %s %s..%s → %d candles",
				req.Instrument, tfStr,
				monthStart.Format("2006-01-02"), monthEnd.Format("2006-01-02"),
				nonZero,
			))
		}

		cursor = cursor.AddDate(0, 1, 0)
	}

	return result, nil
}

// parseTraderTimeframe maps an OANDA timeframe string to a trader.Timeframe.
func parseTraderTimeframe(s string) (trader.Timeframe, error) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "M1":
		return trader.M1, nil
	case "H1":
		return trader.H1, nil
	case "D1", "D":
		return trader.D1, nil
	default:
		return 0, fmt.Errorf("unsupported timeframe %q (use M1, H1, D1)", s)
	}
}

// writeRawOandaMonth preserves the bid+ask OHLC exactly as OANDA returned it.
// Path: <rawDir>/oanda/<INSTR>/<YEAR>/<MM>/<INSTR>-<YEAR>-<MM>-<tf>.csv
func writeRawOandaMonth(rawDir, instrument, tfStr string, monthStart time.Time, candles []oanda.Candle) error {
	dir := filepath.Join(rawDir, "oanda", instrument,
		fmt.Sprintf("%04d", monthStart.Year()),
		fmt.Sprintf("%02d", int(monthStart.Month())))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	filename := fmt.Sprintf("%s-%04d-%02d-%s.csv",
		instrument, monthStart.Year(), int(monthStart.Month()), strings.ToLower(tfStr))
	path := filepath.Join(dir, filename)

	monthEnd := monthStart.AddDate(0, 1, 0)

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := fmt.Fprintf(f, "# schema=raw-v1 source=oanda instrument=%s tf=%s year=%d month=%02d\n",
		instrument, strings.ToLower(tfStr), monthStart.Year(), int(monthStart.Month())); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(f, "time,bid_o,bid_h,bid_l,bid_c,ask_o,ask_h,ask_l,ask_c,volume,complete"); err != nil {
		return err
	}

	for _, oc := range candles {
		if !oc.Time.Before(monthEnd) || oc.Time.Before(monthStart) {
			continue
		}
		if _, err := fmt.Fprintf(f,
			"%s,%.5f,%.5f,%.5f,%.5f,%.5f,%.5f,%.5f,%.5f,%d,%t\n",
			oc.Time.UTC().Format(time.RFC3339),
			oc.BidOpen, oc.BidHigh, oc.BidLow, oc.BidClose,
			oc.AskOpen, oc.AskHigh, oc.AskLow, oc.AskClose,
			oc.Volume, oc.Complete,
		); err != nil {
			return err
		}
	}
	return nil
}
