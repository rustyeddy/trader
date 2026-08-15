package oanda

import (
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
	"strings"
	"time"

	"github.com/rustyeddy/trader/instrument"
)

// PartitionStatus classifies the outcome of inspecting one monthly raw
// partition file.
type PartitionStatus uint8

const (
	// PartitionStatusUnknown is PartitionStatus's zero value; Inspect
	// never leaves a Partition with this status.
	PartitionStatusUnknown PartitionStatus = iota
	// PartitionStatusOK means the file was read, hashed, and its rows
	// parsed without error. RowCount, IncompleteCount, DuplicateTimes,
	// FirstTime, LastTime, and Fingerprint are all populated.
	PartitionStatusOK
	// PartitionStatusUnreadable means the file could not be opened or
	// read at the OS level (permissions, an I/O error, or it vanished
	// between listing and reading). Err holds the cause; Fingerprint and
	// the row-derived fields are unset.
	PartitionStatusUnreadable
	// PartitionStatusMalformed means the file was read successfully —
	// Fingerprint is set — but its rows failed to parse: a bad header,
	// schema cross-check mismatch, or malformed row (ErrMalformedData).
	// Err holds the cause; the row-derived fields are unset.
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
// integrity outcome, and summary facts. Inspect reads a partition's
// bytes and rows only to compute these summary facts and a content
// fingerprint; it never retains or exposes the underlying price data,
// and never writes to the file.
type Partition struct {
	Instrument instrument.ID
	Symbol     string
	Interval   RawInterval
	Year       int
	Month      time.Month
	Path       string

	Status PartitionStatus
	// Err is non-nil exactly when Status is not PartitionStatusOK.
	Err error

	// RowCount, IncompleteCount, DuplicateTimes, FirstTime, and LastTime
	// are populated only when Status is PartitionStatusOK.
	RowCount        int
	IncompleteCount int
	// DuplicateTimes lists every Time value that appears more than once
	// in the partition, sorted ascending. It is empty, not nil, when
	// none are found and Status is PartitionStatusOK.
	DuplicateTimes []time.Time
	FirstTime      time.Time
	LastTime       time.Time

	// Fingerprint is an algorithm-qualified content hash of the raw
	// file's bytes, in the "sha256:<hex>" form marketdata.Manifest's
	// RawFingerprint field expects (ADR-020). It is set whenever the
	// file could be read, even if its rows then failed to parse — the
	// fingerprint is a property of the bytes, independent of whether
	// this package could interpret them.
	Fingerprint string
}

// SkippedEntry records an archive file Inspect deliberately did not
// inventory as a Partition, and why. This is not itself an integrity
// problem: an out-of-scope instrument (XAUUSD) or an interval the raw
// corpus does not carry (a stray w1 file) are expected, named exclusions
// (see resolveSymbol and resolveInterval). A file name that does not
// match the PAIR-YYYY-MM-tf.csv shape at all is also recorded here,
// since it has no resolved instrument/interval/year/month to anchor a
// Partition on; Reason is then a wrapped ErrMalformedData, distinguishable
// via errors.Is from the two scope exclusions.
type SkippedEntry struct {
	Path   string
	Reason error
}

// MonthGap is a hole in an otherwise-populated (Instrument, Interval)
// month sequence: at least one month strictly between the earliest and
// latest partitions Inspect found for that pair and interval has no
// partition file at all, even though earlier and later months do.
//
// MonthGap does not judge whether a gap is expected — a pair simply not
// yet trading that early, or not yet backfilled that recently — or a
// real loss; that judgment needs data-start knowledge this package does
// not have. It also does not attempt to detect a missing bar *within* a
// present partition: whether an individual absent interval reflects a
// routine market closure or a genuine gap requires trading-calendar
// knowledge, which belongs to issue #79's coverage engine operating on
// canonical data — this package intentionally never imports the root
// marketdata package (see the package doc comment), so it has no access
// to that knowledge and does not guess.
type MonthGap struct {
	Instrument instrument.ID
	Symbol     string
	Interval   RawInterval
	Year       int
	Month      time.Month
}

// Inventory is a deterministic, read-only summary of a raw OANDA archive
// rooted at Root (issue #75, ADR-020).
type Inventory struct {
	Root string
	// Partitions is sorted by Symbol, then Interval, then Year, then
	// Month.
	Partitions []Partition
	// Skipped is sorted by Path.
	Skipped []SkippedEntry
	// Gaps is sorted by Symbol, then Interval, then Year, then Month.
	Gaps []MonthGap
}

// Inspect walks root — a raw OANDA archive laid out as
// root/PAIR/YYYY/MM/PAIR-YYYY-MM-<tf>.csv — and returns a deterministic
// Inventory describing every partition file found beneath it. Inspect
// never writes, renames, deletes, or otherwise modifies anything under
// root.
//
// Inspect never searches for root; the caller supplies the exact raw
// archive directory to scan, typically Manager's configured raw root. A
// sibling tree that happens to share the same PAIR/YYYY/MM shape — a
// candles backup or save tree, for example — is therefore never seen
// unless the caller mistakenly points root at it directly: Inspect only
// ever descends from the root it is given, and never walks upward or
// sideways to find one. Nor can such a tree be mistaken for the real
// archive by being nested *inside* root (a backup subtree parallel to
// the pair directories, or a stray file filed under the wrong
// pair/year/month directory): every candidate file's path relative to
// root must itself be exactly SYMBOL/YYYY/MM/<filename>, agreeing with
// what the file name resolves to, or it is recorded as skipped rather
// than inventoried as a partition — see verifyPathLayout.
//
// A file that cannot be opened, or whose rows fail to parse, is recorded
// in the returned Inventory with a non-OK PartitionStatus and Err rather
// than aborting the walk: integrity problems are exactly Inspect's
// subject matter, so one bad file must not hide the rest of the
// inventory behind it. Only a failure to list root or one of its
// subdirectories aborts Inspect entirely, since there is nothing
// meaningful to report about entries Inspect could not even enumerate.
//
// Inspect honors ctx cancellation both between files and while one is
// being parsed, so a large full-archive run can be stopped promptly and
// a cancellation arriving mid-file is never misreported as that file
// being malformed (see inspectFile). It performs no long-running
// background work and starts no goroutines; it returns once the walk
// completes, fails, or ctx is done.
func Inspect(ctx context.Context, root string) (Inventory, error) {
	inv := Inventory{Root: root}

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("oanda: inspect: walk %s: %w", path, err)
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if d.IsDir() || filepath.Ext(path) != ".csv" {
			return nil
		}

		partition, skipped, fatalErr := inspectFile(ctx, root, path)
		if fatalErr != nil {
			// Only ctx cancellation/deadline reaches here (see
			// inspectFile): archive corruption is never fatal to the
			// walk, but the caller asking Inspect to stop is.
			return fatalErr
		}
		if skipped != nil {
			inv.Skipped = append(inv.Skipped, *skipped)
			return nil
		}
		inv.Partitions = append(inv.Partitions, partition)
		return nil
	})
	if walkErr != nil {
		return Inventory{}, fmt.Errorf("oanda: inspect %s: %w", root, walkErr)
	}

	sortPartitions(inv.Partitions)
	sortSkipped(inv.Skipped)
	inv.Gaps = findGaps(inv.Partitions)
	return inv, nil
}

