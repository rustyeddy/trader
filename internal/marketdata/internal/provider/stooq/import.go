package stooq

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/rustyeddy/trader/num"
)

// nativeHeader is the exact column header Stooq's own daily-history CSV
// export uses — distinct from rawV1Header, which is this package's own
// raw-archive partition format. Import's job is converting one into the
// other.
const nativeHeader = "Date,Open,High,Low,Close,Volume"

// archiveHeader is the header used by Stooq's downloaded archive files.
// Those files are CSV despite their .txt suffix and include provider
// identity, period, and time columns around the OHLCV fields.
const archiveHeader = "<TICKER>,<PER>,<DATE>,<TIME>,<OPEN>,<HIGH>,<LOW>,<CLOSE>,<VOL>,<OPENINT>"

// ImportResult summarizes one Import call.
type ImportResult struct {
	// MonthsWritten is the number of monthly raw partitions Import
	// wrote (or overwrote) under RawRoot.
	MonthsWritten int
	// RowsImported is the total number of source rows successfully
	// parsed and written across all months.
	RowsImported int
	// FirstDate and LastDate are the earliest and latest record dates
	// imported.
	FirstDate, LastDate time.Time
}

// Import reads csvPath — Stooq's own native daily-history CSV export
// for one instrument (for example a downloaded "spy_us_d.csv") — and
// writes its rows out as monthly raw partitions under rawRoot, in
// Trader's own raw-archive convention (see the package doc comment),
// so the normal Coverage/Plan/Build path can then read and normalize
// them exactly like any other provider's raw data.
//
// Import is the one, explicit, operator-run acquisition step this
// package provides in place of a live Sync (see the package doc
// comment for why: Stooq has no API this package calls, only a
// caller-supplied local file). Running Import again over an updated
// Stooq export is how the raw archive for symbol is brought current;
// Import always overwrites whatever monthly partitions it touches
// (WritePartition's mustNotExist=false), since it has no partial/
// incremental mode — it is intended to be re-run over the full export
// each time, not diffed against the existing archive.
//
// # Validation layering
//
// Import validates only what a raw acquisition step is responsible
// for, mirroring the same split oanda's own raw reader and
// marketdata's normalize_oanda.go already establish: the native
// header must match exactly, and each row must be structurally
// parseable (right field count, valid date, valid decimal prices,
// non-negative integer volume) — a malformed row aborts the entire
// import with a deterministic error naming the offending line, rather
// than silently skipping it or importing a partial file. Sequence-level
// checks (duplicate or out-of-order dates) and OHLC-shape validation
// (High < Low, Open/Close outside [Low, High]) are deliberately not
// checked here: they are marketdata's own normalization-stage
// responsibility (normalize_stooq.go), so raw data is preserved exactly
// as delivered even when it will later be rejected at build time — the
// same "preserve provider-native data, judge it at normalization"
// split ADR-020 already established for oanda.
func Import(ctx context.Context, csvPath, rawRoot, symbol string) (ImportResult, error) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol == "" {
		return ImportResult{}, fmt.Errorf("%w: symbol is required", ErrMalformedData)
	}

	f, err := os.Open(csvPath)
	if err != nil {
		return ImportResult{}, fmt.Errorf("stooq: import: %w", err)
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return ImportResult{}, fmt.Errorf("stooq: import: %s: %w", csvPath, err)
		}
		return ImportResult{}, fmt.Errorf("%w: %s: empty file", ErrMalformedData, csvPath)
	}
	header := strings.TrimSpace(scanner.Text())
	if header != nativeHeader {
		return ImportResult{}, fmt.Errorf("%w: %s: unexpected header %q, want %q", ErrMalformedData, csvPath, header, nativeHeader)
	}

	byMonth := make(map[monthKey][]Record)
	var order []monthKey
	var result ImportResult
	line := 1
	for scanner.Scan() {
		line++
		if err := ctx.Err(); err != nil {
			return ImportResult{}, err
		}
		row := strings.TrimSpace(scanner.Text())
		if row == "" {
			continue
		}
		rec, err := parseNativeRow(csvPath, line, row)
		if err != nil {
			return ImportResult{}, err
		}

		key := monthKey{year: rec.Time.Year(), month: rec.Time.Month()}
		if _, seen := byMonth[key]; !seen {
			order = append(order, key)
		}
		byMonth[key] = append(byMonth[key], rec)

		result.RowsImported++
		// Computed as an actual min/max over every parsed row, not
		// scan order: a reverse-sorted or otherwise unsorted native
		// export must not silently invert FirstDate/LastDate for a
		// caller that builds a query span from this result (Copilot's
		// PR #306 review).
		if result.FirstDate.IsZero() || rec.Time.Before(result.FirstDate) {
			result.FirstDate = rec.Time
		}
		if result.LastDate.IsZero() || rec.Time.After(result.LastDate) {
			result.LastDate = rec.Time
		}
	}
	if err := scanner.Err(); err != nil {
		return ImportResult{}, fmt.Errorf("stooq: import: %s: %w", csvPath, err)
	}

	for _, key := range order {
		if err := ctx.Err(); err != nil {
			return ImportResult{}, err
		}
		if err := WritePartition(ctx, rawRoot, symbol, key.year, key.month, byMonth[key], false); err != nil {
			return ImportResult{}, fmt.Errorf("stooq: import: write %04d-%02d: %w", key.year, int(key.month), err)
		}
		result.MonthsWritten++
	}

	return result, nil
}

