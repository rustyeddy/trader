package accountactivity

// Kind classifies an Activity. Like journal.Kind it is Trader's own closed
// vocabulary, not a provider's, so the zero value is invalid. The provider's
// own type or code is kept verbatim in Activity.ProviderType; Kind is only
// the normalized meaning the import chose for it.
type Kind uint8

const (
	// KindUnknown is Kind's zero value and is never valid.
	KindUnknown Kind = iota
	// KindFill is an execution against an order: instrument, side, quantity
	// and execution price, with the costs the broker attributes to it.
	KindFill
	// KindFinancing is a financing or interest charge or credit.
	KindFinancing
	// KindFee is a commission or other fee not attached to a fill.
	KindFee
	// KindDeposit is cash added to the account.
	KindDeposit
	// KindWithdrawal is cash removed from the account.
	KindWithdrawal
	// KindDividend is a dividend or similar corporate-action cash credit or
	// charge.
	KindDividend
	// KindAdjustment is any other cash adjustment. The provider's type or
	// code is the only description, so it must be kept.
	KindAdjustment
)

var kindNames = [...]string{
	KindUnknown:    "unknown",
	KindFill:       "fill",
	KindFinancing:  "financing",
	KindFee:        "fee",
	KindDeposit:    "deposit",
	KindWithdrawal: "withdrawal",
	KindDividend:   "dividend",
	KindAdjustment: "adjustment",
}

// String returns the kind's lowercase name.
func (k Kind) String() string {
	if int(k) < len(kindNames) {
		return kindNames[k]
	}
	return "unknown"
}

// Valid reports whether k is a defined, non-zero kind.
func (k Kind) Valid() bool {
	return k > KindUnknown && int(k) < len(kindNames)
}

// Environment distinguishes a provider's practice and live systems, which
// share account-ID and activity-ID spaces only by coincidence: a practice
// activity must never collide with a live one.
type Environment uint8

const (
	// EnvironmentUnknown is the zero value and is never valid.
	EnvironmentUnknown Environment = iota
	// EnvironmentPractice is a provider's simulated system (OANDA fxTrade
	// Practice, Alpaca paper trading).
	EnvironmentPractice
	// EnvironmentLive is a provider's real-money system.
	EnvironmentLive
)

// String returns the environment's lowercase name.
func (e Environment) String() string {
	switch e {
	case EnvironmentPractice:
		return "practice"
	case EnvironmentLive:
		return "live"
	default:
		return "unknown"
	}
}

// Valid reports whether e is practice or live.
func (e Environment) Valid() bool {
	return e == EnvironmentPractice || e == EnvironmentLive
}
