package backtest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/strategy"
)

// ErrInvalidManifest marks a ManifestParams missing a required field.
var ErrInvalidManifest = errors.New("backtest: invalid manifest")

// ManifestParams are NewManifest's inputs — see Manifest's own doc
// comment for what each field means and why. StrategyParameters is any
// Go value (a strategy's own concrete parameter struct, typically);
// NewManifest canonically marshals it via json.Marshal so semantically
// identical parameters always produce identical Manifest bytes,
// regardless of how the caller happened to construct that value. Pass
// nil for a strategy with no parameters.
type ManifestParams struct {
	RunID              id.RunID
	StrategyName       string
	StrategyVersion    string
	StrategyParameters any
	Universe           []strategy.DataRequirement
	Span               marketdata.TimeRange
	StartingCapital    num.Money
	RiskFraction       num.Rate
	AdverseDistance    num.Price
	RiskRules          []ComponentInfo
	FillModel          ComponentInfo
	SlippageModel      ComponentInfo
	CommissionModel    ComponentInfo
	Dataset            []marketdata.Manifest
	TraderVersion      string
}

// Manifest is the immutable record of everything needed to identify
// and reproduce one backtest run (issue #215, M5-07; ADR-035's own
// "resolved listing's reproducibility-relevant fields belong in the
// run manifest" note).
//
// RunID identifies this one execution instance; it is not part of what
// makes two runs "the same configuration" — see ConfigDigest for that.
// Nothing about output paths, filenames, or where a run's results were
// written appears anywhere in Manifest, so run identity never depends
// on either (issue #215's own acceptance criterion): that is a
// journal/report concern (a later issue), not this value's.
//
// Manifest is constructed only through NewManifest, never a struct
// literal: every slice and byte field is defensively copied and
// canonically sorted at construction, and every accessor returns a
// fresh copy — so a caller can never mutate a Manifest's internal
// state through a value it was handed, and two Manifests built from
// the same logical inputs in a different construction order (equal
// requirement sets or risk rules listed differently) are equal and
// serialize identically.
type Manifest struct {
	runID id.RunID

	strategyName       string
	strategyVersion    string
	strategyParameters json.RawMessage

	universe        []strategy.DataRequirement
	span            marketdata.TimeRange
	startingCapital num.Money

	riskFraction    num.Rate
	adverseDistance num.Price
	riskRules       []ComponentInfo

	fillModel       ComponentInfo
	slippageModel   ComponentInfo
	commissionModel ComponentInfo

	dataset []marketdata.Manifest

	traderVersion string
}

