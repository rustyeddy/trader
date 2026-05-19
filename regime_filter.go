package trader

// RegimeFilter classifies the current market as trending or ranging.
// The bar loop calls Tick() every bar and suppresses new position opens
// when Trending() returns false.
type RegimeFilter interface {
	// Name returns a human-readable label for reports.
	Name() string

	// Ready reports whether the filter has enough history to classify.
	Ready() bool

	// Tick updates internal indicators. Called every bar.
	Tick(c Candle)

	// Trending returns true when the market is in a trending regime and
	// new entries should be allowed. Returns true while not yet ready so
	// warmup bars are not suppressed.
	Trending() bool
}

// NoopRegime is a pass-through filter that always allows trading.
type NoopRegime struct{}

func (NoopRegime) Name() string    { return "" }
func (NoopRegime) Ready() bool     { return true }
func (NoopRegime) Tick(_ Candle)   {}
func (NoopRegime) Trending() bool  { return true }
