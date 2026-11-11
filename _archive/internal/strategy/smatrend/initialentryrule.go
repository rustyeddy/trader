package smatrend

// InitialEntryContext is what an InitialEntryRule needs to decide
// whether this strategy's very first-ever entry (before it has ever
// held or exited a position) should fire on this bar.
type InitialEntryContext struct {
	// CrossedAboveSMA is true only on the bar the close genuinely
	// crosses from below to above the SMA this bar (crossState's own
	// tracked transition).
	CrossedAboveSMA bool
	// AboveSMA is true whenever the completed close is simply above
	// the SMA this bar, cross or no cross.
	AboveSMA bool
}

// InitialEntryRule decides, once per bar while flat and before this
// strategy has ever held/exited a position, whether to make its very
// first entry (issue #349 review). This is deliberately a separate
// decision from ReEntryRule, which only ever governs an entry that
// follows a prior exit — see Strategy.onFlat for exactly where the
// split happens.
type InitialEntryRule interface {
	ShouldEnter(ctx InitialEntryContext) bool
}

// DefaultInitialEntryModeName is the mode Config resolves to when
// InitialEntryModeName is left empty — EQS-01's own original startup
// behavior (issue #335), so every existing caller that predates issue
// #349's InitialEntryRule keeps identical behavior without editing a
// single Config literal.
const DefaultInitialEntryModeName = "fresh-cross"

// initialEntryRuleRegistry maps a Config.InitialEntryModeName to its
// constructor. A new mode is a new small type plus one entry here —
// never a change to Strategy's own control flow (issue #349).
var initialEntryRuleRegistry = map[string]func(Config) (InitialEntryRule, error){
	"fresh-cross": newFreshCrossInitialEntryRule,
	"above-sma":   newAboveSMAInitialEntryRule,
}

// freshCrossInitialEntryRule is EQS-01's own original, only startup
// behavior (issue #335): require a genuine cross from below to above
// the SMA before ever entering, even if the strategy starts already
// above it — meaning a run or live session that starts mid-trend may
// wait indefinitely for a pullback-and-recross before its first entry.
type freshCrossInitialEntryRule struct{}

func newFreshCrossInitialEntryRule(Config) (InitialEntryRule, error) {
	return freshCrossInitialEntryRule{}, nil
}

func (freshCrossInitialEntryRule) ShouldEnter(ctx InitialEntryContext) bool {
	return ctx.CrossedAboveSMA
}

// aboveSMAInitialEntryRule enters on the first bar the SMA is ready
// and the completed close is already above it, regardless of whether
// a genuine cross occurred this specific bar (issue #349 review): a
// long-hold strategy started mid-trend should not necessarily wait
// months or years for price to fall below the SMA and cross back
// above it merely because the run or live session happened to start
// today.
type aboveSMAInitialEntryRule struct{}

func newAboveSMAInitialEntryRule(Config) (InitialEntryRule, error) {
	return aboveSMAInitialEntryRule{}, nil
}

func (aboveSMAInitialEntryRule) ShouldEnter(ctx InitialEntryContext) bool {
	return ctx.AboveSMA
}
