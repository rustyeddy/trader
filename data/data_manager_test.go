package data

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/rustyeddy/trader/market"
)

func TestBuildDataSets(t *testing.T) {
	start := time.Date(2003, 01, 01, 0, 0, 0, 0, time.UTC)
	dm := NewDataManager(market.InstrumentList, start, time.Now())
	assert.NotNil(t, dm)

	ctx := context.TODO()
	dm.buildDatasets(ctx)
	assert.Equal(t, len(market.InstrumentList), len(dm.data))

	duration := dm.end.Sub(dm.start)
	hours := int(duration.Hours()) + 1

	for sym, ds := range dm.data {
		assert.Equal(t, sym, ds.symbol)
		assert.NotNil(t, ds)
		assert.Equal(t, hours, ds.datafiles)
	}

	// now we have missing and existing lists we need to start sending
	// the data from each slice to the respective queue
	dlQ := dm.download(ctx)
	candleQ := dm.download(ctx)
}

func TestPathRoundTrip(t *testing.T) {
	orig := "../../tmp/dukas/EURUSD/2025/02/03/11h_ticks.bi5"

	df, err := ParseDatafilePath(orig)
	if err != nil {
		t.Fatal(err)
	}

	reconstructed := df.Path()

	if filepath.Clean(orig) != filepath.Clean(reconstructed) {
		t.Fatalf("round trip mismatch:\norig: %s\nnew : %s",
			orig, reconstructed)
	}
}
