package marketdata

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/rustyeddy/trader/instrument"
)

// Sentinel errors returned (wrapped) by Manifest.Validate and
// Manifest.Matches, so callers can classify a failure without parsing its
// message.
var (
	// ErrManifestProvider marks a Manifest with an empty Provider.
	ErrManifestProvider = errors.New("marketdata: manifest provider is empty")
	// ErrManifestInstrument marks a Manifest with a zero instrument
	// identity.
	ErrManifestInstrument = errors.New("marketdata: manifest instrument is zero")
	// ErrManifestInterval marks a Manifest with an unconstructed interval.
	ErrManifestInterval = errors.New("marketdata: manifest interval is invalid")
	// ErrManifestSpan marks a Manifest with an unset half-open span.
	ErrManifestSpan = errors.New("marketdata: manifest span is invalid")
	// ErrManifestBasis marks a Manifest with an unknown price basis.
	ErrManifestBasis = errors.New("marketdata: manifest price basis is unknown")
	// ErrManifestSchemaVersion marks a Manifest whose SchemaVersion is
	// not positive.
	ErrManifestSchemaVersion = errors.New("marketdata: manifest schema version must be positive")
	// ErrManifestFingerprint marks a Manifest whose RawFingerprint is
	// empty or does not match the required "<algorithm>:<hex digest>"
	// form (see the RawFingerprint field doc).
	ErrManifestFingerprint = errors.New("marketdata: manifest raw fingerprint is invalid")
	// ErrManifestVersion marks a Manifest with an empty builder,
	// validator, resampler, or calendar version.
	ErrManifestVersion = errors.New("marketdata: manifest version string is empty")
	// ErrManifestBuiltAt marks a Manifest whose BuiltAt is the zero
	// value.
	ErrManifestBuiltAt = errors.New("marketdata: manifest built-at time is zero")
	// ErrManifestBarCount marks a Manifest with a negative BarCount.
	ErrManifestBarCount = errors.New("marketdata: manifest bar count is negative")
	// ErrManifestBarRange marks a Manifest whose FirstBar/LastBar are
	// inconsistent with each other or with Span.
	ErrManifestBarRange = errors.New("marketdata: manifest bar range is invalid")
	// ErrManifestParent marks a Manifest whose Parent lineage is
	// malformed or contradicts the rest of the Manifest: a Parent field
	// missing a required value, Parent set without a real
	// ResamplerVersion (or vice versa), a parent naming a different
	// instrument, or a parent interval that is not strictly finer than
	// this dataset's own Interval.
	ErrManifestParent = errors.New("marketdata: manifest parent reference is invalid")

	// ErrManifestMismatch marks a Manifest and BarSet that do not
	// describe the same dataset. It is returned (wrapped) by
	// Manifest.Matches.
	ErrManifestMismatch = errors.New("marketdata: manifest does not match bar set")
)

// ParentRef identifies the parent dataset a derived (for example,
// resampled) Manifest was built from. It carries the parent's identity and
// its own Revision, not the parent Manifest itself: ADR-020 does not
// retain superseded revisions, so lineage must not force a derived
// dataset to hold — or keep valid — a full copy of a dataset that may
// since have been rebuilt and reversioned.
type ParentRef struct {
	// Instrument is the parent dataset's canonical instrument identity.
	Instrument instrument.ID
	// Interval is the parent dataset's interval.
	Interval Interval
	// Revision is the parent Manifest's own Revision() at the time this
	// derived Manifest was built. It is an opaque, deterministic
	// fingerprint string, not a live reference: if the parent is later
	// rebuilt, this value simply stops matching the parent's current
	// Revision(), which is exactly the signal a consumer needs to know
	// the derived dataset is stale.
	Revision string
}

// valid reports whether p is a well-formed parent reference. It is only
// called when p is non-nil; Manifest.Parent being nil is itself a valid,
// common state (a dataset built directly from raw provider data, not
// derived from another canonical dataset).
func (p ParentRef) valid() bool {
	return !p.Instrument.IsZero() && p.Interval.Valid() && p.Revision != ""
}

