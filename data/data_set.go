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

func (ds *dataset) buildDatafiles(ctx context.Context, candleQ, dlQ chan *datafile) {
	duration := ds.end.Sub(ds.start)
	hours := duration.Hours()
	ds.datafiles = make([]*datafile, 0, int(hours)+1)

	// for t := ds.start; !t.After(ds.end); t = t.Add(time.Hour) {
	for t := ds.end; !t.Before(ds.start); t = t.Add(-time.Hour) {
		df := datafile{
			symbol:  ds.symbol,
			Time:    t,
			basedir: ds.basedir,
		}
		ds.datafiles = append(ds.datafiles, &df)

		if df.Exists() {
			select {
			case <-ctx.Done():
				return
			case candleQ <- &df:

			}
		} else {
			select {
			case <-ctx.Done():
				return
			case dlQ <- &df:
			}
		}
	}
}
