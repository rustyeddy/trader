package marketdata

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
)

// canonicalManifestSchema and canonicalBarSchema tag the two file
// formats this store reads and writes, the same convention the raw
// OANDA reader uses for its own "# schema=..." line. Neither is a
// public wire format: both are internal to marketdata, checked only to
// catch a file that does not contain what its path claims.
const (
	canonicalManifestSchema = "canonical-manifest-v1"
	canonicalBarSchema      = "canonical-bar-v1"
)

// canonicalCSVHeader is the exact canonical Bar CSV column header.
const canonicalCSVHeader = "time,open,high,low,close,avg_spread,max_spread,ticks"

// Sentinel errors returned (wrapped) by the canonical store.
var (
	// errStoreMalformed marks a data or manifest file that does not
	// parse, or whose content disagrees with what its path or caller-
	// supplied identity claims.
	errStoreMalformed = errors.New("marketdata: store: malformed file")
	// errStoreUnsupportedInterval marks an Interval this store cannot
	// map to a path token.
	errStoreUnsupportedInterval = errors.New("marketdata: store: unsupported interval")
)

// partitionKey identifies one canonical partition file pair (data and
// manifest) within a store root. instrument is carried alongside symbol
// the same way oanda.Meta carries both: symbol is what builds a
// filesystem path, instrument is Trader's canonical identity, and
// neither is reliably derivable from the other without a resolver this
// package does not have.
type partitionKey struct {
	provider   string
	symbol     string
	instrument instrument.ID
	interval   Interval
	year       int
	month      time.Month
}

// dir returns the partition's directory under root, following ADR-020's
// derived-tree convention: root/provider/SYMBOL/YYYY/MM.
func (k partitionKey) dir(root string) string {
	return filepath.Join(root, k.provider, k.symbol, fmt.Sprintf("%04d", k.year), fmt.Sprintf("%02d", int(k.month)))
}

// baseName returns the partition's file base name, without extension:
// SYMBOL-YYYY-MM-<tf>.
func (k partitionKey) baseName() (string, error) {
	token, err := intervalToken(k.interval)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%04d-%02d-%s", k.symbol, k.year, int(k.month), token), nil
}

// dataPath and manifestPath return the two paths a partition occupies.
// Neither ever encodes a revision: ADR-020 requires the version
// identifier to live in the manifest or a file header, never the path.
func (k partitionKey) dataPath(root string) (string, error) {
	base, err := k.baseName()
	if err != nil {
		return "", err
	}
	return filepath.Join(k.dir(root), base+".csv"), nil
}

func (k partitionKey) manifestPath(root string) (string, error) {
	base, err := k.baseName()
	if err != nil {
		return "", err
	}
	return filepath.Join(k.dir(root), base+".manifest"), nil
}

// intervalToken maps a canonical Interval to its path token. Unlike
// oanda.RawInterval (raw-only, no w1), the canonical/derived tree also
// carries W1, since W1 is a derived interval resampled from canonical
// D1 (ADR-020) and lives in this store like any other.
func intervalToken(i Interval) (string, error) {
	switch i {
	case M1:
		return "m1", nil
	case H1:
		return "h1", nil
	case H4:
		return "h4", nil
	case D1:
		return "d1", nil
	case W1:
		return "w1", nil
	default:
		return "", fmt.Errorf("marketdata: store: %w: %s", errStoreUnsupportedInterval, i)
	}
}

// canonicalCSVStore is marketdata's CSV-backed implementation of
// barStore (issue #77, ADR-020). CSV is selected pragmatically because
// the existing raw and canonical archives already use it; it is not a
// public storage contract, and nothing about barStore or partitionKey
// prevents a future Parquet implementation.
type canonicalCSVStore struct {
	rootDir string
}

// newCanonicalCSVStore returns a canonicalCSVStore rooted at root.
func newCanonicalCSVStore(root string) *canonicalCSVStore {
	return &canonicalCSVStore{rootDir: root}
}

var _ barStore = (*canonicalCSVStore)(nil)

// root reports the store's configured root, for diagnostics only.
func (s *canonicalCSVStore) root() string { return s.rootDir }