// Manifest identifies a homogeneous canonical BarSet independently of its
// filesystem path: dataset identity, provenance, revision, coverage
// summary, and build information (ADR-020, issue #73). Bar holds
// observation-local facts and BarSet holds the collection-level metadata
// shared by those observations; Manifest holds why the dataset is
// trustworthy and what produced it.
//
// Manifest is a plain record with exported fields, validated via Validate
// at the boundary that builds or reads it — the same convention Bar and
// BarSet use. Its zero value is not a usable manifest.
//
// # Identity is not a path
//
// Manifest carries no file path, directory layout, or storage-engine
// detail. Dataset identity is Provider, Instrument, Interval, and Span;
// where the bytes happen to live on disk is a storage concern the
// manifest never encodes.
//
// # Revision is computed, not supplied
//
// Manifest deliberately has no stored "Revision" or "ID" field a caller
// could set inconsistently with the rest of the record. Revision computes
// a deterministic fingerprint from every other field, so two Manifest
// values built from identical inputs always produce the same revision,
// and changing any one field — including RawFingerprint, a version
// string, or Parent — changes it. A canonical file header or store
// records this computed string; nothing upstream invents it directly.
//
// # Raw is authoritative
//
// RawFingerprint identifies the raw source artifact(s) this dataset was
// built from, so a rebuild from unchanged raw data is verifiable. It is
// supplied by Manager's own build step (normalizeAndPublish,
// build_normalize.go, #81) that reads raw provider bytes; Manifest
// itself has no provider or storage dependency and never reaches into a
// package such as marketdata/internal/provider/oanda.
type Manifest struct {
	// Provider is the opaque logical name of the data source this
	// dataset was built from, for example "oanda". It is a name, never a
	// provider type, credentials model, or construction mechanism — the
	// same opaque-selection convention ADR-020 requires of any consumer-
	// facing provider reference.
	Provider string

	// Instrument is the dataset's canonical instrument identity.
	Instrument instrument.ID
	// Interval is the dataset's bar interval.
	Interval Interval
	// Span is the half-open [start, end) range this dataset partition
	// covers.
	Span TimeRange
	// Basis records what this dataset's OHLC prices represent. It
	// mirrors BarSet.Basis and must agree with it (see Matches).
	Basis PriceBasis

	// SchemaVersion identifies the canonical Bar/BarSet field layout this
	// dataset was built against. It is bumped when that layout changes in
	// a way that affects how stored bytes must be interpreted. It must be
	// positive; there is no valid "unset" schema version.
	SchemaVersion int
	// RawFingerprint is an algorithm-qualified content fingerprint of the
	// raw source artifact(s) this dataset was built from, in the form
	// "<algorithm>:<hex digest>" — for example
	// "sha256:2c26b46b68ffc68ff99b453c1d30413413422d706483bfa0f98a5e886266e7ae".
	// Qualifying the algorithm rather than assuming sha256 lets a future
	// build step change digest algorithms without becoming ambiguous with
	// an older fingerprint of a different length. It is the
	// authoritative-input identifier: an unchanged RawFingerprint means
	// an unchanged raw input, regardless of storage layout or file name.
	RawFingerprint string
	// BuilderVersion identifies the code path that turned raw records
	// into canonical Bars.
	BuilderVersion string
	// ValidatorVersion identifies the code path that validated/repaired
	// records during normalization.
	ValidatorVersion string
	// ResamplerVersion identifies the code path used to derive this
	// dataset's interval from a finer one. It is still required (non-
	// empty) even when Parent is nil: a dataset built directly from raw,
	// provider-native bars was not resampled, and by convention such a
	// Manifest records ResamplerVersion as "none" rather than leaving it
	// empty, so an empty value always means "not yet set" rather than
	// "not applicable."
	ResamplerVersion string
	// CalendarVersion identifies the Calendar implementation and rules
	// used to align this dataset's bars.
	CalendarVersion string

	// BuiltAt is when this dataset was built/published, taken from
	// Manager's own configured clock (never time.Now) at the moment
	// Manager performs the build. ADR-020 assigns build initiation and
	// ownership exclusively to Manager — not the composition root — so
	// BuiltAt is provenance Manager itself stamps, not a value a caller
	// supplies. It is deliberately excluded from Revision (see Revision):
	// two datasets built from identical inputs at different times must
	// still agree on revision.
	BuiltAt time.Time

	// BarCount, FirstBar, and LastBar are a lightweight coverage summary:
	// how many bars this dataset holds and the observed range they span.
	// This is not the full Coverage/Gap model (issue #79); it is enough
	// for Matches to confirm a Manifest and BarSet describe the same
	// data.
	BarCount int
	FirstBar time.Time
	LastBar  time.Time

	// Parent identifies the dataset this one was derived from, when this
	// dataset is a resampled/derived series. It is nil for a dataset
	// built directly from raw provider data.
	Parent *ParentRef
}

