package alpaca

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/rustyeddy/trader/num"
)

// rawV1Header is the exact raw-v1 column header — identical to
// stooq's own single-price schema, since this package's Record shape
// (post timestamp-normalization) is identical to stooq.Record's.
const rawV1Header = "date,open,high,low,close,volume"

// rawFieldCount is the number of columns in a raw-v1 row.
const rawFieldCount = 6

// Reader streams the rows of one raw Alpaca CSV partition as
// provider-native Record values. It owns the underlying file: the
// caller must Close it when done. Not safe for concurrent use.
type Reader struct {
	path    string
	meta    meta
	feed    Feed
	file    *os.File
	scanner *bufio.Scanner
	line    int
	closed  bool
}

// Open resolves path's partition metadata, opens the file, and
// consumes its schema and column-header lines.
func Open(path string) (*Reader, error) {
	m, err := parsePathMeta(path)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	return newReader(path, m, f, f)
}

func newReaderFromBytes(path string, data []byte) (*Reader, error) {
	m, err := parsePathMeta(path)
	if err != nil {
		return nil, err
	}
	return newReader(path, m, bytes.NewReader(data), nil)
}

func newReader(path string, m meta, src io.Reader, file *os.File) (*Reader, error) {
	r := &Reader{path: path, meta: m, file: file, scanner: bufio.NewScanner(src)}
	r.scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	if err := r.consumeHeader(); err != nil {
		if file != nil {
			_ = file.Close()
		}
		return nil, err
	}
	return r, nil
}

// Meta returns the partition-level context shared by every Record.
// feed is the data feed recorded in the file's own schema comment
// (issue #324, EQ-11) — Feed("") for a partition written before that
// provenance existed, a documented, accepted gap for legacy files
// rather than an error, since there is no way to recover which feed
// actually produced them after the fact.
func (r *Reader) Meta() (symbol string, year int, month time.Month, feed Feed) {
	return r.meta.Symbol, r.meta.Year, r.meta.Month, r.feed
}

func (r *Reader) consumeHeader() error {
	sawColumnHeader := false
	for r.scan() {
		line := strings.TrimSpace(r.scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			if err := r.crossCheckSchema(line); err != nil {
				return err
			}
			continue
		}
		if line != rawV1Header {
			return fmt.Errorf("%w: %s: unexpected column header %q, want %q", ErrMalformedData, r.path, line, rawV1Header)
		}
		sawColumnHeader = true
		break
	}
	if err := r.scanner.Err(); err != nil {
		return fmt.Errorf("%w: %s: %v", ErrMalformedData, r.path, err)
	}
	if !sawColumnHeader {
		return fmt.Errorf("%w: %s: no column header found", ErrMalformedData, r.path)
	}
	return nil
}

// crossCheckSchema validates a "# schema=" comment line against r's own
// path-derived meta, and records its "feed=" token (if any) as r.feed.
// A missing "feed=" token is not an error — it means the partition was
// written before issue #324 (EQ-11) added feed provenance — and leaves
// r.feed at its zero value (Feed("")), the documented "unknown" state
// for legacy data (see Meta's own doc comment).
func (r *Reader) crossCheckSchema(comment string) error {
	trimmed := strings.TrimSpace(strings.TrimPrefix(comment, "#"))
	if !strings.HasPrefix(trimmed, "schema=") {
		return nil
	}
	kv := map[string]string{}
	for _, tok := range strings.Fields(trimmed) {
		k, v, ok := strings.Cut(tok, "=")
		if ok {
			kv[k] = v
		}
	}
	m := r.meta
	if kv["schema"] != "raw-v1" {
		return fmt.Errorf("%w: %s: unsupported schema %q", ErrMalformedData, r.path, kv["schema"])
	}
	if kv["source"] != "alpaca" {
		return fmt.Errorf("%w: %s: unexpected source %q", ErrMalformedData, r.path, kv["source"])
	}
	if got := kv["instrument"]; got != m.Symbol {
		return fmt.Errorf("%w: %s: schema instrument %q disagrees with file name %q", ErrMalformedData, r.path, got, m.Symbol)
	}
	if kv["tf"] != string(RawD1) {
		return fmt.Errorf("%w: %s: unexpected tf %q", ErrMalformedData, r.path, kv["tf"])
	}
	if got := kv["year"]; got != strconv.Itoa(m.Year) {
		return fmt.Errorf("%w: %s: schema year %q disagrees with file name year %d", ErrMalformedData, r.path, got, m.Year)
	}
	if got := kv["month"]; got != fmt.Sprintf("%02d", int(m.Month)) {
		return fmt.Errorf("%w: %s: schema month %q disagrees with file name month %02d", ErrMalformedData, r.path, got, int(m.Month))
	}
	if feed, ok := kv["feed"]; ok {
		r.feed = Feed(feed)
	}
	return nil
}

