package oanda

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Candle is one OANDA candle with both bid and ask OHLC sets.
// Volume is the tick count during the bar.
type Candle struct {
	Time   time.Time
	Volume int

	BidOpen, BidHigh, BidLow, BidClose float64
	AskOpen, AskHigh, AskLow, AskClose float64

	Complete bool
}

// FetchCandlesOptions controls a candle download.
type FetchCandlesOptions struct {
	Instrument  string    // OANDA format, e.g. "USD_JPY"
	Granularity string    // OANDA format, e.g. "M1", "H1", "D"
	From, To    time.Time // inclusive range

	// OANDA caps responses at 5000 candles. Pagination is automatic.
	ChunkSize int // default 5000
}

// candlesPaginatedResp is the JSON shape returned by /v3/instruments/{i}/candles
// when price=BA (bid+ask).
type candlesPaginatedResp struct {
	Instrument  string `json:"instrument"`
	Granularity string `json:"granularity"`
	Candles     []struct {
		Complete bool   `json:"complete"`
		Time     string `json:"time"`
		Volume   int    `json:"volume"`

		Bid *struct {
			O string `json:"o"`
			H string `json:"h"`
			L string `json:"l"`
			C string `json:"c"`
		} `json:"bid,omitempty"`

		Ask *struct {
			O string `json:"o"`
			H string `json:"h"`
			L string `json:"l"`
			C string `json:"c"`
		} `json:"ask,omitempty"`
	} `json:"candles"`
}

// FetchCandles downloads candles in the requested range, paginating as
// needed. Returns bid+ask OHLC for each candle.
func (c *Client) FetchCandles(ctx context.Context, opts FetchCandlesOptions) ([]Candle, error) {
	if c.Token == "" {
		return nil, fmt.Errorf("oanda: missing token")
	}
	if c.BaseURL == "" {
		return nil, fmt.Errorf("oanda: missing base url")
	}
	if opts.Instrument == "" {
		return nil, fmt.Errorf("oanda: missing instrument")
	}
	if opts.Granularity == "" {
		return nil, fmt.Errorf("oanda: missing granularity")
	}
	if opts.From.IsZero() || opts.To.IsZero() {
		return nil, fmt.Errorf("oanda: from and to are required for paginated fetch")
	}
	chunk := opts.ChunkSize
	if chunk <= 0 || chunk > 5000 {
		chunk = 5000
	}

	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	out := make([]Candle, 0, chunk)
	cursor := opts.From.UTC()
	end := opts.To.UTC()

	for cursor.Before(end) {
		u, err := url.Parse(c.BaseURL)
		if err != nil {
			return out, err
		}
		u.Path = fmt.Sprintf("/v3/instruments/%s/candles", opts.Instrument)
		q := u.Query()
		q.Set("granularity", opts.Granularity)
		q.Set("price", "BA")
		q.Set("count", strconv.Itoa(chunk))
		q.Set("from", cursor.Format(time.RFC3339Nano))
		u.RawQuery = q.Encode()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return out, err
		}
		req.Header.Set("Authorization", "Bearer "+c.Token)

		resp, err := httpClient.Do(req)
		if err != nil {
			return out, err
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			return out, fmt.Errorf("oanda candles http %d: %s",
				resp.StatusCode, strings.TrimSpace(string(body)))
		}

		var cr candlesPaginatedResp
		if err := json.Unmarshal(body, &cr); err != nil {
			return out, fmt.Errorf("oanda: parse candles: %w", err)
		}
		if len(cr.Candles) == 0 {
			break
		}

		var lastTime time.Time
		for _, cd := range cr.Candles {
			t, err := time.Parse(time.RFC3339Nano, cd.Time)
			if err != nil {
				continue
			}
			if !t.Before(end) {
				return out, nil
			}
			c := Candle{
				Time:     t,
				Volume:   cd.Volume,
				Complete: cd.Complete,
			}
			if cd.Bid != nil {
				c.BidOpen = parseFloat(cd.Bid.O)
				c.BidHigh = parseFloat(cd.Bid.H)
				c.BidLow = parseFloat(cd.Bid.L)
				c.BidClose = parseFloat(cd.Bid.C)
			}
			if cd.Ask != nil {
				c.AskOpen = parseFloat(cd.Ask.O)
				c.AskHigh = parseFloat(cd.Ask.H)
				c.AskLow = parseFloat(cd.Ask.L)
				c.AskClose = parseFloat(cd.Ask.C)
			}
			out = append(out, c)
			lastTime = t
		}

		// Advance cursor one step past the last candle returned.
		if lastTime.IsZero() {
			break
		}
		// If we got fewer than requested, we're done.
		if len(cr.Candles) < chunk {
			break
		}
		cursor = lastTime.Add(1) // one nanosecond after the last bar
	}

	return out, nil
}

func parseFloat(s string) float64 {
	v, _ := strconv.ParseFloat(s, 64)
	return v
}
