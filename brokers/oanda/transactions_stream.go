package oanda

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// TxEvent is one event on a transaction stream. Either Tx is populated (a
// transaction arrived) or Err is non-nil (the stream ended). On Err, the
// channel is closed after this event.
type TxEvent struct {
	Tx  Transaction
	Err error
}

// Heartbeat is fired by the server roughly every 5s to keep the connection
// open. The LastTxID is the highest transaction ID OANDA knows about; on
// reconnect, poll GetTransactions(sinceID=LastTxID) to recover anything
// missed during the disconnect window.
type Heartbeat struct {
	LastTxID int64
	Time     time.Time
}

// StreamOptions configures a transaction stream subscription.
type StreamOptions struct {
	// OnHeartbeat, if set, is called for each HEARTBEAT message. Useful for
	// reconnect bookkeeping; never required.
	OnHeartbeat func(Heartbeat)
}

// StreamTransactions opens a long-lived HTTP connection to OANDA's
// transaction stream and pushes parsed Transactions onto the returned
// channel. The channel is closed when ctx is cancelled or the stream
// errors out — the final event on the channel has a non-nil Err in the
// latter case.
//
// OANDA hosts the stream endpoints on a separate domain (stream-fxpractice
// or stream-fxtrade); this method derives the stream URL from the
// client's BaseURL automatically.
func (c *Client) StreamTransactions(ctx context.Context, accountID string, opts StreamOptions) (<-chan TxEvent, error) {
	if c.Token == "" {
		return nil, fmt.Errorf("oanda: missing token")
	}
	if accountID == "" {
		return nil, fmt.Errorf("oanda: missing account ID")
	}

	streamURL, err := streamURLFor(c.BaseURL)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/v3/accounts/%s/transactions/stream", streamURL, accountID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept-Encoding", "identity") // disable gzip so we can read line-by-line

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
		return nil, fmt.Errorf("oanda: stream http %d", resp.StatusCode)
	}

	out := make(chan TxEvent, 16)

	go func() {
		defer resp.Body.Close()
		defer close(out)

		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)

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

			// Peek at type field to decide how to handle.
			var probe struct {
				Type              string `json:"type"`
				Time              string `json:"time"`
				LastTransactionID string `json:"lastTransactionID"`
			}
			if err := json.Unmarshal([]byte(line), &probe); err != nil {
				// Send the parse error, but keep the stream alive — corrupt
				// messages have happened in the wild from OANDA.
				select {
				case out <- TxEvent{Err: fmt.Errorf("oanda: bad stream line: %w (%s)", err, trimForErr(line))}:
				case <-ctx.Done():
					return
				}
				continue
			}

			if strings.ToUpper(probe.Type) == "HEARTBEAT" {
				if opts.OnHeartbeat != nil {
					hb := Heartbeat{}
					if t, err := time.Parse(time.RFC3339Nano, probe.Time); err == nil {
						hb.Time = t
					}
					if id := probe.LastTransactionID; id != "" {
						fmt.Sscanf(id, "%d", &hb.LastTxID)
					}
					opts.OnHeartbeat(hb)
				}
				continue
			}

			t, err := parseTransaction([]byte(line))
			if err != nil {
				select {
				case out <- TxEvent{Err: fmt.Errorf("oanda: parse stream tx: %w", err)}:
				case <-ctx.Done():
					return
				}
				continue
			}

			select {
			case out <- TxEvent{Tx: t}:
			case <-ctx.Done():
				return
			}
		}

		if err := sc.Err(); err != nil {
			select {
			case <-ctx.Done():
				return
			default:
			}
			select {
			case out <- TxEvent{Err: fmt.Errorf("oanda: stream read: %w", err)}:
			case <-ctx.Done():
			}
		}
	}()

	return out, nil
}

// streamURLFor turns an OANDA REST base URL into the matching stream base.
// api-fxpractice.oanda.com → stream-fxpractice.oanda.com
// api-fxtrade.oanda.com    → stream-fxtrade.oanda.com
func streamURLFor(baseURL string) (string, error) {
	if baseURL == "" {
		return "", fmt.Errorf("oanda: missing base url")
	}
	if strings.Contains(baseURL, "api-fxpractice.oanda.com") {
		return strings.Replace(baseURL, "api-fxpractice", "stream-fxpractice", 1), nil
	}
	if strings.Contains(baseURL, "api-fxtrade.oanda.com") {
		return strings.Replace(baseURL, "api-fxtrade", "stream-fxtrade", 1), nil
	}
	return "", fmt.Errorf("oanda: cannot derive stream URL from %q", baseURL)
}