// inspectFile inventories one candidate file. It returns either a
// Partition (possibly with a non-OK Status), a non-nil *SkippedEntry, or
// a non-nil fatal error — never more than one of the three.
//
// The fatal return is reserved for ctx being cancelled or timing out
// while inspectFile was itself doing work (as opposed to Inspect's own
// per-file check in its WalkDir callback, which only ever catches
// cancellation observed *between* files). Without distinguishing it,
// cancellation arriving mid-parse would flow through the same path as a
// genuine parse failure and mislabel the partition
// PartitionStatusMalformed instead of stopping the walk — archive
// corruption must never be fatal to Inspect, but the caller asking it to
// stop must be.
func inspectFile(ctx context.Context, root, path string) (Partition, *SkippedEntry, error) {
	meta, _, err := parsePathMeta(path)
	if err != nil {
		// ErrInstrumentOutOfScope, ErrUnsupportedInterval, and a
		// malformed file name (ErrMalformedData, no parts to resolve at
		// all) are all "this file is not a partition Inspect can
		// inventory," distinguished from each other only by Reason.
		return Partition{}, &SkippedEntry{Path: path, Reason: err}, nil
	}
	if err := verifyPathLayout(root, path, meta); err != nil {
		// A file name can resolve to a perfectly valid partition on its
		// own and still not belong where it was found: nested inside an
		// unrelated backup/save subtree beneath root, or filed under the
		// wrong pair/year/month directory entirely. parsePathMeta alone
		// only ever looks at the file's own name, so this is the check
		// that actually stops such a file from being silently accepted
		// as authoritative.
		return Partition{}, &SkippedEntry{Path: path, Reason: err}, nil
	}

	p := Partition{
		Instrument: meta.Instrument,
		Symbol:     meta.Symbol,
		Interval:   meta.Interval,
		Year:       meta.Year,
		Month:      meta.Month,
		Path:       path,
	}

	// Read the file's bytes exactly once: fingerprint them directly, and
	// parse records from the same in-memory copy via
	// newReaderFromBytes rather than reopening and re-reading the file a
	// second time through ReadFile(path). This also means a file that
	// vanishes or becomes unreadable between listing and reading can
	// only ever surface here, as PartitionStatusUnreadable — there is no
	// second filesystem access left that could instead misreport it as
	// PartitionStatusMalformed.
	data, err := os.ReadFile(path)
	if err != nil {
		p.Status = PartitionStatusUnreadable
		p.Err = err
		return p, nil, nil
	}
	sum := sha256.Sum256(data)
	p.Fingerprint = "sha256:" + hex.EncodeToString(sum[:])

	records, readErr := readRecordsFromBytes(ctx, path, data)
	if readErr != nil {
		if errors.Is(readErr, context.Canceled) || errors.Is(readErr, context.DeadlineExceeded) {
			return Partition{}, nil, readErr
		}
		p.Status = PartitionStatusMalformed
		p.Err = readErr
		return p, nil, nil
	}

	p.Status = PartitionStatusOK
	p.RowCount = len(records)
	seen := make(map[time.Time]int, len(records))
	for _, r := range records {
		if !r.Complete {
			p.IncompleteCount++
		}
		seen[r.Time]++
		if p.FirstTime.IsZero() || r.Time.Before(p.FirstTime) {
			p.FirstTime = r.Time
		}
		if r.Time.After(p.LastTime) {
			p.LastTime = r.Time
		}
	}
	for t, n := range seen {
		if n > 1 {
			p.DuplicateTimes = append(p.DuplicateTimes, t)
		}
	}
	sort.Slice(p.DuplicateTimes, func(i, j int) bool { return p.DuplicateTimes[i].Before(p.DuplicateTimes[j]) })
	return p, nil, nil
}

