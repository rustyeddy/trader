package smatrend

import (
	"fmt"
	"strconv"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
)

// ExitDecision is what an ExitRule returns for one bar while long.
// NewStop and ExitNow are mutually exclusive: a rule must never set
// both on the same decision. Strategy.onLong treats this as a
// contract violation (an error, not a silent pick-one), since a rule
// that both requests an immediate exit and a stop adjustment has not
// actually decided anything.
type ExitDecision struct {
	// NewStop, if non-nil, requests placing/ratcheting the protective
	// stop to this price via order.IntentAdjustStop — the mechanism
	// every ExitRule that manages a resting stop uses. Must be nil
	// whenever ExitNow is true.
	NewStop *num.Price
	// ExitNow, if true, requests an immediate order.IntentExit
	// instead — for a rule whose own trigger condition is not itself
	// expressible as a resting broker-side stop (for example "close
	// crosses back below the SMA"). Must be false whenever NewStop is
	// non-nil.
	ExitNow bool
}

// ExitRule decides, once per bar while a long position is open and
// has survived the bar (see Strategy.OnBar's own doc comment for why
// any broker-triggered stop from a prior bar has already resolved by
// the time this runs), what protective action — if any — to take
// (issue #347). It owns whatever state it needs relative to the
// current position (for example a high-water mark); OnEntry
// establishes that state fresh for each new position, never carrying
// over stale state from a previous one.
type ExitRule interface {
	// OnEntry is called exactly once, on the first bar a fresh long
	// position is observed, so the rule can initialize any state
	// relative to this specific position (for example seeding a
	// high-water mark from the entry bar's own High). entryPrice is
	// the position's real average fill price, read from the account's
	// own Position (issue #349 review) — not a signal-bar
	// approximation — for a rule whose trigger depends on gain from
	// entry (for example probationTrendExitRule's trail activation).
	// A rule with no such dependency (trailingStopExitRule,
	// smaCrossExitRule) simply ignores it.
	OnEntry(entryBar marketdata.Bar, entryPrice num.Price)
	// OnLongBar is called once per bar while long, after the position
	// has survived the bar. smaValue is the strategy's own current
	// SMA value, supplied for a rule whose trigger depends on it (for
	// example smaCrossExitRule) rather than requiring every rule to
	// maintain its own SMA independently.
	OnLongBar(bar marketdata.Bar, smaValue float64) (ExitDecision, error)
}

// InitialStopProvider is an optional ExitRule capability (issue #368),
// mirroring phaseReporter's own type-assertion pattern in strategy.go:
// an ExitRule implements it only when its intended first protective
// stop is computable *before* the entry fill, from information
// available at the entry-decision bar. smaValue is that bar's own
// current SMA value — the only signal Strategy already has in hand at
// decision time — so a rule computes InitialStop from exactly the
// same inputs it would otherwise wait one bar to use. Strategy uses
// this to emit order.IntentEnterWithStop (ADR-059) instead of a plain
// order.IntentEnter wherever it is available, closing the entry-fill-
// bar protection gap for whichever ExitRule can offer one.
//
// trailingStopExitRule and smaCrossExitRule never implement this:
// trailingStopExitRule's first stop is defined in terms of the entry/
// fill bar's own High, not known before the fill; smaCrossExitRule
// manages no resting stop at all. Only probationTrendExitRule
// implements it today, since its probation stop is already, by
// design, a pure function of the SMA value alone (Config.
// InitialStopBelowSMA) — never the fill price.
type InitialStopProvider interface {
	InitialStop(smaValue float64) (num.Price, error)
}

// InitialStopSeeder is a second, separate optional ExitRule capability
// (PR #369 review): an ExitRule that implements InitialStopProvider
// may additionally implement this to receive the stop Strategy
// actually placed via the resulting bracket entry, so it can seed its
// own ratchet-floor state from that exact value instead of whatever
// default OnEntry itself establishes — without widening OnEntry's own
// required, exported signature. ExitRule is a public interface any
// external/custom implementation may satisfy; adding a parameter to
// OnEntry would have broken every one of them for a
// probation-trend-only enhancement. Strategy calls SeedInitialStop
// immediately after OnEntry, and only when Strategy actually used a
// bracket entry for this position — never for a plain entry, and
// never before OnEntry has already run its own (now-superseded)
// default initialization.
type InitialStopSeeder interface {
	SeedInitialStop(stop num.Price)
}

