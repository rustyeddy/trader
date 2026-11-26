package canonical

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"time"

	"github.com/rustyeddy/trader/internal/marketdata/layout"
	"github.com/rustyeddy/trader/internal/store"
)

var (
	// ErrSnapshot reports an archived snapshot or its record that is not what its
	// name says: other bytes, or a record that cannot be read.
	ErrSnapshot = errors.New("canonical: bad snapshot archive")
	// ErrNoSnapshotDate reports bytes that were never archived and came without the
	// date they were obtained.
	ErrNoSnapshotDate = errors.New("canonical: a new snapshot needs the date it was obtained")
)

// SnapshotRecord is what is kept beside an archived input snapshot (ADR-095).
type SnapshotRecord struct {
	// Fingerprint is "sha256:" and the hash of the bytes; it names the files.
	Fingerprint string `json:"snapshot_fingerprint"`
	// AsOf is the date the bytes were obtained, YYYY-MM-DD, as the operator
	// declared it.
	AsOf string `json:"snapshot_as_of"`
	// File is the original file name; Member and ArchiveSHA256 are the member's
	// path and the ZIP's hash when the bytes were a ZIP member.
	File          string `json:"file"`
	Member        string `json:"member,omitempty"`
	ArchiveSHA256 string `json:"archive_sha256,omitempty"`
}

// Fingerprint identifies exact bytes: "sha256:" and 64 lower-case hex digits.
func Fingerprint(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// ArchiveSnapshot keeps data, unchanged, as a snapshot of the source's symbol with
// rec beside it, and returns the record in force and the root-relative path of the
// bytes. rec.Fingerprint is set from data. Neither file is ever overwritten: when
// the bytes were archived before, they are verified and the first record is
// returned, its date and origin standing whatever rec says. New bytes need
// rec.AsOf (ErrNoSnapshotDate), a YYYY-MM-DD date.
func ArchiveSnapshot(ctx context.Context, source, symbol string, data []byte, rec SnapshotRecord) (SnapshotRecord, string, error) {
	rec.Fingerprint = Fingerprint(data)
	rawPath, recPath, err := layout.Snapshot(source, symbol, rec.Fingerprint[len("sha256:"):])
	if err != nil {
		return SnapshotRecord{}, "", fmt.Errorf("canonical: %w", err)
	}
	old, found, err := readSnapshotRecord(recPath, rec.Fingerprint)
	if err != nil {
		return SnapshotRecord{}, "", err
	}
	if !found {
		if rec.AsOf == "" {
			return SnapshotRecord{}, "", fmt.Errorf("%w: %s", ErrNoSnapshotDate, rec.Fingerprint)
		}
		if _, err := time.Parse(time.DateOnly, rec.AsOf); err != nil {
			return SnapshotRecord{}, "", fmt.Errorf("%w: %q is not a YYYY-MM-DD date", ErrNoSnapshotDate, rec.AsOf)
		}
	}
	if err := keep(ctx, rawPath, data); err != nil {
		return SnapshotRecord{}, "", err
	}
	if found {
		return old, rawPath, nil
	}
	text, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return SnapshotRecord{}, "", fmt.Errorf("canonical: snapshot record: %w", err)
	}
	err = store.Create(ctx, recPath, append(text, '\n'))
	if errors.Is(err, fs.ErrExist) { // another build recorded it first: its record stands
		if old, found, err = readSnapshotRecord(recPath, rec.Fingerprint); err == nil && found {
			return old, rawPath, nil
		}
	}
	if err != nil {
		return SnapshotRecord{}, "", fmt.Errorf("canonical: snapshot record: %w", err)
	}
	return rec, rawPath, nil
}

// SnapshotDate returns the date already recorded for these bytes, and whether
// they have been archived. Nothing is written.
func SnapshotDate(source, symbol string, data []byte) (string, bool, error) {
	fp := Fingerprint(data)
	_, recPath, err := layout.Snapshot(source, symbol, fp[len("sha256:"):])
	if err != nil {
		return "", false, fmt.Errorf("canonical: %w", err)
	}
	rec, found, err := readSnapshotRecord(recPath, fp)
	return rec.AsOf, found, err
}

// keep creates the file with data, or verifies that the one there holds it.
func keep(ctx context.Context, path string, data []byte) error {
	err := store.Create(ctx, path, data)
	if err == nil {
		return nil
	}
	if !errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("canonical: snapshot: %w", err)
	}
	old, err := store.Read(path)
	if err != nil {
		return fmt.Errorf("canonical: snapshot: %w", err)
	}
	if !bytes.Equal(old, data) {
		return fmt.Errorf("%w: %s does not hold the bytes its name says", ErrSnapshot, path)
	}
	return nil
}

func readSnapshotRecord(path, fingerprint string) (SnapshotRecord, bool, error) {
	data, err := store.Read(path)
	if errors.Is(err, fs.ErrNotExist) {
		return SnapshotRecord{}, false, nil
	}
	if err != nil {
		return SnapshotRecord{}, false, fmt.Errorf("canonical: snapshot record: %w", err)
	}
	var rec SnapshotRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return SnapshotRecord{}, false, fmt.Errorf("%w: %s: %w", ErrSnapshot, path, err)
	}
	if rec.Fingerprint != fingerprint {
		return SnapshotRecord{}, false, fmt.Errorf("%w: %s records %q", ErrSnapshot, path, rec.Fingerprint)
	}
	if _, err := time.Parse(time.DateOnly, rec.AsOf); err != nil {
		return SnapshotRecord{}, false, fmt.Errorf("%w: %s has the date %q", ErrSnapshot, path, rec.AsOf)
	}
	return rec, true, nil
}

// Months returns the keys of the canonical files a series already has, in time
// order. A series with none, or no directory, has no months.
func Months(source, symbol string, interval Interval) ([]Key, error) {
	base, err := layout.StageDir(source, layout.Canonical)
	if err != nil {
		return nil, fmt.Errorf("canonical: %w", err)
	}
	files, err := store.List(filepath.Join(base, symbol))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("canonical: %w", err)
	}
	var keys []Key
	for _, file := range files {
		p, err := layout.Parse(file)
		if err != nil || p.Source != source || p.Instrument != symbol || p.Interval != string(interval) {
			continue // another interval, or not a partition file
		}
		keys = append(keys, Key{Source: source, Symbol: symbol, Interval: interval, Year: p.Year, Month: p.Month})
	}
	slices.SortFunc(keys, func(a, b Key) int {
		return time.Date(a.Year, a.Month, 1, 0, 0, 0, 0, time.UTC).Compare(time.Date(b.Year, b.Month, 1, 0, 0, 0, 0, time.UTC))
	})
	return keys, nil
}