// NewManifest validates p and returns an immutable Manifest.
func NewManifest(p ManifestParams) (Manifest, error) {
	if p.RunID.IsZero() {
		return Manifest{}, fmt.Errorf("%w: run id must be set", ErrInvalidManifest)
	}
	if p.StrategyName == "" {
		return Manifest{}, fmt.Errorf("%w: strategy name must be set", ErrInvalidManifest)
	}
	if len(p.Universe) == 0 {
		return Manifest{}, fmt.Errorf("%w: universe must not be empty", ErrInvalidManifest)
	}
	if p.Span.Duration() <= 0 {
		return Manifest{}, fmt.Errorf("%w: span must be set", ErrInvalidManifest)
	}
	if !p.StartingCapital.IsValid() {
		return Manifest{}, fmt.Errorf("%w: starting capital must be valid", ErrInvalidManifest)
	}
	if p.FillModel.name == "" {
		return Manifest{}, fmt.Errorf("%w: fill model must be set", ErrInvalidManifest)
	}
	if p.SlippageModel.name == "" {
		return Manifest{}, fmt.Errorf("%w: slippage model must be set", ErrInvalidManifest)
	}
	if p.CommissionModel.name == "" {
		return Manifest{}, fmt.Errorf("%w: commission model must be set", ErrInvalidManifest)
	}
	if len(p.Dataset) == 0 {
		return Manifest{}, fmt.Errorf("%w: dataset must not be empty", ErrInvalidManifest)
	}
	for _, d := range p.Dataset {
		if err := d.Validate(); err != nil {
			return Manifest{}, fmt.Errorf("%w: dataset entry: %v", ErrInvalidManifest, err)
		}
	}
	for _, r := range p.RiskRules {
		if r.name == "" {
			return Manifest{}, fmt.Errorf("%w: risk rule must have a name", ErrInvalidManifest)
		}
	}

	// (instrument, interval) is the requirement's own identity
	// elsewhere (Replay, Scheduler both reject a duplicate the same
	// way) — reject it here too, rather than trying to make WarmupBars
	// part of the sort's tie-break, so canonical ordering never has to
	// disambiguate two requirements claiming the same identity with a
	// different WarmupBars (issue #215 review).
	seen := make(map[requirementKey]struct{}, len(p.Universe))
	for _, req := range p.Universe {
		key := requirementKey{instrument: req.Instrument, interval: req.Interval}
		if _, ok := seen[key]; ok {
			return Manifest{}, fmt.Errorf("%w: %s %s", ErrDuplicateRequirement, req.Instrument, req.Interval)
		}
		seen[key] = struct{}{}
	}

	params, err := json.Marshal(p.StrategyParameters)
	if err != nil {
		return Manifest{}, fmt.Errorf("%w: marshaling strategy parameters: %v", ErrInvalidManifest, err)
	}

	universe := append([]strategy.DataRequirement(nil), p.Universe...)
	sort.Slice(universe, func(i, j int) bool { return dataRequirementLess(universe[i], universe[j]) })

	riskRules := append([]ComponentInfo(nil), p.RiskRules...)
	sort.Slice(riskRules, func(i, j int) bool { return componentInfoLess(riskRules[i], riskRules[j]) })

	dataset := append([]marketdata.Manifest(nil), p.Dataset...)
	sort.Slice(dataset, func(i, j int) bool { return datasetManifestLess(dataset[i], dataset[j]) })

	return Manifest{
		runID:              p.RunID,
		strategyName:       p.StrategyName,
		strategyVersion:    p.StrategyVersion,
		strategyParameters: params,
		universe:           universe,
		span:               p.Span,
		startingCapital:    p.StartingCapital,
		riskFraction:       p.RiskFraction,
		adverseDistance:    p.AdverseDistance,
		riskRules:          riskRules,
		fillModel:          p.FillModel,
		slippageModel:      p.SlippageModel,
		commissionModel:    p.CommissionModel,
		dataset:            dataset,
		traderVersion:      p.TraderVersion,
	}, nil
}

func dataRequirementLess(a, b strategy.DataRequirement) bool {
	if ai, bi := a.Instrument.String(), b.Instrument.String(); ai != bi {
		return ai < bi
	}
	if au, bu := a.Interval.Unit(), b.Interval.Unit(); au != bu {
		return au < bu
	}
	return a.Interval.Count() < b.Interval.Count()
}

// componentInfoLess orders by (name, version, parameters) — a total
// order over every field ComponentInfo.Equal itself compares, so two
// ComponentInfo values sort.Slice cannot distinguish are genuinely
// equal, never merely tied on the fields this comparator happened to
// check (issue #215 review: sort.Slice is not stable, so a partial
// comparator can leave canonical order depending on caller input
// order for two same-name/version, different-parameters components).
func componentInfoLess(a, b ComponentInfo) bool {
	if a.name != b.name {
		return a.name < b.name
	}
	if a.version != b.version {
		return a.version < b.version
	}
	return string(a.parameters) < string(b.parameters)
}

// datasetManifestLess orders by (instrument, interval, provider, span
// start, span end, revision) — Revision is marketdata.Manifest's own
// content fingerprint (issue #79/#81), so it is the final tie-breaker
// that makes this a total order over every field that can distinguish
// two dataset Manifests describing the same instrument/interval/span
// (issue #215 review).
func datasetManifestLess(a, b marketdata.Manifest) bool {
	ai, bi := a.Instrument.String(), b.Instrument.String()
	if ai != bi {
		return ai < bi
	}
	if au, bu := a.Interval.Unit(), b.Interval.Unit(); au != bu {
		return au < bu
	}
	if a.Interval.Count() != b.Interval.Count() {
		return a.Interval.Count() < b.Interval.Count()
	}
	if a.Provider != b.Provider {
		return a.Provider < b.Provider
	}
	if !a.Span.Start().Equal(b.Span.Start()) {
		return a.Span.Start().Before(b.Span.Start())
	}
	if !a.Span.End().Equal(b.Span.End()) {
		return a.Span.End().Before(b.Span.End())
	}
	return a.Revision() < b.Revision()
}