// exitRuleRegistry maps a Config.ExitRuleName to its constructor. A
// new ExitRule is a new small type plus one entry here — never a
// change to Strategy's own control flow (issue #347).
var exitRuleRegistry = map[string]func(Config) (ExitRule, error){
	"trailing-stop":   newTrailingStopExitRule,
	"sma-cross":       newSMACrossExitRule,
	"probation-trend": newProbationTrendExitRule,
}

// trailingStopExitRule is EQS-01's own original, only exit mechanism
// (issue #335), now extracted behind ExitRule: maintain a high-water
// mark from each bar's own High while long, and ratchet a protective
// stop to Config's own TrailingStopPercent below it — monotonically
// upward only, never emitting a downward adjustment.
type trailingStopExitRule struct {
	retainFraction num.Rate
	highWaterMark  *num.Price
	lastStop       *num.Price
}

func newTrailingStopExitRule(cfg Config) (ExitRule, error) {
	retain, err := cfg.StopFraction()
	if err != nil {
		return nil, err
	}
	return &trailingStopExitRule{retainFraction: retain}, nil
}

func (r *trailingStopExitRule) OnEntry(entryBar marketdata.Bar, _ num.Price) {
	high := entryBar.High
	r.highWaterMark = &high
	r.lastStop = nil
}

func (r *trailingStopExitRule) OnLongBar(bar marketdata.Bar, smaValue float64) (ExitDecision, error) {
	high := bar.High
	if r.highWaterMark == nil || high.Cmp(*r.highWaterMark) > 0 {
		r.highWaterMark = &high
	}

	newStop, err := r.highWaterMark.MulRate(r.retainFraction)
	if err != nil {
		return ExitDecision{}, fmt.Errorf("smatrend: computing trailing stop from high-water mark: %w", err)
	}

	if r.lastStop != nil && newStop.Cmp(*r.lastStop) <= 0 {
		return ExitDecision{}, nil
	}
	r.lastStop = &newStop
	return ExitDecision{NewStop: &newStop}, nil
}

// smaCrossExitRule exits immediately (order.IntentExit, not a resting
// stop) the first bar the close falls back to or below the current
// SMA value — issue #347's second ExitRule implementation,
// demonstrating a rule whose own trigger is not itself expressible as
// a resting broker-side price at all. Stateless: it needs nothing
// beyond the bar and SMA value OnLongBar is already given.
type smaCrossExitRule struct{}

func newSMACrossExitRule(Config) (ExitRule, error) {
	return smaCrossExitRule{}, nil
}

func (smaCrossExitRule) OnEntry(marketdata.Bar, num.Price) {}

func (smaCrossExitRule) OnLongBar(bar marketdata.Bar, smaValue float64) (ExitDecision, error) {
	// bar.Close.Float64() is ADR-045's explicit exact-to-analytical
	// conversion boundary: a direct numeric conversion, never a
	// String()/strconv.ParseFloat() round-trip.
	if bar.Close.Float64() <= smaValue {
		return ExitDecision{ExitNow: true}, nil
	}
	return ExitDecision{}, nil
}

// probationTrendExitRule implements the SMA Long Hold playbook's full
// FLAT->PROBATION->TRENDING lifecycle (issue #349) as one ExitRule: a
// tight protective stop just below the SMA, plus an independent
// SMA-cross override, until the position's gain from its own entry
// (real average fill price) reaches Config.TrailActivationGain — at
// which point it activates a monotonic trailing stop off the
// high-water mark since entry, and the SMA no longer has any exit
// power at all.
//
// Activation is evaluated on the completed Close only, never the
// High, against the real fill price OnEntry is given (issue #349
// review's explicit preference), and only takes effect starting the
// *next* OnLongBar call: the bar that first satisfies the activation
// condition still computes and ratchets its stop under the
// *previous* phase's own rule first — a same-bar activation must
// never retrospectively tighten or loosen the stop that bar itself
// already decided. The handoff itself never loosens protection either
// (issue #349 review): TRENDING's own ratchet floor starts at
// whatever level PROBATION last protected, so if TRENDING's own
// high-water-mark formula would imply a lower stop than that, nothing
// is emitted until it genuinely ratchets past it — see onProbationBar.
//
// Formerly known limitation (PR #350 review; closed by issue #368):
// the entry fill bar itself used to be unprotected by this rule's own
// intended probation stop, since that stop was only computed and
// emitted as an AdjustStop intent on the bar OnBar first observed the
// fresh Long position — one bar after backtest.Scheduler's own
// broker-side resting-order machinery had already resolved that same
// bar's own intrabar price action (see Strategy.OnBar's own doc
// comment). This rule now implements InitialStopProvider: its initial
// probation stop is computable from the SMA value at the entry
// *decision* bar, before the fill, so Strategy emits
// order.IntentEnterWithStop (ADR-059) instead of a plain
// order.IntentEnter, and the stop is already resting by the time the
// fill bar's own intrabar action is checked. trailingStopExitRule and
// smaCrossExitRule still have the gap in principle (their first stop
// genuinely depends on the fill/entry bar's own High, or manages no
// resting stop at all), but neither is the playbook's own
// probation/tight-stop-on-entry use case this issue exists for. See
// regression_test.go's own
// TestSMATrend_ProbationEntryBarBreachClosesSameBar for the
// regression proving the fix.
//
// Phase reports this rule's own current lifecycle state; Strategy
// mirrors it via Strategy.Phase (see phase.go) so it is directly
// observable/testable rather than staying private to this one rule.
type probationTrendExitRule struct {
	belowSMA       num.Rate // Config.InitialStopBelowSMA
	activationGain num.Rate // Config.TrailActivationGain
	retainFraction num.Rate // Config.StopFraction() — same TrailingStopPercent trailing-stop already uses

	phase         Phase
	entryPrice    num.Price
	highWaterMark *num.Price
	probationStop *num.Price
	trendStop     *num.Price
}

