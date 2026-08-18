package oanda

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// ErrPartitionAlreadyExists marks an attempt to write a brand-new raw
// partition (see WritePartition's mustNotExist parameter) at a path that
// already has a file. It is never returned for an intentional extend —
// see the Missing/Extend distinction in sync.go — only for the case
// where a caller asked for a new file and one unexpectedly already
// exists, which is a planning/state inconsistency to surface loudly
// rather than silently overwrite (issue #80's "prevent accidental
// replacement of an existing authoritative raw artifact" constraint).
var ErrPartitionAlreadyExists = errors.New("oanda: raw partition already exists")

// partitionPath returns the path a raw partition for
// (symbol, interval, year, month) lives at under root, matching the
// layout Inspect and Open already expect:
// root/SYMBOL/YYYY/MM/SYMBOL-YYYY-MM-tf.csv.
func partitionPath(root, symbol string, interval RawInterval, year int, month time.Month) string {
	dir := filepath.Join(root, symbol, fmt.Sprintf("%04d", year), fmt.Sprintf("%02d", int(month)))
	base := fmt.Sprintf("%s-%04d-%02d-%s.csv", symbol, year, int(month), interval)
	return filepath.Join(dir, base)
}

// WritePartition atomically writes records as a raw-v1 partition file
// for (symbol, interval, year, month) under root: a schema comment, the
// raw-v1 column header (reader.go's rawV1Header, reused verbatim so a
// file this writes and a file Open reads never drift apart), then one
// row per record in ascending Time order. records need not already be
// sorted; a sorted copy is written, so the file this function produces
// always matches what Inspect/Open expect.
//
// mustNotExist, when true, rejects (ErrPartitionAlreadyExists) writing
// over a path that already has a file — the "new" case, used for an
// ActionDownloadRaw reason "missing" (sync.go). When false, an existing
// file at path is replaced — used only for reason "extend", where the
// caller has already merged the existing file's own records into
// records itself; WritePartition never reads an existing file or merges
// anything on its own.
//
// The write is atomic regardless: a temporary file alongside the
// destination, written, flushed, and synced, then renamed into place —
// the same discipline canonicalCSVStore.publish
// (marketdata/store_csv.go, #77) uses for canonical data, applied here
// to raw data. A failure or a context cancellation observed before the
// rename leaves any existing file at path completely untouched; ctx is
// checked before writing begins, once per record while encoding (a
// large M1 partition can run to tens of thousands of rows), and once
// more immediately before the rename — the actual commit point.
func WritePartition(ctx context.Context, root, symbol string, interval RawInterval, year int, month time.Month, records []Record, mustNotExist bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path := partitionPath(root, symbol, interval, year, month)

	if mustNotExist {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%w: %s", ErrPartitionAlreadyExists, path)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("oanda: write partition: %w", err)
		}
	}

	sorted := append([]Record(nil), records...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Time.Before(sorted[j].Time) })

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("oanda: write partition: %w", err)
	}

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("oanda: write partition: %w", err)
	}
	tmpPath := tmp.Name()
	succeeded := false
	defer func() {
		_ = tmp.Close()
		if !succeeded {
			_ = os.Remove(tmpPath)
		}
	}()

	bw := bufio.NewWriter(tmp)
	if err := encodeRawPartition(ctx, bw, symbol, interval, year, month, sorted); err != nil {
		return fmt.Errorf("oanda: write partition: %w", err)
	}
	if err := bw.Flush(); err != nil {
		return fmt.Errorf("oanda: write partition: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("oanda: write partition: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("oanda: write partition: %w", err)
	}
	// The commit point: checked once more here even though the caller
	// and encodeRawPartition may already have observed ctx earlier, so a
	// cancellation up to and including this instant still leaves path
	// completely untouched.
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("oanda: write partition: %w", err)
	}
	succeeded = true
	return nil
}

// encodeRawPartition writes the schema comment, column header, and one
// row per record, in the exact raw-v1 shape Open/Inspect parse.
func encodeRawPartition(ctx context.Context, w *bufio.Writer, symbol string, interval RawInterval, year int, month time.Month, records []Record) error {
	if _, err := fmt.Fprintf(w, "# schema=raw-v1 source=oanda instrument=%s tf=%s year=%04d month=%02d\n",
		symbol, interval, year, int(month)); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, rawV1Header); err != nil {
		return err
	}
	for _, r := range records {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "%s,%s,%s,%s,%s,%s,%s,%s,%s,%d,%t\n",
			r.Time.UTC().Format(time.RFC3339), r.BidOpen, r.BidHigh, r.BidLow, r.BidClose,
			r.AskOpen, r.AskHigh, r.AskLow, r.AskClose, r.Volume, r.Complete); err != nil {
			return err
		}
	}
	return nil
}

// ReadPartitionRecords reads and returns every record in the raw
// partition for (symbol, interval, year, month) under root, in file
// order (already ascending Time, per WritePartition). It exists so an
// "extend" sync can merge new records with what is already on disk
// without duplicating Open's own parsing; it is a thin wrapper over
// Open/Reader that discards Meta, since the caller already knows it.
func ReadPartitionRecords(ctx context.Context, root, symbol string, interval RawInterval, year int, month time.Month) ([]Record, error) {
	path := partitionPath(root, symbol, interval, year, month)
	r, err := Open(path)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	var records []Record
	for {
		if err := ctx.Err(); err != nil {
			return records, err
		}
		rec, err := r.Next(ctx)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return records, err
		}
		records = append(records, rec)
	}
	return records, nil
}