// ImportArchive reads one native Stooq archive file and writes its daily
// records into Trader's existing monthly raw-partition pipeline. It accepts
// the deep, provider-native archive layout without modifying the source path.
// Only daily rows (period D) are supported in this first slice.
func ImportArchive(ctx context.Context, csvPath, rawRoot, symbol string) (ImportResult, error) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol == "" {
		return ImportResult{}, fmt.Errorf("%w: symbol is required", ErrMalformedData)
	}
	f, err := os.Open(csvPath)
	if err != nil {
		return ImportResult{}, fmt.Errorf("stooq: archive import: %w", err)
	}
	defer func() { _ = f.Close() }()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return ImportResult{}, fmt.Errorf("stooq: archive import: %w", err)
		}
		return ImportResult{}, fmt.Errorf("%w: %s: empty file", ErrMalformedData, csvPath)
	}
	if strings.TrimSpace(scanner.Text()) != archiveHeader {
		return ImportResult{}, fmt.Errorf("%w: %s: unexpected header %q, want %q", ErrMalformedData, csvPath, scanner.Text(), archiveHeader)
	}
	var records []Record
	line := 1
	for scanner.Scan() {
		line++
		if err := ctx.Err(); err != nil {
			return ImportResult{}, err
		}
		row := strings.TrimSpace(scanner.Text())
		if row == "" {
			continue
		}
		rec, err := parseArchiveRow(csvPath, line, row, symbol)
		if err != nil {
			return ImportResult{}, err
		}
		records = append(records, rec)
	}
	if err := scanner.Err(); err != nil {
		return ImportResult{}, fmt.Errorf("stooq: archive import: %w", err)
	}
	return writeImportedRecords(ctx, rawRoot, symbol, records)
}

