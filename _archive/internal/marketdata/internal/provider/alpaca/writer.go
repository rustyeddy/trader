package alpaca

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// ErrPartitionAlreadyExists marks an attempt to write a brand-new raw
// partition (see WritePartition's mustNotExist parameter) at a path
// that already has a file — stooq.ErrPartitionAlreadyExists's own
// counterpart.
var ErrPartitionAlreadyExists = errors.New("alpaca: raw partition already exists")

// WritePartition atomically writes records as a raw-v1 partition file
// for (symbol, year, month) under root: a schema comment recording
// feed as this partition's own feed provenance, the raw-v1 column
// header, then one row per record in exactly the order given.
//
// Like stooq.WritePartition, records is deliberately *not* sorted
// before writing: Sync (marketdata/sync.go) merges newly fetched
// records with any already on disk and is responsible for the
// resulting order, and a live API fetch's own pagination order is not
// a substitute for an explicit ordering guarantee at this layer either.
//
// feed records which Alpaca data feed (FeedIEX/FeedSIP) produced
// records — issue #324 (EQ-11)'s own requirement that feed provenance
// be recorded rather than inferred later, since IEX and SIP are not
// directly comparable data for the same symbol/date (ADR-050/052).
// Sync (marketdata/sync.go) is responsible for refusing to silently
// mix two different feeds' data into one partition file; WritePartition
// itself only ever records whatever feed it is given.
//
// mustNotExist, when true, rejects (ErrPartitionAlreadyExists) writing
// over a path that already has a file. When false, an existing file at
// path is replaced — Sync always calls with false when merging fetched
// records into an existing partition, and with true only for a
// brand-new month with no existing file at all.
//
// The write is atomic: a temporary file alongside the destination,
// written, flushed, and synced first, then linked (mustNotExist) or
// renamed (replace) into place — the same discipline stooq.WritePartition
// and oanda.WritePartition already apply.
func WritePartition(ctx context.Context, root, symbol string, year int, month time.Month, feed Feed, records []Record, mustNotExist bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path := partitionPath(root, symbol, year, month)

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("alpaca: write partition: %w", err)
	}

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("alpaca: write partition: %w", err)
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
	if err := encodeRawPartition(ctx, bw, symbol, year, month, feed, records); err != nil {
		return fmt.Errorf("alpaca: write partition: %w", err)
	}
	if err := bw.Flush(); err != nil {
		return fmt.Errorf("alpaca: write partition: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("alpaca: write partition: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("alpaca: write partition: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	if mustNotExist {
		if err := os.Link(tmpPath, path); err != nil {
			if errors.Is(err, fs.ErrExist) {
				return fmt.Errorf("%w: %s", ErrPartitionAlreadyExists, path)
			}
			return fmt.Errorf("alpaca: write partition: %w", err)
		}
		succeeded = true
		_ = os.Remove(tmpPath)
		return nil
	}

	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("alpaca: write partition: %w", err)
	}
	succeeded = true
	return nil
}

func encodeRawPartition(ctx context.Context, w *bufio.Writer, symbol string, year int, month time.Month, feed Feed, records []Record) error {
	if _, err := fmt.Fprintf(w, "# schema=raw-v1 source=alpaca instrument=%s tf=%s year=%04d month=%02d feed=%s\n",
		symbol, RawD1, year, int(month), feed); err != nil {
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