// RunID returns this manifest's own run identity.
func (m Manifest) RunID() id.RunID { return m.runID }

// StrategyName returns the strategy's own configured name.
func (m Manifest) StrategyName() string { return m.strategyName }

// StrategyVersion returns the strategy's own configured version.
func (m Manifest) StrategyVersion() string { return m.strategyVersion }

// StrategyParameters returns a defensive copy of the strategy's own
// canonically marshaled JSON parameters.
func (m Manifest) StrategyParameters() json.RawMessage {
	return append(json.RawMessage(nil), m.strategyParameters...)
}

// Universe returns a defensive copy of the run's declared
// DataRequirements, in canonical (instrument, interval) order.
func (m Manifest) Universe() []strategy.DataRequirement {
	return append([]strategy.DataRequirement(nil), m.universe...)
}

// Span returns the run's own replayed time range.
func (m Manifest) Span() marketdata.TimeRange { return m.span }

// StartingCapital returns the run's own starting account balance.
func (m Manifest) StartingCapital() num.Money { return m.startingCapital }

// RiskFraction returns the run's own configured position-sizing risk
// fraction.
func (m Manifest) RiskFraction() num.Rate { return m.riskFraction }

// AdverseDistance returns the run's own configured adverse-price-
// distance sizing assumption.
func (m Manifest) AdverseDistance() num.Price { return m.adverseDistance }

// RiskRules returns a defensive copy of the run's configured risk
// rules, in canonical (name, version) order.
func (m Manifest) RiskRules() []ComponentInfo {
	return append([]ComponentInfo(nil), m.riskRules...)
}

// FillModel returns the run's own configured fill model.
func (m Manifest) FillModel() ComponentInfo { return m.fillModel }

// SlippageModel returns the run's own configured slippage model.
func (m Manifest) SlippageModel() ComponentInfo { return m.slippageModel }

// CommissionModel returns the run's own configured commission model.
func (m Manifest) CommissionModel() ComponentInfo { return m.commissionModel }

// Dataset returns a defensive copy of the canonical dataset provenance
// (one marketdata.Manifest per touched partition) this run replayed
// from, in canonical (instrument, interval, span start) order.
func (m Manifest) Dataset() []marketdata.Manifest {
	return append([]marketdata.Manifest(nil), m.dataset...)
}

// DatasetSummary is one dataset partition's provenance/revision
// identity, projected out of marketdata.Manifest into plain built-in
// types and instrument.ID (issue #254, EMA-09) — report must not
// import marketdata itself (ADR-035/ADR-038's own boundary), so this
// is the backtest-owned surface it projects instead, the same role
// InstrumentMetrics/SideMetrics already play for per-instrument/
// per-side breakdowns.
type DatasetSummary struct {
	Provider   string
	Instrument instrument.ID
	Interval   string
	SpanStart  time.Time
	SpanEnd    time.Time
	Revision   string
}

// DatasetSummaries returns Dataset() projected into DatasetSummary,
// preserving the same canonical order.
func (m Manifest) DatasetSummaries() []DatasetSummary {
	out := make([]DatasetSummary, len(m.dataset))
	for i, dm := range m.dataset {
		out[i] = DatasetSummary{
			Provider:   dm.Provider,
			Instrument: dm.Instrument,
			Interval:   dm.Interval.String(),
			SpanStart:  dm.Span.Start(),
			SpanEnd:    dm.Span.End(),
			Revision:   dm.Revision(),
		}
	}
	return out
}

// TraderVersion returns the caller-supplied Trader build/version
// string, or "" if none was given.
func (m Manifest) TraderVersion() string { return m.traderVersion }