// rawFingerprintPattern matches RawFingerprint's required
// "<algorithm>:<hex digest>" form: a lowercase algorithm token, a colon,
// and one or more lowercase hex characters. It does not fix the digest
// length to sha256's 64 characters, since RawFingerprint documents the
// algorithm as qualified rather than assumed.
var rawFingerprintPattern = regexp.MustCompile(`^[a-z0-9]+:[0-9a-f]+$`)

// noResampler is the ResamplerVersion value a Manifest built directly
// from raw provider data (no Parent) records, so an empty
// ResamplerVersion always means "not yet set" rather than "not
// applicable." See the ResamplerVersion field doc.
const noResampler = "none"

// Validate reports whether m is a well-formed manifest, returning a
// wrapped sentinel error for the first invariant it violates and nil when
// m is valid. It does not check m against any particular BarSet; see
// Matches for that.
func (m Manifest) Validate() error {
	if m.Provider == "" {
		return fmt.Errorf("marketdata: manifest validate: %w", ErrManifestProvider)
	}
	if m.Instrument.IsZero() {
		return fmt.Errorf("marketdata: manifest validate: %w", ErrManifestInstrument)
	}
	if !m.Interval.Valid() {
		return fmt.Errorf("marketdata: manifest validate: %w", ErrManifestInterval)
	}
	if m.Span.start.IsZero() || m.Span.end.IsZero() || !m.Span.end.After(m.Span.start) {
		return fmt.Errorf("marketdata: manifest validate: %w", ErrManifestSpan)
	}
	if !m.Basis.valid() {
		return fmt.Errorf("marketdata: manifest validate: basis %s: %w", m.Basis, ErrManifestBasis)
	}
	if m.SchemaVersion <= 0 {
		return fmt.Errorf("marketdata: manifest validate: schema version %d: %w", m.SchemaVersion, ErrManifestSchemaVersion)
	}
	if !rawFingerprintPattern.MatchString(m.RawFingerprint) {
		return fmt.Errorf("marketdata: manifest validate: raw fingerprint %q: %w", m.RawFingerprint, ErrManifestFingerprint)
	}
	// Checked in fixed field order, not via map iteration, so the
	// reported error is deterministic when more than one version string
	// is empty.
	if m.BuilderVersion == "" {
		return fmt.Errorf("marketdata: manifest validate: builder version: %w", ErrManifestVersion)
	}
	if m.ValidatorVersion == "" {
		return fmt.Errorf("marketdata: manifest validate: validator version: %w", ErrManifestVersion)
	}
	if m.ResamplerVersion == "" {
		return fmt.Errorf("marketdata: manifest validate: resampler version: %w", ErrManifestVersion)
	}
	if m.CalendarVersion == "" {
		return fmt.Errorf("marketdata: manifest validate: calendar version: %w", ErrManifestVersion)
	}
	if m.BuiltAt.IsZero() {
		return fmt.Errorf("marketdata: manifest validate: %w", ErrManifestBuiltAt)
	}
	if m.BarCount < 0 {
		return fmt.Errorf("marketdata: manifest validate: bar count %d: %w", m.BarCount, ErrManifestBarCount)
	}
	switch m.BarCount {
	case 0:
		// An empty dataset's coverage summary must itself be empty; a
		// non-zero FirstBar/LastBar with no bars would contradict
		// BarCount.
		if !m.FirstBar.IsZero() || !m.LastBar.IsZero() {
			return fmt.Errorf("marketdata: manifest validate: bar count is zero but bar range is set: %w", ErrManifestBarRange)
		}
	case 1:
		if m.FirstBar.IsZero() || m.LastBar.IsZero() || !m.FirstBar.Equal(m.LastBar) {
			return fmt.Errorf("marketdata: manifest validate: single-bar dataset requires equal first/last bar: %w", ErrManifestBarRange)
		}
		if !m.Span.Contains(m.FirstBar) {
			return fmt.Errorf("marketdata: manifest validate: bar range outside span: %w", ErrManifestBarRange)
		}
	default:
		if m.FirstBar.IsZero() || m.LastBar.IsZero() || !m.LastBar.After(m.FirstBar) {
			return fmt.Errorf("marketdata: manifest validate: %w", ErrManifestBarRange)
		}
		if !m.Span.Contains(m.FirstBar) || !m.Span.Contains(m.LastBar) {
			return fmt.Errorf("marketdata: manifest validate: bar range outside span: %w", ErrManifestBarRange)
		}
	}
	if err := m.validateLineage(); err != nil {
		return err
	}
	return nil
}

