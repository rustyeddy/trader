package data

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	traderpkg "github.com/rustyeddy/trader"
	"github.com/rustyeddy/trader/brokers/oanda"
)

func newOandaDownloadCmd(rc *traderpkg.RootConfig) *cobra.Command {
	var (
		instrument string
		timeframe  string
		fromStr    string
		toStr      string
		token      string
		env        string
		rawDir     string
	)

	cmd := &cobra.Command{
		Use:   "oanda",
		Short: "Download candles from OANDA into the canonical candle store",
		RunE: func(cmd *cobra.Command, args []string) error {
			if token == "" {
				token = readOandaToken()
			}
			if token == "" {
				return fmt.Errorf("no OANDA token: set OANDA_TOKEN, use --token, or save to ~/.config/oanda/pat.txt")
			}

			tf, err := parseTraderTimeframe(timeframe)
			if err != nil {
				return err
			}
			tfStr := strings.ToUpper(strings.TrimSpace(timeframe))

			from, err := time.Parse("2006-01-02", fromStr)
			if err != nil {
				return fmt.Errorf("bad --from %q: %w", fromStr, err)
			}
			to, err := time.Parse("2006-01-02", toStr)
			if err != nil {
				return fmt.Errorf("bad --to %q: %w", toStr, err)
			}
			if !from.Before(to) {
				return fmt.Errorf("--from must be before --to")
			}

			baseURL, err := oanda.BaseURL(env)
			if err != nil {
				return err
			}
			client := &oanda.Client{BaseURL: baseURL, Token: token}
			store := traderpkg.GetStore()
			ctx := context.Background()

			instrTrader := strings.ReplaceAll(instrument, "_", "")

			// Walk month by month so each output file is a month.
			cursor := time.Date(from.Year(), from.Month(), 1, 0, 0, 0, 0, time.UTC)
			endMonth := time.Date(to.Year(), to.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 1, 0)

			for cursor.Before(endMonth) {
				monthStart := cursor
				monthEnd := cursor.AddDate(0, 1, 0)
				if monthStart.Before(from) {
					monthStart = from
				}
				if monthEnd.After(to.AddDate(0, 0, 1)) {
					monthEnd = to.AddDate(0, 0, 1)
				}

				fmt.Printf("fetching %s %s %s..%s ", instrument, tfStr, monthStart.Format("2006-01-02"), monthEnd.Format("2006-01-02"))

				candles, err := client.FetchCandles(ctx, oanda.FetchCandlesOptions{
					Instrument:  instrument,
					Granularity: tfStr,
					From:        monthStart,
					To:          monthEnd,
				})
				if err != nil {
					return fmt.Errorf("fetch %s: %w", monthStart.Format("2006-01"), err)
				}

				monthSlotStart := time.Date(cursor.Year(), cursor.Month(), 1, 0, 0, 0, 0, time.UTC)
				stepSec := int64(tf)
				slotCount := int(monthSlotStart.AddDate(0, 1, 0).Sub(monthSlotStart).Seconds() / float64(stepSec))

				traderCandles := make([]traderpkg.Candle, slotCount)
				for _, oc := range candles {
					if oc.BidClose == 0 && oc.AskClose == 0 {
						continue
					}
					idx := int(oc.Time.Unix()-monthSlotStart.Unix()) / int(stepSec)
					if idx < 0 || idx >= slotCount {
						continue
					}
					// Approximate avg/max spread from the four OHLC sample points
					// since OANDA candles don't expose tick-level spread.
					spreads := [4]float64{
						oc.AskOpen - oc.BidOpen,
						oc.AskHigh - oc.BidHigh,
						oc.AskLow - oc.BidLow,
						oc.AskClose - oc.BidClose,
					}
					var sum, max float64
					for _, s := range spreads {
						sum += s
						if s > max {
							max = s
						}
					}
					traderCandles[idx] = traderpkg.Candle{
						Open:      traderpkg.PriceFromFloat(oc.BidOpen),
						High:      traderpkg.PriceFromFloat(oc.BidHigh),
						Low:       traderpkg.PriceFromFloat(oc.BidLow),
						Close:     traderpkg.PriceFromFloat(oc.BidClose),
						AvgSpread: traderpkg.PriceFromFloat(sum / 4),
						MaxSpread: traderpkg.PriceFromFloat(max),
						Ticks:     int32(oc.Volume),
					}
				}

				if err := store.WriteMonthlyCandles("oanda", instrTrader, tf, monthSlotStart, traderCandles); err != nil {
					return fmt.Errorf("write %s: %w", monthSlotStart.Format("2006-01"), err)
				}

				// Raw preservation: keep bid+ask OHLC unmodified so we can later
				// recompute spread, derive other timeframes, or build a
				// fill-accurate execution model without re-hitting the API.
				if err := writeRawOandaMonth(rawDir, instrTrader, tfStr, monthSlotStart, candles); err != nil {
					return fmt.Errorf("write raw %s: %w", monthSlotStart.Format("2006-01"), err)
				}

				fmt.Printf("→ %d candles\n", countNonZero(traderCandles))

				cursor = cursor.AddDate(0, 1, 0)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&instrument, "instrument", "", "OANDA-format instrument, e.g. USD_JPY (required)")
	cmd.Flags().StringVar(&timeframe, "timeframe", "", "Timeframe: M1, M5, H1, H4, D (required)")
	cmd.Flags().StringVar(&fromStr, "from", "", "Start date YYYY-MM-DD inclusive (required)")
	cmd.Flags().StringVar(&toStr, "to", "", "End date YYYY-MM-DD inclusive (required)")
	cmd.Flags().StringVar(&token, "token", os.Getenv("OANDA_TOKEN"), "OANDA API token (falls back to ~/.config/oanda/pat.txt)")
	cmd.Flags().StringVar(&env, "env", "practice", "OANDA environment: practice|live")
	cmd.Flags().StringVar(&rawDir, "raw-dir", "/data/raw", "Root directory for raw bid+ask candle preservation")
	_ = cmd.MarkFlagRequired("instrument")
	_ = cmd.MarkFlagRequired("timeframe")
	_ = cmd.MarkFlagRequired("from")
	_ = cmd.MarkFlagRequired("to")

	return cmd
}

// writeRawOandaMonth preserves the bid+ask OHLC exactly as OANDA returned it,
// before any spread/scale derivation. Lets us later recompute spread, derive
// other timeframes, or build fill-accurate execution models without re-hitting
// the API.
//
// Path: <rawDir>/oanda/<INSTR>/<YEAR>/<MM>/<INSTR>-<YEAR>-<MM>-<tf>.csv
// Format: ISO timestamps, float prices (no scaling), one row per candle.
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

	written := 0
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
		written++
	}
	return nil
}

func parseTraderTimeframe(s string) (traderpkg.Timeframe, error) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "M1":
		return traderpkg.M1, nil
	case "H1":
		return traderpkg.H1, nil
	case "D1", "D":
		return traderpkg.D1, nil
	default:
		return 0, fmt.Errorf("unsupported timeframe %q (use M1, H1, D1)", s)
	}
}

func readOandaToken() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(home, ".config", "oanda", "pat.txt"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func countNonZero(cs []traderpkg.Candle) int {
	n := 0
	for i := range cs {
		if !cs[i].IsZero() {
			n++
		}
	}
	return n
}

// avoid unused-import warnings when math is needed later
var _ = math.Floor
