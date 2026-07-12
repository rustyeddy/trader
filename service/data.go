package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rustyeddy/trader/datamanager"
	oandaprovider "github.com/rustyeddy/trader/datamanager/oanda"
	"github.com/rustyeddy/trader/market"
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
//
// All acquisition, gap-analysis, and raw/canonical writes are delegated to
// DataManager via the OANDA CandleProvider — this method just adapts the
// request/result shapes and progress-line formatting for CLI/REST/MCP
// callers.
func (s *Service) DownloadOandaCandles(ctx context.Context, req DownloadOandaCandlesRequest) (*DownloadOandaCandlesResult, error) {
	if req.Instrument == "" {
		return nil, fmt.Errorf("missing Instrument")
	}
	if req.Timeframe == "" {
		return nil, fmt.Errorf("missing Timeframe")
	}
	if req.From.IsZero() || req.To.IsZero() {
		return nil, fmt.Errorf("from and to are required")
	}
	if !req.From.Before(req.To) {
		return nil, fmt.Errorf("from must be before to")
	}

	tf, err := parseTraderTimeframe(req.Timeframe)
	if err != nil {
		return nil, err
	}

	provider := oandaprovider.New(s.OANDA)
	dm := datamanager.GetDataManager()

	syncResult, err := dm.FetchCandleMonths(ctx, provider, req.Instrument, tf, req.From, req.To, req.RawDir,
		func(p datamanager.CandleSyncProgress) {
			if req.OnProgress != nil {
				req.OnProgress(fmt.Sprintf("fetched %s %s %s..%s → %d candles",
					req.Instrument, toOandaGranularity(req.Timeframe),
					p.MonthStart.Format("2006-01-02"), p.MonthEnd.Format("2006-01-02"),
					p.CandlesWritten,
				))
			}
		})
	result := &DownloadOandaCandlesResult{}
	if syncResult != nil {
		result.MonthsProcessed = syncResult.MonthsProcessed
		result.CandlesWritten = syncResult.CandlesWritten
	}
	return result, err
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

// ── derive canonical from raw ─────────────────────────────────────────────────

// DeriveResult is returned by DeriveCanonicalFromRaw.
type DeriveResult = datamanager.DeriveResult

// DeriveCanonicalFromRaw reads a raw OANDA month CSV (bid+ask OHLC) from
// rawPath and writes the canonical candle CSV for key into the store. It
// also checks every expected market-hours slot and reports any that the raw
// file did not contain, so gaps can be surfaced immediately rather than
// found by a follow-up validate pass. Delegates entirely to DataManager.
func (s *Service) DeriveCanonicalFromRaw(ctx context.Context, rawPath string, key datamanager.Key) (*DeriveResult, error) {
	return datamanager.GetDataManager().DeriveCanonicalFromRaw(ctx, rawPath, key)
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
// starting from the day after the last non-zero candle already on disk
// (per DataManager.LastCompleteDate — the single shared gap-analysis
// implementation). When no existing data is found for a pair it returns an
// error for that pair (rather than downloading everything since 2000) so
// the caller can decide whether to seed with a full download first.
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

	dm := datamanager.GetDataManager()
	result := &UpdateOandaCandlesResult{
		Results: make(map[string]UpdateItemResult),
	}

	for _, inst := range req.Instruments {
		for _, tfStr := range req.Timeframes {
			key := inst + "/" + tfStr
			tf, err := parseTraderTimeframe(tfStr)
			if err != nil {
				result.Results[key] = UpdateItemResult{Err: err}
				if req.OnProgress != nil {
					req.OnProgress(fmt.Sprintf("%-12s %-4s  ERROR: %v", inst, tfStr, err))
				}
				continue
			}

			from, err := dm.LastCompleteDate(inst, tf, market.SourceOanda)
			if err != nil {
				if !req.SeedFrom.IsZero() {
					from = req.SeedFrom
				} else {
					result.Results[key] = UpdateItemResult{Err: fmt.Errorf("detect last candle: %w", err)}
					if req.OnProgress != nil {
						req.OnProgress(fmt.Sprintf("%-12s %-4s  ERROR: %v (use --from YYYY-MM-DD to seed)", inst, tfStr, err))
					}
					continue
				}
			} else {
				from = from.AddDate(0, 0, 1) // day after last complete candle
			}
			if !from.Before(to) {
				result.Results[key] = UpdateItemResult{From: from, To: to, CandlesWritten: 0}
				if req.OnProgress != nil {
					req.OnProgress(fmt.Sprintf("%-12s %-4s  already up to date (%s)", inst, tfStr, to.Format("2006-01-02")))
				}
				continue
			}

			dl, err := s.DownloadOandaCandles(ctx, DownloadOandaCandlesRequest{
				Instrument: inst,
				Timeframe:  tfStr,
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
					req.OnProgress(fmt.Sprintf("%-12s %-4s  ERROR: %v", inst, tfStr, itemErr))
				} else {
					req.OnProgress(fmt.Sprintf("%-12s %-4s  %s → %s  %d candles",
						inst, tfStr, from.Format("2006-01-02"), to.Format("2006-01-02"), written))
				}
			}
		}
	}
	return result, nil
}
