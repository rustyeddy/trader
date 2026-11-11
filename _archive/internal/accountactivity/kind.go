package accountactivity

import "github.com/rustyeddy/trader/num"

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
	// KindOther is a provider event Trader does not model economically: an
	// order created or cancelled, a client-configuration change, anything an
	// importer does not recognize. It exists so no provider transaction is
	// lost: identity, time, provider type and broker order ID are kept, and
	// it carries no economics.
	KindOther
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
	KindOther:      "other",
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

// TradeRole is how a fill relates to a broker trade.
type TradeRole uint8

const (
	// TradeRoleUnknown is the zero value and is never valid.
	TradeRoleUnknown TradeRole = iota
	// TradeRoleOpened means the fill opened the trade.
	TradeRoleOpened
	// TradeRoleReduced means the fill partly closed the trade.
	TradeRoleReduced
	// TradeRoleClosed means the fill closed the trade.
	TradeRoleClosed
)

// String returns the role's lowercase name.
func (r TradeRole) String() string {
	switch r {
	case TradeRoleOpened:
		return "opened"
	case TradeRoleReduced:
		return "reduced"
	case TradeRoleClosed:
		return "closed"
	default:
		return "unknown"
	}
}

// Valid reports whether r is opened, reduced or closed.
func (r TradeRole) Valid() bool {
	return r == TradeRoleOpened || r == TradeRoleReduced || r == TradeRoleClosed
}

// TradeEffect is what a fill did to one broker trade: it opened it, reduced
// it, or closed it (a single fill can do several, such as closing two trades
// and opening a third). Beyond the trade's ID and the role it carries the
// execution facts the broker embeds for that trade, each with its own
// provenance and unknown when the broker did not supply it. The per-trade
// price is the broker's exact, official execution price for that part of the
// fill (ADR-081); the fill-level Price is not.
//
// An effect with only an ID and a role is valid.
type TradeEffect struct {
	BrokerTradeID string
	Role          TradeRole

	// Units is the absolute size of the effect.
	Units Value[num.Quantity]
	// Price is the exact execution price for this trade.
	Price Value[num.Price]
	// RealizedPL and Financing are realized by a reduce or close, signed from
	// the account's point of view.
	RealizedPL Value[num.Money]
	Financing  Value[num.Money]
	// SpreadCost keeps the sign the broker reported, like Activity.SpreadCost.
	SpreadCost Value[num.Money]
	// GuaranteedExecutionFee is a cost, negative (account-centric).
	GuaranteedExecutionFee Value[num.Money]
}
