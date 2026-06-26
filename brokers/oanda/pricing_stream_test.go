package oanda

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pricingStreamServer returns a test server that writes newline-delimited
// JSON messages to the client exactly as OANDA's pricing stream does.
func pricingStreamServer(t *testing.T, messages ...any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		enc := json.NewEncoder(w)
		for _, msg := range messages {
			_ = enc.Encode(msg) // Encode appends '\n'
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
}

func priceMsg(instrument, bid, ask string, tradeable bool) map[string]any {
	return map[string]any{
		"type":       "PRICE",
		"time":       "2024-03-15T10:00:00.000000000Z",
		"instrument": instrument,
		"tradeable":  tradeable,
		"bids":       []any{map[string]any{"price": bid}},
		"asks":       []any{map[string]any{"price": ask}},
	}
}

func heartbeatMsg(t string) map[string]any {
	return map[string]any{"type": "HEARTBEAT", "time": t}
}

// ── StreamPricing ─────────────────────────────────────────────────────────────

func TestStreamPricing_DeliversTick(t *testing.T) {
	srv := pricingStreamServer(t, priceMsg("EUR_USD", "1.08490", "1.08510", true))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Token: "tok", HTTP: srv.Client()}
	ch, err := c.StreamPricing(context.Background(), PricingStreamOptions{
		AccountID:   "ACC1",
		Instruments: []string{"EUR_USD"},
	})
	require.NoError(t, err)

	ev, ok := <-ch
	require.True(t, ok)
	require.NoError(t, ev.Err)
	assert.Equal(t, "EUR_USD", ev.Tick.Instrument)
	assert.InDelta(t, 1.08490, ev.Tick.Bid, 1e-9)
	assert.InDelta(t, 1.08510, ev.Tick.Ask, 1e-9)
	assert.InDelta(t, 1.08500, ev.Tick.Mid, 1e-9)
	assert.Equal(t, "2024-03-15T10:00:00Z", ev.Tick.Time.UTC().Format(time.RFC3339))
}

func TestStreamPricing_MultipleTicksInOrder(t *testing.T) {
	srv := pricingStreamServer(t,
		priceMsg("EUR_USD", "1.08490", "1.08510", true),
		priceMsg("USD_JPY", "149.990", "150.010", true),
	)
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Token: "tok", HTTP: srv.Client()}
	ch, err := c.StreamPricing(context.Background(), PricingStreamOptions{
		AccountID:   "ACC1",
		Instruments: []string{"EUR_USD", "USD_JPY"},
	})
	require.NoError(t, err)

	ev1 := <-ch
	require.NoError(t, ev1.Err)
	assert.Equal(t, "EUR_USD", ev1.Tick.Instrument)

	ev2 := <-ch
	require.NoError(t, ev2.Err)
	assert.Equal(t, "USD_JPY", ev2.Tick.Instrument)
}

func TestStreamPricing_HeartbeatCallbackFired(t *testing.T) {
	hbTime := "2024-03-15T10:00:05.000000000Z"
	srv := pricingStreamServer(t, heartbeatMsg(hbTime), priceMsg("EUR_USD", "1.08490", "1.08510", true))
	defer srv.Close()

	var got time.Time
	c := &Client{BaseURL: srv.URL, Token: "tok", HTTP: srv.Client()}
	ch, err := c.StreamPricing(context.Background(), PricingStreamOptions{
		AccountID:   "ACC1",
		Instruments: []string{"EUR_USD"},
		OnHeartbeat: func(t time.Time) { got = t },
	})
	require.NoError(t, err)

	// Drain the channel so the goroutine runs the heartbeat callback.
	for range ch {
	}

	assert.Equal(t, "2024-03-15T10:00:05Z", got.UTC().Format(time.RFC3339))
}

func TestStreamPricing_NonTradeableSkipped(t *testing.T) {
	srv := pricingStreamServer(t,
		priceMsg("EUR_USD", "1.08490", "1.08510", false), // non-tradeable
		priceMsg("USD_JPY", "149.990", "150.010", true),
	)
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Token: "tok", HTTP: srv.Client()}
	ch, err := c.StreamPricing(context.Background(), PricingStreamOptions{
		AccountID:   "ACC1",
		Instruments: []string{"EUR_USD", "USD_JPY"},
	})
	require.NoError(t, err)

	ev := <-ch
	require.NoError(t, ev.Err)
	assert.Equal(t, "USD_JPY", ev.Tick.Instrument, "non-tradeable EUR_USD should be skipped")
}

func TestStreamPricing_UnknownMessageTypeSkipped(t *testing.T) {
	srv := pricingStreamServer(t,
		map[string]any{"type": "PRICING_HEARTBEAT"}, // unknown type
		priceMsg("EUR_USD", "1.08490", "1.08510", true),
	)
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Token: "tok", HTTP: srv.Client()}
	ch, err := c.StreamPricing(context.Background(), PricingStreamOptions{
		AccountID:   "ACC1",
		Instruments: []string{"EUR_USD"},
	})
	require.NoError(t, err)

	ev := <-ch
	require.NoError(t, ev.Err)
	assert.Equal(t, "EUR_USD", ev.Tick.Instrument)
}

func TestStreamPricing_BadJSONSendsErrEvent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "not json at all")
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Token: "tok", HTTP: srv.Client()}
	ch, err := c.StreamPricing(context.Background(), PricingStreamOptions{
		AccountID:   "ACC1",
		Instruments: []string{"EUR_USD"},
	})
	require.NoError(t, err)

	ev := <-ch
	require.Error(t, ev.Err)
	assert.Contains(t, ev.Err.Error(), "bad pricing stream json")
}