// publish validates m, bs, and their pairing (Manifest.Matches), then
// writes both to disk.
//
// # Atomicity
//
// os.Rename is atomic for one file on the same filesystem, but there is
// no single-operation atomic rename across two files — and ADR-020
// rules out the usual revision-suffixed-filename-plus-pointer-file
// trick that would sidestep that ("the version identifier ... does not
// appear in the path"). publish therefore writes the new data and
// manifest to temporary names first, then renames data into place
// followed by manifest into place — manifest last, deliberately.
//
// A reader must always call Manifest.Matches(bs) before trusting a
// loaded pair (load does this). That makes the narrow window between
// the two renames safe: a reader that lands there sees new data paired
// with the still-old manifest, which fails Matches and is correctly
// read as "not currently published" rather than silently served as
// valid, mismatched data. If publish is cancelled or fails before
// either rename — including while still writing the temporary files —
// the previously published pair is completely untouched.
func (s *canonicalCSVStore) publish(ctx context.Context, key partitionKey, m Manifest, bs BarSet) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := m.Validate(); err != nil {
		return fmt.Errorf("marketdata: store: publish: manifest: %w", err)
	}
	if err := bs.Validate(); err != nil {
		return fmt.Errorf("marketdata: store: publish: bar set: %w", err)
	}
	if err := m.Matches(bs); err != nil {
		return fmt.Errorf("marketdata: store: publish: %w", err)
	}

	dataPath, err := key.dataPath(s.rootDir)
	if err != nil {
		return err
	}
	manifestPath, err := key.manifestPath(s.rootDir)
	if err != nil {
		return err
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	if err := writeFileAtomic(dataPath, func(w *bufio.Writer) error {
		return encodeBarSet(w, key, bs)
	}); err != nil {
		return fmt.Errorf("marketdata: store: publish: data: %w", err)
	}

	if err := ctx.Err(); err != nil {
		// Data is already published under the new revision; the old
		// manifest is still in place and will fail Matches against it
		// (see the doc comment above) until a later successful publish
		// completes both writes.
		return err
	}
	if err := writeFileAtomic(manifestPath, func(w *bufio.Writer) error {
		return encodeManifest(w, m)
	}); err != nil {
		return fmt.Errorf("marketdata: store: publish: manifest: %w", err)
	}
	return nil
}

// load reads the manifest and data files for key, verifies they Match,
// and returns them. It reports an error — never a partial or
// best-effort result — for a missing file, a malformed file, or a
// manifest/data pair that disagrees.
func (s *canonicalCSVStore) load(ctx context.Context, key partitionKey) (Manifest, BarSet, error) {
	if err := ctx.Err(); err != nil {
		return Manifest{}, BarSet{}, err
	}

	manifestPath, err := key.manifestPath(s.rootDir)
	if err != nil {
		return Manifest{}, BarSet{}, err
	}
	dataPath, err := key.dataPath(s.rootDir)
	if err != nil {
		return Manifest{}, BarSet{}, err
	}

	m, err := readManifestFile(manifestPath, key.instrument)
	if err != nil {
		return Manifest{}, BarSet{}, err
	}
	bars, err := readBarSetFile(dataPath, key)
	if err != nil {
		return Manifest{}, BarSet{}, err
	}
	bs := BarSet{
		Instrument: m.Instrument,
		Interval:   m.Interval,
		Span:       m.Span,
		Basis:      m.Basis,
		Bars:       bars,
	}
	if err := bs.Validate(); err != nil {
		return Manifest{}, BarSet{}, fmt.Errorf("marketdata: store: load: %w", err)
	}
	if err := m.Matches(bs); err != nil {
		return Manifest{}, BarSet{}, fmt.Errorf("marketdata: store: load: %w", err)
	}
	return m, bs, nil
}

// writeFileAtomic calls encode with a buffered writer over a temporary
// file created alongside finalPath, then renames it into place only
// once encode and the flush both succeed. On any failure the temporary
// file is removed and finalPath is left untouched.
func writeFileAtomic(finalPath string, encode func(*bufio.Writer) error) error {
	dir := filepath.Dir(finalPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(finalPath)+".tmp-*")
	if err != nil {
		return err
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
	if err := encode(bw); err != nil {
		return err
	}
	if err := bw.Flush(); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		return err
	}
	succeeded = true
	return nil
}

