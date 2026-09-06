package stooq

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/rustyeddy/trader/num"
)

// rawV1Header is the exact raw-v1 column header. It is matched verbatim
// so a reordered or altered header cannot be misread positionally —
// oanda.rawV1Header's own reasoning, applied to this package's
// single-price schema.
const rawV1Header = "date,open,high,low,close,volume"

// rawFieldCount is the number of columns in a raw-v1 row: date, open,
// high, low, close, volume.
const rawFieldCount = 6

// Reader streams the rows of one raw Stooq CSV partition as
// provider-native Record values. It owns the underlying file: the
// caller must Close it when done, whether iteration finished, errored,
// or was cancelled.
//
// A Reader is not safe for concurrent use; a single goroutine drives
// Next.
type Reader struct {
	path    string
	meta    meta
	file    *os.File
	scanner *bufio.Scanner
	line    int
	closed  bool
}

// Open resolves path's partition metadata, opens the file, and consumes
// its schema and column-header lines, leaving the Reader positioned at
// the first data row.
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

// newReaderFromBytes builds a Reader over data already read into
// memory, resolving path's metadata the same way Open does — for a
// caller (Inspect, ReadPartitionSnapshot) that already has a
// partition's raw bytes for another purpose and would otherwise read
// the same file twice.
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
func (r *Reader) Meta() (symbol string, year int, month time.Month) {
	return r.meta.Symbol, r.meta.Year, r.meta.Month
}

func (r *Reader) consumeHeader() error {
	sawColumnHeader := false
	for r.scan() {
		line := strings.TrimSpace(r.scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			if err := crossCheckSchema(line, r.path, r.meta); err != nil {
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

// crossCheckSchema validates a "# schema=" comment line against m, the
// same cross-check oanda.crossCheckSchema performs.
func crossCheckSchema(comment, path string, m meta) error {
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
	if kv["schema"] != "raw-v1" {
		return fmt.Errorf("%w: %s: unsupported schema %q", ErrMalformedData, path, kv["schema"])
	}
	if kv["source"] != "stooq" {
		return fmt.Errorf("%w: %s: unexpected source %q", ErrMalformedData, path, kv["source"])
	}
	if got := kv["instrument"]; got != m.Symbol {
		return fmt.Errorf("%w: %s: schema instrument %q disagrees with file name %q", ErrMalformedData, path, got, m.Symbol)
	}
	if kv["tf"] != string(RawD1) {
		return fmt.Errorf("%w: %s: unexpected tf %q", ErrMalformedData, path, kv["tf"])
	}
	if got := kv["year"]; got != strconv.Itoa(m.Year) {
		return fmt.Errorf("%w: %s: schema year %q disagrees with file name year %d", ErrMalformedData, path, got, m.Year)
	}
	if got := kv["month"]; got != fmt.Sprintf("%02d", int(m.Month)) {
		return fmt.Errorf("%w: %s: schema month %q disagrees with file name month %02d", ErrMalformedData, path, got, int(m.Month))
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
// exhausted. It honors ctx cancellation before reading.
func (r *Reader) Next(ctx context.Context) (Record, error) {
	if r.closed {
		return Record{}, fmt.Errorf("stooq: read %s: reader is closed", r.path)
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

// Close releases the underlying file, if any. It is safe to call more
// than once.
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
