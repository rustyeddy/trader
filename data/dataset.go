package data

import (
	"context"
	"errors"
	"time"
)

type dataset struct {
	symbol    string    // EURUSD, USDJPY, etc.
	start     time.Time // 1/1/2003
	end       time.Time // time.Now
	datafiles []*datafile

	// maybe belong to client and filesystem
	basedir string // root where data is to be stored (need this here?)
	baseurl string // base url for the data
}

const defaultBase = "https://datafeed.dukascopy.com/datafeed"

var ErrRetryable = errors.New("retryable")

func newDataset(sym string, start, end time.Time, basedir string) *dataset {
	if start.After(end) {
		panic("start data is after the end date")
	}
	if end.After(time.Now()) {
		panic("end date is in the future")
	}
	return &dataset{
		symbol:  sym,
		start:   start,
		end:     end,
		basedir: basedir,
	}
}

func (ds *dataset) buildDatafiles(ctx context.Context, candleQ chan<- *datafile, dlQ chan<- *datafile) {
	duration := ds.end.Sub(ds.start)
	hours := int(duration.Hours())
	ds.datafiles = make([]*datafile, 0, hours+1)

	for t := ds.end; !t.Before(ds.start); t = t.Add(-time.Hour) {
		df := newDatafile(ds.basedir, ds.symbol, t) // <-- new pointer each loop
		ds.datafiles = append(ds.datafiles, df)

		// IsValid will remove the file if it is true
		if df.IsValid(ctx) == nil {
			if df.bytes == 0 {
				continue
			}
			select {
			case <-ctx.Done():
				return
			case candleQ <- df:
			}
		} else {
			select {
			case <-ctx.Done():
				return
			case dlQ <- df:
			}
		}
	}
}