// encodeBarSet writes bs's bars as canonical CSV rows, preceded by a
// schema comment cross-checked against key on read. It never writes a
// dense, zero-filled placeholder row: an empty bs.Bars produces a valid
// file with a header and no data rows at all.
func encodeBarSet(w *bufio.Writer, key partitionKey, bs BarSet) error {
	token, err := intervalToken(key.interval)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "# schema=%s provider=%s symbol=%s interval=%s year=%04d month=%02d\n",
		canonicalBarSchema, key.provider, key.symbol, token, key.year, int(key.month)); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, canonicalCSVHeader); err != nil {
		return err
	}
	for _, b := range bs.Bars {
		if _, err := fmt.Fprintf(w, "%s,%s,%s,%s,%s,%s,%s,%d\n",
			b.Time.Format(time.RFC3339Nano), b.Open, b.High, b.Low, b.Close, b.AvgSpread, b.MaxSpread, b.Ticks); err != nil {
			return err
		}
	}
	return nil
}

// readBarSetFile reads and parses the data file at path, cross-checking
// its schema comment against key, and returns its bars in file order.
func readBarSetFile(path string, key partitionKey) ([]Bar, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	if !scanner.Scan() {
		return nil, fmt.Errorf("%w: %s: empty file", errStoreMalformed, path)
	}
	if err := crossCheckBarSchema(scanner.Text(), path, key); err != nil {
		return nil, err
	}
	if !scanner.Scan() {
		return nil, fmt.Errorf("%w: %s: missing column header", errStoreMalformed, path)
	}
	if header := scanner.Text(); header != canonicalCSVHeader {
		return nil, fmt.Errorf("%w: %s: unexpected column header %q, want %q", errStoreMalformed, path, header, canonicalCSVHeader)
	}

	var bars []Bar
	line := 2
	for scanner.Scan() {
		line++
		row := scanner.Text()
		if strings.TrimSpace(row) == "" {
			continue
		}
		b, err := parseBarRow(row)
		if err != nil {
			return nil, fmt.Errorf("%w: %s:%d: %v", errStoreMalformed, path, line, err)
		}
		bars = append(bars, b)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("%w: %s: %v", errStoreMalformed, path, err)
	}
	return bars, nil
}

func crossCheckBarSchema(comment, path string, key partitionKey) error {
	trimmed := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(comment), "#"))
	kv := map[string]string{}
	for tok := range strings.FieldsSeq(trimmed) {
		k, v, ok := strings.Cut(tok, "=")
		if ok {
			kv[k] = v
		}
	}
	if kv["schema"] != canonicalBarSchema {
		return fmt.Errorf("%w: %s: unexpected schema %q", errStoreMalformed, path, kv["schema"])
	}
	if kv["provider"] != key.provider {
		return fmt.Errorf("%w: %s: schema provider %q disagrees with %q", errStoreMalformed, path, kv["provider"], key.provider)
	}
	if kv["symbol"] != key.symbol {
		return fmt.Errorf("%w: %s: schema symbol %q disagrees with %q", errStoreMalformed, path, kv["symbol"], key.symbol)
	}
	token, err := intervalToken(key.interval)
	if err != nil {
		return err
	}
	if kv["interval"] != token {
		return fmt.Errorf("%w: %s: schema interval %q disagrees with %q", errStoreMalformed, path, kv["interval"], token)
	}
	if kv["year"] != fmt.Sprintf("%04d", key.year) {
		return fmt.Errorf("%w: %s: schema year %q disagrees with %04d", errStoreMalformed, path, kv["year"], key.year)
	}
	if kv["month"] != fmt.Sprintf("%02d", int(key.month)) {
		return fmt.Errorf("%w: %s: schema month %q disagrees with %02d", errStoreMalformed, path, kv["month"], int(key.month))
	}
	return nil
}

