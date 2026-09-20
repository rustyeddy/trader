package sdk

import "context"

// Strategy is the contract an out-of-tree Go strategy implements to
// speak Strategy Protocol v1 through Serve — sdk's own
// counterpart to strategy.Strategy, deliberately not that interface
// itself (see the package doc comment). OnBar returns described
// intents and described signals rather than real, canonical ones: a
// guest never constructs an order.Intent or a journal.Record
// directly.
type Strategy interface {
	// Describe returns this strategy's identity and data
	// requirements, carried verbatim in Handshake.
	Describe() Descriptor

	// Start is called once, before any OnBar call, with this
	// session's own Environment.
	Start(ctx context.Context, env Environment) error

	// OnBar is called once per completed bar this strategy required,
	// after any configured warm-up period has elapsed. An empty
	// intents or signals slice is a valid, explicit "nothing this
	// bar" response, not a lack of one — the same convention
	// strategy.proto's own OnBarResponse documents.
	OnBar(ctx context.Context, event BarEvent, view View) ([]DescribedIntent, []DescribedSignal, error)
}

// FillHandler is an optional Strategy capability — sdk's own
// counterpart to strategy.FillHandler (ADR-060). Implementing it
// advertises CAPABILITY_FILL_HANDLER at Handshake; Serve negotiates it
// automatically based on whether the Strategy value passed to it
// implements this interface.
type FillHandler interface {
	OnFill(ctx context.Context, event FillEvent, view View) error
}
