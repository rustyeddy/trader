package oanda

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type CandlesOptions struct {
	Instrument  string
	Granularity string // e.g. M1, H1, D
	Price       string // M, B, A, BA

	From  time.Time // optional
	To    time.Time // optional
	Count int       // optional (used if >0)
}

type candlesResp struct {
	Instrument  string `json:"instrument"`
	Granularity string `json:"granularity"`
	Candles     []struct {
		Complete bool   `json:"complete"`
		Time     string `json:"time"`
		Volume   int    `json:"volume"`

		Mid *struct {
			O string `json:"o"`
			H string `json:"h"`
			L string `json:"l"`
			C string `json:"c"`
		} `json:"mid,omitempty"`

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

func (c *Client) DownloadCandlesToCSV(ctx context.Context, opts CandlesOptions, w io.Writer) (int, error) {
	if c.Token == "" {
		return 0, fmt.Errorf("oanda: missing token")
	}
	if c.BaseURL == "" {
		return 0, fmt.Errorf("oanda: missing base url")
	}
	if opts.Instrument == "" {
		return 0, fmt.Errorf("oanda: missing instrument")
	}
	if opts.Granularity == "" {
		return 0, fmt.Errorf("oanda: missing granularity")
	}
	price := strings.ToUpper(strings.TrimSpace(opts.Price))
	if price == "" {
		price = "M"
	}

	u, err := url.Parse(c.BaseURL)
	if err != nil {
		return 0, err
	}
	u.Path = fmt.Sprintf("/v3/instruments/%s/candles", opts.Instrument)

	q := u.Query()
	q.Set("granularity", opts.Granularity)
	q.Set("price", price)

	if opts.Count > 0 {
		q.Set("count", strconv.Itoa(opts.Count))
	} else {
		if !opts.From.IsZero() {
			q.Set("from", opts.From.UTC().Format(time.RFC3339Nano))
		}
		if !opts.To.IsZero() {
			q.Set("to", opts.To.UTC().Format(time.RFC3339Nano))
		}
	}

	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")

	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		return 0, fmt.Errorf("oanda candles http %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	var cr candlesResp
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		return 0, err
	}

	cw := csv.NewWriter(w)
	// Canonical candle CSV (single OHLC set):
	// time,instrument,granularity,complete,volume,o,h,l,c
	if err := cw.Write([]string{"time", "instrument", "granularity", "complete", "volume", "o", "h", "l", "c"}); err != nil {
		return 0, err
	}

	written := 0

	for _, cd := range cr.Candles {
		// pick which component to write
		var ohlc *struct{ O, H, L, C string }
		switch price {
		case "M":
			if cd.Mid == nil {
				continue
			}
			ohlc = &struct{ O, H, L, C string }{cd.Mid.O, cd.Mid.H, cd.Mid.L, cd.Mid.C}
		case "B":
			if cd.Bid == nil {
				continue
			}
			ohlc = &struct{ O, H, L, C string }{cd.Bid.O, cd.Bid.H, cd.Bid.L, cd.Bid.C}
		case "A":
			if cd.Ask == nil {
				continue
			}
			ohlc = &struct{ O, H, L, C string }{cd.Ask.O, cd.Ask.H, cd.Ask.L, cd.Ask.C}
		default:
			// BA returns both bid and ask sets in the API; for now we keep it simple and reject.
			return written, fmt.Errorf("price=BA not supported for CSV output yet; use M/B/A")
		}

		row := []string{
			cd.Time,
			cr.Instrument,
			cr.Granularity,
			strconv.FormatBool(cd.Complete),
			strconv.Itoa(cd.Volume),
			ohlc.O, ohlc.H, ohlc.L, ohlc.C,
		}
		if err := cw.Write(row); err != nil {
			return written, err
		}
		written++
	}

	cw.Flush()
	if err := cw.Error(); err != nil {
		return written, err
	}

	return written, nil
}