func (r *Reader) scan() bool {
	if !r.scanner.Scan() {
		return false
	}
	r.line++
	return true
}

// Next returns the next Record, or io.EOF when the partition is
// exhausted.
func (r *Reader) Next(ctx context.Context) (Record, error) {
	if r.closed {
		return Record{}, fmt.Errorf("alpaca: read %s: reader is closed", r.path)
	}
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	if !r.scan() {
		if err := r.scanner.Err(); err != nil {
			return Record{}, fmt.Errorf("%w: %s: %v", ErrMalformedData, r.path, err)
		}
		return Record{}, io.EOF
	}
	return r.parseRow(r.scanner.Text())
}

func (r *Reader) parseRow(line string) (Record, error) {
	fields := strings.Split(line, ",")
	if len(fields) != rawFieldCount {
		return Record{}, fmt.Errorf("%w: %s:%d: expected %d fields, got %d", ErrMalformedData, r.path, r.line, rawFieldCount, len(fields))
	}

	date, err := time.Parse("2006-01-02", fields[0])
	if err != nil {
		return Record{}, fmt.Errorf("%w: %s:%d: invalid date %q: %v", ErrMalformedData, r.path, r.line, fields[0], err)
	}

	open, err := num.ParsePrice(fields[1])
	if err != nil {
		return Record{}, fmt.Errorf("%w: %s:%d: invalid open %q: %v", ErrMalformedData, r.path, r.line, fields[1], err)
	}
	high, err := num.ParsePrice(fields[2])
	if err != nil {
		return Record{}, fmt.Errorf("%w: %s:%d: invalid high %q: %v", ErrMalformedData, r.path, r.line, fields[2], err)
	}
	low, err := num.ParsePrice(fields[3])
	if err != nil {
		return Record{}, fmt.Errorf("%w: %s:%d: invalid low %q: %v", ErrMalformedData, r.path, r.line, fields[3], err)
	}
	closePrice, err := num.ParsePrice(fields[4])
	if err != nil {
		return Record{}, fmt.Errorf("%w: %s:%d: invalid close %q: %v", ErrMalformedData, r.path, r.line, fields[4], err)
	}

	volume, err := strconv.ParseInt(strings.TrimSpace(fields[5]), 10, 64)
	if err != nil {
		return Record{}, fmt.Errorf("%w: %s:%d: invalid volume %q: %v", ErrMalformedData, r.path, r.line, fields[5], err)
	}
	if volume < 0 {
		return Record{}, fmt.Errorf("%w: %s:%d: negative volume %d", ErrMalformedData, r.path, r.line, volume)
	}

	return Record{
		Time:   date.UTC(),
		Open:   open,
		High:   high,
		Low:    low,
		Close:  closePrice,
		Volume: volume,
	}, nil
}

// Close releases the underlying file, if any. Safe to call more than
// once.
func (r *Reader) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	if r.file != nil {
		return r.file.Close()
	}
	return nil
}

// ReadPartitionRecords reads every record from the raw partition file
// for (symbol, year, month) under root, in file order. Reports
// fs.ErrNotExist (wrapped) if no such file exists — the same contract
// oanda.ReadPartitionRecords/stooq's own reader-based helpers provide,
// used by Sync to decide whether a partition is brand new or being
// extended.
func ReadPartitionRecords(ctx context.Context, root, symbol string, year int, month time.Month) ([]Record, error) {
	path := partitionPath(root, symbol, year, month)
	r, err := Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()

	var records []Record
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		rec, err := r.Next(ctx)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}
		records = append(records, rec)
	}
	return records, nil
}
