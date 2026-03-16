package data

import (
	"context"
	"net/http"
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

func BuildHourJobs(base, instr string, start, end time.Time) []*datafile {
	var out []*datafile
	t := start.UTC().Truncate(time.Hour)
	end = end.UTC().Truncate(time.Hour)

	for t.Before(end) {
		out = append(out, newDatafile(instr, t)) // you implement
		t = t.Add(time.Hour)
	}
	return out
}
