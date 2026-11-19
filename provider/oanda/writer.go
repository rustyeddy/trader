package oanda

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"sort"
	"time"

	"github.com/rustyeddy/trader/internal/store"
	"github.com/rustyeddy/trader/num"
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

// rawTF is the interval spelling in the schema comment. The daily partition
// names its interval d1 but writes tf=d, exactly as the archive's files do, so
// a file this writes is byte-identical to one already on disk.
func rawTF(interval RawInterval) string {
	if interval == RawD1 {
		return "d"
	}
	return string(interval)
}

// WritePartition atomically writes records as a raw-v1 partition file
// for (symbol, interval, year, month) under the data root: a schema comment, the
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
// more immediately before the commit step — the actual commit point. A
// cancellation that arrives after the commit does not undo it.
//
// The store holds the partition's process-local lock for the duration of the
// write, so a replace never interleaves with a read-merge-commit by
// UpdatePartition on the same partition.
func WritePartition(ctx context.Context, key PartitionKey, records []Record, mustNotExist bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := key.Path()
	if err != nil {
		return err
	}
	data, err := encodePartition(ctx, key, records)
	if err != nil {
		return err
	}
	if mustNotExist {
		// Atomic create-if-absent in the store (link(2), not a Stat check
		// followed by a rename): see the doc comment above.
		if err := store.Create(ctx, path, data); err != nil {
			if errors.Is(err, fs.ErrExist) {
				return fmt.Errorf("%w: %s", ErrPartitionAlreadyExists, path)
			}
			return fmt.Errorf("oanda: write partition: %w", err)
		}
		return nil
	}
	if err := store.Write(ctx, path, data); err != nil {
		return fmt.Errorf("oanda: write partition: %w", err)
	}
	return nil
}

// encodePartition renders records as the partition's raw-v1 file: it rejects a
// record outside the partition's month and writes a sorted copy.
func encodePartition(ctx context.Context, key PartitionKey, records []Record) ([]byte, error) {
	for _, r := range records {
		if err := checkRecordMonth(key, r.Time); err != nil {
			return nil, fmt.Errorf("oanda: write partition: %w", err)
		}
	}
	sorted := append([]Record(nil), records...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Time.Before(sorted[j].Time) })

	var buf bytes.Buffer
	bw := bufio.NewWriter(&buf)
	if err := encodeRawPartition(ctx, bw, key, sorted); err != nil {
		return nil, fmt.Errorf("oanda: write partition: %w", err)
	}
	if err := bw.Flush(); err != nil {
		return nil, fmt.Errorf("oanda: write partition: %w", err)
	}
	return buf.Bytes(), nil
}

// rawPriceDecimals is the fixed number of decimals every price has in the
// archive's raw-v1 files, for every pair (107.27200 and 1.08050 alike).
const rawPriceDecimals = 5

// encodeRawPartition writes the schema comment, column header, and one
// row per record, in the exact raw-v1 shape Open/Inspect parse.
func encodeRawPartition(ctx context.Context, w *bufio.Writer, key PartitionKey, records []Record) error {
	if _, err := fmt.Fprintf(w, "# schema=raw-v1 source=oanda instrument=%s tf=%s year=%04d month=%02d\n",
		key.Symbol, rawTF(key.Interval), key.Year, int(key.Month)); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, rawV1Header); err != nil {
		return err
	}
	for _, r := range records {
		if err := ctx.Err(); err != nil {
			return err
		}
		var prices [8]string
		for i, p := range [8]num.Price{r.BidOpen, r.BidHigh, r.BidLow, r.BidClose, r.AskOpen, r.AskHigh, r.AskLow, r.AskClose} {
			text, err := p.FormatFixed(rawPriceDecimals)
			if err != nil {
				return fmt.Errorf("record at %s: %w: %w", r.Time.UTC().Format(time.RFC3339), ErrMalformedData, err)
			}
			prices[i] = text
		}
		if _, err := fmt.Fprintf(w, "%s,%s,%s,%s,%s,%s,%s,%s,%s,%d,%t\n",
			r.Time.UTC().Format(time.RFC3339), prices[0], prices[1], prices[2], prices[3],
			prices[4], prices[5], prices[6], prices[7], r.Volume, r.Complete); err != nil {
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
func ReadPartitionRecords(ctx context.Context, key PartitionKey) ([]Record, error) {
	path, err := key.Path()
	if err != nil {
		return nil, err
	}
	data, err := store.Read(path)
	if err != nil {
		return nil, err
	}
	return decodeRecords(ctx, path, data)
}

// decodeRecords parses the raw-v1 bytes of the partition at path into records.
func decodeRecords(ctx context.Context, path string, data []byte) ([]Record, error) {
	r, err := newReaderFromBytes(path, data)
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
func FingerprintPartition(key PartitionKey) (string, error) {
	path, err := key.Path()
	if err != nil {
		return "", err
	}
	data, err := store.Read(path)
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
func ReadPartitionSnapshot(ctx context.Context, key PartitionKey) (PartitionSnapshot, error) {
	path, err := key.Path()
	if err != nil {
		return PartitionSnapshot{}, err
	}
	data, err := store.Read(path)
	if err != nil {
		return PartitionSnapshot{}, err
	}
	records, err := decodeRecords(ctx, path, data)
	if err != nil {
		return PartitionSnapshot{}, err
	}
	return PartitionSnapshot{Records: records, Fingerprint: fingerprintBytes(data)}, nil
}

// UpdatePartition is the safe read-merge-commit for extending a partition: it
// holds the partition's lock across reading the existing records (none when the
// file does not exist), calling merge with them, and replacing the file with the
// result, so two overlapping updates cannot both start from the same revision
// and silently lose one writer's records. merge decides the policy (what is new,
// what wins on a duplicate time); a merge error leaves the file untouched.
// merge runs while the lock is held, so it must not write the same partition
// (WritePartition or UpdatePartition on it from inside merge would deadlock).
//
// The lock is process-local: two processes updating one partition are not
// coordinated (ADR-089). A failure or a context cancellation observed before the
// commit leaves any existing file untouched; one that arrives after does not
// undo it.
func UpdatePartition(ctx context.Context, key PartitionKey, merge func(existing []Record) ([]Record, error)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := key.Path()
	if err != nil {
		return err
	}
	return store.Update(ctx, path, func(old []byte, exists bool) ([]byte, error) {
		var existing []Record
		if exists {
			existing, err = decodeRecords(ctx, path, old)
			if err != nil {
				return nil, err
			}
		}
		merged, err := merge(existing)
		if err != nil {
			return nil, err
		}
		return encodePartition(ctx, key, merged)
	})
}
