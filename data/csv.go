package data

import (
	"encoding/csv"
	"os"
	"strconv"

	"github.com/rustyeddy/trader/market"
)

type M1CSVWriter struct {
	f *os.File
	w *csv.Writer
}

func newM1CSVWriter(path string) (*M1CSVWriter, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}

	w := csv.NewWriter(f)
	if err := w.Write([]string{"timestamp", "open", "high", "low", "close", "volume"}); err != nil {
		_ = f.Close()
		return nil, err
	}

	return &M1CSVWriter{
		f: f,
		w: w,
	}, nil
}

func (w *M1CSVWriter) Write(c market.Candle) error {
	return w.w.Write([]string{
		// TODO need to get the time value to write to csv?
		// strconv.FormatInt(c.Timestamp, 10),
		strconv.FormatInt(int64(c.Open), 10),
		strconv.FormatInt(int64(c.High), 10),
		strconv.FormatInt(int64(c.Low), 10),
		strconv.FormatInt(int64(c.Close), 10),
		strconv.FormatInt(int64(c.AvgSpread), 10),
		strconv.FormatInt(int64(c.MaxSpread), 10),
		strconv.FormatInt(int64(c.Ticks), 10),
	})
}

func (w *M1CSVWriter) Close() error {
	w.w.Flush()
	if err := w.w.Error(); err != nil {
		_ = w.f.Close()
		return err
	}
	return w.f.Close()
}
