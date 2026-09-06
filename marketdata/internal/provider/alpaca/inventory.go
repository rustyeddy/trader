package alpaca

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
// partition file — stooq.PartitionStatus's own counterpart.
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

// Partition is one inspected monthly raw partition file — stooq.Partition's
// own counterpart. LastComplete is always true when Status is
// PartitionStatusOK and RowCount > 0: every Record this package's
// client writes already represents a fully closed trading day. This is
// not merely an assumption about what Phase 1 happens to exercise — the
// caller (marketdata.Manager.Sync's syncOneAlpaca) actively excludes the
// current, still-forming trading day from every fetch range until
// USEquityCalendar.RegularSessionEnd reports that day's regular session
// (including a configured half day) has actually closed, so a
// provisional/incomplete bar is never fetched or written in the first
// place (PR #312 review).
type Partition struct {
	Symbol string
	Year   int
	Month  time.Month
	Path   string

	Status PartitionStatus
	// Err is non-nil exactly when Status is not PartitionStatusOK.
	Err error

	RowCount     int
	FirstTime    time.Time
	LastTime     time.Time
	LastComplete bool

	Fingerprint string
}

// Inventory is a deterministic, read-only summary of a raw Alpaca
// archive rooted at Root.
type Inventory struct {
	Root string
	// Partitions is sorted by Symbol, then Year, then Month.
	Partitions []Partition
}

// Inspect walks root — a raw Alpaca archive laid out as
// root/SYMBOL/YYYY/MM/SYMBOL-YYYY-MM-d1.csv — and returns a
// deterministic Inventory describing every partition file found
// beneath it. Mirrors stooq.Inspect's identical walk/skip/error
// discipline exactly.
func Inspect(ctx context.Context, root string) (Inventory, error) {
	inv := Inventory{Root: root}

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("alpaca: inspect: walk %s: %w", path, err)
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
		return Inventory{}, fmt.Errorf("alpaca: inspect %s: %w", root, walkErr)
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
		if rowCount == 0 || rec.Time.Before(first) {
			first = rec.Time
		}
		if rowCount == 0 || rec.Time.After(last) {
			last = rec.Time
		}
		rowCount++
	}

	p.Status = PartitionStatusOK
	p.RowCount = rowCount
	p.FirstTime = first
	p.LastTime = last
	p.LastComplete = rowCount > 0
	return p, false, nil
}
