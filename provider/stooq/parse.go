package stooq

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
)

// Source is the provider's name in data paths.
const Source = "stooq"

var (
	// ErrMalformedData reports a file that is not Stooq daily data: a wrong
	// header, a row with the wrong number of fields, a date or number that is not
	// one.
	ErrMalformedData = errors.New("stooq: malformed data")
	// ErrUnsupportedSeries reports rows of another series than the one asked for:
	// another ticker, or a period other than daily.
	ErrUnsupportedSeries = errors.New("stooq: unsupported series")
)

const header = "<TICKER>,<PER>,<DATE>,<TIME>,<OPEN>,<HIGH>,<LOW>,<CLOSE>,<VOL>,<OPENINT>"

// Parse maps the daily history of the US symbol (such as "SPY") to one partition
// per calendar month of the rows' dates, in month order, each with its rows in
// file order. A row keeps its 1-based position among the file's data rows, and a
// partition's fingerprint is of the exact bytes of its rows, line endings
// included.
//
// Each row becomes a marketdata.SourceRow of the trade series, complete, with Time
// at midnight UTC of its date. A row whose price or volume cannot be held exactly
// is passed on as Unrepresentable, and order, duplicates and the shape of a bar
// are left to the market-data service. Anything that cannot be read at all fails
// the whole file.
func Parse(symbol string, data []byte) ([]marketdata.SourcePartition, error) {
	if symbol == "" {
		return nil, fmt.Errorf("%w: no symbol", ErrUnsupportedSeries)
	}
	lines := bytes.SplitAfter(data, []byte("\n"))
	if n := len(lines); n > 0 && len(lines[n-1]) == 0 {
		lines = lines[:n-1] // the file ends with a newline
	}
	if len(lines) == 0 || trimEOL(lines[0]) != header {
		return nil, fmt.Errorf("%w: the first line is not %s", ErrMalformedData, header)
	}
	if len(lines) == 1 {
		return nil, fmt.Errorf("%w: no data rows", ErrMalformedData)
	}
	ticker := symbol + ".US"
	byMonth := map[time.Time]*marketdata.SourcePartition{}
	raw := map[time.Time][]byte{}
	for i, line := range lines[1:] {
		n := i + 1
		row, err := parseRow(ticker, trimEOL(line))
		if err != nil {
			return nil, fmt.Errorf("data row %d: %w", n, err)
		}
		row.Row, row.Bar.Instrument = n, symbol
		month := time.Date(row.Bar.Time.Year(), row.Bar.Time.Month(), 1, 0, 0, 0, 0, time.UTC)
		p := byMonth[month]
		if p == nil {
			p = &marketdata.SourcePartition{Source: Source, Instrument: symbol, Interval: "d1", Year: month.Year(), Month: month.Month()}
			byMonth[month] = p
		}
		p.Rows = append(p.Rows, row)
		raw[month] = append(raw[month], line...)
	}
	months := make([]time.Time, 0, len(byMonth))
	for m := range byMonth {
		months = append(months, m)
	}
	slices.SortFunc(months, time.Time.Compare)
	out := make([]marketdata.SourcePartition, 0, len(months))
	for _, m := range months {
		p := byMonth[m]
		sum := sha256.Sum256(raw[m])
		p.Fingerprint = "sha256:" + hex.EncodeToString(sum[:])
		out = append(out, *p)
	}
	return out, nil
}

func trimEOL(line []byte) string {
	return strings.TrimSuffix(strings.TrimSuffix(string(line), "\n"), "\r")
}

// parseRow reads one data row of ticker.
func parseRow(ticker, line string) (marketdata.SourceRow, error) {
	var none marketdata.SourceRow
	f := strings.Split(line, ",")
	if len(f) != 10 {
		return none, fmt.Errorf("%w: %d fields, want 10", ErrMalformedData, len(f))
	}
	switch {
	case f[0] != ticker:
		return none, fmt.Errorf("%w: ticker %q, want %s", ErrUnsupportedSeries, f[0], ticker)
	case f[1] != "D" || f[3] != "000000":
		return none, fmt.Errorf("%w: period %q at time %q, want daily (D, 000000)", ErrUnsupportedSeries, f[1], f[3])
	case f[9] != "0":
		return none, fmt.Errorf("%w: open interest %q, want 0 for a share series", ErrUnsupportedSeries, f[9])
	}
	date, err := time.Parse("20060102", f[2])
	if err != nil {
		return none, fmt.Errorf("%w: date %q is not YYYYMMDD", ErrMalformedData, f[2])
	}
	row := marketdata.SourceRow{Complete: true, Bar: marketdata.Bar{Time: date}}
	var prices [4]num.Price
	for k := range prices {
		p, err := num.ParsePrice(f[4+k])
		switch {
		case errors.Is(err, num.ErrPrecision), errors.Is(err, num.ErrRange), errors.Is(err, num.ErrNegative):
			row.Unrepresentable = true
		case err != nil:
			return none, fmt.Errorf("%w: price %q: %w", ErrMalformedData, f[4+k], err)
		}
		prices[k] = p
	}
	volume, whole, err := parseVolume(f[8])
	if err != nil {
		return none, err
	}
	if row.Unrepresentable || !whole {
		row.Unrepresentable = true
		return row, nil
	}
	row.Bar.Volume = volume
	row.Bar.Trade = marketdata.OHLC{Open: prices[0], High: prices[1], Low: prices[2], Close: prices[3]}
	return row, nil
}

// parseVolume reads a count of shares: a decimal number. whole is false when it is
// one that an int64 count cannot hold exactly, a fraction or a value out of range.
func parseVolume(s string) (volume int64, whole bool, err error) {
	digits, frac, point := strings.Cut(strings.TrimPrefix(s, "-"), ".")
	if digits == "" || point && frac == "" || strings.Trim(digits+frac, "0123456789") != "" {
		return 0, false, fmt.Errorf("%w: volume %q is not a number", ErrMalformedData, s)
	}
	if strings.Trim(frac, "0") != "" {
		return 0, false, nil
	}
	text, _, _ := strings.Cut(s, ".")
	v, perr := strconv.ParseInt(text, 10, 64)
	if perr != nil {
		return 0, false, nil // only a range error is left
	}
	return v, true, nil
}
