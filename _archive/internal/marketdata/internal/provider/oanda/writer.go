package oanda

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
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
// mustNotExist's guarantee is enforced atomically, not by checking
// os.Stat before writing: an initial existence check followed later by
// os.Rename leaves a window in between — on Unix, Rename silently
// replaces an existing destination regardless of what a prior Stat saw,
// so a file created in that window would be silently overwritten
// exactly the way this parameter promises not to. Instead, the new-file
// case links (os.Link) the fully-written temporary file to path:
// link(2) atomically fails with EEXIST if path already exists, with no
// separate check-then-act window at all, and this package's own
// TestWritePartition_ConcurrentMustNotExistIsRaceFree proves exactly
// one of two concurrent callers targeting the same path can ever
// succeed. The extend case (mustNotExist false) still uses Rename,
// which is the correct, cheaper primitive there: replacing an existing
// file is exactly what an authorized extend means to do.
//
// The write is atomic regardless of which commit primitive is used: a
// temporary file alongside the destination, written, flushed, and
// synced first. A failure or a context cancellation observed before the
// commit leaves any existing file at path completely untouched; ctx is
// checked before writing begins, once per record while encoding (a
// large M1 partition can run to tens of thousands of rows), and once
// more immediately before the commit step — the actual commit point.
func WritePartition(ctx context.Context, root, symbol string, interval RawInterval, year int, month time.Month, records []Record, mustNotExist bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path := partitionPath(root, symbol, interval, year, month)

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

	if mustNotExist {
		// Atomic create-if-not-exists: see the doc comment above for why
		// this is Link, not a Stat check followed by Rename.
		if err := os.Link(tmpPath, path); err != nil {
			if errors.Is(err, fs.ErrExist) {
				return fmt.Errorf("%w: %s", ErrPartitionAlreadyExists, path)
			}
			return fmt.Errorf("oanda: write partition: %w", err)
		}
		succeeded = true
		// The temporary name is now a second link to the same file
		// Link just created at path; remove it so only path remains.
		// Best-effort: the durable copy is already safely at path
		// regardless of whether this cleanup succeeds.
		_ = os.Remove(tmpPath)
		return nil
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
	defer func() { _ = r.Close() }()

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

// FingerprintPartition returns the "sha256:<hex>" content fingerprint
// (Inspect's own Partition.Fingerprint form, and the form
// marketdata.Manifest.RawFingerprint expects, ADR-020) of the raw
// partition file for (symbol, interval, year, month) under root — a
// targeted, single-file equivalent of what Inspect computes while
// walking an entire archive, for a caller (marketdata's canonical build,
// issue #81) that already knows exactly which partition it needs and
// should not have to re-walk the whole raw tree to get one file's hash.
//
// Calling FingerprintPartition and ReadPartitionRecords separately opens
// the file twice, which admits a window in which Sync (#80) atomically
// replaces the file — via its own Link/Rename commit — in between the
// two reads: the records parsed would then belong to one revision while
// the recorded fingerprint names another. A caller that needs both a
// partition's records and its fingerprint describing the exact same
// bytes must use ReadPartitionSnapshot instead, not this function
// followed by ReadPartitionRecords (or the reverse order).
func FingerprintPartition(root, symbol string, interval RawInterval, year int, month time.Month) (string, error) {
	path := partitionPath(root, symbol, interval, year, month)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return fingerprintBytes(data), nil
}

// fingerprintBytes is FingerprintPartition/ReadPartitionSnapshot's
// shared "sha256:<hex>" encoding of already-read file content.
func fingerprintBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// PartitionSnapshot pairs the records parsed from a raw partition with
// the content fingerprint of the exact bytes they were parsed from.
type PartitionSnapshot struct {
	Records     []Record
	Fingerprint string
}

// ReadPartitionSnapshot reads the raw partition file for (symbol,
// interval, year, month) under root exactly once — a single
// os.ReadFile — then both fingerprints and parses that same in-memory
// byte slice, so Records and Fingerprint necessarily describe one
// identical revision of the file. This is the atomic-snapshot
// counterpart to calling FingerprintPartition and ReadPartitionRecords
// separately (see FingerprintPartition's own doc comment for the race
// that would otherwise admit): a caller — marketdata's canonical
// normalization build, issue #81 — that needs a raw fingerprint to
// record alongside the canonical bars built from that exact data must
// use this function, not the two-open pattern.
func ReadPartitionSnapshot(ctx context.Context, root, symbol string, interval RawInterval, year int, month time.Month) (PartitionSnapshot, error) {
	path := partitionPath(root, symbol, interval, year, month)
	data, err := os.ReadFile(path)
	if err != nil {
		return PartitionSnapshot{}, err
	}
	fingerprint := fingerprintBytes(data)

	r, err := newReaderFromBytes(path, data)
	if err != nil {
		return PartitionSnapshot{}, err
	}
	defer func() { _ = r.Close() }()

	var records []Record
	for {
		if err := ctx.Err(); err != nil {
			return PartitionSnapshot{}, err
		}
		rec, err := r.Next(ctx)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return PartitionSnapshot{}, err
		}
		records = append(records, rec)
	}
	return PartitionSnapshot{Records: records, Fingerprint: fingerprint}, nil
}