func newProbationTrendExitRule(cfg Config) (ExitRule, error) {
	retain, err := cfg.StopFraction()
	if err != nil {
		return nil, err
	}
	return &probationTrendExitRule{
		belowSMA:       cfg.InitialStopBelowSMA,
		activationGain: cfg.TrailActivationGain,
		retainFraction: retain,
	}, nil
}

// Phase implements the optional phaseReporter capability strategy.go
// reads from.
func (r *probationTrendExitRule) Phase() Phase { return r.phase }

func (r *probationTrendExitRule) OnEntry(entryBar marketdata.Bar, entryPrice num.Price) {
	r.phase = PhaseProbation
	r.entryPrice = entryPrice
	high := entryBar.High
	r.highWaterMark = &high
	r.probationStop = nil
	r.trendStop = nil
}

// InitialStop implements InitialStopProvider: the same probation-stop
// formula onProbationBar uses, computed from smaValue alone so it is
// knowable before the entry fill (issue #368) — never from the fill
// bar's own High/Low/Close, which is not yet known when Strategy asks
// for this value at the entry-decision bar.
func (r *probationTrendExitRule) InitialStop(smaValue float64) (num.Price, error) {
	stop, err := probationStopFromSMA(smaValue, r.belowSMA)
	if err != nil {
		return num.Price{}, fmt.Errorf("smatrend: computing initial probation stop: %w", err)
	}
	return stop, nil
}

// SeedInitialStop implements InitialStopSeeder (PR #369 review):
// called by Strategy immediately after OnEntry, only when a bracket
// entry was actually used, this overrides the nil OnEntry just
// established with the stop Strategy actually placed — the floor
// onProbationBar's own ratchet-only comparison
// (stop.Cmp(*r.probationStop) > 0) must never emit a decision that
// would loosen protection below.
func (r *probationTrendExitRule) SeedInitialStop(stop num.Price) {
	r.probationStop = &stop
}

func (r *probationTrendExitRule) OnLongBar(bar marketdata.Bar, smaValue float64) (ExitDecision, error) {
	// The high-water mark is tracked every bar regardless of phase:
	// the playbook's own trailing stop, once activated, is based on
	// the highest price reached since entry, not merely since
	// activation.
	if r.highWaterMark == nil || bar.High.Cmp(*r.highWaterMark) > 0 {
		high := bar.High
		r.highWaterMark = &high
	}

	switch r.phase {
	case PhaseProbation:
		return r.onProbationBar(bar, smaValue)
	case PhaseTrending:
		return r.onTrendingBar()
	default:
		return ExitDecision{}, fmt.Errorf("smatrend: probation-trend exit rule invoked outside an active phase (%s)", r.phase)
	}
}