// parseBarRow parses one canonical CSV data row into a Bar. Every value
// moves through num.ParsePrice/time.Parse — never float64.
func parseBarRow(row string) (Bar, error) {
	fields := strings.Split(row, ",")
	if len(fields) != 8 {
		return Bar{}, fmt.Errorf("expected 8 fields, got %d", len(fields))
	}
	t, err := time.Parse(time.RFC3339Nano, fields[0])
	if err != nil {
		return Bar{}, fmt.Errorf("time: %w", err)
	}
	prices := make([]num.Price, 6)
	names := []string{"open", "high", "low", "close", "avg_spread", "max_spread"}
	for i, name := range names {
		p, err := num.ParsePrice(fields[i+1])
		if err != nil {
			return Bar{}, fmt.Errorf("%s: %w", name, err)
		}
		prices[i] = p
	}
	ticks, err := strconv.ParseInt(fields[7], 10, 64)
	if err != nil {
		return Bar{}, fmt.Errorf("ticks: %w", err)
	}
	return Bar{
		Time:      t.UTC(),
		Open:      prices[0],
		High:      prices[1],
		Low:       prices[2],
		Close:     prices[3],
		AvgSpread: prices[4],
		MaxSpread: prices[5],
		Ticks:     ticks,
	}, nil
}

