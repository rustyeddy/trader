package data

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"sync"
	"time"
)

type Limiter struct{ ch <-chan time.Time }

func NewLimiter(reqPerSec int) *Limiter {
	if reqPerSec < 1 {
		reqPerSec = 1
	}
	return &Limiter{ch: time.NewTicker(time.Second / time.Duration(reqPerSec)).C}
}
func (l *Limiter) Wait(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-l.ch:
		return nil
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

type Result struct {
	File *datafile
	Err  error
}

func DownloadFiles(ctx context.Context, files []*datafile, workers, reqPerSec int) error {
	client := newHTTPClient()
	lim := NewLimiter(reqPerSec)

	jobs := make(chan *datafile, workers*4)
	results := make(chan Result, workers*4)

	var wg sync.WaitGroup
	wg.Add(workers)

	for i := 0; i < workers; i++ {
		go func(workerID int) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(time.Now().UnixNano() + int64(workerID)))

			for f := range jobs {
				// Skip if already valid
				if f.Exists() {
					results <- Result{File: f, Err: nil}
					continue
				}

				var err error
				for attempt := 0; attempt < 8; attempt++ {
					if err = lim.Wait(ctx); err != nil {
						results <- Result{File: f, Err: err}
						return
					}

					// slightly longer timeout than 30s helps when server is slow
					reqCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
					err = f.download(reqCtx, client)
					cancel()

					if err == nil {
						break
					}
					if !errors.Is(err, ErrRetryable) {
						// non-retryable
						break
					}

					// backoff + jitter
					backoff := time.Duration(300*(1<<attempt)) * time.Millisecond
					if backoff > 30*time.Second {
						backoff = 30 * time.Second
					}
					jitter := time.Duration(rng.Int63n(int64(backoff / 2)))
					sleep := backoff + jitter

					select {
					case <-ctx.Done():
						results <- Result{File: f, Err: ctx.Err()}
						return
					case <-time.After(sleep):
					}
				}

				results <- Result{File: f, Err: err}
			}
		}(i)
	}

	go func() {
		defer close(jobs)
		for _, f := range files {
			select {
			case <-ctx.Done():
				return
			case jobs <- f:
			}
		}
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect + report
	var ok, skipped, failed int
	for r := range results {
		if r.Err == nil {
			ok++
			continue
		}
		failed++
		fmt.Printf("FAIL: %s (%v)\n", r.File.URL(), r.Err)
	}

	fmt.Printf("done: ok=%d failed=%d, skipped=%d\n", ok, failed, skipped)
	if failed > 0 {
		return fmt.Errorf("download finished with %d failures", failed)
	}
	return nil
}

func BuildHourJobs(base, instr string, start, end time.Time) []*datafile {
	var out []*datafile
	t := start.UTC().Truncate(time.Hour)
	end = end.UTC().Truncate(time.Hour)

	for t.Before(end) {
		out = append(out, newDatafile(base, instr, t)) // you implement
		t = t.Add(time.Hour)
	}
	return out
}
