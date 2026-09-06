package stooq

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// ErrPartitionAlreadyExists marks an attempt to write a brand-new raw
// partition (see WritePartition's mustNotExist parameter) at a path
// that already has a file — oanda.ErrPartitionAlreadyExists's own
// counterpart.
var ErrPartitionAlreadyExists = errors.New("stooq: raw partition already exists")

// WritePartition atomically writes records as a raw-v1 partition file
// for (symbol, year, month) under root: a schema comment, the raw-v1
// column header, then one row per record in ascending Time order.
// records need not already be sorted.
//
// mustNotExist, when true, rejects (ErrPartitionAlreadyExists) writing
// over a path that already has a file. When false, an existing file at
// path is replaced — Import (import.go) always calls with false, since
// an operator re-running Import over an updated Stooq export is exactly
// how this package's raw archive is kept current (there is no live
// "extend" the way oanda.Client provides — see the package doc
// comment).
//
// The write is atomic: a temporary file alongside the destination,
// written, flushed, and synced first, then linked (mustNotExist) or
// renamed (replace) into place — the same discipline oanda.WritePartition
// and marketdata.canonicalCSVStore.publish already apply.
func WritePartition(ctx context.Context, root, symbol string, year int, month time.Month, records []Record, mustNotExist bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path := partitionPath(root, symbol, year, month)

	sorted := append([]Record(nil), records...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Time.Before(sorted[j].Time) })

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("stooq: write partition: %w", err)
	}

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("stooq: write partition: %w", err)
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
	if err := encodeRawPartition(ctx, bw, symbol, year, month, sorted); err != nil {
		return fmt.Errorf("stooq: write partition: %w", err)
	}
	if err := bw.Flush(); err != nil {
		return fmt.Errorf("stooq: write partition: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("stooq: write partition: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("stooq: write partition: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	if mustNotExist {
		if err := os.Link(tmpPath, path); err != nil {
			if errors.Is(err, fs.ErrExist) {
				return fmt.Errorf("%w: %s", ErrPartitionAlreadyExists, path)
			}
			return fmt.Errorf("stooq: write partition: %w", err)
		}
		succeeded = true
		_ = os.Remove(tmpPath)
		return nil
	}

	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("stooq: write partition: %w", err)
	}
	succeeded = true
	return nil
}

func encodeRawPartition(ctx context.Context, w *bufio.Writer, symbol string, year int, month time.Month, records []Record) error {
	if _, err := fmt.Fprintf(w, "# schema=raw-v1 source=stooq instrument=%s tf=%s year=%04d month=%02d\n",
		symbol, RawD1, year, int(month)); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, rawV1Header); err != nil {
		return err
	}
	for _, r := range records {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "%s,%s,%s,%s,%s,%d\n",
			r.Time.UTC().Format("2006-01-02"), r.Open, r.High, r.Low, r.Close, r.Volume); err != nil {
			return err
		}
	}
	return nil
}
