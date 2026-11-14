package canonical

import (
	"bufio"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/rustyeddy/trader/num"
)

// columns are the canonical-v2 column names, in order.
var columns = []string{"time", "available_at",
	"bid_o", "bid_h", "bid_l", "bid_c", "ask_o", "ask_h", "ask_l", "ask_c", "mid_o", "mid_h", "mid_l", "mid_c", "volume"}

// The fixed number of decimals prices are written with: five for bid and ask, as
// the raw store does, and six for mid, since the midpoint of two five-decimal
// prices can end in a half pip. An absent series is written as empty fields.
const (
	priceDecimals = 5
	midDecimals   = 6
)

// metaJSON is the "# meta" line. Counts and the first and last bar are derived
// from the file's contents on write and checked against them on read.
type metaJSON struct {
	RawFingerprint     string `json:"raw_fingerprint"`
	BuilderVersion     string `json:"builder_version"`
	ValidatorVersion   string `json:"validator_version"`
	AvailabilityPolicy string `json:"availability_policy"`
	BuiltAt            string `json:"built_at"`
	BarCount           *int   `json:"bar_count"` // a pointer, so a missing or null count is detectable
	BadCount           *int   `json:"bad_count"`
	FirstBar           string `json:"first_bar,omitempty"`
	LastBar            string `json:"last_bar,omitempty"`
}

func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// parseTime reads an RFC 3339 instant that must be written in UTC ("Z").
func parseTime(s string) (time.Time, error) {
	if !strings.HasSuffix(s, "Z") {
		return time.Time{}, fmt.Errorf("time %q is not UTC (must end in Z)", s)
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("time %q: %v", s, err)
	}
	return t, nil
}

// formatPrice renders p as exact decimal text padded to the given decimals. A
// price with more precision is an error, never rounded.
func formatPrice(p num.Price, decimals int) (string, error) {
	whole, frac, _ := strings.Cut(p.String(), ".")
	if len(frac) > decimals {
		return "", fmt.Errorf("%w: price %s has more than %d decimals", ErrMalformedData, p, decimals)
	}
	return whole + "." + frac + strings.Repeat("0", decimals-len(frac)), nil
}

// parsePrice reads a price written with exactly the given decimals and digits only.
func parsePrice(s string, decimals int) (num.Price, error) {
	whole, frac, ok := strings.Cut(s, ".")
	if !ok || whole == "" || len(frac) != decimals || strings.Trim(whole+frac, "0123456789") != "" {
		return num.Price{}, fmt.Errorf("price %q: want digits with exactly %d decimals", s, decimals)
	}
	return num.ParsePrice(s)
}

// formatOHLC writes the four prices of one series, or four empty fields when the
// series is absent.
func formatOHLC(o OHLC, decimals int) ([]string, error) {
	if !o.Present() {
		return []string{"", "", "", ""}, nil
	}
	out := make([]string, 0, 4)
	for _, p := range []num.Price{o.Open, o.High, o.Low, o.Close} {
		s, err := formatPrice(p, decimals)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

// parseOHLC reads four fields as one series: all empty is an absent series, and
// a series with some fields empty is an error.
func parseOHLC(fields []string, decimals int) (OHLC, error) {
	empty := 0
	for _, f := range fields {
		if f == "" {
			empty++
		}
	}
	if empty == len(fields) {
		return OHLC{}, nil
	}
	if empty != 0 {
		return OHLC{}, fmt.Errorf("a price series is either all present or all empty, got %q", fields)
	}
	var p [4]num.Price
	for i, f := range fields {
		var err error
		if p[i], err = parsePrice(f, decimals); err != nil {
			return OHLC{}, err
		}
	}
	return OHLC{p[0], p[1], p[2], p[3]}, nil
}

// Encode writes f in canonical-v2 form after validating it: the schema comment,
// the "# meta" line, one "# bad" line per rejected record, the column header and
// one row per bar.
func Encode(ctx context.Context, w io.Writer, f File) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := f.Validate(); err != nil {
		return err
	}
	// A bufio.Writer keeps its first write error and returns it from Flush, so
	// the individual writes below need not be checked.
	bw := bufio.NewWriter(w)
	k := f.Key
	_, _ = fmt.Fprintf(bw, "# schema=%s provider=%s symbol=%s interval=%s year=%04d month=%02d\n",
		Schema, k.Source, k.Symbol, string(k.Interval), k.Year, int(k.Month))

	barCount, badCount := len(f.Bars), len(f.Bad)
	meta := metaJSON{
		RawFingerprint: f.Meta.RawFingerprint, BuilderVersion: f.Meta.BuilderVersion,
		ValidatorVersion: f.Meta.ValidatorVersion, AvailabilityPolicy: f.Meta.AvailabilityPolicy,
		BuiltAt: formatTime(f.Meta.BuiltAt), BarCount: &barCount, BadCount: &badCount,
	}
	if n := len(f.Bars); n > 0 {
		meta.FirstBar, meta.LastBar = formatTime(f.Bars[0].Time), formatTime(f.Bars[n-1].Time)
	}
	js, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("canonical: encode meta: %w", err)
	}
	_, _ = fmt.Fprintf(bw, "# meta %s\n", js)
	for _, b := range f.Bad {
		_, _ = fmt.Fprintf(bw, "# bad time=%s reason=%s raw_row=%d\n", formatTime(b.Time), string(b.Reason), b.RawRow)
	}
	_, _ = fmt.Fprintln(bw, strings.Join(columns, ","))
	for _, b := range f.Bars {
		if err := ctx.Err(); err != nil {
			return err
		}
		row := []string{formatTime(b.Time), formatTime(b.AvailableAt)}
		for _, s := range []struct {
			o        OHLC
			decimals int
		}{{b.Bid, priceDecimals}, {b.Ask, priceDecimals}, {b.Mid, midDecimals}} {
			fields, err := formatOHLC(s.o, s.decimals)
			if err != nil {
				return err
			}
			row = append(row, fields...)
		}
		row = append(row, strconv.FormatInt(b.Volume, 10))
		_, _ = fmt.Fprintln(bw, strings.Join(row, ","))
	}
	if err := bw.Flush(); err != nil {
		return fmt.Errorf("canonical: encode: %w", err)
	}
	return nil
}