func TestStreamPricing_ContextCancelClosesChannel(t *testing.T) {
	// Server that streams indefinitely.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		tick := priceMsg("EUR_USD", "1.08490", "1.08510", true)
		b, _ := json.Marshal(tick)
		for {
			select {
			case <-r.Context().Done():
				return
			default:
				fmt.Fprintf(w, "%s\n", b)
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
			}
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	c := &Client{BaseURL: srv.URL, Token: "tok", HTTP: srv.Client()}
	ch, err := c.StreamPricing(ctx, PricingStreamOptions{
		AccountID:   "ACC1",
		Instruments: []string{"EUR_USD"},
	})
	require.NoError(t, err)

	// Read one tick, then cancel.
	<-ch
	cancel()

	// Channel must close within a reasonable time.
	timeout := time.After(2 * time.Second)
	for {
		select {
		case _, open := <-ch:
			if !open {
				return // success
			}
		case <-timeout:
			t.Fatal("channel not closed after context cancel")
		}
	}
}

func TestStreamPricing_HTTPErrorReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Token: "tok", HTTP: srv.Client()}
	_, err := c.StreamPricing(context.Background(), PricingStreamOptions{
		AccountID:   "ACC1",
		Instruments: []string{"EUR_USD"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")
}

func TestStreamPricing_MissingTokenReturnsError(t *testing.T) {
	c := &Client{BaseURL: "http://localhost", Token: ""}
	_, err := c.StreamPricing(context.Background(), PricingStreamOptions{
		AccountID:   "ACC1",
		Instruments: []string{"EUR_USD"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing token")
}

func TestStreamPricing_MissingAccountIDReturnsError(t *testing.T) {
	c := &Client{BaseURL: "http://localhost", Token: "tok"}
	_, err := c.StreamPricing(context.Background(), PricingStreamOptions{
		AccountID:   "",
		Instruments: []string{"EUR_USD"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing account ID")
}

func TestStreamPricing_EmptyInstrumentsReturnsError(t *testing.T) {
	c := &Client{BaseURL: "http://localhost", Token: "tok"}
	_, err := c.StreamPricing(context.Background(), PricingStreamOptions{
		AccountID:   "ACC1",
		Instruments: nil,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "instruments list is empty")
}

// ── StreamPricingToCSV ────────────────────────────────────────────────────────

func TestStreamPricingToCSV_WritesHeaderAndRows(t *testing.T) {
	srv := pricingStreamServer(t,
		priceMsg("EUR_USD", "1.08490", "1.08510", true),
		priceMsg("USD_JPY", "149.990", "150.010", true),
	)
	defer srv.Close()

	var buf bytes.Buffer
	c := &Client{BaseURL: srv.URL, Token: "tok", HTTP: srv.Client()}
	n, err := c.StreamPricingToCSV(context.Background(), PricingStreamOptions{
		AccountID:   "ACC1",
		Instruments: []string{"EUR_USD", "USD_JPY"},
	}, &buf, 0)
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Len(t, lines, 3) // header + 2 rows
	assert.Equal(t, "time,instrument,bid,ask", lines[0])
	assert.Contains(t, lines[1], "EUR_USD")
	assert.Contains(t, lines[1], "1.0849")
	assert.Contains(t, lines[2], "USD_JPY")
}

func TestStreamPricingToCSV_RespectsMaxTicks(t *testing.T) {
	srv := pricingStreamServer(t,
		priceMsg("EUR_USD", "1.08490", "1.08510", true),
		priceMsg("EUR_USD", "1.08500", "1.08520", true),
		priceMsg("EUR_USD", "1.08510", "1.08530", true),
	)
	defer srv.Close()

	var buf bytes.Buffer
	c := &Client{BaseURL: srv.URL, Token: "tok", HTTP: srv.Client()}
	n, err := c.StreamPricingToCSV(context.Background(), PricingStreamOptions{
		AccountID:   "ACC1",
		Instruments: []string{"EUR_USD"},
	}, &buf, 2)
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	assert.Len(t, lines, 3) // header + 2 rows (stopped after maxTicks)
}

// ── streamURLFor ─────────────────────────────────────────────────────────────

func TestStreamURLFor_PracticeEnv(t *testing.T) {
	got, err := streamURLFor("https://api-fxpractice.oanda.com")
	require.NoError(t, err)
	assert.Equal(t, "https://stream-fxpractice.oanda.com", got)
}

func TestStreamURLFor_LiveEnv(t *testing.T) {
	got, err := streamURLFor("https://api-fxtrade.oanda.com")
	require.NoError(t, err)
	assert.Equal(t, "https://stream-fxtrade.oanda.com", got)
}

func TestStreamURLFor_LocalhostPassthrough(t *testing.T) {
	got, err := streamURLFor("http://127.0.0.1:8080")
	require.NoError(t, err)
	assert.Equal(t, "http://127.0.0.1:8080", got)
}

func TestStreamURLFor_UnknownBaseURLReturnsError(t *testing.T) {
	_, err := streamURLFor("https://some-other-host.example.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot derive stream URL")
}

func TestStreamURLFor_EmptyReturnsError(t *testing.T) {
	_, err := streamURLFor("")
	require.Error(t, err)
}
