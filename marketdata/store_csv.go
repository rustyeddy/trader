package marketdata

import (
	"bufio"
	"context"
	"encoding/json"
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

// canonicalSchema tags this store's one file format, the same
// convention the raw OANDA reader uses for its own "# schema=..." line.
// It is not a public wire format: it is internal to marketdata, checked
// only to catch a file that does not contain what its path claims.
const canonicalSchema = "canonical-v1"

// canonicalCSVHeader is the exact canonical Bar CSV column header.
const canonicalCSVHeader = "time,open,high,low,close,avg_spread,max_spread,ticks"

// Sentinel errors returned (wrapped) by the canonical store.
var (
	// errStoreMalformed marks a file that does not parse, whose content
	// disagrees with what its path or caller-supplied identity claims,
	// or whose stored revision disagrees with its own recomputed one.
	errStoreMalformed = errors.New("marketdata: store: malformed file")
	// errStoreUnsupportedInterval marks an Interval this store cannot
	// map to a path token.
	errStoreUnsupportedInterval = errors.New("marketdata: store: unsupported interval")
	// errStoreInvalidPartitionKey marks a partitionKey that is not safe
	// or complete enough to build a path from.
	errStoreInvalidPartitionKey = errors.New("marketdata: store: invalid partition key")
	// errStorePartitionKeyMismatch marks a partitionKey that disagrees
	// with the Manifest being published under it.
	errStorePartitionKeyMismatch = errors.New("marketdata: store: partition key disagrees with manifest")
)

// partitionKey identifies one canonical partition file within a store
// root. instrument is carried alongside symbol the same way oanda.Meta
// carries both: symbol is what builds a filesystem path, instrument is
// Trader's canonical identity, and neither is reliably derivable from
// the other without a resolver this package does not have.
type partitionKey struct {
	provider   string
	symbol     string
	instrument instrument.ID
	interval   Interval
	year       int
	month      time.Month
}

// validate reports whether k is safe and complete enough to build a
// path and publish under. provider and symbol are checked as path
// components specifically because, even though this store is internal,
// both are name-based values that ultimately originate outside it (a
// provider identifier, an instrument's display symbol) — a value
// containing ".." or a path separator must never be able to escape the
// intended provider/symbol partition.
func (k partitionKey) validate() error {
	if err := validatePathComponent(k.provider); err != nil {
		return fmt.Errorf("%w: provider: %v", errStoreInvalidPartitionKey, err)
	}
	if err := validatePathComponent(k.symbol); err != nil {
		return fmt.Errorf("%w: symbol: %v", errStoreInvalidPartitionKey, err)
	}
	if k.instrument.IsZero() {
		return fmt.Errorf("%w: zero instrument", errStoreInvalidPartitionKey)
	}
	if !k.interval.Valid() {
		return fmt.Errorf("%w: invalid interval", errStoreInvalidPartitionKey)
	}
	if k.month < time.January || k.month > time.December {
		return fmt.Errorf("%w: invalid month %d", errStoreInvalidPartitionKey, int(k.month))
	}
	return nil
}

// validatePathComponent reports whether name is safe to use as exactly
// one path component: non-empty, not "." or "..", and free of any path
// separator.
func validatePathComponent(name string) error {
	if name == "" {
		return errors.New("empty")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("reserved name %q", name)
	}
	if strings.ContainsRune(name, '/') || (os.PathSeparator != '/' && strings.ContainsRune(name, os.PathSeparator)) {
		return fmt.Errorf("contains a path separator: %q", name)
	}
	return nil
}

// dir returns the partition's directory under root, following ADR-020's
// derived-tree convention: root/provider/SYMBOL/YYYY/MM.
func (k partitionKey) dir(root string) string {
	return filepath.Join(root, k.provider, k.symbol, fmt.Sprintf("%04d", k.year), fmt.Sprintf("%02d", int(k.month)))
}

// path returns the partition's one file path: SYMBOL-YYYY-MM-<tf>.csv.
// It never encodes a revision: ADR-020 requires the version identifier
// to live in the file's own header, never the path. As a defense-in-depth
// check beyond validate's path-component rules, path also verifies the
// resolved path is still beneath root.
func (k partitionKey) path(root string) (string, error) {
	token, err := intervalToken(k.interval)
	if err != nil {
		return "", err
	}
	base := fmt.Sprintf("%s-%04d-%02d-%s.csv", k.symbol, k.year, int(k.month), token)
	full := filepath.Join(k.dir(root), base)

	cleanRoot := filepath.Clean(root)
	if full != cleanRoot && !strings.HasPrefix(full, cleanRoot+string(os.PathSeparator)) {
		return "", fmt.Errorf("%w: resolved path %q escapes root %q", errStoreInvalidPartitionKey, full, cleanRoot)
	}
	return full, nil
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

// barStore is the internal contract for reading and writing canonical
// Bar/Manifest pairs. canonicalCSVStore (issue #77, ADR-020) is its one
// implementation today; the interface exists so a later implementation
// (a Parquet store, say) can be substituted without changing Manager or
// its wiring, and so the store's own contract tests can run against any
// implementation satisfying it.
//
// canonicalCSVStore is marketdata's CSV-backed implementation of
// barStore. CSV is selected pragmatically because the existing raw and
// canonical archives already use it; it is not a public storage
// contract, and nothing about barStore or partitionKey prevents a
// future Parquet implementation.
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

// publish validates key, m, bs, and their mutual agreement, then writes
// both as one file, atomically.
//
// # One file, not two
//
// An earlier draft of this store published data and manifest as two
// separate files, relying on os.Rename's single-file atomicity plus a
// documented ordering (data renamed, then manifest) and the expectation
// that Manifest.Matches would reject the pair a reader could observe
// mid-sequence. A design review correctly identified that this still
// violated the issue's own acceptance criterion that failure or
// cancellation must leave the prior published revision intact: once the
// data file's rename succeeded, the *old* data was already gone,
// replaced by the new file's bytes — Matches failing on the resulting
// pair meant the corruption was detected, but the previously published
// dataset was no longer loadable at all until a further successful
// publish. Detecting a problem is not the same as leaving the prior
// revision intact.
//
// The fix is the other option the issue's own scope explicitly allowed
// ("the dataset manifest or agreed file header"): manifest and data now
// share one file, so there is exactly one os.Rename — genuinely atomic,
// not "atomic per file with a window between two files." A publish that
// is cancelled or fails before that one rename leaves the previously
// published file completely untouched and fully loadable; a publish
// that completes it replaces the whole (manifest, data) pair in a
// single indivisible step. There is no intermediate state to reach at
// all.
func (s *canonicalCSVStore) publish(ctx context.Context, key partitionKey, m Manifest, bs BarSet) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := key.validate(); err != nil {
		return fmt.Errorf("marketdata: store: publish: %w", err)
	}
	if err := checkKeyMatchesManifest(key, m); err != nil {
		return fmt.Errorf("marketdata: store: publish: %w", err)
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

	path, err := key.path(s.rootDir)
	if err != nil {
		return err
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	if err := writeFileAtomic(path, func(w *bufio.Writer) error {
		return encodePartition(w, key, m, bs)
	}); err != nil {
		return fmt.Errorf("marketdata: store: publish: %w", err)
	}
	return nil
}

// checkKeyMatchesManifest verifies key's identity fields agree with m,
// so publish can never succeed while producing a partition that could
// not subsequently be loaded (a zero or mismatched instrument, or a
// file filed under the wrong provider/interval/calendar-month
// directory).
//
// The year/month check is deliberately an overlap check, not exact
// containment: m.Span is checked against [key's month start, key's
// month end) as a half-open range, not required to fall entirely
// within it. A session-aligned D1 span can legitimately start in the
// closing hours of the previous UTC day — near a month boundary, the
// previous UTC month — while still correctly belonging to the calendar
// month a caller filed it under; overlap rather than containment avoids
// rejecting that real case while still catching a gross mismatch (data
// from an unrelated month published under this key).
func checkKeyMatchesManifest(key partitionKey, m Manifest) error {
	if !key.instrument.Equal(m.Instrument) {
		return fmt.Errorf("%w: key instrument %s != manifest instrument %s",
			errStorePartitionKeyMismatch, key.instrument, m.Instrument)
	}
	if key.provider != m.Provider {
		return fmt.Errorf("%w: key provider %q != manifest provider %q",
			errStorePartitionKeyMismatch, key.provider, m.Provider)
	}
	if key.interval != m.Interval {
		return fmt.Errorf("%w: key interval %s != manifest interval %s",
			errStorePartitionKeyMismatch, key.interval, m.Interval)
	}
	monthStart := time.Date(key.year, key.month, 1, 0, 0, 0, 0, time.UTC)
	monthEnd := monthStart.AddDate(0, 1, 0)
	if !m.Span.Start().Before(monthEnd) || !m.Span.End().After(monthStart) {
		return fmt.Errorf("%w: manifest span [%s, %s) does not overlap key month %04d-%02d",
			errStorePartitionKeyMismatch, m.Span.Start(), m.Span.End(), key.year, int(key.month))
	}
	return nil
}

// load reads and parses the file for key, verifying its header's
// recomputed Revision agrees with its stored one and that the resulting
// Manifest and BarSet Match, before returning either. It reports an
// error — never a partial or best-effort result — for a missing file,
// a malformed file, or a manifest/data pair that disagrees.
func (s *canonicalCSVStore) load(ctx context.Context, key partitionKey) (Manifest, BarSet, error) {
	if err := ctx.Err(); err != nil {
		return Manifest{}, BarSet{}, err
	}
	if err := key.validate(); err != nil {
		return Manifest{}, BarSet{}, fmt.Errorf("marketdata: store: load: %w", err)
	}

	path, err := key.path(s.rootDir)
	if err != nil {
		return Manifest{}, BarSet{}, err
	}

	m, bars, err := readPartitionFile(path, key)
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

// manifestJSON is the exact, unambiguous structure a partition file's
// manifest header line encodes. Using JSON rather than hand-joined
// key=value lines removes an encoding ambiguity a line-oriented format
// has: Manifest.Validate only requires its string fields (Provider, the
// version fields, Parent.Revision) to be non-empty, not free of "\n" or
// "\r", so a key=value-per-line encoding cannot round-trip every value
// Validate accepts. JSON quotes and escapes each string field, so no
// field's content can be misread as a line boundary — the same reason
// Manifest.Revision (#73) already hashes a JSON-encoded struct rather
// than a delimited string.
type manifestJSON struct {
	Provider         string      `json:"provider"`
	Instrument       string      `json:"instrument"`
	IntervalUnit     Unit        `json:"interval_unit"`
	IntervalCount    int         `json:"interval_count"`
	SpanStart        string      `json:"span_start"`
	SpanEnd          string      `json:"span_end"`
	Basis            PriceBasis  `json:"basis"`
	SchemaVersion    int         `json:"schema_version"`
	RawFingerprint   string      `json:"raw_fingerprint"`
	BuilderVersion   string      `json:"builder_version"`
	ValidatorVersion string      `json:"validator_version"`
	ResamplerVersion string      `json:"resampler_version"`
	CalendarVersion  string      `json:"calendar_version"`
	BuiltAt          string      `json:"built_at"`
	BarCount         int         `json:"bar_count"`
	FirstBar         string      `json:"first_bar,omitempty"`
	LastBar          string      `json:"last_bar,omitempty"`
	Parent           *parentJSON `json:"parent,omitempty"`
	// Revision is m.Revision() at encode time. load recomputes it from
	// the decoded Manifest and rejects the file if the two disagree —
	// the cheapest possible defense against a hand-edited or partially
	// corrupted header whose individual fields still happen to parse.
	Revision string `json:"revision"`
}

type parentJSON struct {
	Instrument    string `json:"instrument"`
	IntervalUnit  Unit   `json:"interval_unit"`
	IntervalCount int    `json:"interval_count"`
	Revision      string `json:"revision"`
}

// encodePartition writes key's schema line, m's JSON header line, the
// canonical CSV column header, and bs's rows in order. It never writes
// a dense, zero-filled placeholder row: an empty bs.Bars produces a
// valid file with a header and no data rows at all.
func encodePartition(w *bufio.Writer, key partitionKey, m Manifest, bs BarSet) error {
	token, err := intervalToken(key.interval)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "# schema=%s provider=%s symbol=%s interval=%s year=%04d month=%02d\n",
		canonicalSchema, key.provider, key.symbol, token, key.year, int(key.month)); err != nil {
		return err
	}

	header := manifestToJSON(m)
	encoded, err := json.Marshal(header)
	if err != nil {
		return fmt.Errorf("marketdata: store: encode manifest: %w", err)
	}
	if _, err := w.Write(encoded); err != nil {
		return err
	}
	if err := w.WriteByte('\n'); err != nil {
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

func manifestToJSON(m Manifest) manifestJSON {
	h := manifestJSON{
		Provider:         m.Provider,
		Instrument:       m.Instrument.String(),
		IntervalUnit:     m.Interval.Unit(),
		IntervalCount:    m.Interval.Count(),
		SpanStart:        m.Span.Start().Format(time.RFC3339Nano),
		SpanEnd:          m.Span.End().Format(time.RFC3339Nano),
		Basis:            m.Basis,
		SchemaVersion:    m.SchemaVersion,
		RawFingerprint:   m.RawFingerprint,
		BuilderVersion:   m.BuilderVersion,
		ValidatorVersion: m.ValidatorVersion,
		ResamplerVersion: m.ResamplerVersion,
		CalendarVersion:  m.CalendarVersion,
		BuiltAt:          m.BuiltAt.Format(time.RFC3339Nano),
		BarCount:         m.BarCount,
		FirstBar:         formatOptionalTime(m.FirstBar),
		LastBar:          formatOptionalTime(m.LastBar),
		Revision:         m.Revision(),
	}
	if m.Parent != nil {
		h.Parent = &parentJSON{
			Instrument:    m.Parent.Instrument.String(),
			IntervalUnit:  m.Parent.Interval.Unit(),
			IntervalCount: m.Parent.Interval.Count(),
			Revision:      m.Parent.Revision,
		}
	}
	return h
}

// readPartitionFile reads and parses the file at path, cross-checking
// its schema line against key, and returns the decoded Manifest and its
// bars in file order.
func readPartitionFile(path string, key partitionKey) (Manifest, []Bar, error) {
	f, err := os.Open(path)
	if err != nil {
		return Manifest{}, nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	if !scanner.Scan() {
		return Manifest{}, nil, fmt.Errorf("%w: %s: empty file", errStoreMalformed, path)
	}
	if err := crossCheckPartitionSchema(scanner.Text(), path, key); err != nil {
		return Manifest{}, nil, err
	}

	if !scanner.Scan() {
		return Manifest{}, nil, fmt.Errorf("%w: %s: missing manifest header", errStoreMalformed, path)
	}
	m, err := decodeManifestJSON(scanner.Text(), path, key.instrument)
	if err != nil {
		return Manifest{}, nil, err
	}

	if !scanner.Scan() {
		return Manifest{}, nil, fmt.Errorf("%w: %s: missing column header", errStoreMalformed, path)
	}
	if got := scanner.Text(); got != canonicalCSVHeader {
		return Manifest{}, nil, fmt.Errorf("%w: %s: unexpected column header %q, want %q", errStoreMalformed, path, got, canonicalCSVHeader)
	}

	var bars []Bar
	line := 3
	for scanner.Scan() {
		line++
		row := scanner.Text()
		if strings.TrimSpace(row) == "" {
			continue
		}
		b, err := parseBarRow(row)
		if err != nil {
			return Manifest{}, nil, fmt.Errorf("%w: %s:%d: %v", errStoreMalformed, path, line, err)
		}
		bars = append(bars, b)
	}
	if err := scanner.Err(); err != nil {
		return Manifest{}, nil, fmt.Errorf("%w: %s: %v", errStoreMalformed, path, err)
	}
	return m, bars, nil
}

func crossCheckPartitionSchema(comment, path string, key partitionKey) error {
	trimmed := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(comment), "#"))
	kv := map[string]string{}
	for tok := range strings.FieldsSeq(trimmed) {
		k, v, ok := strings.Cut(tok, "=")
		if ok {
			kv[k] = v
		}
	}
	if kv["schema"] != canonicalSchema {
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

// decodeManifestJSON parses line as a manifestJSON header, cross-checks
// its instrument (and, when present, its parent's instrument) against
// expectedInstrument, builds the resulting Manifest, and verifies that
// Manifest's own recomputed Revision agrees with the header's stored
// one before returning it.
//
// expectedInstrument supplies Manifest.Instrument directly rather than
// parsing it out of the header's own instrument string: this package
// cannot parse an instrument.ID back out of its String() form (there is
// no inverse of instrument.CurrencyPairID and friends), so the caller
// supplies the identity it already knows from the partition key, and
// the stored text is cross-checked against it instead.
func decodeManifestJSON(line, path string, expectedInstrument instrument.ID) (Manifest, error) {
	var h manifestJSON
	if err := json.Unmarshal([]byte(line), &h); err != nil {
		return Manifest{}, fmt.Errorf("%w: %s: manifest header: %v", errStoreMalformed, path, err)
	}
	if h.Instrument != expectedInstrument.String() {
		return Manifest{}, fmt.Errorf("%w: %s: manifest instrument %q disagrees with expected %q",
			errStoreMalformed, path, h.Instrument, expectedInstrument.String())
	}

	interval, err := NewInterval(h.IntervalUnit, h.IntervalCount)
	if err != nil {
		return Manifest{}, fmt.Errorf("%w: %s: interval: %v", errStoreMalformed, path, err)
	}
	spanStart, err := time.Parse(time.RFC3339Nano, h.SpanStart)
	if err != nil {
		return Manifest{}, fmt.Errorf("%w: %s: span_start: %v", errStoreMalformed, path, err)
	}
	spanEnd, err := time.Parse(time.RFC3339Nano, h.SpanEnd)
	if err != nil {
		return Manifest{}, fmt.Errorf("%w: %s: span_end: %v", errStoreMalformed, path, err)
	}
	span, err := NewTimeRange(spanStart.UTC(), spanEnd.UTC())
	if err != nil {
		return Manifest{}, fmt.Errorf("%w: %s: span: %v", errStoreMalformed, path, err)
	}
	builtAt, err := time.Parse(time.RFC3339Nano, h.BuiltAt)
	if err != nil {
		return Manifest{}, fmt.Errorf("%w: %s: built_at: %v", errStoreMalformed, path, err)
	}
	firstBar, err := parseOptionalTime(h.FirstBar)
	if err != nil {
		return Manifest{}, fmt.Errorf("%w: %s: first_bar: %v", errStoreMalformed, path, err)
	}
	lastBar, err := parseOptionalTime(h.LastBar)
	if err != nil {
		return Manifest{}, fmt.Errorf("%w: %s: last_bar: %v", errStoreMalformed, path, err)
	}

	m := Manifest{
		Provider:         h.Provider,
		Instrument:       expectedInstrument,
		Interval:         interval,
		Span:             span,
		Basis:            h.Basis,
		SchemaVersion:    h.SchemaVersion,
		RawFingerprint:   h.RawFingerprint,
		BuilderVersion:   h.BuilderVersion,
		ValidatorVersion: h.ValidatorVersion,
		ResamplerVersion: h.ResamplerVersion,
		CalendarVersion:  h.CalendarVersion,
		BuiltAt:          builtAt.UTC(),
		BarCount:         h.BarCount,
		FirstBar:         firstBar,
		LastBar:          lastBar,
	}

	if h.Parent != nil {
		if h.Parent.Instrument != expectedInstrument.String() {
			return Manifest{}, fmt.Errorf("%w: %s: parent instrument %q disagrees with expected %q",
				errStoreMalformed, path, h.Parent.Instrument, expectedInstrument.String())
		}
		parentInterval, err := NewInterval(h.Parent.IntervalUnit, h.Parent.IntervalCount)
		if err != nil {
			return Manifest{}, fmt.Errorf("%w: %s: parent interval: %v", errStoreMalformed, path, err)
		}
		m.Parent = &ParentRef{
			Instrument: expectedInstrument,
			Interval:   parentInterval,
			Revision:   h.Parent.Revision,
		}
	}

	if err := m.Validate(); err != nil {
		return Manifest{}, fmt.Errorf("%w: %s: %v", errStoreMalformed, path, err)
	}
	if got, want := m.Revision(), h.Revision; got != want {
		return Manifest{}, fmt.Errorf("%w: %s: recomputed revision %s disagrees with stored revision %s",
			errStoreMalformed, path, got, want)
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
