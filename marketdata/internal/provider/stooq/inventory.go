package stooq

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// PartitionStatus classifies the outcome of inspecting one monthly raw
// partition file — oanda.PartitionStatus's own counterpart, minus the
// bid/ask-specific incomplete-count concept Stooq's data has no
// equivalent of.
type PartitionStatus uint8

const (
	// PartitionStatusUnknown is PartitionStatus's zero value; Inspect
	// never leaves a Partition with this status.
	PartitionStatusUnknown PartitionStatus = iota
	// PartitionStatusOK means the file was read, hashed, and its rows
	// parsed without error.
	PartitionStatusOK
	// PartitionStatusUnreadable means the file could not be opened or
	// read at the OS level.
	PartitionStatusUnreadable
	// PartitionStatusMalformed means the file was read successfully —
	// Fingerprint is set — but its rows failed to parse.
	PartitionStatusMalformed
)

// String returns a human-readable PartitionStatus name.
func (s PartitionStatus) String() string {
	switch s {
	case PartitionStatusUnknown:
		return "unknown"
	case PartitionStatusOK:
		return "ok"
	case PartitionStatusUnreadable:
		return "unreadable"
	case PartitionStatusMalformed:
		return "malformed"
	default:
		return fmt.Sprintf("PartitionStatus(%d)", uint8(s))
	}
}

// Partition is one inspected monthly raw partition file: its identity,
// integrity outcome, and summary facts — oanda.Partition's own
// counterpart. Stooq's daily data has no provider-declared
// "incomplete" concept (unlike an OANDA candle still forming when
// fetched): every successfully parsed row is, by construction, a
// closed trading day, so there is no IncompleteCount/LastComplete pair
// to report — LastComplete is always true when Status is
// PartitionStatusOK and RowCount > 0, letting Manager's needsExtend-
// style logic reuse the same field name across providers even though
// Stooq's own Provider implementation never actually calls it (see
// marketdata's own rawprovider.go).
type Partition struct {
	Symbol string
	Year   int
	Month  time.Month
	Path   string

	Status PartitionStatus
	// Err is non-nil exactly when Status is not PartitionStatusOK.
	Err error

	// RowCount, FirstTime, and LastTime are populated only when Status
	// is PartitionStatusOK.
	RowCount  int
	FirstTime time.Time
	LastTime  time.Time
	// LastComplete is always true for a PartitionStatusOK partition
	// with RowCount > 0 — see the type doc comment.
	LastComplete bool

	// Fingerprint is the "sha256:<hex>" content fingerprint of the raw
	// file's bytes (ADR-020). Set whenever the file could be read, even
	// if its rows then failed to parse.
	Fingerprint string
}

// Inventory is a deterministic, read-only summary of a raw Stooq
// archive rooted at Root.
type Inventory struct {
	Root string
	// Partitions is sorted by Symbol, then Year, then Month.
	Partitions []Partition
}

// Inspect walks root — a raw Stooq archive laid out as
// root/SYMBOL/YYYY/MM/SYMBOL-YYYY-MM-d1.csv — and returns a
// deterministic Inventory describing every partition file found beneath
// it. Inspect never writes, renames, deletes, or otherwise modifies
// anything under root, and never searches for root itself.
//
// A file that cannot be opened, or whose rows fail to parse, is
// recorded with a non-OK PartitionStatus and Err rather than aborting
// the walk — oanda.Inspect's own "one bad file must not hide the rest"
// discipline. A file whose name does not resolve to a partition at all
// (parsePathMeta failure) or whose location relative to root disagrees
// with its own name (verifyPathLayout failure) is silently skipped, not
// recorded as a malformed Partition: it has no resolved identity to
// anchor one on. This package does not track the reason for each
// skip the way oanda.SkippedEntry does — Stooq's own archive layout is
// this package's own, private convention with no legacy corpus quirks
// to diagnose, unlike oanda's preserved historical archive.
func Inspect(ctx context.Context, root string) (Inventory, error) {
	inv := Inventory{Root: root}

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("stooq: inspect: walk %s: %w", path, err)
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if d.IsDir() || filepath.Ext(path) != ".csv" {
			return nil
		}

		partition, skip, fatalErr := inspectFile(ctx, root, path)
		if fatalErr != nil {
			return fatalErr
		}
		if skip {
			return nil
		}
		inv.Partitions = append(inv.Partitions, partition)
		return nil
	})
	if walkErr != nil {
		return Inventory{}, fmt.Errorf("stooq: inspect %s: %w", root, walkErr)
	}

	sort.Slice(inv.Partitions, func(i, j int) bool {
		a, b := inv.Partitions[i], inv.Partitions[j]
		if a.Symbol != b.Symbol {
			return a.Symbol < b.Symbol
		}
		if a.Year != b.Year {
			return a.Year < b.Year
		}
		return a.Month < b.Month
	})
	return inv, nil
}

func inspectFile(ctx context.Context, root, path string) (Partition, bool, error) {
	m, err := parsePathMeta(path)
	if err != nil {
		return Partition{}, true, nil
	}
	if err := verifyPathLayout(root, path, m); err != nil {
		return Partition{}, true, nil
	}

	p := Partition{Symbol: m.Symbol, Year: m.Year, Month: m.Month, Path: path}

	if err := ctx.Err(); err != nil {
		return Partition{}, false, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return Partition{}, false, ctxErr
		}
		p.Status = PartitionStatusUnreadable
		p.Err = err
		return p, false, nil
	}
	p.Fingerprint = fingerprintBytes(data)

	r, err := newReaderFromBytes(path, data)
	if err != nil {
		p.Status = PartitionStatusMalformed
		p.Err = err
		return p, false, nil
	}
	defer func() { _ = r.Close() }()

	var rowCount int
	var first, last time.Time
	for {
		if err := ctx.Err(); err != nil {
			return Partition{}, false, err
		}
		rec, err := r.Next(ctx)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			p.Status = PartitionStatusMalformed
			p.Err = err
			return p, false, nil
		}
		if rowCount == 0 {
			first = rec.Time
		}
		last = rec.Time
		rowCount++
	}

	p.Status = PartitionStatusOK
	p.RowCount = rowCount
	p.FirstTime = first
	p.LastTime = last
	p.LastComplete = rowCount > 0
	return p, false, nil
}
