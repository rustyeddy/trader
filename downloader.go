package trader

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"
)

type downloader struct {
	*http.Client
	downloaders int
}

type saveResult struct {
	Key       Key
	Path      string
	Size      int64
	Exists    bool
	Complete  bool
	UpdatedAt time.Time
}

func NewDownloader() *downloader {
	return &downloader{
		Client:      newHTTPClient(),
		downloaders: 8,
	}
}

func (dl *downloader) download(ctx context.Context, key Key) (*saveResult, error) {
	df := newDatafile(key.Instrument, key.Time())

	// Correctness-first timeout
	reqCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	url := df.URL()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}

	// resp, err := http.DefaultClient.Do(req)
	resp, err := dl.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: http %d", url, resp.StatusCode)
	}

	// TODO 1. Log error, 2. Makesure inventory is updated
	path, err := store.SaveFile(key, resp.Body)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}

	result := &saveResult{
		Key:       key,
		Path:      path,
		Size:      info.Size(),
		Exists:    true,
		Complete:  true,
		UpdatedAt: info.ModTime(),
	}
	return result, nil
}

// runDownloadPool starts N workers that read from dlQ until dlQ is closed
// or ctx is cancelled. It returns a WaitGroup you can Wait() on.
func (dl *downloader) startDownloader(ctx context.Context, dm *DataManager, dlQ <-chan Key) *sync.WaitGroup {
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
					result, err := dl.download(ctx, key)
					if err != nil {
						fmt.Printf("\terror downloading %+v: %v\n", key, err)
						continue
					}
					dm.inventory.Put(Asset{
						Key:       result.Key,
						Path:      result.Path,
						Exists:    result.Exists,
						Complete:  result.Size > 0,
						Size:      result.Size,
						UpdatedAt: result.UpdatedAt,
						Range:     key.Range(),
					})
				case <-ctx.Done():
					return
				}
			}
		}(i)
	}

	return &wg
}