func malformed(line int, format string, args ...any) error {
	return fmt.Errorf("%w: line %d: %s", ErrMalformedData, line, fmt.Sprintf(format, args...))
}

// Decode reads one canonical-v2 file. It is strict: an unknown or missing header
// field, an unknown comment line, a row that does not parse, counts that
// disagree with the metadata, or any breach of File.Validate is ErrMalformedData.
// The returned File carries the good bars and the bad records both.
func Decode(r io.Reader) (File, error) {
	br := bufio.NewReader(r)
	n := 0
	next := func() (string, error) {
		n++
		line, err := br.ReadString('\n')
		if err != nil && (!errors.Is(err, io.EOF) || line == "") {
			if errors.Is(err, io.EOF) {
				return "", malformed(n, "unexpected end of file")
			}
			return "", fmt.Errorf("canonical: read: %w", err)
		}
		return strings.TrimRight(line, "\r\n"), nil
	}

	first, err := next()
	if err != nil {
		return File{}, err
	}
	key, err := parseSchemaLine(first)
	if err != nil {
		return File{}, malformed(1, "%v", err)
	}

	f := File{Key: key}
	var meta metaJSON
	haveMeta := false
	var header string
	for {
		line, err := next()
		if err != nil {
			return File{}, err
		}
		if !strings.HasPrefix(line, "#") {
			header = line
			break
		}
		switch {
		case strings.HasPrefix(line, "# meta "):
			if haveMeta {
				return File{}, malformed(n, "more than one meta line")
			}
			dec := json.NewDecoder(strings.NewReader(strings.TrimPrefix(line, "# meta ")))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&meta); err != nil {
				return File{}, malformed(n, "meta: %v", err)
			}
			if _, err := dec.Token(); !errors.Is(err, io.EOF) {
				return File{}, malformed(n, "meta: unexpected content after the metadata object")
			}
			haveMeta = true
		case strings.HasPrefix(line, "# bad "):
			b, err := parseBadLine(strings.TrimPrefix(line, "# bad "))
			if err != nil {
				return File{}, malformed(n, "%v", err)
			}
			f.Bad = append(f.Bad, b)
		default:
			return File{}, malformed(n, "unknown comment %q", line)
		}
	}
	if !haveMeta {
		return File{}, malformed(n, "no meta line before the column header")
	}
	if header != strings.Join(columns, ",") {
		return File{}, malformed(n, "columns %q, want %q", header, strings.Join(columns, ","))
	}
	builtAt, err := parseTime(meta.BuiltAt)
	if err != nil {
		return File{}, malformed(2, "meta built_at: %v", err)
	}
	f.Meta = Meta{RawFingerprint: meta.RawFingerprint, BuilderVersion: meta.BuilderVersion,
		ValidatorVersion: meta.ValidatorVersion, AvailabilityPolicy: meta.AvailabilityPolicy, BuiltAt: builtAt}

	cr := csv.NewReader(br)
	cr.FieldsPerRecord = len(columns)
	for {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			// A failed Read has no fields, so FieldPos would panic; a parse
			// error carries its own line.
			var pe *csv.ParseError
			line := 0
			if errors.As(err, &pe) {
				line = pe.StartLine
			}
			return File{}, malformed(n+line, "%v", err)
		}
		line, _ := cr.FieldPos(0)
		b, err := parseBar(rec)
		if err != nil {
			return File{}, malformed(n+line, "%v", err)
		}
		f.Bars = append(f.Bars, b)
	}

	if err := checkMetaCounts(meta, f); err != nil {
		return File{}, err
	}
	if err := f.Validate(); err != nil {
		return File{}, err
	}
	return f, nil
}

