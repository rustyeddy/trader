package data

import (
	"context"
	"fmt"
	"time"
)

type DataManager struct {
	Start       time.Time
	End         time.Time
	Basedir     string
	Instruments []string

	data map[string]*dataset
}

// dataset returns the dataset for the given instrument represented by
// symbol
func (dm *DataManager) dataset(sym string) *dataset {
	return dm.data[sym]
}

// buildDatasets will produce the existing and missing datasets for
// each of the instruments. The missing files will need to be
// downloaded, the existing files can be checked for candles.  If the
// candles do not already exist then they will be created.
func (dm *DataManager) BuildDatasets(ctx context.Context) {
	if dm.data == nil {
		dm.data = make(map[string]*dataset)
		for _, sym := range dm.Instruments {
			dm.data[sym] = newDataset(sym, dm.Start, dm.End, dm.Basedir)
		}
	}

	candleQ := make(chan *datafile)
	dlQ := make(chan *datafile)
	defer close(candleQ)
	defer close(dlQ)

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

	for _, ds := range dm.data {
		go ds.buildDatafiles(ctx, candleQ, dlQ)
	}

	// wait until we recieve a done signal, when we do we'll close out
	<-ctx.Done()
}

// walk the missing datafiles for each of the symbols datasets and
// queue them up for download.
func (dm *DataManager) buildCandles(df *datafile) {
	fmt.Printf("Build candle from: %s\n", df.Path())
}
