package data

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// DataManager is responsible for identifing data files that are
// missing accross all instruments. For missing datasets, ensure they
// are downloaded, for datasets that are downloaded, make sure they
// are made into candles.
type DataManager struct {
	Start       time.Time
	End         time.Time
	Basedir     string
	Instruments []string
	*downloader

	data map[string]*dataset
}

// Init will get DataManager ready to go.
func (dm *DataManager) Init() {
	if dm.data == nil {
		dm.data = make(map[string]*dataset)
		for _, sym := range dm.Instruments {
			dm.data[sym] = newDataset(sym, dm.Start, dm.End, dm.Basedir)
		}
	}

	if dm.downloader == nil {
		dm.downloader = &downloader{
			Client: newHTTPClient(),
		}
	}
}

// buildDatasets will produce the existing and missing datasets for
// each of the instruments. The missing files will need to be
// downloaded, the existing files can be checked for candles.  If the
// candles do not already exist then they will be created.
func (dm *DataManager) BuildDatasets(ctx context.Context) {
	dm.Init()

	// Buffered channels help reduce scheduling stalls when producers are fast.
	// Start worker pools FIRST so producers can enqueue immediately.
	candleQ := make(chan *datafile, 1024)
	candleWG := dm.startCandleMaker(ctx, candleQ) // e.g. 4 candle builders

	dlQ := make(chan *datafile, 1024)
	dlWG := dm.downloader.startDownloader(ctx, dlQ, candleQ) // e.g. 8 downloaders

	// Producers: scan/build datafiles and enqueue work.
	var prodWG sync.WaitGroup
	for _, ds := range dm.data {
		ds := ds // IMPORTANT: capture loop var
		prodWG.Add(1)
		go func() {
			defer prodWG.Done()
			ds.buildDatafiles(ctx, candleQ, dlQ)
		}()
	}

	prodWG.Wait()
	close(dlQ)     // no more download jobs coming from producers
	dlWG.Wait()    // wait for downloads to finish (and enqueue candles)
	close(candleQ) // now safe: no more candle jobs will be produced
	candleWG.Wait()
}

// ValidateDatasets will walk the entire directory tree, identify invalid
// datafiles then delete them
func (dm *DataManager) populateFromPath(ctx context.Context, tickQ chan *datafile) error {
	var valid, invalid int

	// Call filepath.WalkDir with the root and an anonymous callback function
	err := filepath.WalkDir(dm.Basedir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Handle the error for the specific path, but continue the walk
			fmt.Printf("preventing error at path %s: %v\n", path, err)
			return err
		}

		// Check if it's a file and print its path
		if d.IsDir() {
			return nil
		}

		df, err := datafileFromPath(path)
		if err != nil {
			return nil
		}

		if !df.Exists() {
			return nil
		}

		if df.bytes == 0 {
			return nil
		}

		if err = df.IsValid(ctx); err != nil {
			invalid++
			fmt.Printf("removing invalid %s - %s\n", path, err)
			os.Remove(path)
			return nil
		}

		tickQ <- df
		valid++
		return nil // Returning nil continues the walk
	})

	if err != nil {
		log.Fatalf("error walking the path %s: %v\n", dm.Basedir, err)
		return err
	}
	fmt.Printf("valid: %d / invalid %d\n", valid, invalid)
	return nil
}

func (dm *DataManager) Validate(ctx context.Context) error {
	q := make(chan *datafile)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return

			case <-q:
				fmt.Println("valid!")
			}
		}
	}()

	if err := dm.populateFromPath(ctx, q); err != nil {
		return err
	}
	return nil
}

// walk the missing datafiles for each of the symbols datasets and
// queue them up for download.
func (dm *DataManager) BuildCandles(ctx context.Context) error {

	// Buffered channels help reduce scheduling stalls when producers are fast.
	// Start worker pools FIRST so producers can enqueue immediately.

	tickq := make(chan *datafile, 1024)
	wg := dm.startCandleMaker(ctx, tickq) // e.g. 4 candle builders
	err := dm.populateFromPath(ctx, tickq)
	if err != nil {
		return err
	}

	wg.Wait()
	return nil
}
