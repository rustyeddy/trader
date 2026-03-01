package data

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type DataManager struct {
	Start       time.Time
	End         time.Time
	Basedir     string
	Instruments []string

	data map[string]*dataset
}

// NewDataManager creates a DataManager for the given instruments and time range,
// pre-populating the dataset map. Call BuildDatasets to download and process data.
func NewDataManager(instruments []string, start, end time.Time) *DataManager {
	dm := &DataManager{
		Start:       start,
		End:         end,
		Instruments: instruments,
		data:        make(map[string]*dataset),
	}
	for _, sym := range instruments {
		dm.data[sym] = newDataset(sym, start, end, "")
	}
	return dm
}

// dataset returns the dataset for the given instrument represented by
// symbol
func (dm *DataManager) dataset(sym string) *dataset {
	return dm.data[sym]
}

// BuildDatasets builds a dataset for each instrument and processes their
// datafiles concurrently. It returns once all sender goroutines have
// finished (either by completing all work or by context cancellation).
func (dm *DataManager) BuildDatasets(ctx context.Context) {
	if dm.data == nil {
		dm.data = make(map[string]*dataset)
		for _, sym := range dm.Instruments {
			dm.data[sym] = newDataset(sym, dm.Start, dm.End, dm.Basedir)
		}
	}

	candleQ := make(chan *datafile)
	dlQ := make(chan *datafile)

	go func() {
		for df := range dlQ {
			err := df.download(ctx, newHTTPClient())
			if err != nil {
				fmt.Printf("ERROR downloading %s\n", df.Path())
			}
		}
	}()

	go func() {
		for df := range candleQ {
			dm.buildCandles(df)
		}
	}()

	// Use a WaitGroup so channels are closed only after all senders finish,
	// preventing a send-on-closed-channel panic.
	var wg sync.WaitGroup
	for _, ds := range dm.data {
		wg.Add(1)
		go func(d *dataset) {
			defer wg.Done()
			d.buildDatafiles(ctx, candleQ, dlQ)
		}(ds)
	}

	wg.Wait()
	close(candleQ)
	close(dlQ)
}

// walk the missing datafiles for each of the symbols datasets and
// queue them up for download.
func (dm *DataManager) buildCandles(df *datafile) {
	fmt.Printf("Build candle from: %s\n", df.Path())
}
