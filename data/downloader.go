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
	downloaders int
}

func NewDownloader() *downloader {
	return &downloader{
		Client:      newHTTPClient(),
		downloaders: 8,
	}
}

// TODO move df.download() to here
func (dl *downloader) downloadOld(ctx context.Context, key Key) error {

	// if err := df.download(ctx, dl.Client); err != nil {
	// 	df.err = err
	// 	return fmt.Errorf("download %s: %w", store.PathForAsset(df.key), err)
	// }

	return nil
}

func (dl *downloader) download(ctx context.Context, key Key) error {
	df := newDatafile(key.Instrument, key.Time())

	// Correctness-first timeout
	reqCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	url := df.URL()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}

	// resp, err := http.DefaultClient.Do(req)
	resp, err := dl.Client.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: http %d", url, resp.StatusCode)
	}

	// TODO 1. Log error, 2. Makesure inventory is updated
	err = store.SaveFile(key, resp.Body)
	return err
}

// runDownloadPool starts N workers that read from dlQ until dlQ is closed
// or ctx is cancelled. It returns a WaitGroup you can Wait() on.
func (dl *downloader) startDownloader(ctx context.Context, dlQ <-chan Key) *sync.WaitGroup {
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