// verifyPathLayout checks that path, relative to root, is exactly
// SYMBOL/YYYY/MM/<filename> and that those three directory components
// agree with meta — itself resolved from path's file name alone by
// parsePathMeta, which never looks at directory structure. This is what
// actually prevents a nested backup/save subtree, or a file simply filed
// under the wrong pair/year/month directory, from being accepted as an
// authoritative partition merely because its own file name happens to
// parse: both would otherwise produce a valid Meta while disagreeing
// with where the file was actually found.
func verifyPathLayout(root, path string, meta Meta) error {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return fmt.Errorf("%w: %s: not under root %s: %v", ErrMalformedData, path, root, err)
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) != 4 {
		return fmt.Errorf("%w: %s: expected SYMBOL/YYYY/MM/filename beneath root, got %d path component(s)",
			ErrMalformedData, path, len(parts))
	}
	dirSymbol, dirYear, dirMonth := parts[0], parts[1], parts[2]
	if dirSymbol != meta.Symbol {
		return fmt.Errorf("%w: %s: directory %q disagrees with file name instrument %q",
			ErrMalformedData, path, dirSymbol, meta.Symbol)
	}
	if wantYear := fmt.Sprintf("%04d", meta.Year); dirYear != wantYear {
		return fmt.Errorf("%w: %s: directory year %q disagrees with file name year %q",
			ErrMalformedData, path, dirYear, wantYear)
	}
	if wantMonth := fmt.Sprintf("%02d", int(meta.Month)); dirMonth != wantMonth {
		return fmt.Errorf("%w: %s: directory month %q disagrees with file name month %q",
			ErrMalformedData, path, dirMonth, wantMonth)
	}
	return nil
}