func parseArchiveRow(path string, line int, row, symbol string) (Record, error) {
	fields := strings.Split(row, ",")
	if len(fields) != 10 {
		return Record{}, fmt.Errorf("%w: %s:%d: expected 10 fields, got %d", ErrMalformedData, path, line, len(fields))
	}
	ticker := strings.TrimSuffix(strings.ToUpper(strings.TrimSpace(fields[0])), ".US")
	if ticker != symbol {
		return Record{}, fmt.Errorf("%w: %s:%d: ticker %q does not match %q", ErrMalformedData, path, line, fields[0], symbol)
	}
	if strings.TrimSpace(fields[1]) != "D" {
		return Record{}, fmt.Errorf("%w: %s:%d: unsupported period %q, only D is supported", ErrMalformedData, path, line, fields[1])
	}
	if strings.TrimSpace(fields[3]) != "000000" {
		return Record{}, fmt.Errorf("%w: %s:%d: daily row has non-midnight time %q", ErrMalformedData, path, line, fields[3])
	}
	date, err := time.Parse("20060102", strings.TrimSpace(fields[2]))
	if err != nil {
		return Record{}, fmt.Errorf("%w: %s:%d: invalid date %q: %v", ErrMalformedData, path, line, fields[2], err)
	}
	parsePrice := func(name, value string) (num.Price, error) {
		p, err := num.ParsePrice(strings.TrimSpace(value))
		if err != nil {
			return num.Price{}, fmt.Errorf("%w: %s:%d: invalid %s %q: %v", ErrMalformedData, path, line, name, value, err)
		}
		return p, nil
	}
	open, err := parsePrice("open", fields[4])
	if err != nil {
		return Record{}, err
	}
	high, err := parsePrice("high", fields[5])
	if err != nil {
		return Record{}, err
	}
	low, err := parsePrice("low", fields[6])
	if err != nil {
		return Record{}, err
	}
	closePrice, err := parsePrice("close", fields[7])
	if err != nil {
		return Record{}, err
	}
	volume, err := strconv.ParseInt(strings.TrimSpace(fields[8]), 10, 64)
	if err != nil || volume < 0 {
		return Record{}, fmt.Errorf("%w: %s:%d: invalid volume %q", ErrMalformedData, path, line, fields[8])
	}
	// OPENINT is provider metadata outside Trader's OHLCV model; validate
	// the field count but deliberately do not persist it.
	return Record{Time: date.UTC(), Open: open, High: high, Low: low, Close: closePrice, Volume: volume}, nil
}

func writeImportedRecords(ctx context.Context, rawRoot, symbol string, records []Record) (ImportResult, error) {
	result := ImportResult{}
	byMonth := make(map[monthKey][]Record)
	var order []monthKey
	for _, rec := range records {
		key := monthKey{year: rec.Time.Year(), month: rec.Time.Month()}
		if _, seen := byMonth[key]; !seen {
			order = append(order, key)
		}
		byMonth[key] = append(byMonth[key], rec)
		result.RowsImported++
		if result.FirstDate.IsZero() || rec.Time.Before(result.FirstDate) {
			result.FirstDate = rec.Time
		}
		if result.LastDate.IsZero() || rec.Time.After(result.LastDate) {
			result.LastDate = rec.Time
		}
	}
	for _, key := range order {
		if err := WritePartition(ctx, rawRoot, symbol, key.year, key.month, byMonth[key], false); err != nil {
			return ImportResult{}, fmt.Errorf("stooq: import: write %04d-%02d: %w", key.year, int(key.month), err)
		}
		result.MonthsWritten++
	}
	return result, nil
}

type monthKey struct {
	year  int
	month time.Month
}

func parseNativeRow(path string, line int, row string) (Record, error) {
	// parseRow (reader.go) already implements exactly the field-count/
	// date/price/volume parsing Import needs for a native row — the
	// native and raw-v1 schemas share the same column order and count
	// (date,open,high,low,close,volume), differing only in header text
	// and date/number formatting conventions Stooq and this package's
	// own raw-v1 form happen to already share (YYYY-MM-DD dates, plain
	// decimal prices). Reusing it here, through a throwaway Reader
	// wrapping just this one row, keeps the parsing and error-message
	// shape identical between the raw-v1 reader and this native
	// importer rather than maintaining two copies.
	r := &Reader{path: path, line: line}
	rec, err := r.parseRow(row)
	if err != nil {
		return Record{}, err
	}
	return rec, nil
}
