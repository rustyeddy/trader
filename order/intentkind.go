package order

import "fmt"

// IntentKind identifies a guest's requested trading action. It is a closed
// vocabulary; the zero value is invalid. The host constructs canonical
// runtime intents and owns sizing, execution, risk, and identity.
type IntentKind uint8

const (
	_ IntentKind = iota

	// IntentEnter expresses "open or increase a position in Side's
	// direction." It carries no Quantity — sizing is risk's
	// responsibility (ADR-006), not the intent's.
	IntentEnter

	// IntentExit expresses "remove whatever exposure currently exists
	// for Instrument," in whichever direction that happens to be. It
	// carries no Side or Quantity: the strategy does not need to know
	// the current position's direction or size to ask for it to be
	// closed.
	IntentExit

	// IntentAdjustStop expresses "move Instrument's protective stop to
	// StopPrice," an absolute price, not a broker-native trailing or
	// offset instruction.
	IntentAdjustStop

	// IntentTargetExposure expresses "reach exactly this position,"
	// carrying the desired absolute Side and Quantity rather than a
	// delta from whatever position currently exists.
	IntentTargetExposure

	// IntentEnterWithStop expresses "open a position in Side's
	// direction, and ensure it has a protective stop at StopPrice
	// active from the same fill" (issue #351, ADR-059) — a bracket
	// entry, closing the gap where a strategy's own first protective
	// stop (emitted only once the position is observed open, one bar
	// after the fill) leaves the entry bar itself completely
	// unprotected. Like IntentEnter, it carries no Quantity — sizing
	// remains risk's responsibility (ADR-006). StopPrice must be
	// computable before the fill (for example from an indicator value,
	// never from the real average fill price), since the two legs this
	// intent expands into are submitted before that fill price is
	// known to the caller.
	//
	IntentEnterWithStop
)

// String returns a human-readable IntentKind name.
func (k IntentKind) String() string {
	switch k {
	case IntentEnter:
		return "enter"
	case IntentExit:
		return "exit"
	case IntentAdjustStop:
		return "adjust_stop"
	case IntentTargetExposure:
		return "target_exposure"
	case IntentEnterWithStop:
		return "enter_with_stop"
	default:
		return fmt.Sprintf("IntentKind(%d)", uint8(k))
	}
}

// Valid reports whether the value belongs to the defined vocabulary.
func (k IntentKind) Valid() bool {
	switch k {
	case IntentEnter, IntentExit, IntentAdjustStop, IntentTargetExposure, IntentEnterWithStop:
		return true
	default:
		return false
	}
}