// readRecordsFromBytes parses path's records from data, already read
// into memory by the caller, via newReaderFromBytes — avoiding a second
// disk read of a file inspectFile has already fingerprinted.
func readRecordsFromBytes(ctx context.Context, path string, data []byte) ([]Record, error) {
	r, err := newReaderFromBytes(path, data)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	var records []Record
	for {
		rec, err := r.Next(ctx)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		records = append(records, rec)
	}
	return records, nil
}

func sortPartitions(ps []Partition) {
	sort.Slice(ps, func(i, j int) bool {
		a, b := ps[i], ps[j]
		if a.Symbol != b.Symbol {
			return a.Symbol < b.Symbol
		}
		if a.Interval != b.Interval {
			return a.Interval < b.Interval
		}
		if a.Year != b.Year {
			return a.Year < b.Year
		}
		return a.Month < b.Month
	})
}

func sortSkipped(ss []SkippedEntry) {
	sort.Slice(ss, func(i, j int) bool { return ss[i].Path < ss[j].Path })
}

// monthKey is a comparable (year, month) pair used to detect gaps in a
// partition sequence without needing a calendar.
type monthKey struct {
	year  int
	month time.Month
}

// next returns the month key immediately following k.
func (k monthKey) next() monthKey {
	if k.month == time.December {
		return monthKey{year: k.year + 1, month: time.January}
	}
	return monthKey{year: k.year, month: k.month + 1}
}

// before reports whether k precedes o.
func (k monthKey) before(o monthKey) bool {
	if k.year != o.year {
		return k.year < o.year
	}
	return k.month < o.month
}

// findGaps groups partitions by (Instrument, Interval) and reports every
// month strictly between each group's earliest and latest partition that
// has no partition file at all, regardless of that present partition's
// Status — a malformed or unreadable file still means the file exists,
// so its month is not a gap.
func findGaps(partitions []Partition) []MonthGap {
	type groupKey struct {
		instrument instrument.ID
		interval   RawInterval
	}
	type group struct {
		symbol  string
		present map[monthKey]struct{}
		min     monthKey
		max     monthKey
	}

	groups := make(map[groupKey]*group)
	for _, p := range partitions {
		gk := groupKey{p.Instrument, p.Interval}
		mk := monthKey{p.Year, p.Month}
		g, ok := groups[gk]
		if !ok {
			g = &group{symbol: p.Symbol, present: make(map[monthKey]struct{}), min: mk, max: mk}
			groups[gk] = g
		}
		g.present[mk] = struct{}{}
		if mk.before(g.min) {
			g.min = mk
		}
		if g.max.before(mk) {
			g.max = mk
		}
	}

	var gaps []MonthGap
	for gk, g := range groups {
		for mk := g.min.next(); mk.before(g.max); mk = mk.next() {
			if _, ok := g.present[mk]; ok {
				continue
			}
			gaps = append(gaps, MonthGap{
				Instrument: gk.instrument,
				Symbol:     g.symbol,
				Interval:   gk.interval,
				Year:       mk.year,
				Month:      mk.month,
			})
		}
	}
	sort.Slice(gaps, func(i, j int) bool {
		a, b := gaps[i], gaps[j]
		if a.Symbol != b.Symbol {
			return a.Symbol < b.Symbol
		}
		if a.Interval != b.Interval {
			return a.Interval < b.Interval
		}
		if a.Year != b.Year {
			return a.Year < b.Year
		}
		return a.Month < b.Month
	})
	return gaps
}
