package marketdata

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
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
	// ErrManifestFingerprint marks a Manifest with an empty raw
	// fingerprint.
	ErrManifestFingerprint = errors.New("marketdata: manifest raw fingerprint is empty")
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
	// ErrManifestParent marks a Manifest with a malformed Parent
	// reference (set, but missing a required field).
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
// summary, and build information (ADR-020, issue #73). BarSet holds
// observation-local metadata; Manifest holds why the dataset is
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
// supplied by the (not yet implemented) build step that reads raw
// provider bytes; Manifest itself has no provider or storage dependency
// and never reaches into a package such as
// marketdata/internal/provider/oanda.
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
	// a way that affects how stored bytes must be interpreted.
	SchemaVersion int
	// RawFingerprint is a hex-encoded content fingerprint (for example
	// sha256) of the raw source artifact(s) this dataset was built from.
	// It is the authoritative-input identifier: an unchanged
	// RawFingerprint means an unchanged raw input, regardless of storage
	// layout or file name.
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

	// BuiltAt is when this dataset was built/published, taken from an
	// injected clock (never time.Now) at the composition root that
	// performs the build.
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
	if m.RawFingerprint == "" {
		return fmt.Errorf("marketdata: manifest validate: %w", ErrManifestFingerprint)
	}
	for name, v := range map[string]string{
		"builder":   m.BuilderVersion,
		"validator": m.ValidatorVersion,
		"resampler": m.ResamplerVersion,
		"calendar":  m.CalendarVersion,
	} {
		if v == "" {
			return fmt.Errorf("marketdata: manifest validate: %s version: %w", name, ErrManifestVersion)
		}
	}
	if m.BuiltAt.IsZero() {
		return fmt.Errorf("marketdata: manifest validate: %w", ErrManifestBuiltAt)
	}
	if m.BarCount < 0 {
		return fmt.Errorf("marketdata: manifest validate: bar count %d: %w", m.BarCount, ErrManifestBarCount)
	}
	if m.BarCount > 0 {
		if m.FirstBar.IsZero() || m.LastBar.IsZero() || m.LastBar.Before(m.FirstBar) {
			return fmt.Errorf("marketdata: manifest validate: %w", ErrManifestBarRange)
		}
		if !m.Span.Contains(m.FirstBar) || !m.Span.Contains(m.LastBar) {
			return fmt.Errorf("marketdata: manifest validate: bar range outside span: %w", ErrManifestBarRange)
		}
	}
	if m.Parent != nil && !m.Parent.valid() {
		return fmt.Errorf("marketdata: manifest validate: %w", ErrManifestParent)
	}
	return nil
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
	return nil
}

// Revision returns a deterministic, hex-encoded sha256 fingerprint of
// every field on m except itself. Two Manifest values with identical
// field values — built independently, possibly by different processes —
// always produce the same Revision; changing any single field, including
// Parent, changes it.
//
// Revision is computed on demand rather than stored, so it can never be
// set inconsistently with the fields it describes. A canonical file
// header or store records this string as the dataset's version
// identifier (ADR-020: the version identifier lives in the manifest or a
// canonical file header, never in the path).
//
// Revision does not validate m; it is defined over the zero Manifest and
// every other value, so a caller can compute it before or independently
// of calling Validate.
func (m Manifest) Revision() string {
	h := sha256.New()
	fmt.Fprintf(h, "provider=%s\n", m.Provider)
	fmt.Fprintf(h, "instrument=%s\n", m.Instrument.String())
	fmt.Fprintf(h, "interval=%d:%d\n", m.Interval.Unit(), m.Interval.Count())
	fmt.Fprintf(h, "span=%d:%d\n", m.Span.start.UnixNano(), m.Span.end.UnixNano())
	fmt.Fprintf(h, "basis=%d\n", m.Basis)
	fmt.Fprintf(h, "schema=%d\n", m.SchemaVersion)
	fmt.Fprintf(h, "rawfp=%s\n", m.RawFingerprint)
	fmt.Fprintf(h, "builder=%s\n", m.BuilderVersion)
	fmt.Fprintf(h, "validator=%s\n", m.ValidatorVersion)
	fmt.Fprintf(h, "resampler=%s\n", m.ResamplerVersion)
	fmt.Fprintf(h, "calendar=%s\n", m.CalendarVersion)
	fmt.Fprintf(h, "builtat=%d\n", m.BuiltAt.UnixNano())
	fmt.Fprintf(h, "barcount=%d\n", m.BarCount)
	fmt.Fprintf(h, "firstbar=%d\n", m.FirstBar.UnixNano())
	fmt.Fprintf(h, "lastbar=%d\n", m.LastBar.UnixNano())
	if m.Parent != nil {
		fmt.Fprintf(h, "parent=%s:%d:%d:%s\n",
			m.Parent.Instrument.String(), m.Parent.Interval.Unit(), m.Parent.Interval.Count(), m.Parent.Revision)
	} else {
		fmt.Fprintf(h, "parent=none\n")
	}
	return hex.EncodeToString(h.Sum(nil))
}
