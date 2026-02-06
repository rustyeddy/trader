package oanda

import (
	"bufio"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	BaseURL string // e.g. https://api-fxpractice.oanda.com
	Token   string
	HTTP    *http.Client
}

func BaseURL(env string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(env)) {
	case "practice", "demo":
		return "https://api-fxpractice.oanda.com", nil
	case "live", "trade":
		return "https://api-fxtrade.oanda.com", nil
	default:
		return "", fmt.Errorf("unknown OANDA env %q (want practice|live)", env)
	}
}

type PricingStreamOptions struct {
	AccountID   string
	Instruments []string
}

type pricingStreamMsg struct {
	Type       string `json:"type"`
	Time       string `json:"time"`
	Instrument string `json:"instrument"`

	Bids []struct {
		Price string `json:"price"`
	} `json:"bids"`

	Asks []struct {
		Price string `json:"price"`
	} `json:"asks"`
}

// StreamPricingToCSV connects to OANDA pricing stream and writes rows:
// time,instrument,bid,ask
// It stops when:
// - ctx is done, or
// - maxTicks > 0 and that many tick rows were written.
func (c *Client) StreamPricingToCSV(
	ctx context.Context,
	opts PricingStreamOptions,
	w io.Writer,
	maxTicks int,
) (int, error) {
	if c.Token == "" {
		return 0, fmt.Errorf("oanda: missing token")
	}
	if c.BaseURL == "" {
		return 0, fmt.Errorf("oanda: missing base url")
	}
	if opts.AccountID == "" {
		return 0, fmt.Errorf("oanda: missing AccountID")
	}
	if len(opts.Instruments) == 0 {
		return 0, fmt.Errorf("oanda: missing Instruments")
	}

	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	u, err := url.Parse(c.BaseURL)
	if err != nil {
		return 0, err
	}
	u.Path = fmt.Sprintf("/v3/accounts/%s/pricing/stream", opts.AccountID)
	q := u.Query()
	q.Set("instruments", strings.Join(opts.Instruments, ","))
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)

	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		return 0, fmt.Errorf("oanda pricing stream http %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	cw := csv.NewWriter(w)
	// header
	if err := cw.Write([]string{"time", "instrument", "bid", "ask"}); err != nil {
		return 0, err
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		return 0, err
	}

	sc := bufio.NewScanner(resp.Body)
	// OANDA stream messages can be long; bump max token
	sc.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)

	written := 0

	for sc.Scan() {
		select {
		case <-ctx.Done():
			return written, ctx.Err()
		default:
		}

		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}

		var msg pricingStreamMsg
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			// keep going, but expose the bad line context
			return written, fmt.Errorf("oanda: bad json: %w (line=%q)", err, trimForErr(line))
		}

		// HEARTBEAT messages exist; ignore them
		if strings.ToUpper(msg.Type) == "HEARTBEAT" {
			continue
		}
		if strings.ToUpper(msg.Type) != "PRICE" {
			continue
		}
		if msg.Instrument == "" || len(msg.Bids) == 0 || len(msg.Asks) == 0 {
			continue
		}

		t := msg.Time
		if t == "" {
			t = time.Now().UTC().Format(time.RFC3339Nano)
		}

		row := []string{t, msg.Instrument, msg.Bids[0].Price, msg.Asks[0].Price}
		if err := cw.Write(row); err != nil {
			return written, err
		}
		cw.Flush()
		if err := cw.Error(); err != nil {
			return written, err
		}

		written++
		if maxTicks > 0 && written >= maxTicks {
			return written, nil
		}
	}

	if err := sc.Err(); err != nil {
		// if ctx was cancelled, surface that instead
		select {
		case <-ctx.Done():
			return written, ctx.Err()
		default:
		}
		return written, err
	}

	return written, nil
}

func trimForErr(s string) string {
	const n = 200
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
