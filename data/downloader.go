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

// func (dl *downloader) download(ctx context.Context, dlQ chan *datafile) {
// 	go func() {
// 		for df := range dlQ {
// 			err := df.download(ctx, dl.Client)
// 			if err != nil {
// 				df.err = err
// 				fmt.Printf(" ERROR downloading %s\n", df.Path())
// 			}
// 		}
// 	}()
// }

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
					candles, err := df.buildM1(ctx)
					if err != nil {
						fmt.Printf("\terror building candle %s: %v\n", df.Path(), err)
						df.err = err
						df.Time = time.Time{}
						df.bytes = 0
						df.modtime = time.Time{}
						continue
					}
					_ = candles
					// TODO: ADD CANDLESET HERE
				}
			}
		}()
	}

	return &wg
}
