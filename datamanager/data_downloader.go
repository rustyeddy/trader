package datamanager

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"
)

type downloader struct {
	client  *http.Client
	workers int
}

const (
	defaultDownloadWorkers = 8
	downloadRequestTimeout = 120 * time.Second
)

func NewDownloader() *downloader {
	return &downloader{
		client:  newHTTPClient(),
		workers: defaultDownloadWorkers,
	}
}

func newHTTPClient() *http.Client {
	tr := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          200,
		MaxIdleConnsPerHost:   200,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	return &http.Client{
		Transport: tr,
		Timeout:   0, // use per-request ctx timeout
	}
}

func (dl *downloader) httpClient() *http.Client {
	if dl.client == nil {
		dl.client = newHTTPClient()
	}
	return dl.client
}

func (dl *downloader) workerCount() int {
	if dl.workers <= 0 {
		return defaultDownloadWorkers
	}
	return dl.workers
}

func (dl *downloader) download(ctx context.Context, key Key) (Asset, error) {
	provider, err := Get(key.Source)
	if err != nil {
		return Asset{}, fmt.Errorf("download %+v: %w", key, err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, downloadRequestTimeout)
	defer cancel()

	url := provider.SourceURL(SourceParams{
		Instrument: key.Instrument,
		Time:       key.Time(),
		Timeframe:  "tick",
	})
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return Asset{}, fmt.Errorf("new request %s: %w", url, err)
	}

	resp, err := dl.httpClient().Do(req)
	if err != nil {
		return Asset{}, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return Asset{}, fmt.Errorf("GET %s: http %d", url, resp.StatusCode)
	}

	path, err := globalStore.SaveFile(key, resp.Body)
	if err != nil {
		return Asset{}, fmt.Errorf("save %+v: %w", key, err)
	}

	info, err := os.Stat(path)
	if err != nil {
		return Asset{}, fmt.Errorf("stat %s: %w", path, err)
	}

	rng, _ := key.Range()
	return Asset{
		Key:       key,
		Path:      path,
		Size:      info.Size(),
		Exists:    true,
		Complete:  info.Size() > 0,
		UpdatedAt: info.ModTime(),
		Range:     rng,
		Flags:     FlagUsable,
	}, nil
}

func downloadFailureAsset(key Key, err error) Asset {
	path, pathErr := globalStore.KeyPath(key)
	if pathErr != nil {
		path = ""
	}
	rng, _ := key.Range()
	return Asset{
		Key:    key,
		Path:   path,
		Range:  rng,
		Reason: err.Error(),
		Flags:  FlagDownloadFailed,
	}
}

// runDownloadPool starts N workers that read from dlQ until dlQ is closed
// or ctx is cancelled. It returns a WaitGroup you can Wait() on.
func (dl *downloader) startDownloader(ctx context.Context, dm *DataManager, dlQ <-chan Key) *sync.WaitGroup {
	workers := dl.workerCount()

	var wg sync.WaitGroup
	wg.Add(workers)

	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()

			for {
				select {
				case <-ctx.Done():
					return
				case key, ok := <-dlQ:
					if !ok {
						return
					}
					asset, err := dl.download(ctx, key)
					if err != nil {
						if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
							return
						}
						dm.inventory.Put(downloadFailureAsset(key, err))
						continue
					}
					dm.inventory.Put(asset)
				}
			}
		}()
	}

	return &wg
}