// validateLineage checks that Parent and ResamplerVersion agree with each
// other and, when Parent is set, that the parent's identity is a
// plausible source for this dataset: the same instrument, at a strictly
// finer interval than this Manifest's own Interval. A resampled dataset
// only ever derives a coarser interval from a finer one, so a parent at
// the same or a coarser interval is rejected as impossible lineage rather
// than merely unverified.
func (m Manifest) validateLineage() error {
	switch {
	case m.Parent == nil && m.ResamplerVersion != noResampler:
		return fmt.Errorf("marketdata: manifest validate: resampler version %q set without a parent: %w", m.ResamplerVersion, ErrManifestParent)
	case m.Parent == nil:
		return nil
	case m.ResamplerVersion == noResampler:
		return fmt.Errorf("marketdata: manifest validate: parent set but resampler version is %q: %w", noResampler, ErrManifestParent)
	}
	if !m.Parent.valid() {
		return fmt.Errorf("marketdata: manifest validate: %w", ErrManifestParent)
	}
	if !m.Parent.Instrument.Equal(m.Instrument) {
		return fmt.Errorf("marketdata: manifest validate: parent instrument %s != %s: %w",
			m.Parent.Instrument, m.Instrument, ErrManifestParent)
	}
	if !intervalFiner(m.Parent.Interval, m.Interval) {
		return fmt.Errorf("marketdata: manifest validate: parent interval %s is not strictly finer than %s: %w",
			m.Parent.Interval, m.Interval, ErrManifestParent)
	}
	return nil
}

// intervalFiner reports whether a's nominal duration is strictly shorter
// than b's. It compares approximate calendar durations (not exact,
// session-aligned ones — day and week are nominal 24h/7d) because it
// exists only to reject impossible Manifest lineage, not to perform
// calendar-accurate resampling.
func intervalFiner(a, b Interval) bool {
	return nominalDuration(a) < nominalDuration(b)
}

// nominalDuration returns i's approximate elapsed duration: exact for
// minute/hour, nominal (DST- and holiday-ignorant) for day/week.
func nominalDuration(i Interval) time.Duration {
	var unit time.Duration
	switch i.Unit() {
	case UnitMinute:
		unit = time.Minute
	case UnitHour:
		unit = time.Hour
	case UnitDay:
		unit = 24 * time.Hour
	case UnitWeek:
		unit = 7 * 24 * time.Hour
	}
	return unit * time.Duration(i.Count())
}

// Matches reports whether m and bs describe the same dataset, returning a
// wrapped ErrManifestMismatch identifying the first field that disagrees,
// and nil when they are a valid pair. It does not call Validate on either
// argument; callers that need both a structural check and a pairing check
// should call both.
func (m Manifest) Matches(bs BarSet) error {
	if !m.Instrument.Equal(bs.Instrument) {
		return fmt.Errorf("marketdata: manifest matches: instrument %s != bar set instrument %s: %w",
			m.Instrument, bs.Instrument, ErrManifestMismatch)
	}
	if m.Interval != bs.Interval {
		return fmt.Errorf("marketdata: manifest matches: interval %s != bar set interval %s: %w",
			m.Interval, bs.Interval, ErrManifestMismatch)
	}
	if m.Basis != bs.Basis {
		return fmt.Errorf("marketdata: manifest matches: basis %s != bar set basis %s: %w",
			m.Basis, bs.Basis, ErrManifestMismatch)
	}
	if !m.Span.start.Equal(bs.Span.start) || !m.Span.end.Equal(bs.Span.end) {
		return fmt.Errorf("marketdata: manifest matches: span [%s, %s) != bar set span [%s, %s): %w",
			m.Span.start, m.Span.end, bs.Span.start, bs.Span.end, ErrManifestMismatch)
	}
	if m.BarCount != len(bs.Bars) {
		return fmt.Errorf("marketdata: manifest matches: bar count %d != bar set length %d: %w",
			m.BarCount, len(bs.Bars), ErrManifestMismatch)
	}
	if len(bs.Bars) == 0 {
		if !m.FirstBar.IsZero() || !m.LastBar.IsZero() {
			return fmt.Errorf("marketdata: manifest matches: bar set is empty but manifest bar range is set: %w", ErrManifestMismatch)
		}
		return nil
	}
	if first := bs.Bars[0].Time; !m.FirstBar.Equal(first) {
		return fmt.Errorf("marketdata: manifest matches: first bar %s != bar set first bar %s: %w",
			m.FirstBar, first, ErrManifestMismatch)
	}
	if last := bs.Bars[len(bs.Bars)-1].Time; !m.LastBar.Equal(last) {
		return fmt.Errorf("marketdata: manifest matches: last bar %s != bar set last bar %s: %w",
			m.LastBar, last, ErrManifestMismatch)
	}
	return nil
}

