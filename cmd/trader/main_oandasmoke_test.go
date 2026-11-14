//go:build oandasmoke

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/internal/marketdata/oanda"
)

// TestSmokeOANDA downloads a small real range from OANDA into a temporary data
// root and checks what lands on disk. It is opt-in (go test -tags oandasmoke),
// makes real requests with your token, and never touches /srv/data: the root is
// t.TempDir(). The token is read from TRADER_OANDA_TOKEN only, and the host from
// TRADER_OANDA_BASE_URL (default: OANDA's practice host; a live-account token
// needs https://api-fxtrade.oanda.com). Nothing secret is printed.
//
//	export TRADER_OANDA_TOKEN   # set it in your own shell, not on a command line
//	go test -tags oandasmoke ./cmd/trader -run TestSmokeOANDA -v -count=1
func TestSmokeOANDA(t *testing.T) {
	token := os.Getenv(envOANDAToken)
	if token == "" {
		t.Fatalf("%s is not set: export it (never put it on the command line or in a file)", envOANDAToken)
	}
	env := map[string]string{envOANDAToken: token}
	baseURL := os.Getenv("TRADER_OANDA_BASE_URL")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	root := t.TempDir()
	now := time.Now().UTC().Truncate(24 * time.Hour)

	for _, tc := range []struct {
		interval string
		from, to time.Time
	}{
		{"h1", now.AddDate(0, 0, -8), now.AddDate(0, 0, -1)},  // a week of hourly bars
		{"d1", now.AddDate(0, 0, -45), now.AddDate(0, 0, -1)}, // daily bars across a month boundary
	} {
		t.Run(tc.interval, func(t *testing.T) {
			args := []string{"data", "download", "--config", writeConfig(t, ""), "--data-root", root, "--instrument", "EURUSD",
				"--interval", tc.interval, "--from", tc.from.Format(time.DateOnly), "--to", tc.to.Format(time.DateOnly), "--base-url", baseURL}

			out, logs, err := runData(t, env, args...)
			t.Log("\n" + out)

			require.NoError(t, err)
			assert.NotContains(t, out+logs, token, "the token must not appear in output or logs")
			assert.Regexp(t, `total: fetched [1-9]\d*, added [1-9]\d*`, out, "some candles were fetched and stored")

			var rows int
			for y := tc.from.Year(); y <= tc.to.Year(); y++ {
				for m := time.January; m <= time.December; m++ {
					key := oanda.PartitionKey{Symbol: "EURUSD", Interval: oanda.RawInterval(tc.interval), Year: y, Month: m}
					path, perr := key.Path(root)
					require.NoError(t, perr)
					if _, serr := os.Stat(path); serr != nil {
						continue
					}
					recs, rerr := oanda.ReadPartitionRecords(context.Background(), root, key)
					require.NoError(t, rerr, "every file the run wrote reads back, so the UTC-month rule holds")
					assert.True(t, strings.HasPrefix(path, filepath.Join(root, "oanda", "raw", "EURUSD")), path)
					for i, r := range recs {
						assert.True(t, r.Complete, "a forming candle is never stored")
						assert.False(t, r.Time.Before(tc.from) || !r.Time.Before(tc.to), "%s is inside the requested range", r.Time)
						if i > 0 {
							assert.True(t, r.Time.After(recs[i-1].Time), "ascending, no duplicates")
						}
					}
					rows += len(recs)
				}
			}
			assert.Positive(t, rows)

			// A second run over the same range adds nothing.
			again, _, err := runData(t, env, args...)
			require.NoError(t, err)
			assert.Regexp(t, `total: fetched [1-9]\d*, added 0, kept [1-9]\d*`, again)
		})
	}
}