func checkMetaCounts(m metaJSON, f File) error {
	if m.BarCount == nil || m.BadCount == nil {
		return fmt.Errorf("%w: meta must give bar_count and bad_count", ErrMalformedData)
	}
	if *m.BarCount != len(f.Bars) || *m.BadCount != len(f.Bad) {
		return fmt.Errorf("%w: meta says %d bars and %d bad records, the file has %d and %d",
			ErrMalformedData, *m.BarCount, *m.BadCount, len(f.Bars), len(f.Bad))
	}
	var first, last string
	if n := len(f.Bars); n > 0 {
		first, last = formatTime(f.Bars[0].Time), formatTime(f.Bars[n-1].Time)
	}
	if m.FirstBar != first || m.LastBar != last {
		return fmt.Errorf("%w: meta first/last bar %q/%q, the file has %q/%q", ErrMalformedData, m.FirstBar, m.LastBar, first, last)
	}
	return nil
}

// parseSchemaLine reads "# schema=canonical-v2 provider=... symbol=... interval=...
// year=... month=..." and requires exactly those fields.
func parseSchemaLine(line string) (Key, error) {
	rest, ok := strings.CutPrefix(line, "# ")
	if !ok {
		return Key{}, fmt.Errorf("first line %q is not a schema comment", line)
	}
	got := map[string]string{}
	for _, f := range strings.Fields(rest) {
		k, v, ok := strings.Cut(f, "=")
		if !ok || got[k] != "" {
			return Key{}, fmt.Errorf("schema field %q is malformed or repeated", f)
		}
		got[k] = v
	}
	if got["schema"] != Schema {
		return Key{}, fmt.Errorf("schema %q, want %s", got["schema"], Schema)
	}
	if len(got) != 6 {
		return Key{}, fmt.Errorf("schema line needs exactly schema, provider, symbol, interval, year and month")
	}
	year, err1 := strconv.Atoi(got["year"])
	month, err2 := strconv.Atoi(got["month"])
	if err1 != nil || err2 != nil || got["provider"] == "" || got["symbol"] == "" {
		return Key{}, fmt.Errorf("schema line has a missing or non-numeric field")
	}
	return Key{Source: got["provider"], Symbol: got["symbol"], Interval: Interval(got["interval"]),
		Year: year, Month: time.Month(month)}, nil
}

// parseBadLine reads "time=... reason=... raw_row=...".
func parseBadLine(s string) (BadRecord, error) {
	got := map[string]string{}
	for _, f := range strings.Fields(s) {
		k, v, ok := strings.Cut(f, "=")
		if !ok || got[k] != "" {
			return BadRecord{}, fmt.Errorf("bad-record field %q is malformed or repeated", f)
		}
		got[k] = v
	}
	if len(got) != 3 {
		return BadRecord{}, errors.New("a bad record needs exactly time, reason and raw_row")
	}
	t, err := parseTime(got["time"])
	if err != nil {
		return BadRecord{}, err
	}
	row, err := strconv.Atoi(got["raw_row"])
	if err != nil {
		return BadRecord{}, fmt.Errorf("raw_row %q: %v", got["raw_row"], err)
	}
	return BadRecord{Time: t, Reason: Reason(got["reason"]), RawRow: row}, nil
}

func parseBar(rec []string) (Bar, error) {
	t, err := parseTime(rec[0])
	if err != nil {
		return Bar{}, err
	}
	avail, err := parseTime(rec[1])
	if err != nil {
		return Bar{}, err
	}
	bid, err := parseOHLC(rec[2:6], priceDecimals)
	if err != nil {
		return Bar{}, fmt.Errorf("bid: %w", err)
	}
	ask, err := parseOHLC(rec[6:10], priceDecimals)
	if err != nil {
		return Bar{}, fmt.Errorf("ask: %w", err)
	}
	mid, err := parseOHLC(rec[10:14], midDecimals)
	if err != nil {
		return Bar{}, fmt.Errorf("mid: %w", err)
	}
	vol, err := strconv.ParseInt(rec[14], 10, 64)
	if err != nil {
		return Bar{}, fmt.Errorf("volume %q: %v", rec[14], err)
	}
	return Bar{Time: t, AvailableAt: avail, Bid: bid, Ask: ask, Mid: mid, Volume: vol}, nil
}
