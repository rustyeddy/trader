package stooq

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ErrAdjustmentMismatch marks a MergeArchive call whose export
// disagrees with the existing raw archive on the last date both hold
// (Stooq history is split-adjusted, so a corporate action rewrites older
// rows) and which cannot safely be repaired by a full re-import because
// the export does not reach back to the first date already on disk.
// Overwriting would silently truncate history; appending would mix
// adjusted and unadjusted rows. The operator must supply a fuller export
// (or force a full import).
var ErrAdjustmentMismatch = errors.New("stooq: export disagrees with existing raw history")

// MergeArchive is the incremental counterpart of ImportArchive (issue
// #459). It reads one native Stooq archive file and brings the symbol's
// raw archive forward from its last raw date to the export's last row,
// instead of rewriting every monthly partition the export contains.
//
//   - No raw history yet: identical to ImportArchive.
//   - The export ends before the last raw date: nothing is written. An
//     older or shorter export never deletes existing rows.
//   - Otherwise the export's row for the last raw date is compared with
//     the stored one. When the closes agree, only rows after the last raw
//     date are written: the last existing month is rewritten with its
//     existing rows followed by the new ones, later months are created,
//     and every earlier partition is left untouched. If the export ends
//     on the last raw date, nothing is written.
//   - When the closes disagree, or the export lacks the last raw date so
//     the comparison is impossible, history may have been re-adjusted. If
//     the export reaches back to the first raw date, the symbol is fully
//     re-imported (ImportResult.FullReimport). If it does not, the call
//     fails with ErrAdjustmentMismatch and writes nothing.
//
// The comparison is on Close only. A last day captured before the
// session finished therefore also triggers the full re-import, which is
// safe but not minimal.
//
// RowsImported, FirstDate and LastDate describe the whole export, as
// they do for ImportArchive; RowsAdded and MonthsWritten describe only
// what this call changed. Malformed input aborts before any write.
func MergeArchive(ctx context.Context, csvPath, rawRoot, symbol string) (ImportResult, error) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	records, err := readArchive(ctx, csvPath, symbol)
	if err != nil {
		return ImportResult{}, err
	}
	result := ImportResult{RowsImported: len(records)}
	for _, rec := range records {
		if result.FirstDate.IsZero() || rec.Time.Before(result.FirstDate) {
			result.FirstDate = rec.Time
		}
		if result.LastDate.IsZero() || rec.Time.After(result.LastDate) {
			result.LastDate = rec.Time
		}
	}

	months, err := rawMonths(rawRoot, symbol)
	if err != nil {
		return ImportResult{}, err
	}
	if len(months) == 0 {
		return writeImportedRecords(ctx, rawRoot, symbol, records)
	}
	if len(records) == 0 {
		return result, nil
	}

	lastKey, firstKey := months[len(months)-1], months[0]
	lastRecs, err := readPartitionRecords(ctx, partitionPath(rawRoot, symbol, lastKey.year, lastKey.month))
	if err != nil {
		return ImportResult{}, err
	}
	firstRecs := lastRecs
	if firstKey != lastKey {
		if firstRecs, err = readPartitionRecords(ctx, partitionPath(rawRoot, symbol, firstKey.year, firstKey.month)); err != nil {
			return ImportResult{}, err
		}
	}
	rawLast, rawFirst := lastRecs[0], firstRecs[0]
	for _, r := range lastRecs {
		if r.Time.After(rawLast.Time) {
			rawLast = r
		}
	}
	for _, r := range firstRecs {
		if r.Time.Before(rawFirst.Time) {
			rawFirst = r
		}
	}

	if result.LastDate.Before(rawLast.Time) {
		return result, nil // older or shorter export: never delete
	}
	var overlap *Record
	for i := range records {
		if records[i].Time.Equal(rawLast.Time) {
			overlap = &records[i]
			break
		}
	}
	if overlap == nil || !overlap.Close.Equal(rawLast.Close) {
		if result.FirstDate.After(rawFirst.Time) {
			return ImportResult{}, fmt.Errorf("%w: %s on %s (export starts %s, raw starts %s)", ErrAdjustmentMismatch,
				symbol, rawLast.Time.Format("2006-01-02"), result.FirstDate.Format("2006-01-02"), rawFirst.Time.Format("2006-01-02"))
		}
		full, err := writeImportedRecords(ctx, rawRoot, symbol, records)
		full.FullReimport = true
		return full, err
	}
	if result.LastDate.Equal(rawLast.Time) {
		return result, nil
	}

	byMonth := map[monthKey][]Record{lastKey: lastRecs}
	order := []monthKey{lastKey}
	for _, rec := range records {
		if !rec.Time.After(rawLast.Time) {
			continue
		}
		key := monthKey{year: rec.Time.Year(), month: rec.Time.Month()}
		if _, seen := byMonth[key]; !seen {
			order = append(order, key)
		}
		byMonth[key] = append(byMonth[key], rec)
		result.RowsAdded++
	}
	for _, key := range order {
		if err := WritePartition(ctx, rawRoot, symbol, key.year, key.month, byMonth[key], false); err != nil {
			return ImportResult{}, fmt.Errorf("stooq: merge: write %04d-%02d: %w", key.year, int(key.month), err)
		}
		result.MonthsWritten++
	}
	return result, nil
}

// rawMonths lists the (year, month) partitions that exist for symbol
// under root, oldest first. A symbol with no directory has none.
func rawMonths(root, symbol string) ([]monthKey, error) {
	var months []monthKey
	years, err := os.ReadDir(filepath.Join(root, symbol))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("stooq: merge: %w", err)
	}
	for _, y := range years {
		year, err := strconv.Atoi(y.Name())
		if err != nil || !y.IsDir() || len(y.Name()) != 4 {
			continue
		}
		entries, err := os.ReadDir(filepath.Join(root, symbol, y.Name()))
		if err != nil {
			return nil, fmt.Errorf("stooq: merge: %w", err)
		}
		for _, m := range entries {
			month, err := strconv.Atoi(m.Name())
			if err != nil || !m.IsDir() || month < 1 || month > 12 {
				continue
			}
			key := monthKey{year: year, month: time.Month(month)}
			if _, err := os.Stat(partitionPath(root, symbol, key.year, key.month)); err == nil {
				months = append(months, key)
			}
		}
	}
	sort.Slice(months, func(i, j int) bool {
		if months[i].year != months[j].year {
			return months[i].year < months[j].year
		}
		return months[i].month < months[j].month
	})
	return months, nil
}

// readPartitionRecords reads every row of one raw partition. An
// existing partition with no rows is malformed: the merge could not say
// where the archive ends.
func readPartitionRecords(ctx context.Context, path string) ([]Record, error) {
	r, err := Open(path)
	if err != nil {
		return nil, fmt.Errorf("stooq: merge: %w", err)
	}
	defer func() { _ = r.Close() }()
	var recs []Record
	for {
		rec, err := r.Next(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("stooq: merge: %w", err)
		}
		recs = append(recs, rec)
	}
	if len(recs) == 0 {
		return nil, fmt.Errorf("%w: %s: existing partition has no rows", ErrMalformedData, path)
	}
	return recs, nil
}
