//go:build ignore

	duration := dm.end.Sub(dm.start)
	hours := int(duration.Hours()) + 1

	for sym, ds := range dm.data {
		assert.Equal(t, sym, ds.symbol)
		assert.NotNil(t, ds)
		assert.Equal(t, hours, ds.datafiles)
	}

	// now we have missing and existing lists we need to start sending
	// the data from each slice to the respective queue
	_ = dm.download(ctx)
	_ = dm.download(ctx)
}

func TestPathRoundTrip(t *testing.T) {
	orig := "../../tmp/dukas/EURUSD/2025/02/03/11h_ticks.bi5"

	df, err := ParseDatafilePath(orig)
	if err != nil {
		t.Fatal(err)
	}

	reconstructed := df.Path()