// encodeManifest writes m as key=value lines, one per field, plus its
// computed Revision for a cheap sanity check on read.
func encodeManifest(w *bufio.Writer, m Manifest) error {
	lines := []string{
		"schema=" + canonicalManifestSchema,
		"provider=" + m.Provider,
		"instrument=" + m.Instrument.String(),
		fmt.Sprintf("interval_unit=%d", m.Interval.Unit()),
		fmt.Sprintf("interval_count=%d", m.Interval.Count()),
		"span_start=" + m.Span.Start().Format(time.RFC3339Nano),
		"span_end=" + m.Span.End().Format(time.RFC3339Nano),
		fmt.Sprintf("basis=%d", m.Basis),
		fmt.Sprintf("schema_version=%d", m.SchemaVersion),
		"raw_fingerprint=" + m.RawFingerprint,
		"builder_version=" + m.BuilderVersion,
		"validator_version=" + m.ValidatorVersion,
		"resampler_version=" + m.ResamplerVersion,
		"calendar_version=" + m.CalendarVersion,
		"built_at=" + m.BuiltAt.Format(time.RFC3339Nano),
		fmt.Sprintf("bar_count=%d", m.BarCount),
		"first_bar=" + formatOptionalTime(m.FirstBar),
		"last_bar=" + formatOptionalTime(m.LastBar),
	}
	if m.Parent != nil {
		lines = append(lines,
			"parent_instrument="+m.Parent.Instrument.String(),
			fmt.Sprintf("parent_interval_unit=%d", m.Parent.Interval.Unit()),
			fmt.Sprintf("parent_interval_count=%d", m.Parent.Interval.Count()),
			"parent_revision="+m.Parent.Revision,
		)
	}
	lines = append(lines, "revision="+m.Revision())

	for _, line := range lines {
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	return nil
}

// readManifestFile reads and parses the manifest file at path.
// expectedInstrument supplies Manifest.Instrument (and, when present,
// ParentRef.Instrument, which Manifest's own lineage rules require to
// equal the child's instrument): this package cannot parse an
// instrument.ID back out of its own String() form, so the caller
// supplies the identity it already knows from the partition key, and
// the stored text is cross-checked against it rather than parsed.
func readManifestFile(path string, expectedInstrument instrument.ID) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}

	kv := make(map[string]string)
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			return Manifest{}, fmt.Errorf("%w: %s: malformed line %q", errStoreMalformed, path, line)
		}
		kv[k] = v
	}

	if kv["schema"] != canonicalManifestSchema {
		return Manifest{}, fmt.Errorf("%w: %s: unexpected schema %q", errStoreMalformed, path, kv["schema"])
	}
	if kv["instrument"] != expectedInstrument.String() {
		return Manifest{}, fmt.Errorf("%w: %s: manifest instrument %q disagrees with expected %q",
			errStoreMalformed, path, kv["instrument"], expectedInstrument.String())
	}

	intervalUnit, err := parseUint8(kv["interval_unit"])
	if err != nil {
		return Manifest{}, fmt.Errorf("%w: %s: interval_unit: %v", errStoreMalformed, path, err)
	}
	intervalCount, err := strconv.Atoi(kv["interval_count"])
	if err != nil {
		return Manifest{}, fmt.Errorf("%w: %s: interval_count: %v", errStoreMalformed, path, err)
	}
	interval, err := NewInterval(Unit(intervalUnit), intervalCount)
	if err != nil {
		return Manifest{}, fmt.Errorf("%w: %s: interval: %v", errStoreMalformed, path, err)
	}

	spanStart, err := time.Parse(time.RFC3339Nano, kv["span_start"])
	if err != nil {
		return Manifest{}, fmt.Errorf("%w: %s: span_start: %v", errStoreMalformed, path, err)
	}
	spanEnd, err := time.Parse(time.RFC3339Nano, kv["span_end"])
	if err != nil {
		return Manifest{}, fmt.Errorf("%w: %s: span_end: %v", errStoreMalformed, path, err)
	}
	span, err := NewTimeRange(spanStart.UTC(), spanEnd.UTC())
	if err != nil {
		return Manifest{}, fmt.Errorf("%w: %s: span: %v", errStoreMalformed, path, err)
	}

	basisVal, err := parseUint8(kv["basis"])
	if err != nil {
		return Manifest{}, fmt.Errorf("%w: %s: basis: %v", errStoreMalformed, path, err)
	}
	schemaVersion, err := strconv.Atoi(kv["schema_version"])
	if err != nil {
		return Manifest{}, fmt.Errorf("%w: %s: schema_version: %v", errStoreMalformed, path, err)
	}
	builtAt, err := time.Parse(time.RFC3339Nano, kv["built_at"])
	if err != nil {
		return Manifest{}, fmt.Errorf("%w: %s: built_at: %v", errStoreMalformed, path, err)
	}
	barCount, err := strconv.Atoi(kv["bar_count"])
	if err != nil {
		return Manifest{}, fmt.Errorf("%w: %s: bar_count: %v", errStoreMalformed, path, err)
	}
	firstBar, err := parseOptionalTime(kv["first_bar"])
	if err != nil {
		return Manifest{}, fmt.Errorf("%w: %s: first_bar: %v", errStoreMalformed, path, err)
	}
	lastBar, err := parseOptionalTime(kv["last_bar"])
	if err != nil {
		return Manifest{}, fmt.Errorf("%w: %s: last_bar: %v", errStoreMalformed, path, err)
	}

	m := Manifest{
		Provider:         kv["provider"],
		Instrument:       expectedInstrument,
		Interval:         interval,
		Span:             span,
		Basis:            PriceBasis(basisVal),
		SchemaVersion:    schemaVersion,
		RawFingerprint:   kv["raw_fingerprint"],
		BuilderVersion:   kv["builder_version"],
		ValidatorVersion: kv["validator_version"],
		ResamplerVersion: kv["resampler_version"],
		CalendarVersion:  kv["calendar_version"],
		BuiltAt:          builtAt.UTC(),
		BarCount:         barCount,
		FirstBar:         firstBar,
		LastBar:          lastBar,
	}

	if kv["parent_revision"] != "" {
		parentUnit, err := parseUint8(kv["parent_interval_unit"])
		if err != nil {
			return Manifest{}, fmt.Errorf("%w: %s: parent_interval_unit: %v", errStoreMalformed, path, err)
		}
		parentCount, err := strconv.Atoi(kv["parent_interval_count"])
		if err != nil {
			return Manifest{}, fmt.Errorf("%w: %s: parent_interval_count: %v", errStoreMalformed, path, err)
		}
		parentInterval, err := NewInterval(Unit(parentUnit), parentCount)
		if err != nil {
			return Manifest{}, fmt.Errorf("%w: %s: parent_interval: %v", errStoreMalformed, path, err)
		}
		// Manifest's own lineage rules (validateLineage) require a
		// derived dataset's parent to name the same instrument as the
		// child, so expectedInstrument is correct for the parent too.
		m.Parent = &ParentRef{
			Instrument: expectedInstrument,
			Interval:   parentInterval,
			Revision:   kv["parent_revision"],
		}
	}

	if err := m.Validate(); err != nil {
		return Manifest{}, fmt.Errorf("%w: %s: %v", errStoreMalformed, path, err)
	}
	return m, nil
}

// formatOptionalTime formats t as RFC3339Nano, or the empty string when
// t is the zero value (Manifest's FirstBar/LastBar are zero exactly
// when BarCount is zero).
func formatOptionalTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339Nano)
}

// parseOptionalTime is formatOptionalTime's inverse.
func parseOptionalTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}

// parseUint8 parses s as a base-10 uint8.
func parseUint8(s string) (uint8, error) {
	v, err := strconv.ParseUint(s, 10, 8)
	if err != nil {
		return 0, err
	}
	return uint8(v), nil
}