func (r *probationTrendExitRule) onProbationBar(bar marketdata.Bar, smaValue float64) (ExitDecision, error) {
	// The SMA-cross override is independent of the protective stop:
	// exit outright, exactly like smaCrossExitRule, rather than
	// waiting for the (much tighter) stop to be touched.
	if bar.Close.Float64() <= smaValue {
		return ExitDecision{ExitNow: true}, nil
	}

	stop, err := probationStopFromSMA(smaValue, r.belowSMA)
	if err != nil {
		return ExitDecision{}, fmt.Errorf("smatrend: computing probation stop: %w", err)
	}

	var decision ExitDecision
	if r.probationStop == nil || stop.Cmp(*r.probationStop) > 0 {
		r.probationStop = &stop
		decision = ExitDecision{NewStop: &stop}
	}

	// Activation is checked after this bar's own stop decision above
	// (issue #349 review): the phase transition only takes effect
	// starting the next OnLongBar call, never this one.
	threshold, err := activationThreshold(r.entryPrice, r.activationGain)
	if err != nil {
		return ExitDecision{}, fmt.Errorf("smatrend: computing trail activation threshold: %w", err)
	}
	if bar.Close.Cmp(threshold) >= 0 {
		r.phase = PhaseTrending
		// Never-loosen handoff (issue #349 review): the high-water-mark
		// trailing stop computed from TRENDING's own formula can land
		// below the level PROBATION already protected (a wider trailing
		// percentage against a high-water mark that hasn't run up much
		// yet). Seeding TRENDING's own ratchet floor at the last
		// protected probation level — never nil here, since the early
		// return above for an SMA-cross exit is the only path that
		// reaches this point without first setting r.probationStop —
		// means onTrendingBar's existing ratchet-only comparison simply
		// emits nothing until its own HWM-derived stop actually exceeds
		// that floor, exactly like a normal missed ratchet. Graduating
		// out of PROBATION never increases dollar risk on the position.
		r.trendStop = r.probationStop
	}
	return decision, nil
}

func (r *probationTrendExitRule) onTrendingBar() (ExitDecision, error) {
	// r.highWaterMark is always non-nil here: OnEntry seeds it and
	// OnLongBar's own unconditional update runs before this is ever
	// reached.
	stop, err := r.highWaterMark.MulRate(r.retainFraction)
	if err != nil {
		return ExitDecision{}, fmt.Errorf("smatrend: computing trailing stop from high-water mark: %w", err)
	}
	if r.trendStop != nil && stop.Cmp(*r.trendStop) <= 0 {
		return ExitDecision{}, nil
	}
	r.trendStop = &stop
	return ExitDecision{NewStop: &stop}, nil
}

// activationThreshold returns entryPrice * (1 + gain), fully in the
// exact num domain — no float64 involved, since both entryPrice and
// gain are already exact.
func activationThreshold(entryPrice num.Price, gain num.Rate) (num.Price, error) {
	one := num.MustParseRate("1")
	factor, err := one.Add(gain)
	if err != nil {
		return num.Price{}, err
	}
	return entryPrice.MulRate(factor)
}

// probationStopFromSMA returns smaValue * (1 - belowSMA) as an exact
// num.Price.
func probationStopFromSMA(smaValue float64, belowSMA num.Rate) (num.Price, error) {
	smaPrice, err := priceFromFloat64(smaValue)
	if err != nil {
		return num.Price{}, fmt.Errorf("converting sma value to price: %w", err)
	}
	one := num.MustParseRate("1")
	retain, err := one.Sub(belowSMA)
	if err != nil {
		return num.Price{}, err
	}
	return smaPrice.MulRate(retain)
}

// priceFromFloat64 constructs a num.Price from an analytical float64
// result — needed here because indicator.SMA's own Value() is
// float64 (ADR-004's own sanctioned analytical domain), and the
// playbook's probation stop is defined directly in terms of the SMA
// value itself, not a bar's already-exact High/Close/Open/Low the
// way every other ExitRule computes its stop.
//
// ADR-045 sanctions an analytical float64 becoming authoritative
// again "through the normal checked, quantized, semantically-
// validated construction path" but adds no float64-to-exact
// constructor itself, instead pointing at "round to the listing's
// tick size, then construct via the type's own constructor" — an
// option unavailable here, since a strategy.Strategy implementation
// has no instrument.Listing/tick-size access at all (ADR-056 records
// this exact gap and resolves it one layer downstream instead:
// execution rounds any strategy-supplied AdjustStop price to the
// listing's tick before submission). This quantizes to num.Price's
// own native 1e8 scale via formatted decimal text — the same
// precision num.ParsePrice already accepts as input, adding no false
// precision beyond what float64 actually carried — rather than
// leaving the SMA value itself unquantized. This is the first case in
// this codebase needing this specific (float64 to exact) direction of
// conversion; flagged explicitly for architecture review in issue
// #349's own PR rather than treated as a fully settled boundary.
func priceFromFloat64(f float64) (num.Price, error) {
	return num.ParsePrice(strconv.FormatFloat(f, 'f', 8, 64))
}
