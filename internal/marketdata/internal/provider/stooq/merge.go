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
// (Stooq history is back-adjusted for splits and dividends, ADR-073, so
// every split or ex-dividend date rewrites older rows) and which cannot
// safely be repaired by a full re-import because the export lacks a date
// already on disk. Overwriting would drop that row or leave its month on
// the old price basis; appending would mix adjusted and unadjusted
// rows. The operator must supply a complete export (or force a full
// import).
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
//     the comparison is impossible, history has been or may have been
//     re-adjusted (for a dividend payer, any ex-dividend date since the
//     last download does this, so this is the usual case for them). If
//     the export holds every date already in raw, the symbol is fully
//     re-imported (ImportResult.FullReimport), oldest month first so an
//     interrupted rewrite leaves the last month on the old basis and the
//     next run detects the mismatch again. If the export lacks any
//     existing raw date, the call fails with ErrAdjustmentMismatch and
//     writes nothing: replacing only part of the history would leave
//     months on two price bases, or drop rows.
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
		return writeImportedRecords(ctx, rawRoot, symbol, records, nil)
	}
	if len(records) == 0 {
		return result, nil
	}

	lastKey := months[len(months)-1]
	lastRecs, err := readPartitionRecords(ctx, partitionPath(rawRoot, symbol, lastKey.year, lastKey.month))
	if err != nil {
		return ImportResult{}, err
	}
	rawLast := lastRecs[0]
	for _, r := range lastRecs {
		if r.Time.After(rawLast.Time) {
			rawLast = r
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
		// History has been, or may have been, re-adjusted. Replacing it is
		// only coherent if the export holds every date already in raw:
		// otherwise a month the export omits, or a date it lacks inside a
		// month it rewrites, would stay on the old price basis or be lost.
		known, err := rawDates(ctx, rawRoot, symbol, months, true)
		if err != nil {
			return ImportResult{}, err
		}
		if missing, ok := firstMissingDate(known, records); ok {
			return ImportResult{}, fmt.Errorf("%w: %s: export lacks raw date %s (last raw date %s): supply a complete export or force a full import",
				ErrAdjustmentMismatch, symbol, missing.Format("2006-01-02"), rawLast.Time.Format("2006-01-02"))
		}
		full, err := writeImportedRecords(ctx, rawRoot, symbol, records, known)
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

// rawDates returns every trading date already stored for symbol across
// months. With strict, an unreadable or empty partition is an error (a
// full-reimport decision cannot be made around data it cannot see);
// without it such a partition is skipped, so a forced import can still
// repair a damaged one.
func rawDates(ctx context.Context, root, symbol string, months []monthKey, strict bool) (map[time.Time]struct{}, error) {
	known := make(map[time.Time]struct{})
	for _, key := range months {
		recs, err := readPartitionRecords(ctx, partitionPath(root, symbol, key.year, key.month))
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			if strict {
				return nil, err
			}
			continue
		}
		for _, r := range recs {
			known[r.Time] = struct{}{}
		}
	}
	return known, nil
}

// firstMissingDate returns the earliest date in known that records does
// not contain.
func firstMissingDate(known map[time.Time]struct{}, records []Record) (time.Time, bool) {
	have := make(map[time.Time]struct{}, len(records))
	for _, r := range records {
		have[r.Time] = struct{}{}
	}
	var missing []time.Time
	for d := range known {
		if _, ok := have[d]; !ok {
			missing = append(missing, d)
		}
	}
	if len(missing) == 0 {
		return time.Time{}, false
	}
	sort.Slice(missing, func(i, j int) bool { return missing[i].Before(missing[j]) })
	return missing[0], true
}
