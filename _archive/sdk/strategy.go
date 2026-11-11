package sdk

import "context"

// ConsumerBase is the lifecycle and identity contract every external
// consumer shares, whatever market-data delivery shape it uses. It is
// deliberately separate from BarHandler and BarsHandler so a consumer
// implements only the delivery callback it actually needs: a scanner
// or study never provides a dummy OnBar, and a bar-by-bar strategy
// never provides a dummy OnBars (issue #467).
type ConsumerBase interface {
	// Describe returns this consumer's identity and data
	// requirements, carried verbatim in Handshake.
	Describe() Descriptor

	// Start is called once, before any bar callback, with this
	// session's own Environment.
	Start(ctx context.Context, env Environment) error
}

// BarHandler is the single-bar delivery shape: one callback per
// completed bar a consumer required.
type BarHandler interface {
	// OnBar is called once per completed bar this consumer required,
	// after any configured warm-up period has elapsed. An empty
	// intents or signals slice is a valid, explicit "nothing this
	// bar" response, not a lack of one — the same convention
	// strategy.proto's own OnBarResponse documents. The consumer
	// returns described intents and described signals rather than
	// real, canonical ones: a guest never constructs an order.Intent
	// or a journal.Record directly.
	OnBar(ctx context.Context, event BarEvent, view View) ([]DescribedIntent, []DescribedSignal, error)
}

// BarsHandler is the multi-bar (snapshot) delivery shape: one callback
// per completed time boundary, carrying the whole subscribed universe.
// Response and failure rules are identical to BarHandler's.
type BarsHandler interface {
	OnBars(ctx context.Context, event BarsEvent, view View) ([]DescribedIntent, []DescribedSignal, error)
}

// BarConsumer is a bar-by-bar consumer — the traditional strategy
// shape, and the public contract for Serve's single-bar delivery mode.
type BarConsumer interface {
	ConsumerBase
	BarHandler
}

// BarsConsumer is a snapshot consumer — a scanner, study, or
// cross-sectional/portfolio strategy that evaluates a whole universe
// at one completed boundary.
type BarsConsumer interface {
	ConsumerBase
	BarsHandler
}

// Strategy is the original name of the bar-by-bar contract. It is kept
// as an alias of BarConsumer so existing SDK users compile unchanged.
type Strategy = BarConsumer

// FillHandler is an optional Strategy capability — sdk's own
// counterpart to strategy.FillHandler (ADR-060). Implementing it
// advertises CAPABILITY_FILL_HANDLER at Handshake; Serve negotiates it
// automatically based on whether the Strategy value passed to it
// implements this interface.
type FillHandler interface {
	OnFill(ctx context.Context, event FillEvent, view View) error
}
