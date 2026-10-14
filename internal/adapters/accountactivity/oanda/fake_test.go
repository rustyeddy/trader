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
	window   [][2]int64
	status   int // forced status for every request, if non-zero
	badBody  string
	requests []*http.Request
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
		var pages []string
		for _, w := range f.window {
			pages = append(pages, "https://api.example"+prefix+"/transactions/idrange?from="+strconv.FormatInt(w[0], 10)+"&to="+strconv.FormatInt(w[1], 10))
		}
		b, _ := json.Marshal(map[string]any{"count": len(pages), "pages": pages})
		return respond(200, string(b))
	case prefix + "/transactions/idrange":
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
