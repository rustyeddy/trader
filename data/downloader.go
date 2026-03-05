package data

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"
)

type downloader struct {
	*http.Client
	downloaders  int
	candleMakers int
}

// runDownloadPool starts N workers that read from dlQ until dlQ is closed
// or ctx is cancelled. It returns a WaitGroup you can Wait() on.
func (dl *downloader) startDownloader(ctx context.Context, dlQ <-chan *datafile, candleQ chan<- *datafile) *sync.WaitGroup {
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
				case df, ok := <-dlQ:
					if !ok {
						return // channel closed, we're done
					}

					if err := df.download(ctx, dl.Client); err != nil {
						df.err = err
						fmt.Printf("\terror downloading %s: %v\n", df.Path(), err)
						continue
					}
					if df.bytes == 0 {
						continue
					}
					select {
					case <-ctx.Done():
						return
					case candleQ <- df:
					}
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
		go func() {
			defer wg.Done()

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

					m1.WriteCSV(".")
					h1, err := m1.Aggregate(3600, "Dukascopy H1 (from M1)")
					if err != nil {
						panic(err)
					}
					h1.WriteCSV(".")

					d1, err := h1.Aggregate(86400, "Dukascopy D1 (from H1)")
					if err != nil {
						panic(err)
					}
					d1.WriteCSV(".")

					// candles.PrintStats(os.Stdout)
					// fmt.Printf("Candle count: %d\n", len(candles.Candles))
					// fmt.Printf("+v\n", candles)
				}
			}
		}()
	}

	return &wg
}
