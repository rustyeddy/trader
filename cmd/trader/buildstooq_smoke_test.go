//go:build stooqsmoke

package main

import (
	"archive/zip"
	"context"
	"encoding/csv"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/internal/store"
	"github.com/rustyeddy/trader/internal/store/storetest"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/service"
)

// TestSmokeStooq builds SPY from a local Stooq daily bundle into a temporary data
// root, reads every bar back through service.ReadBars and compares it with the
// bundle's own rows. It is opt-in, uses no network, and only reads the bundle,
// through a symbolic link. The figures are those of the bundle inventoried in
// docs/research/equity-data-inventory.md (and ADR-095).
//
//	TRADER_STOOQ_BUNDLE=/srv/data/stooq/raw/d_us_txt.zip \
//	  go test -tags stooqsmoke ./cmd/trader -run TestSmokeStooq -v -count=1
func TestSmokeStooq(t *testing.T) {
	const (
		member   = "data/daily/us/nyse etfs/2/spy.us.txt"
		snapshot = "sha256:54b64bba890d89495bc2108824ba378a3f7f92349d5021595499bbc47696ee6b"
	)
	bundle := os.Getenv("TRADER_STOOQ_BUNDLE")
	if bundle == "" {
		t.Fatal("TRADER_STOOQ_BUNDLE is not set: the path of a local d_us_txt.zip")
	}
	bundle, err := filepath.Abs(bundle)
	require.NoError(t, err)
	root := storetest.Root(t)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "stooq", "raw"), 0o755))
	require.NoError(t, os.Symlink(bundle, filepath.Join(root, "stooq", "raw", "d_us_txt.zip")))
	before, err := store.Checksum(context.Background(), "stooq/raw/d_us_txt.zip")
	require.NoError(t, err)
	args := []string{"data", "build", "--config", writeConfig(t, ""), "--data-root", root, "--source", "stooq",
		"--instrument", "SPY", "--input", "stooq/raw/d_us_txt.zip", "--member", member, "--snapshot-as-of", "2026-09-10"}

	out, _, err := runData(t, nil, args...)
	require.NoError(t, err)
	assert.Contains(t, out, "snapshot: "+snapshot+" as of 2026-09-10")
	assert.Contains(t, out, "total: built 260, unchanged 0, replaced 0; good bars 5418, bad records 0")
	again, _, err := runData(t, nil, args...)
	require.NoError(t, err)
	assert.Contains(t, again, "total: built 0, unchanged 260, replaced 0; good bars 5418, bad records 0")

	zr, err := zip.OpenReader(bundle)
	require.NoError(t, err)
	defer func() { _ = zr.Close() }()
	raw, err := zr.Open(member)
	require.NoError(t, err)
	rows := csv.NewReader(raw)
	_, err = rows.Read() // header
	require.NoError(t, err)

	req := service.BarsRequest{Source: "stooq", Instrument: "SPY", Interval: "d1", Limit: 500,
		From: time.Date(2005, 1, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}
	ny, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	n, early := 0, 0
	var gaps []service.Gap
	for {
		page, err := service.ReadBars(context.Background(), nil, req)
		require.NoError(t, err)
		require.NotNil(t, page.Data)
		assert.Equal(t, snapshot, page.Data.SnapshotFingerprint)
		assert.Equal(t, "total-return", page.Data.Adjustment)
		gaps = append(gaps, page.Coverage.Gaps...)
		for _, b := range page.Bars {
			f, err := rows.Read()
			require.NoError(t, err)
			require.Equal(t, f[2], b.Time.In(ny).Format("20060102"))
			require.Equal(t, "09:30", b.Time.In(ny).Format("15:04"))
			closes := b.AvailableAt.In(ny).Format("20060102 15:04")
			require.Contains(t, []string{f[2] + " 16:00", f[2] + " 13:00"}, closes)
			if closes == f[2]+" 13:00" {
				early++
			}
			require.Equal(t, []num.Price{num.MustParsePrice(f[4]), num.MustParsePrice(f[5]), num.MustParsePrice(f[6]), num.MustParsePrice(f[7])},
				[]num.Price{b.Trade.Open, b.Trade.High, b.Trade.Low, b.Trade.Close}, f[2])
			require.Equal(t, f[8], strconv.FormatInt(b.Volume, 10), f[2])
			require.True(t, b.Bid.IsZero() && b.Ask.IsZero() && b.Mid.IsZero())
			n++
		}
		if page.NextAfter.IsZero() {
			break
		}
		req.After = page.NextAfter
	}
	_, err = rows.Read()
	assert.ErrorIs(t, err, io.EOF, "every row was served")
	assert.Equal(t, 5418, n)
	require.Len(t, gaps, 1, "closures are not gaps; only the one unexplained session is")
	assert.Equal(t, service.GapUnexplained, gaps[0].Kind)
	assert.Equal(t, "2011-02-17", gaps[0].Start.Format(time.DateOnly))
	assert.Equal(t, 1, gaps[0].Bars)
	t.Logf("%d bars, %d on early closes", n, early)

	req.After, req.Strict = time.Time{}, true
	_, err = service.ReadBars(context.Background(), nil, req)
	assert.ErrorIs(t, err, service.ErrBarsIncomplete, "a strict read covering 2011-02-17 fails")
	req.From = time.Date(2011, 3, 1, 0, 0, 0, 0, time.UTC)
	_, err = service.ReadBars(context.Background(), nil, req)
	assert.NoError(t, err, "a strict read after it succeeds")

	after, err := store.Checksum(context.Background(), "stooq/raw/d_us_txt.zip")
	require.NoError(t, err)
	assert.Equal(t, before, after)
}
