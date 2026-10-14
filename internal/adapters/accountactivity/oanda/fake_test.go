package oanda

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	testAccount = "101-001-1234567-001"
	testToken   = "secret-token-do-not-leak"
)

// fakeOANDA serves fixture data over the HTTPDoer seam: no network, no
// sockets. It records every request.
type fakeOANDA struct {
	mu       sync.Mutex
	txs      []json.RawMessage // the account's transactions, IDs 1..n
	currency string
	// window, if non-nil, is what the transactions list endpoint reports:
	// the idrange pages covering a time window.
	window [][2]int64
	// byTime, if set, derives the list response from the transactions' own
	// timestamps, both ends inclusive like OANDA, instead of window.
	byTime bool
	// lists records the (from, to) of every transactions-list request.
	lists []listReq
	// listBody and idrangeBody, if set, replace those endpoints' bodies.
	listBody, idrangeBody string
	status                int // forced status for every request, if non-zero
	badBody               string
	requests              []*http.Request
}

func loadFixture(t *testing.T) *fakeOANDA {
	t.Helper()
	data, err := os.ReadFile("testdata/transactions.json")
	require.NoError(t, err)
	var txs []json.RawMessage
	require.NoError(t, json.Unmarshal(data, &txs))
	return &fakeOANDA{txs: txs, currency: "USD"}
}

func (f *fakeOANDA) Do(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, req)

	respond := func(code int, body string) (*http.Response, error) {
		return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}, Request: req}, nil
	}
	if f.status != 0 {
		return respond(f.status, `{"errorMessage":"nope"}`)
	}
	if f.badBody != "" {
		return respond(200, f.badBody)
	}
	prefix := "/v3/accounts/" + testAccount
	path := req.URL.Path
	q := req.URL.Query()
	switch path {
	case prefix + "/summary":
		last := strconv.Itoa(len(f.txs))
		return respond(200, `{"account":{"currency":"`+f.currency+`","lastTransactionID":"`+last+`"},"lastTransactionID":"`+last+`"}`)
	case prefix + "/transactions":
		from, _ := time.Parse(time.RFC3339Nano, q.Get("from"))
		to, _ := time.Parse(time.RFC3339Nano, q.Get("to"))
		f.lists = append(f.lists, listReq{from, to})
		if f.listBody != "" {
			return respond(200, f.listBody)
		}
		window := f.window
		count := 0
		if f.byTime {
			window = nil
			lo, hi := int64(-1), int64(-1)
			for i, tx := range f.txs {
				var w struct{ Time string }
				_ = json.Unmarshal(tx, &w)
				at, _ := time.Parse(time.RFC3339Nano, w.Time)
				if (from.IsZero() || !at.Before(from)) && (to.IsZero() || !at.After(to)) { // inclusive both ends
					count++
					if lo < 0 {
						lo = int64(i + 1)
					}
					hi = int64(i + 1)
				}
			}
			if lo >= 0 {
				window = [][2]int64{{lo, hi}}
			}
		}
		pages := []string{} // OANDA returns [] for an empty window, never null
		for _, w := range window {
			pages = append(pages, "https://api.example"+prefix+"/transactions/idrange?from="+strconv.FormatInt(w[0], 10)+"&to="+strconv.FormatInt(w[1], 10))
		}
		if !f.byTime {
			count = len(pages)
		}
		b, _ := json.Marshal(map[string]any{"count": count, "pages": pages})
		return respond(200, string(b))
	case prefix + "/transactions/idrange":
		if f.idrangeBody != "" {
			return respond(200, f.idrangeBody)
		}
		from, _ := strconv.Atoi(q.Get("from"))
		to, _ := strconv.Atoi(q.Get("to"))
		var out []json.RawMessage
		for i, tx := range f.txs {
			if id := i + 1; id >= from && id <= to {
				out = append(out, tx)
			}
		}
		b, _ := json.Marshal(map[string]any{"transactions": out, "lastTransactionID": strconv.Itoa(len(f.txs))})
		return respond(200, string(b))
	}
	return respond(404, `{}`)
}

func (f *fakeOANDA) idranges() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, r := range f.requests {
		if strings.HasSuffix(r.URL.Path, "/idrange") {
			out = append(out, r.URL.Query().Get("from")+"-"+r.URL.Query().Get("to"))
		}
	}
	return out
}

type listReq struct{ from, to time.Time }

type token string

func (t token) Token(context.Context) (string, error) { return string(t), nil }

func newClient(t *testing.T, f *fakeOANDA) *Client {
	t.Helper()
	c, err := NewClient("https://api.example", f, token(testToken))
	require.NoError(t, err)
	return c
}

func rawTx(t *testing.T, f *fakeOANDA, id int) json.RawMessage {
	t.Helper()
	return bytes.Clone(f.txs[id-1])
}
