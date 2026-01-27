package cmd

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var oa2csvCmd = &cobra.Command{
	Use:   "oa2csv",
	Short: "Download OANDA candle data to CSV format",
	Long: `Download historical candle data from OANDA and convert to CSV format
suitable for backtesting.

Requires OANDA API token (set via -token flag or OANDA_TOKEN environment variable).

Example:
  trader oa2csv -token YOUR_TOKEN -instrument EUR_USD \
    -from 2024-01-01T00:00:00Z -to 2025-01-01T00:00:00Z \
    -granularity H1 -out eurusd.csv`,
	RunE: runOA2CSV,
}

var (
	oa2Env          string
	oa2Token        string
	oa2Instrument   string
	oa2Granularity  string
	oa2Price        string
	oa2From         string
	oa2To           string
	oa2Out          string
	oa2CompleteOnly bool
)

type candleResp struct {
	Instrument  string   `json:"instrument"`
	Granularity string   `json:"granularity"`
	Candles     []candle `json:"candles"`
}

type candle struct {
	Complete bool   `json:"complete"`
	Time     string `json:"time"`
	Volume   int    `json:"volume"`
	Mid      *ohlc  `json:"mid,omitempty"`
	Bid      *ohlc  `json:"bid,omitempty"`
	Ask      *ohlc  `json:"ask,omitempty"`
}

type ohlc struct {
	O string `json:"o"`
	H string `json:"h"`
	L string `json:"l"`
	C string `json:"c"`
}

func init() {
	rootCmd.AddCommand(oa2csvCmd)

	oa2csvCmd.Flags().StringVar(&oa2Env, "env", "practice", "OANDA environment: practice or live")
	oa2csvCmd.Flags().StringVar(&oa2Token, "token", "", "OANDA personal access token (or set OANDA_TOKEN env var)")
	oa2csvCmd.Flags().StringVar(&oa2Instrument, "instrument", "EUR_USD", "Instrument, e.g. EUR_USD")
	oa2csvCmd.Flags().StringVar(&oa2Granularity, "granularity", "H1", "Candlestick granularity, e.g. H1, H4, D")
	oa2csvCmd.Flags().StringVar(&oa2Price, "price", "BA", "Price components: BA (bid/ask) or M (mid)")
	oa2csvCmd.Flags().StringVar(&oa2From, "from", "", "RFC3339 start time, e.g. 2024-01-01T00:00:00Z (required)")
	oa2csvCmd.Flags().StringVar(&oa2To, "to", "", "RFC3339 end time, e.g. 2025-01-01T00:00:00Z (required)")
	oa2csvCmd.Flags().StringVar(&oa2Out, "out", "oanda_ticks.csv", "Output CSV path (time,instrument,bid,ask)")
	oa2csvCmd.Flags().BoolVar(&oa2CompleteOnly, "complete-only", true, "Only write complete candles")

	oa2csvCmd.MarkFlagRequired("from")
	oa2csvCmd.MarkFlagRequired("to")
}