// manifestRevisionVersion tags the encoding Revision hashes, so a future
// change to that encoding is an explicit, versioned break rather than a
// silent reinterpretation of old bytes.
const manifestRevisionVersion = "manifest-revision-v1"

// revisionInput is the exact, unambiguous structure Revision hashes.
// Using encoding/json rather than a hand-built delimited string removes
// the encoding ambiguity a delimiter-joined string would have: JSON
// quotes and escapes each string field, so no field's content — for
// example an embedded "\n" or another field's own key-like text — can be
// misread as a field boundary. Field order is fixed by this struct's
// declaration, not by map iteration, so encoding/json's (already
// order-preserving, for structs) output is deterministic. BuiltAt is
// deliberately absent: see the BuiltAt field doc on Manifest.
type revisionInput struct {
	Version          string             `json:"version"`
	Provider         string             `json:"provider"`
	Instrument       string             `json:"instrument"`
	IntervalUnit     Unit               `json:"interval_unit"`
	IntervalCount    int                `json:"interval_count"`
	SpanStart        int64              `json:"span_start"`
	SpanEnd          int64              `json:"span_end"`
	Basis            PriceBasis         `json:"basis"`
	SchemaVersion    int                `json:"schema_version"`
	RawFingerprint   string             `json:"raw_fingerprint"`
	BuilderVersion   string             `json:"builder_version"`
	ValidatorVersion string             `json:"validator_version"`
	ResamplerVersion string             `json:"resampler_version"`
	CalendarVersion  string             `json:"calendar_version"`
	BarCount         int                `json:"bar_count"`
	FirstBar         int64              `json:"first_bar"`
	LastBar          int64              `json:"last_bar"`
	Parent           *parentRevisionRef `json:"parent"`
}

// parentRevisionRef is ParentRef's contribution to revisionInput.
type parentRevisionRef struct {
	Instrument    string `json:"instrument"`
	IntervalUnit  Unit   `json:"interval_unit"`
	IntervalCount int    `json:"interval_count"`
	Revision      string `json:"revision"`
}

// Revision returns a deterministic, hex-encoded sha256 fingerprint of m.
// Two Manifest values with identical field values — built independently,
// possibly by different processes — always produce the same Revision;
// changing any single field other than BuiltAt, including Parent, changes
// it. BuiltAt is excluded: it is provenance (when Manager happened to
// publish this dataset), not identity, and two datasets built from
// identical inputs at different times must still agree on revision.
//
// Revision is computed on demand rather than stored, so it can never be
// set inconsistently with the fields it describes. A canonical file
// header or store records this string as the dataset's version
// identifier (ADR-020: the version identifier lives in the manifest or a
// canonical file header, never in the path).
//
// Revision does not validate m; it is defined over the zero Manifest and
// every other value, so a caller can compute it before or independently
// of calling Validate. Revision panics if the underlying JSON encoding
// fails, which is not reachable for this struct's field types (strings,
// ints, and a nested struct of the same).
func (m Manifest) Revision() string {
	in := revisionInput{
		Version:          manifestRevisionVersion,
		Provider:         m.Provider,
		Instrument:       m.Instrument.String(),
		IntervalUnit:     m.Interval.Unit(),
		IntervalCount:    m.Interval.Count(),
		SpanStart:        m.Span.start.UnixNano(),
		SpanEnd:          m.Span.end.UnixNano(),
		Basis:            m.Basis,
		SchemaVersion:    m.SchemaVersion,
		RawFingerprint:   m.RawFingerprint,
		BuilderVersion:   m.BuilderVersion,
		ValidatorVersion: m.ValidatorVersion,
		ResamplerVersion: m.ResamplerVersion,
		CalendarVersion:  m.CalendarVersion,
		BarCount:         m.BarCount,
		FirstBar:         m.FirstBar.UnixNano(),
		LastBar:          m.LastBar.UnixNano(),
	}
	if m.Parent != nil {
		in.Parent = &parentRevisionRef{
			Instrument:    m.Parent.Instrument.String(),
			IntervalUnit:  m.Parent.Interval.Unit(),
			IntervalCount: m.Parent.Interval.Count(),
			Revision:      m.Parent.Revision,
		}
	}
	encoded, err := json.Marshal(in)
	if err != nil {
		panic(fmt.Sprintf("marketdata: manifest revision: encoding: %v", err))
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}
