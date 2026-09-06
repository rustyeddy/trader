package alpaca

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"time"
)

// fingerprintBytes returns the "sha256:<hex>" content fingerprint form
// marketdata.Manifest.RawFingerprint expects (ADR-020) — identical to
// oanda.fingerprintBytes/stooq.fingerprintBytes.
func fingerprintBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// PartitionSnapshot pairs the records parsed from a raw partition with
// the content fingerprint of the exact bytes they were parsed from —
// stooq.PartitionSnapshot's own counterpart.
type PartitionSnapshot struct {
	Records     []Record
	Fingerprint string
}

// ReadPartitionSnapshot reads the raw partition file for (symbol, year,
// month) under root exactly once, then both fingerprints and parses
// that same in-memory byte slice, so Records and Fingerprint
// necessarily describe one identical revision of the file — the same
// one-read guarantee oanda/stooq's own ReadPartitionSnapshot provide.
func ReadPartitionSnapshot(ctx context.Context, root, symbol string, year int, month time.Month) (PartitionSnapshot, error) {
	path := partitionPath(root, symbol, year, month)
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