// configWire is every reproducibility-relevant field's JSON shape —
// deliberately everything in Manifest *except* RunID, which is run
// identity, not configuration (issue #215 review's "separate run
// identity from reproducibility identity"). It is embedded into
// manifestWire (which adds RunID) for MarshalJSON, and marshaled
// directly, on its own, for ConfigDigest — id.RunID.MarshalJSON
// itself rejects the zero value, so ConfigDigest cannot simply zero
// out a RunID field and marshal the same shape; omitting the field
// entirely is the only option, hence the split type rather than one
// shared struct with RunID zeroed.
type configWire struct {
	StrategyName       string                     `json:"strategy_name"`
	StrategyVersion    string                     `json:"strategy_version,omitempty"`
	StrategyParameters json.RawMessage            `json:"strategy_parameters,omitempty"`
	Universe           []strategy.DataRequirement `json:"universe"`
	Span               marketdata.TimeRange       `json:"span"`
	StartingCapital    num.Money                  `json:"starting_capital"`
	RiskFraction       num.Rate                   `json:"risk_fraction"`
	AdverseDistance    num.Price                  `json:"adverse_distance"`
	RiskRules          []componentInfoWire        `json:"risk_rules,omitempty"`
	FillModel          componentInfoWire          `json:"fill_model"`
	SlippageModel      componentInfoWire          `json:"slippage_model"`
	CommissionModel    componentInfoWire          `json:"commission_model"`
	Dataset            []marketdata.Manifest      `json:"dataset"`
	TraderVersion      string                     `json:"trader_version,omitempty"`
}

// manifestWire is Manifest's full exported JSON shape: RunID plus
// every configWire field.
type manifestWire struct {
	RunID id.RunID `json:"run_id"`
	configWire
}

type componentInfoWire struct {
	Name       string          `json:"name"`
	Version    string          `json:"version,omitempty"`
	Parameters json.RawMessage `json:"parameters,omitempty"`
}

func (c ComponentInfo) wire() componentInfoWire {
	return componentInfoWire{Name: c.name, Version: c.version, Parameters: c.parameters}
}

func (m Manifest) configWire() configWire {
	riskRules := make([]componentInfoWire, len(m.riskRules))
	for i, r := range m.riskRules {
		riskRules[i] = r.wire()
	}
	return configWire{
		StrategyName:       m.strategyName,
		StrategyVersion:    m.strategyVersion,
		StrategyParameters: m.strategyParameters,
		Universe:           m.universe,
		Span:               m.span,
		StartingCapital:    m.startingCapital,
		RiskFraction:       m.riskFraction,
		AdverseDistance:    m.adverseDistance,
		RiskRules:          riskRules,
		FillModel:          m.fillModel.wire(),
		SlippageModel:      m.slippageModel.wire(),
		CommissionModel:    m.commissionModel.wire(),
		Dataset:            m.dataset,
		TraderVersion:      m.traderVersion,
	}
}

// MarshalJSON implements json.Marshaler. Field order and every
// collection's canonical sort (established by NewManifest) make the
// output deterministic across independently constructed Manifests
// describing the same logical inputs.
func (m Manifest) MarshalJSON() ([]byte, error) {
	return json.Marshal(manifestWire{RunID: m.runID, configWire: m.configWire()})
}

// ConfigDigest returns a deterministic "sha256:<hex>" fingerprint
// (matching marketdata.Manifest.RawFingerprint's own convention) of
// every reproducibility-relevant field *except* RunID — issue #215
// review's "separate run identity from reproducibility identity."
// Two Manifests built from the same logical run configuration share
// one ConfigDigest even though each has its own unique RunID; this is
// what lets a later run be recognized as "the same configuration" as
// an earlier one.
func (m Manifest) ConfigDigest() string {
	data, err := json.Marshal(m.configWire())
	if err != nil {
		// configWire only contains types this package already proved
		// marshal cleanly (NewManifest itself round-trips them);
		// reaching this would be a bug in this package, not bad input.
		panic(fmt.Sprintf("backtest: manifest: computing config digest: %v", err))
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Equal reports whether m and o describe the same run configuration
// and the same run identity — RunID included. Use ConfigDigest to
// compare configuration alone, ignoring RunID.
func (m Manifest) Equal(o Manifest) bool {
	if !m.runID.Equal(o.runID) {
		return false
	}
	return m.ConfigDigest() == o.ConfigDigest()
}
