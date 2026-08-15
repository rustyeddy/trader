package oanda

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rustyeddy/trader/num"
)

// rawV1Header is the exact raw-v1 column header. It is matched verbatim so a
// reordered or altered header cannot be misread positionally.
const rawV1Header = "time,bid_o,bid_h,bid_l,bid_c,ask_o,ask_h,ask_l,ask_c,volume,complete"

// rawFieldCount is the number of columns in a raw-v1 row:
// time, bid O/H/L/C, ask O/H/L/C, volume, complete.
const rawFieldCount = 11

// Reader streams the rows of one raw OANDA CSV partition as provider-native
// Record values. It owns the underlying file: the caller must Close it when
// done, whether iteration finished, errored, or was cancelled.
//
// A Reader is not safe for concurrent use; a single goroutine drives Next.
type Reader struct {
	path    string
	meta    Meta
	file    *os.File
	scanner *bufio.Scanner
	line    int // 1-based line number of the most recently read line
	closed  bool
}

// Open resolves path's partition metadata, opens the file, and consumes its
// schema and column-header lines, leaving the Reader positioned at the first
// data row.
//
// It resolves the instrument and interval from the file name before opening
// anything, so an out-of-scope partition (ErrInstrumentOutOfScope, e.g.
// XAUUSD) or an unsupported interval (ErrUnsupportedInterval, e.g. w1) is
// reported without touching the filesystem. When a "# schema=" comment is
// present it is cross-checked against the file name; a disagreement is
// ErrMalformedData.
func Open(path string) (*Reader, error) {
	meta, token, err := parsePathMeta(path)
	if err != nil {
		return nil, err
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	return newReader(path, meta, token, f, f)
}

// newReaderFromBytes builds a Reader over data already read into memory,
// resolving path's metadata the same way Open does. It exists for a
// caller (inventory.go's Inspect) that already has a partition's raw
// bytes for another purpose — fingerprinting them — and would otherwise
// have to read the same file from disk a second time just to parse it.
// There is no file for Close to release; Close is a no-op beyond marking
// the Reader closed.
func newReaderFromBytes(path string, data []byte) (*Reader, error) {
	meta, token, err := parsePathMeta(path)
	if err != nil {
		return nil, err
	}
	return newReader(path, meta, token, bytes.NewReader(data), nil)
}

// newReader is Open and newReaderFromBytes's shared constructor: it wraps
// src in a Reader, consuming its schema and column-header lines. file is
// non-nil only when there is an underlying *os.File Close must release.
func newReader(path string, meta Meta, token string, src io.Reader, file *os.File) (*Reader, error) {
	r := &Reader{path: path, meta: meta, file: file, scanner: bufio.NewScanner(src)}
	// m1 partitions can be a few MB; grow the scanner's line budget well past
	// bufio's 64 KiB default even though rows are short, to be safe.
	r.scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	if err := r.consumeHeader(meta, token); err != nil {
		if file != nil {
			_ = file.Close()
		}
		return nil, err
	}
	return r, nil
}

// Meta returns the partition-level context shared by every Record.
func (r *Reader) Meta() Meta { return r.meta }

// consumeHeader reads the leading comment and column-header lines, validating
// a "# schema=" comment against the file name when present, and stops with
// the scanner positioned so the next Scan yields the first data row.
func (r *Reader) consumeHeader(meta Meta, token string) error {
	sawColumnHeader := false
	for r.scan() {
		line := strings.TrimSpace(r.scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			if err := crossCheckSchema(line, r.path, meta, token); err != nil {
				return err
			}
			continue
		}
		// The first non-comment line must be the raw-v1 column header, matched
		// exactly. Every data field is read positionally, so a reordered,
		// renamed, or extended header would silently misassign values to the
		// wrong columns — the exact match is what prevents that.
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

// Next returns the next Record, or io.EOF when the partition is exhausted. It
// honors ctx cancellation before reading, so a cancelled iteration stops
// promptly rather than reading to the end of a large file.
func (r *Reader) Next(ctx context.Context) (Record, error) {
	if r.closed {
		return Record{}, fmt.Errorf("oanda: read %s: reader is closed", r.path)
	}
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	for r.scan() {
		line := strings.TrimSpace(r.scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		return r.parseRow(line)
	}
	if err := r.scanner.Err(); err != nil {
		return Record{}, fmt.Errorf("%w: %s:%d: %v", ErrMalformedData, r.path, r.line, err)
	}
	return Record{}, io.EOF
}

// Close releases the underlying file. It is safe to call more than once.
func (r *Reader) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	if r.file == nil {
		return nil
	}
	return r.file.Close()
}

// scan advances the scanner one line and tracks the line number.
func (r *Reader) scan() bool {
	if r.scanner.Scan() {
		r.line++
		return true
	}
	return false
}

// parseRow converts one data line into a Record, reporting ErrMalformedData
// (wrapping the field-specific cause) for any structural or numeric problem.
func (r *Reader) parseRow(line string) (Record, error) {
	fields := strings.Split(line, ",")
	if len(fields) != rawFieldCount {
		return Record{}, fmt.Errorf("%w: %s:%d: expected %d fields, got %d",
			ErrMalformedData, r.path, r.line, rawFieldCount, len(fields))
	}

	ts, err := time.Parse(time.RFC3339, strings.TrimSpace(fields[0]))
	if err != nil {
		return Record{}, r.fieldErr("time", err)
	}

	prices := [8]num.Price{}
	names := [8]string{"bid_o", "bid_h", "bid_l", "bid_c", "ask_o", "ask_h", "ask_l", "ask_c"}
	for i := range 8 {
		p, err := num.ParsePrice(strings.TrimSpace(fields[i+1]))
		if err != nil {
			return Record{}, r.fieldErr(names[i], err)
		}
		prices[i] = p
	}

	volume, err := strconv.ParseInt(strings.TrimSpace(fields[9]), 10, 64)
	if err != nil {
		return Record{}, r.fieldErr("volume", err)
	}

	complete, err := strconv.ParseBool(strings.TrimSpace(fields[10]))
	if err != nil {
		return Record{}, r.fieldErr("complete", err)
	}

	return Record{
		Time:     ts.UTC(),
		BidOpen:  prices[0],
		BidHigh:  prices[1],
		BidLow:   prices[2],
		BidClose: prices[3],
		AskOpen:  prices[4],
		AskHigh:  prices[5],
		AskLow:   prices[6],
		AskClose: prices[7],
		Volume:   volume,
		Complete: complete,
	}, nil
}

// fieldErr wraps a per-field parse failure with the field name and location.
func (r *Reader) fieldErr(field string, cause error) error {
	return fmt.Errorf("%w: %s:%d: parse %s: %v", ErrMalformedData, r.path, r.line, field, cause)
}

// ReadFile reads an entire raw OANDA partition into a slice of Records. It is
// a convenience over Open/Next for callers that want the whole month at once
// and do not need incremental iteration; it owns and closes the file itself.
// The Meta it returns is the same one Open exposes.
func ReadFile(ctx context.Context, path string) (Meta, []Record, error) {
	r, err := Open(path)
	if err != nil {
		return Meta{}, nil, err
	}
	defer r.Close()

	var records []Record
	for {
		rec, err := r.Next(ctx)
		if err == io.EOF {
			break
		}
		if err != nil {
			return Meta{}, nil, err
		}
		records = append(records, rec)
	}
	return r.meta, records, nil
}

// parsePathMeta resolves a partition's Meta and its raw interval token from a
// file path named PAIR-YYYY-MM-<tf>.csv. Resolving the instrument and interval
// here lets Open reject an out-of-scope or unsupported partition before it
// opens the file.
func parsePathMeta(path string) (Meta, string, error) {
	base := strings.TrimSuffix(filepath.Base(path), ".csv")
	parts := strings.Split(base, "-")
	if len(parts) != 4 {
		return Meta{}, "", fmt.Errorf("%w: file name %q is not PAIR-YYYY-MM-tf.csv", ErrMalformedData, filepath.Base(path))
	}
	symbol, yearStr, monthStr, token := parts[0], parts[1], parts[2], parts[3]

	id, err := resolveSymbol(symbol)
	if err != nil {
		return Meta{}, "", err
	}
	interval, err := resolveInterval(token)
	if err != nil {
		return Meta{}, "", err
	}
	year, err := strconv.Atoi(yearStr)
	if err != nil {
		return Meta{}, "", fmt.Errorf("%w: file name %q: year: %v", ErrMalformedData, filepath.Base(path), err)
	}
	month, err := strconv.Atoi(monthStr)
	if err != nil || month < 1 || month > 12 {
		return Meta{}, "", fmt.Errorf("%w: file name %q: month %q", ErrMalformedData, filepath.Base(path), monthStr)
	}

	return Meta{
		Instrument: id,
		Interval:   interval,
		Year:       year,
		Month:      time.Month(month),
		Symbol:     symbol,
	}, token, nil
}

// crossCheckSchema validates a "# schema=..." comment against the metadata
// already resolved from the file name. A "# ..." comment that is not a schema
// line is ignored; a schema line that disagrees on instrument, interval,
// year, or month is ErrMalformedData — a partition whose contents contradict
// its path is corrupt, not silently trusted.
func crossCheckSchema(comment, path string, meta Meta, token string) error {
	trimmed := strings.TrimSpace(strings.TrimPrefix(comment, "#"))
	if !strings.HasPrefix(trimmed, "schema=") {
		return nil
	}
	kv := map[string]string{}
	for tok := range strings.FieldsSeq(trimmed) {
		k, v, ok := strings.Cut(tok, "=")
		if ok {
			kv[k] = v
		}
	}
	if kv["schema"] != "raw-v1" {
		return fmt.Errorf("%w: %s: unsupported schema %q", ErrMalformedData, path, kv["schema"])
	}
	if kv["source"] != "oanda" {
		return fmt.Errorf("%w: %s: unexpected source %q", ErrMalformedData, path, kv["source"])
	}
	if got := kv["instrument"]; got != meta.Symbol {
		return fmt.Errorf("%w: %s: schema instrument %q disagrees with file name %q", ErrMalformedData, path, got, meta.Symbol)
	}
	// The path token and the comment's tf may spell the daily interval
	// differently (d1 vs d); compare the intervals they resolve to.
	commentInterval, err := resolveInterval(kv["tf"])
	if err != nil {
		return fmt.Errorf("%w: %s: schema tf %q: %v", ErrMalformedData, path, kv["tf"], err)
	}
	if commentInterval != meta.Interval {
		return fmt.Errorf("%w: %s: schema tf %q disagrees with file name token %q", ErrMalformedData, path, kv["tf"], token)
	}
	if got := kv["year"]; got != strconv.Itoa(meta.Year) {
		return fmt.Errorf("%w: %s: schema year %q disagrees with file name year %d", ErrMalformedData, path, got, meta.Year)
	}
	if got := kv["month"]; got != fmt.Sprintf("%02d", int(meta.Month)) {
		return fmt.Errorf("%w: %s: schema month %q disagrees with file name month %02d", ErrMalformedData, path, got, int(meta.Month))
	}
	return nil
}
