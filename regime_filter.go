package trader

// RegimeFilter classifies the current market as trending or ranging.
// The bar loop calls Tick() every bar and suppresses new position opens
// when Trending() returns false.
type RegimeFilter interface {
	// Name returns a human-readable label for reports.
	Name() string

	// Ready reports whether the filter has enough history to classify.
	Ready() bool

	// Tick updates internal indicators with the current bar. The full
	// CandleTime is provided so implementations can use the timestamp
	// (e.g. to aggregate sub-daily bars into daily bars).
	Tick(ct CandleTime)

	// Trending returns true when the market is in a trending regime and
	// new entries should be allowed. Returns true while not yet ready so
	// warmup bars are not suppressed.
	Trending() bool

	// AllowSide returns true when new entries on the given side are permitted.
	// Trending() == false already blocks all opens; AllowSide provides
	// directional filtering when Trending() == true.
	AllowSide(side Side) bool
}

// NoopRegime is a pass-through filter that always allows trading.
type NoopRegime struct{}

func (NoopRegime) Name() string            { return "" }
func (NoopRegime) Ready() bool             { return true }
func (NoopRegime) Tick(_ CandleTime)       {}
func (NoopRegime) Trending() bool          { return true }
func (NoopRegime) AllowSide(_ Side) bool   { return true }
