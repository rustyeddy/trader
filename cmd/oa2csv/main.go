package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
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

// Config holds all configuration parameters for the oa2csv tool.
type Config struct {
	Env          string
	Token        string
	Instrument   string
	Granularity  string
	Price        string
	FromStr      string
	ToStr        string
	OutPath      string
	CompleteOnly bool
}

func main() {
	cfg := &Config{}
	flag.StringVar(&cfg.Env, "env", "practice", "OANDA environment: practice or live")
	flag.StringVar(&cfg.Token, "token", "", "OANDA personal access token (or set OANDA_TOKEN env var)")
	flag.StringVar(&cfg.Instrument, "instrument", "EUR_USD", "Instrument, e.g. EUR_USD")
	flag.StringVar(&cfg.Granularity, "granularity", "H1", "Candlestick granularity, e.g. H1, H4, D")
	flag.StringVar(&cfg.Price, "price", "BA", "Price components: BA (bid/ask) or M (mid)")
	flag.StringVar(&cfg.FromStr, "from", "", "RFC3339 start time, e.g. 2024-01-01T00:00:00Z")
	flag.StringVar(&cfg.ToStr, "to", "", "RFC3339 end time, e.g. 2025-01-01T00:00:00Z")
	flag.StringVar(&cfg.OutPath, "out", "oanda_ticks.csv", "Output CSV path (time,instrument,bid,ask)")
	flag.BoolVar(&cfg.CompleteOnly, "complete-only", true, "Only write complete candles")
	flag.Parse()

	if cfg.Token == "" {
		cfg.Token = os.Getenv("OANDA_TOKEN")
	}
	if cfg.Token == "" {
		fatalf("missing token: pass -token or set OANDA_TOKEN")
	}
	if cfg.FromStr == "" || cfg.ToStr == "" {
		fatalf("missing time range: both -from and -to are required")
	}

	from, err := time.Parse(time.RFC3339, cfg.FromStr)
	if err != nil {
		fatalf("bad -from: %v", err)
	}
	to, err := time.Parse(time.RFC3339, cfg.ToStr)
	if err != nil {
		fatalf("bad -to: %v", err)
	}
	if !from.Before(to) {
		fatalf("-from must be before -to")
	}

	baseURL := baseForEnv(cfg.Env)
	if baseURL == "" {
		fatalf("unknown -env %q (use practice or live)", cfg.Env)
	}

	// Create output CSV
	f, err := os.Create(cfg.OutPath)
	if err != nil {
		fatalf("create output: %v", err)
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	// Header matches replay.CSV header detection ("time" in col 0)
	if err := w.Write([]string{"time", "instrument", "bid", "ask"}); err != nil {
		fatalf("write header: %v", err)
	}

	ctx := context.Background()
	client := &http.Client{Timeout: 30 * time.Second}

	// Pagination approach:
	// - Request candles with from + to time range (API returns up to 5000 candles per request).
	// - Advance "from" by 1ns past the last candle time to avoid duplicates.
	// - Stop once last candle time >= to or no candles returned.
	seen := make(map[string]struct{}, 1024)

	cur := from
	total := 0

	for {
		if !cur.Before(to) {
			break
		}

		resp, err := fetchCandles(ctx, client, baseURL, cfg.Token, cfg.Instrument, cfg.Granularity, cfg.Price, cur, to)
		if err != nil {
			fatalf("fetch: %v", err)
		}
		if len(resp.Candles) == 0 {
			break
		}

		lastT := time.Time{}
		for _, c := range resp.Candles {
			if cfg.CompleteOnly && !c.Complete {
				continue
			}
			t, err := parseOandaTime(c.Time)
			if err != nil {
				fatalf("parse candle time %q: %v", c.Time, err)
			}
			if t.Before(from) || !t.Before(to) {
				continue
			}

			tsKey := t.Format(time.RFC3339Nano)
			if _, ok := seen[tsKey]; ok {
				continue
			}
			seen[tsKey] = struct{}{}

			bid, ask, err := pickBidAsk(cfg.Price, c)
			if err != nil {
				fatalf("pick prices at %s: %v", tsKey, err)
			}

			if err := w.Write([]string{
				t.Format(time.RFC3339Nano),
				cfg.Instrument,
				fmtFloat(bid),
				fmtFloat(ask),
			}); err != nil {
				fatalf("write row: %v", err)
			}
			total++
			lastT = t
		}

		w.Flush()
		if err := w.Error(); err != nil {
			fatalf("csv flush: %v", err)
		}

		// Advance cursor
		if lastT.IsZero() {
			// Nothing useful written; advance a bit to avoid infinite loop.
			cur = cur.Add(1 * time.Hour)
			continue
		}

		// 1ns past last candle to avoid repeats
		cur = lastT.Add(1 * time.Nanosecond)

		// If the response already includes candles beyond our to, we'll stop naturally next loop
		total = total
	}

	fmt.Fprintf(os.Stderr, "Wrote %d rows to %s\n", total, cfg.OutPath)
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
	// Use count-based pagination to respect max=5000, and let "to" be a hard cap
	q.Set("count", "5000")
	//q.Set("count", "0")
	q.Set("includeFirst", "true")
	// Optional "to" cap to prevent extra beyond our window
	//q.Set("to", to.UTC().Format(time.RFC3339Nano))

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
	// OANDA often returns RFC3339 with nanos; time.RFC3339Nano handles both.
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
		// If we only have mid, treat bid=ask=mid for backtesting simplicity
		return m, m, nil

	default:
		return 0, 0, fmt.Errorf("unsupported price=%q (use BA or M)", priceParam)
	}
}

func fmtFloat(v float64) string {
	// Keep enough precision for FX
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func fatalf(msg string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+msg+"\n", args...)
	os.Exit(1)
}
