package oanda

import (
	"bufio"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// PriceTick is one tradeable price update from the OANDA pricing stream.
type PriceTick struct {
	Instrument string
	Bid        float64
	Ask        float64
	Mid        float64
	Time       time.Time
}

// PriceEvent is one message on the pricing stream channel. Either Tick is
// populated (a tradeable price update arrived) or Err is non-nil (the stream
// ended with an error). On Err, the channel is closed after this event.
type PriceEvent struct {
	Tick PriceTick
	Err  error
}

// PricingStreamOptions configures a pricing stream subscription.
type PricingStreamOptions struct {
	AccountID   string
	Instruments []string
	// OnHeartbeat, if set, is called for each heartbeat the server sends (~5 s).
	OnHeartbeat func(t time.Time)
}

// StreamPricing opens a long-lived HTTP connection to OANDA's pricing stream
// and pushes parsed PriceTick values onto the returned channel. The channel
// is closed when ctx is cancelled or the stream errors — the final event on
// the channel carries a non-nil Err in the latter case.
//
// Pricing streams are hosted on stream-fxpractice / stream-fxtrade (not the
// api subdomain); this method derives the correct URL from Client.BaseURL.
func (c *Client) StreamPricing(ctx context.Context, opts PricingStreamOptions) (<-chan PriceEvent, error) {
	if c.Token == "" {
		return nil, fmt.Errorf("oanda: missing token")
	}
	if opts.AccountID == "" {
		return nil, fmt.Errorf("oanda: missing account ID")
	}
	if len(opts.Instruments) == 0 {
		return nil, fmt.Errorf("oanda: instruments list is empty")
	}

	streamBase, err := streamURLFor(c.BaseURL)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/v3/accounts/%s/pricing/stream?instruments=%s",
		streamBase, opts.AccountID, strings.Join(opts.Instruments, ","))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept-Encoding", "identity") // disable gzip for line-by-line reading

	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		resp.Body.Close()
		return nil, fmt.Errorf("oanda: pricing stream http %d", resp.StatusCode)
	}

	out := make(chan PriceEvent, 64)

	go func() {
		defer resp.Body.Close()
		defer close(out)
		parsePricingStream(ctx, resp.Body, opts.OnHeartbeat, out)
	}()

	return out, nil
}

// pricingStreamMsg is the raw shape of each newline-delimited JSON message
// on the pricing stream.
type pricingStreamMsg struct {
	Type       string `json:"type"`
	Time       string `json:"time"`
	Instrument string `json:"instrument"`
	Tradeable  bool   `json:"tradeable"`
	Bids       []struct {
		Price string `json:"price"`
	} `json:"bids"`
	Asks []struct {
		Price string `json:"price"`
	} `json:"asks"`
}

// parsePricingStream reads newline-delimited JSON from r and sends parsed
// PriceTick events on out. Extracted for unit-testability.
func parsePricingStream(ctx context.Context, r io.Reader, onHB func(time.Time), out chan<- PriceEvent) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)

	send := func(ev PriceEvent) bool {
		select {
		case out <- ev:
			return true
		case <-ctx.Done():
			return false
		}
	}

	for sc.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}

		var msg pricingStreamMsg
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			if !send(PriceEvent{Err: fmt.Errorf("oanda: bad pricing stream json: %w", err)}) {
				return
			}
			continue
		}

		switch strings.ToUpper(msg.Type) {
		case "HEARTBEAT":
			if onHB != nil {
				t, _ := time.Parse(time.RFC3339Nano, msg.Time)
				onHB(t)
			}
			continue
		case "PRICE":
			// handled below
		default:
			continue
		}

		// Skip non-tradeable snapshots (e.g. market closed).
		if !msg.Tradeable || msg.Instrument == "" || len(msg.Bids) == 0 || len(msg.Asks) == 0 {
			continue
		}

		bid, err := strconv.ParseFloat(msg.Bids[0].Price, 64)
		if err != nil {
			if !send(PriceEvent{Err: fmt.Errorf("oanda: parse bid %q: %w", msg.Bids[0].Price, err)}) {
				return
			}
			continue
		}
		ask, err := strconv.ParseFloat(msg.Asks[0].Price, 64)
		if err != nil {
			if !send(PriceEvent{Err: fmt.Errorf("oanda: parse ask %q: %w", msg.Asks[0].Price, err)}) {
				return
			}
			continue
		}

		t, _ := time.Parse(time.RFC3339Nano, msg.Time)
		tick := PriceTick{
			Instrument: msg.Instrument,
			Bid:        bid,
			Ask:        ask,
			Mid:        (bid + ask) / 2,
			Time:       t,
		}
		if !send(PriceEvent{Tick: tick}) {
			return
		}
	}

	if err := sc.Err(); err != nil {
		select {
		case <-ctx.Done():
			return
		default:
		}
		send(PriceEvent{Err: fmt.Errorf("oanda: pricing stream read: %w", err)})
	}
}

// StreamPricingToCSV opens the OANDA pricing stream and writes each tradeable
// price update as a CSV row (time, instrument, bid, ask) to w.
// It stops when ctx is done or maxTicks > 0 rows have been written.
func (c *Client) StreamPricingToCSV(ctx context.Context, opts PricingStreamOptions, w io.Writer, maxTicks int) (int, error) {
	ch, err := c.StreamPricing(ctx, opts)
	if err != nil {
		return 0, err
	}

	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"time", "instrument", "bid", "ask"}); err != nil {
		return 0, err
	}
	cw.Flush()

	written := 0
	for ev := range ch {
		if ev.Err != nil {
			return written, ev.Err
		}
		t := ev.Tick
		row := []string{
			t.Time.Format(time.RFC3339Nano),
			t.Instrument,
			strconv.FormatFloat(t.Bid, 'f', -1, 64),
			strconv.FormatFloat(t.Ask, 'f', -1, 64),
		}
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
	return written, nil
}
