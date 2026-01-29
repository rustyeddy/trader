//go:build blackbox

package blackbox

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestOandACandles(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "candles.csv")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// assert r.URL.Path == "/v3/instruments/EUR_USD/candles"
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{ "instrument":"EUR_USD", "granularity":"H1", "candles":[ ... ] }`))
	}))
	defer srv.Close()

	out, _ := run(t,
		"data", "oanda", "candles",
		"--base-url", srv.URL,
		"--token", "dummy",
		"--instrument", "EUR_USD",
		"--granularity", "H1",
		"--count", "5",
		"--out", csvPath,
	)

	fmt.Printf("otuput: %s\n", out)
}
