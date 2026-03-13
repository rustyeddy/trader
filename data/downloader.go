package data

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"
)

type Downloader struct {
	*http.Client
	downloaders  int
	candleMakers int
	basePath     string
}

func NewDownloader(basedir string) *Downloader {
	return &Downloader{
		Client:       newHTTPClient(),
		downloaders:  8,
		candleMakers: 4,
		basePath:     basedir,
	}
}

// runDownloadPool starts N workers that read from dlQ until dlQ is closed
// or ctx is cancelled. It returns a WaitGroup you can Wait() on.
func (dl *Downloader) startDownloader(ctx context.Context, dlQ <-chan AssetKey) *sync.WaitGroup {
	if dl.downloaders <= 0 {
		dl.downloaders = 8
	}

	var wg sync.WaitGroup
	wg.Add(dl.downloaders)

	for i := 0; i < dl.downloaders; i++ {
		go func(workerID int) {
			defer wg.Done()

			for {
				select {
				case <-ctx.Done():
					return
				case key, ok := <-dlQ:
					if !ok {
						return // channel closed, we're done
					}
					df := newDatafile(dl.basePath, key.Instrument, key.Time())
					if err := df.download(ctx, dl.Client); err != nil {
						df.err = err
						fmt.Printf("\terror downloading %s: %v\n", df.Path(), err)
						continue
					}
				case <-ctx.Done():
					return
				}
			}
		}(i)
	}

	return &wg
}

func (dm *DataManager) startCandleMaker(ctx context.Context, candleQ <-chan *datafile) *sync.WaitGroup {
	if dm.candleMakers <= 0 {
		dm.candleMakers = 4
	}

	var wg sync.WaitGroup
	wg.Add(dm.candleMakers)

	for i := 0; i < dm.candleMakers; i++ {

		// move this to datafile.go
		go func() {
			defer wg.Done()

			cstore := CandleStore{
				Basedir: "../../tmp",
				Source:  "dukas",
			}
			for {
				select {
				case <-ctx.Done():
					return
				case df, ok := <-candleQ:
					if !ok || df.bytes == 0 {
						return
					}

					m1, err := df.buildM1(ctx)
					if err != nil {
						fmt.Printf("\terror building candle %s: %v\n", df.Path(), err)
						df.err = err
						df.Time = time.Time{}
						df.bytes = 0
						df.modtime = time.Time{}
						continue
					}
					// writeQ <- m1
					fmt.Println(cstore.CandlePath(m1.Instrument.Name, "M1", m1.Time(1).Year()))
				}
			}
		}()
	}

	return &wg
}