func runOA2CSV(cmd *cobra.Command, args []string) error {
	if oa2Token == "" {
		oa2Token = os.Getenv("OANDA_TOKEN")
	}
	if oa2Token == "" {
		return fmt.Errorf("missing token: pass -token or set OANDA_TOKEN")
	}

	from, err := time.Parse(time.RFC3339, oa2From)
	if err != nil {
		return fmt.Errorf("bad -from: %w", err)
	}
	to, err := time.Parse(time.RFC3339, oa2To)
	if err != nil {
		return fmt.Errorf("bad -to: %w", err)
	}
	if !from.Before(to) {
		return fmt.Errorf("-from must be before -to")
	}

	baseURL := baseForEnv(oa2Env)
	if baseURL == "" {
		return fmt.Errorf("unknown -env %q (use practice or live)", oa2Env)
	}

	f, err := os.Create(oa2Out)
	if err != nil {
		return fmt.Errorf("create output: %w", err)
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	if err := w.Write([]string{"time", "instrument", "bid", "ask"}); err != nil {
		return fmt.Errorf("write header: %w", err)
	}

	ctx := context.Background()
	client := &http.Client{Timeout: 30 * time.Second}

	seen := make(map[string]struct{}, 1024)
	cur := from
	total := 0

	fmt.Printf("Downloading %s data from %s to %s...\n", oa2Instrument, oa2From, oa2To)

	for {
		if !cur.Before(to) {
			break
		}

		resp, err := fetchCandles(ctx, client, baseURL, oa2Token, oa2Instrument, oa2Granularity, oa2Price, cur, to)
		if err != nil {
			return fmt.Errorf("fetch: %w", err)
		}
		if len(resp.Candles) == 0 {
			break
		}

		lastT := time.Time{}
		for _, c := range resp.Candles {
			if oa2CompleteOnly && !c.Complete {
				continue
			}
			t, err := parseOandaTime(c.Time)
			if err != nil {
				return fmt.Errorf("parse candle time %q: %w", c.Time, err)
			}
			if t.Before(from) || !t.Before(to) {
				continue
			}

			tsKey := t.Format(time.RFC3339Nano)
			if _, ok := seen[tsKey]; ok {
				continue
			}
			seen[tsKey] = struct{}{}

			bid, ask, err := pickBidAsk(oa2Price, c)
			if err != nil {
				return fmt.Errorf("pick prices at %s: %w", tsKey, err)
			}

			if err := w.Write([]string{
				t.Format(time.RFC3339Nano),
				oa2Instrument,
				fmtFloat(bid),
				fmtFloat(ask),
			}); err != nil {
				return fmt.Errorf("write row: %w", err)
			}
			total++
			lastT = t
		}

		w.Flush()
		if err := w.Error(); err != nil {
			return fmt.Errorf("csv flush: %w", err)
		}

		if lastT.IsZero() {
			cur = cur.Add(1 * time.Hour)
			continue
		}

		cur = lastT.Add(1 * time.Nanosecond)
	}

	fmt.Printf("Downloaded %d candles to %s\n", total, oa2Out)
	return nil
}

func baseForEnv(env string) string {
	switch strings.ToLower(strings.TrimSpace(env)) {
	case "practice", "fxpractice":
		return "https://api-fxpractice.oanda.com"
	case "live", "fxtrade", "trade":
		return "https://api-fxtrade.oanda.com"
	default:
		return ""
	}
}

func fetchCandles(
	ctx context.Context,
	client *http.Client,
	baseURL string,
	token string,
	instrument string,
	granularity string,
	price string,
	from time.Time,
	to time.Time,
) (*candleResp, error) {
	u, _ := url.Parse(baseURL)
	u.Path = fmt.Sprintf("/v3/instruments/%s/candles", instrument)

	q := u.Query()
	q.Set("granularity", granularity)
	q.Set("price", price)
	q.Set("from", from.UTC().Format(time.RFC3339Nano))
	q.Set("count", "5000")
	q.Set("includeFirst", "true")

	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 8<<10))
		return nil, fmt.Errorf("oanda http %d: %s", res.StatusCode, strings.TrimSpace(string(b)))
	}

	var out candleResp
	dec := json.NewDecoder(res.Body)
	if err := dec.Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

func parseOandaTime(s string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, s)
}

func pickBidAsk(priceParam string, c candle) (bid float64, ask float64, err error) {
	switch strings.ToUpper(strings.TrimSpace(priceParam)) {
	case "BA":
		if c.Bid == nil || c.Ask == nil {
			return 0, 0, fmt.Errorf("missing bid/ask in candle")
		}
		bid, err = strconv.ParseFloat(c.Bid.C, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("bad bid close %q: %w", c.Bid.C, err)
		}
		ask, err = strconv.ParseFloat(c.Ask.C, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("bad ask close %q: %w", c.Ask.C, err)
		}
		return bid, ask, nil

	case "M":
		if c.Mid == nil {
			return 0, 0, fmt.Errorf("missing mid in candle")
		}
		m, err := strconv.ParseFloat(c.Mid.C, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("bad mid close %q: %w", c.Mid.C, err)
		}
		return m, m, nil

	default:
		return 0, 0, fmt.Errorf("unsupported price=%q (use BA or M)", priceParam)
	}
}

func fmtFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}
