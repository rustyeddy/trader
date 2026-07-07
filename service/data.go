package service

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rustyeddy/trader/brokers/oanda"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/marketdata"
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
	tfStr := toOandaGranularity(req.Timeframe)

	store := marketdata.GetStore()
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

		traderCandles := make([]market.Candle, slotCount)
		nonZero := 0
		for _, oc := range candles {
			if oc.BidClose == 0 && oc.AskClose == 0 {
				continue
			}
			if oc.Time.Before(monthSlotStart) {
				// Guard against Go's integer division truncating toward zero:
				// a candle timestamped a few hours before the month boundary
				// (e.g. OANDA's daily candles open at 21:00 UTC the previous
				// day) has a small negative delta that truncates to index 0
				// instead of flooring to -1, silently duplicating it into
				// this month's slot 0 unless excluded here first.
				continue
			}
			idx := int(oc.Time.Unix()-monthSlotStart.Unix()) / int(stepSec)
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
			traderCandles[idx] = market.Candle{
				Open:      market.PriceFromFloat(oc.BidOpen),
				High:      market.PriceFromFloat(oc.BidHigh),
				Low:       market.PriceFromFloat(oc.BidLow),
				Close:     market.PriceFromFloat(oc.BidClose),
				AvgSpread: market.PriceFromFloat(sum / 4),
				MaxSpread: market.PriceFromFloat(max),
				Ticks:     int32(oc.Volume),
			}
			nonZero++
		}

		if err := store.WriteMonthlyCandles(market.SourceOanda, instrTrader, tf, monthSlotStart, traderCandles); err != nil {
			return result, fmt.Errorf("write %s: %w", monthSlotStart.Format("2006-01"), err)
		}

		if req.RawDir != "" {
			rawKey := marketdata.Key{
				Kind:       marketdata.KindCandle,
				Source:     market.SourceOanda,
				Instrument: instrTrader,
				TF:         tf,
				Year:       cursor.Year(),
				Month:      int(cursor.Month()),
			}
			if err := writeRawOandaMonth(store, rawKey, monthSlotStart, candles); err != nil {
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

// parseTraderTimeframe maps an OANDA timeframe string to a market.Timeframe.
func parseTraderTimeframe(s string) (market.Timeframe, error) {
	tf, err := market.ParseTimeframe(s)
	if err != nil {
		return 0, fmt.Errorf("unsupported timeframe %q (use M1, H1, D1)", s)
	}
	return tf, nil
}

// toOandaGranularity converts a trader timeframe string to the OANDA API
// granularity value. OANDA uses "D" not "D1".
func toOandaGranularity(s string) string {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "D1", "D":
		return "D"
	default:
		return strings.ToUpper(strings.TrimSpace(s))
	}
}

// writeRawOandaMonth preserves the bid+ask OHLC exactly as OANDA returned it.
// The path is determined by the store so ownership of file placement stays centralised.
func writeRawOandaMonth(s *marketdata.Store, key marketdata.Key, monthStart time.Time, candles []oanda.Candle) error {
	path, err := s.RawCandlePath(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	monthEnd := monthStart.AddDate(0, 1, 0)

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	tf := strings.ToLower(key.TF.String())
	if _, err := fmt.Fprintf(f, "# schema=raw-v1 source=oanda instrument=%s tf=%s year=%d month=%02d\n",
		key.Instrument, tf, key.Year, key.Month); err != nil {
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

// ── derive canonical from raw ─────────────────────────────────────────────────

// DeriveResult is returned by DeriveCanonicalFromRaw.
type DeriveResult struct {
	CandlesWritten int
	// MissingSlots is the count of market-hours slots the raw file had no data for.
	MissingSlots int
	// SampleMissing holds up to 10 RFC3339 timestamps of missing market-hours slots.
	SampleMissing []string
}

// DeriveCanonicalFromRaw reads a raw OANDA month CSV (bid+ask OHLC) from
// rawPath and writes the canonical candle CSV for key into the store.
// It also checks every expected market-hours slot and reports any that the raw
// file did not contain, so gaps can be surfaced immediately rather than found
// by a follow-up validate pass.
func (s *Service) DeriveCanonicalFromRaw(ctx context.Context, rawPath string, key marketdata.Key) (*DeriveResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	tf := key.TF
	monthStart := time.Date(key.Year, time.Month(key.Month), 1, 0, 0, 0, 0, time.UTC)
	monthEnd := monthStart.AddDate(0, 1, 0)
	stepSec := int64(tf)
	slotCount := int(monthEnd.Sub(monthStart).Seconds() / float64(stepSec))

	candles := make([]market.Candle, slotCount)
	filled := make([]bool, slotCount)

	f, err := os.Open(rawPath)
	if err != nil {
		return nil, fmt.Errorf("open raw %s: %w", rawPath, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, ",")
		if len(fields) < 11 {
			continue
		}
		if strings.ToLower(strings.TrimSpace(fields[0])) == "time" {
			continue
		}

		ts, err := time.Parse(time.RFC3339, strings.TrimSpace(fields[0]))
		if err != nil {
			continue
		}
		ts = ts.UTC()
		if ts.Before(monthStart) || !ts.Before(monthEnd) {
			continue
		}

		complete, err := strconv.ParseBool(strings.TrimSpace(fields[10]))
		if err != nil || !complete {
			continue
		}

		parseF := func(s string) float64 {
			v, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
			return v
		}
		bidO := parseF(fields[1])
		bidH := parseF(fields[2])
		bidL := parseF(fields[3])
		bidC := parseF(fields[4])
		askO := parseF(fields[5])
		askH := parseF(fields[6])
		askL := parseF(fields[7])
		askC := parseF(fields[8])
		vol, _ := strconv.ParseInt(strings.TrimSpace(fields[9]), 10, 64)

		if bidC == 0 && askC == 0 {
			continue
		}

		idx := int((ts.Unix() - monthStart.Unix()) / stepSec)
		if idx < 0 || idx >= slotCount {
			continue
		}

		spreads := [4]float64{askO - bidO, askH - bidH, askL - bidL, askC - bidC}
		var sumSpread, maxSpread float64
		for _, sp := range spreads {
			sumSpread += sp
			if sp > maxSpread {
				maxSpread = sp
			}
		}
		candles[idx] = market.Candle{
			Open:      market.PriceFromFloat(bidO),
			High:      market.PriceFromFloat(bidH),
			Low:       market.PriceFromFloat(bidL),
			Close:     market.PriceFromFloat(bidC),
			AvgSpread: market.PriceFromFloat(sumSpread / 4),
			MaxSpread: market.PriceFromFloat(maxSpread),
			Ticks:     int32(vol),
		}
		filled[idx] = true
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan raw %s: %w", rawPath, err)
	}

	// Check every expected market-hours slot for gaps in the raw file.
	result := &DeriveResult{}
	step := time.Duration(stepSec) * time.Second
	for i := 0; i < slotCount; i++ {
		slotStart := monthStart.Add(time.Duration(i) * step)
		if !marketdata.SlotMayHaveForexData(slotStart, slotStart.Add(step)) {
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

	store := marketdata.GetStore()
	if err := store.WriteMonthlyCandles(key.Source, key.Instrument, tf, monthStart, candles); err != nil {
		return nil, fmt.Errorf("write canonical %s: %w", rawPath, err)
	}
	return result, nil
}

// ── update (catch-up download) ────────────────────────────────────────────────

// UpdateOandaCandlesRequest specifies a catch-up download for one or more
// instruments. From is auto-detected from the store when zero.
type UpdateOandaCandlesRequest struct {
	Instruments []string // OANDA format, e.g. ["EUR_USD", "GBP_USD"]
	Timeframes  []string // e.g. ["M1", "H1", "H4", "D"]
	// To defaults to yesterday (last complete UTC day) when zero.
	To time.Time
	// SeedFrom is used as the start date when no prior data exists for a
	// pair (instead of erroring). Zero means error on missing baseline.
	SeedFrom time.Time
	RawDir   string
	// OnProgress is called after each instrument+timeframe completes.
	OnProgress func(msg string)
}

// UpdateOandaCandlesResult summarises one update run.
type UpdateOandaCandlesResult struct {
	// Results is keyed by "INSTRUMENT/TIMEFRAME", e.g. "EUR_USD/H1".
	Results map[string]UpdateItemResult
}

// UpdateItemResult is the outcome for one instrument+timeframe pair.
type UpdateItemResult struct {
	From           time.Time
	To             time.Time
	CandlesWritten int
	Err            error
}

// UpdateOandaCandles downloads candles for every instrument+timeframe pair,
// starting from the day after the last non-zero candle already on disk.
// When no existing data is found for a pair it returns an error for that pair
// (rather than downloading everything since 2000) so the caller can decide
// whether to seed with a full download first.
func (s *Service) UpdateOandaCandles(ctx context.Context, req UpdateOandaCandlesRequest) (*UpdateOandaCandlesResult, error) {
	if len(req.Instruments) == 0 {
		return nil, fmt.Errorf("update: at least one instrument required")
	}
	if len(req.Timeframes) == 0 {
		return nil, fmt.Errorf("update: at least one timeframe required")
	}

	to := req.To
	if to.IsZero() {
		// Yesterday — last fully-completed UTC day.
		now := time.Now().UTC()
		to = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -1)
	}

	result := &UpdateOandaCandlesResult{
		Results: make(map[string]UpdateItemResult),
	}

	for _, inst := range req.Instruments {
		for _, tf := range req.Timeframes {
			key := inst + "/" + tf
			from, err := lastNonZeroCandleDate(marketdata.GetStore(), inst, tf)
			if err != nil {
				if !req.SeedFrom.IsZero() {
					from = req.SeedFrom
				} else {
					result.Results[key] = UpdateItemResult{Err: fmt.Errorf("detect last candle: %w", err)}
					if req.OnProgress != nil {
						req.OnProgress(fmt.Sprintf("%-12s %-4s  ERROR: %v (use --from YYYY-MM-DD to seed)", inst, tf, err))
					}
					continue
				}
			} else {
				from = from.AddDate(0, 0, 1) // day after last complete candle
			}
			if !from.Before(to) {
				result.Results[key] = UpdateItemResult{From: from, To: to, CandlesWritten: 0}
				if req.OnProgress != nil {
					req.OnProgress(fmt.Sprintf("%-12s %-4s  already up to date (%s)", inst, tf, to.Format("2006-01-02")))
				}
				continue
			}

			dl, err := s.DownloadOandaCandles(ctx, DownloadOandaCandlesRequest{
				Instrument: inst,
				Timeframe:  tf,
				From:       from,
				To:         to,
				RawDir:     req.RawDir,
				OnProgress: func(line string) {
					if req.OnProgress != nil {
						req.OnProgress("  " + line)
					}
				},
			})
			itemErr := err
			written := 0
			if dl != nil {
				written = dl.CandlesWritten
			}
			result.Results[key] = UpdateItemResult{From: from, To: to, CandlesWritten: written, Err: itemErr}
			if req.OnProgress != nil {
				if itemErr != nil {
					req.OnProgress(fmt.Sprintf("%-12s %-4s  ERROR: %v", inst, tf, itemErr))
				} else {
					req.OnProgress(fmt.Sprintf("%-12s %-4s  %s → %s  %d candles",
						inst, tf, from.Format("2006-01-02"), to.Format("2006-01-02"), written))
				}
			}
		}
	}
	return result, nil
}

// lastNonZeroCandleDate returns the date of the last candle with actual price
// data in the most recent monthly CSV file for the given instrument+timeframe.
// It returns an error if no candle files exist yet.
func lastNonZeroCandleDate(store *marketdata.Store, instrument, timeframe string) (time.Time, error) {
	tf, err := market.ParseTimeframe(timeframe)
	if err != nil {
		return time.Time{}, fmt.Errorf("unknown timeframe %q", timeframe)
	}
	instrTrader := strings.ReplaceAll(instrument, "_", "")

	// Find the latest monthly file that exists on disk.
	now := time.Now().UTC()
	var latestPath string
	for year := now.Year(); year >= 2010; year-- {
		startMonth := 12
		if year == now.Year() {
			startMonth = int(now.Month())
		}
		for month := startMonth; month >= 1; month-- {
			k := marketdata.Key{
				Kind:       marketdata.KindCandle,
				Source:     market.SourceOanda,
				Instrument: instrTrader,
				TF:         tf,
				Year:       year,
				Month:      month,
			}
			path := store.PathForMonthlyCandle(k)
			if _, err := os.Stat(path); err == nil {
				latestPath = path
				goto found
			}
		}
	}
	return time.Time{}, fmt.Errorf("no candle files found for %s/%s", instrument, timeframe)

found:
	date, err := lastNonZeroDate(latestPath)
	if err != nil {
		return time.Time{}, err
	}
	return date, nil
}

// lastNonZeroDate reads a candle CSV and returns the UTC day (00:00:00)
// of the latest row that has real candle data (flags bit 0x0001 set).
func lastNonZeroDate(path string) (time.Time, error) {
	f, err := os.Open(path)
	if err != nil {
		return time.Time{}, err
	}
	defer f.Close()

	var lastDate time.Time
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		lower := strings.ToLower(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(lower, "timestamp,") || strings.HasPrefix(lower, "time,") {
			continue
		}
		fields := strings.Split(line, ",")
		// CSV format: Timestamp,High,Open,Low,Close,avgspread,maxspread,ticks,flags
		if len(fields) < 9 {
			continue
		}
		ts, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			continue
		}
		flags, err := strconv.ParseUint(strings.TrimSpace(fields[8]), 0, 64)
		if err != nil {
			continue
		}
		if flags&0x0001 == 0 {
			continue // zero/empty candle slot
		}
		t := time.Unix(ts, 0).UTC()
		t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
		if t.After(lastDate) {
			lastDate = t
		}
	}
	if err := scanner.Err(); err != nil {
		return time.Time{}, fmt.Errorf("scan %s: %w", path, err)
	}
	if lastDate.IsZero() {
		return time.Time{}, fmt.Errorf("no non-zero candles in %s", path)
	}
	return lastDate, nil
}
