package data

import (
	"context"
	"fmt"
	"net/http"
	"sync"
)

type Downloader struct {
	*http.Client
	downloaders int
	basePath    string
}

func NewDownloader(basedir string) *Downloader {
	return &Downloader{
		Client:      newHTTPClient(),
		downloaders: 8,
		basePath:    basedir,
	}
}

func (dl *Downloader) download(ctx context.Context, key Key) error {
	df := newDatafile(dl.basePath, key.Instrument, key.Time())

	if err := df.download(ctx, dl.Client); err != nil {
		df.err = err
		return fmt.Errorf("download %s: %w", df.Path(), err)
	}

	return nil
}

// runDownloadPool starts N workers that read from dlQ until dlQ is closed
// or ctx is cancelled. It returns a WaitGroup you can Wait() on.
func (dl *Downloader) startDownloader(ctx context.Context, dlQ <-chan Key) *sync.WaitGroup {
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
					if err := dl.download(ctx, key); err != nil {
						fmt.Printf("\terror downloading %+v: %v\n", key, err)
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
