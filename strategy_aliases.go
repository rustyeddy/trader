package trader

// Transition shim re-exporting the strategy package (strategy core, exit
// strategies, regime filters, and their config/param helpers — extracted from
// the root trader package) so existing callers compile unchanged during the
// migration. Remove entries as callers move to
// github.com/rustyeddy/trader/strategy directly. See docs/pkg-migration.org.

import "github.com/rustyeddy/trader/strategy"

// Strategy core.
type (
	Strategy            = strategy.Strategy
	StrategyContext     = strategy.StrategyContext
	LotView             = strategy.LotView
	StrategyPlan        = strategy.StrategyPlan
	StrategyConstructor = strategy.StrategyConstructor
	StrategyConfig      = strategy.StrategyConfig
)

// Exit strategies.
type (
	ExitStrategy   = strategy.ExitStrategy
	NoopExit       = strategy.NoopExit
	ExitConfig     = strategy.ExitConfig
	ChandelierExit = strategy.ChandelierExit
)

// Regime filters.
type (
	RegimeFilter          = strategy.RegimeFilter
	NoopRegime            = strategy.NoopRegime
	RegimeConfig          = strategy.RegimeConfig
	CompositeRegimeFilter = strategy.CompositeRegimeFilter
	ATRPercentileFilter   = strategy.ATRPercentileFilter
	ChoppinessFilter      = strategy.ChoppinessFilter
	D1ADXFilter           = strategy.D1ADXFilter
	D1ChoppinessFilter    = strategy.D1ChoppinessFilter
	SessionFilter         = strategy.SessionFilter
	WeeklyEMAFilter       = strategy.WeeklyEMAFilter
)

// Strategy plan helpers / registry.
var (
	DefaultStrategyPlan  = strategy.DefaultStrategyPlan
	DefaultPlan          = strategy.DefaultPlan
	HoldPlan             = strategy.HoldPlan
	RegisterStrategy     = strategy.RegisterStrategy
	MustRegisterStrategy = strategy.MustRegisterStrategy
	LookupStrategy       = strategy.LookupStrategy
	RegisteredStrategies = strategy.RegisteredStrategies
	GetStrategy          = strategy.GetStrategy
)

// Exit / regime factories and constructors.
var (
	GetExitStrategy          = strategy.GetExitStrategy
	NewChandelierExit        = strategy.NewChandelierExit
	GetRegimeFilter          = strategy.GetRegimeFilter
	NewCompositeRegimeFilter = strategy.NewCompositeRegimeFilter
	NewATRPercentileFilter   = strategy.NewATRPercentileFilter
	NewChoppinessFilter      = strategy.NewChoppinessFilter
	NewD1ADXFilter           = strategy.NewD1ADXFilter
	NewD1ChoppinessFilter    = strategy.NewD1ChoppinessFilter
	NewSessionFilter         = strategy.NewSessionFilter
	NewWeeklyEMAFilter       = strategy.NewWeeklyEMAFilter
)

// News-day helper and strategy param parsing.
var (
	LoadNewsDays    = strategy.LoadNewsDays
	GetIntParam     = strategy.GetIntParam
	GetInt32Param   = strategy.GetInt32Param
	GetFloat64Param = strategy.GetFloat64Param
	GetBoolParam    = strategy.GetBoolParam
	GetStringParam  = strategy.GetStringParam
)
