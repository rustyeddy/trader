package accountactivity

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/internal/account"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

// Activity is one historical fact about a broker account, as the broker
// reported it (ADR-079). It is an immutable-by-convention value: build it,
// Validate it, then treat it as read-only.
//
// Activity is not a journal record and carries no RunID: imported history
// has no Trader run. It is also not a trade; trades are a view derived from
// activities.
//
// All Money fields are denominated in the account's home currency, and
// Validate requires every known one to share a currency.
type Activity struct {
	// Provider-native identity (Key). Provider is the lowercase canonical
	// provider name, for example "oanda".
	Provider           string
	Environment        Environment
	ProviderAccountID  string
	ProviderActivityID string

	// ProviderType is the provider's own transaction type or code, verbatim
	// (for example "ORDER_FILL"). Required, so an activity can always be
	// traced back to, and later re-normalized from, what the broker said.
	ProviderType string

	// Account optionally correlates the activity with a Trader account. The
	// zero value means no correlation. The provider-native identity above is
	// authoritative; this is additive linkage.
	Account account.Reference

	Kind Kind

	// OccurredAt is when the broker says the activity happened.
	OccurredAt time.Time
	// ImportedAt is when Trader observed it, from an injected clock. It is
	// ingestion metadata only: it is not part of Key and SameRecord ignores
	// it, so re-importing later is not a different record.
	ImportedAt time.Time

	// Instrument and Side apply to a fill, and Instrument may also qualify
	// financing or a fee.
	Instrument instrument.ID
	Side       order.Side

	Quantity Value[num.Quantity]
	// Price is the execution price.
	Price Value[num.Price]

	// BrokerOrderID preserves the broker's order link. It is optional and
	// not interpreted here.
	BrokerOrderID string
	// BrokerBatchID preserves the broker's grouping of transactions that
	// were created together (OANDA's batchID), so the grouping survives
	// import. Optional and not interpreted here.
	BrokerBatchID string
	// ProviderInstrument is the provider-native instrument name exactly as
	// the broker wrote it (for example "EUR_USD" or "SPX500_USD"). It is kept
	// beside the canonical Instrument, which is empty when Trader cannot yet
	// identify the instrument, so an event can be re-normalized later
	// without going back to the provider. Optional on every kind.
	ProviderInstrument string
	// TradeLinks preserve every broker trade a fill touched, with the
	// relationship: one OANDA ORDER_FILL can open one trade, reduce
	// another and close several, all under one provider activity ID.
	// Order is the provider's and is significant to SameRecord. Fill only.
	TradeLinks []TradeLink

	// Economics, all monetary fields signed from the account's point of
	// view (a cost is negative, a credit positive, zero neutral), in the
	// account home currency. This includes SlippageCost.
	RealizedPL Value[num.Money]
	// SpreadCost is the monetary cost of crossing the spread (OANDA
	// halfSpreadCost), not a bid/ask spread in price units. Unlike the other
	// monetary fields it keeps the sign the broker reported: OANDA defines
	// halfSpreadCost as either positive or negative, so it is not forced into
	// the account-centric convention (ADR-081).
	SpreadCost Value[num.Money]
	Commission Value[num.Money]
	Financing  Value[num.Money]
	OtherFees  Value[num.Money]
	// Cash is the signed effect of a cash activity on the balance:
	// positive for a deposit, negative for a withdrawal.
	Cash Value[num.Money]

	// Execution quality. Benchmark is the reference price slippage is
	// measured against; its source (Value.Source) is required and it must
	// be positive. Slippage is the difference in quote price units and
	// SlippageCost its monetary effect on the account. Both follow one
	// sign convention, the account's point of view: negative means the
	// execution was worse for the trader than the benchmark (a buy filled
	// above it, a sell below it), positive means better, zero neutral.
	// Validate rejects the two having strictly opposite signs. Both are
	// always derived (the broker does not report them) and either requires
	// a benchmark. num has no signed price type, so the price-unit
	// difference is a num.Rate. Deriving them is issue #513.
	Benchmark    Value[num.Price]
	Slippage     Value[num.Rate]
	SlippageCost Value[num.Money]
}

// Key returns a's provider-native identity.
func (a Activity) Key() Key {
	return Key{
		Provider:           a.Provider,
		Environment:        a.Environment,
		ProviderAccountID:  a.ProviderAccountID,
		ProviderActivityID: a.ProviderActivityID,
	}
}

// SameRecord reports whether a and o are the same economic record: equal in
// every field except ImportedAt, with times compared as instants.
func (a Activity) SameRecord(o Activity) bool {
	return reflect.DeepEqual(a.canonical(), o.canonical())
}

func (a Activity) canonical() Activity {
	a.ImportedAt = time.Time{}
	a.OccurredAt = a.OccurredAt.UTC().Round(0)
	if len(a.TradeLinks) == 0 {
		a.TradeLinks = nil
	}
	return a
}

// field is one optional Activity field, for the per-kind rules.
type field struct {
	name  string
	known bool
}

func (a Activity) fields() []field {
	return []field{
		{"instrument", !a.Instrument.IsZero()},
		{"trade_links", len(a.TradeLinks) > 0},
		{"side", a.Side != 0},
		{"quantity", a.Quantity.Known()},
		{"price", a.Price.Known()},
		{"realized_pl", a.RealizedPL.Known()},
		{"spread_cost", a.SpreadCost.Known()},
		{"commission", a.Commission.Known()},
		{"financing", a.Financing.Known()},
		{"other_fees", a.OtherFees.Known()},
		{"cash", a.Cash.Known()},
		{"benchmark", a.Benchmark.Known()},
		{"slippage", a.Slippage.Known()},
		{"slippage_cost", a.SlippageCost.Known()},
	}
}

// kindRules says which optional fields a kind requires and allows; any
// other field must be absent. The table is what keeps one struct from
// becoming an unvalidated bag of optionals.
var kindRules = map[Kind]struct{ required, allowed []string }{
	KindFill: {
		required: []string{"instrument", "side", "quantity", "price"},
		allowed:  []string{"trade_links", "realized_pl", "spread_cost", "commission", "financing", "other_fees", "benchmark", "slippage", "slippage_cost"},
	},
	KindFinancing: {
		required: []string{"financing"},
		allowed:  []string{"instrument"},
	},
	KindFee: {
		allowed: []string{"instrument", "commission", "other_fees"},
	},
	KindDeposit:    {required: []string{"cash"}},
	KindWithdrawal: {required: []string{"cash"}},
	KindDividend:   {required: []string{"cash"}, allowed: []string{"instrument"}},
	KindAdjustment: {required: []string{"cash"}, allowed: []string{"instrument"}},
	KindOther:      {allowed: []string{"instrument"}},
}

// Validate checks a's structure. It never rewrites a value. The returned
// error wraps ErrInvalidActivity and names the field and the rule, never an
// amount.
func (a Activity) Validate() error {
	bad := func(format string, args ...any) error {
		return fmt.Errorf("%w: %s", ErrInvalidActivity, fmt.Sprintf(format, args...))
	}

	if a.Provider == "" || a.Provider != strings.ToLower(strings.TrimSpace(a.Provider)) {
		return bad("provider must be a non-empty lowercase name")
	}
	if !a.Environment.Valid() {
		return bad("environment must be practice or live")
	}
	if a.ProviderAccountID == "" {
		return bad("provider account id must be set")
	}
	if a.ProviderActivityID == "" {
		return bad("provider activity id must be set")
	}
	if a.ProviderType == "" {
		return bad("provider type must be set")
	}
	if a.Account != (account.Reference{}) {
		if _, err := account.NewReference(a.Account); err != nil {
			return bad("account reference: %v", err)
		}
	}
	if !a.Kind.Valid() {
		return bad("kind must be set")
	}
	if a.OccurredAt.IsZero() {
		return bad("occurred_at must be set")
	}
	if a.ImportedAt.IsZero() {
		return bad("imported_at must be set (from an injected clock)")
	}
	if a.Side != 0 && !a.Side.Valid() {
		return bad("side is not buy or sell")
	}

	rules := kindRules[a.Kind]
	present := map[string]bool{}
	for _, f := range a.fields() {
		present[f.name] = f.known
	}
	for _, name := range rules.required {
		if !present[name] {
			return bad("%s requires %s", a.Kind, name)
		}
	}
	allowed := map[string]bool{}
	for _, n := range rules.required {
		allowed[n] = true
	}
	for _, n := range rules.allowed {
		allowed[n] = true
	}
	for _, f := range a.fields() {
		if f.known && !allowed[f.name] {
			return bad("%s does not apply to %s", f.name, a.Kind)
		}
	}

	if err := a.validateValues(bad); err != nil {
		return err
	}
	return a.validateKindSpecific(bad)
}

func (a Activity) validateValues(bad func(string, ...any) error) error {
	moneys := []struct {
		name string
		v    Value[num.Money]
	}{
		{"realized_pl", a.RealizedPL}, {"spread_cost", a.SpreadCost}, {"commission", a.Commission},
		{"financing", a.Financing}, {"other_fees", a.OtherFees}, {"cash", a.Cash},
		{"slippage_cost", a.SlippageCost},
	}
	var currency num.Currency
	for _, m := range moneys {
		amount, ok := m.v.Get()
		if !ok {
			continue
		}
		if !amount.IsValid() {
			return bad("%s has no currency", m.name)
		}
		if !currency.IsValid() {
			currency = amount.Currency()
		} else if !currency.Equal(amount.Currency()) {
			return bad("%s is not in the activity's currency %s", m.name, currency)
		}
	}

	if a.Quantity.Known() {
		if q, _ := a.Quantity.Get(); q.IsZero() {
			return bad("quantity must be positive")
		}
	}

	if p, ok := a.Price.Get(); ok && p.IsZero() {
		return bad("execution price must be positive")
	}
	if p, ok := a.Benchmark.Get(); ok && p.IsZero() {
		return bad("benchmark price must be positive")
	}
	seen := map[TradeLink]bool{}
	for _, l := range a.TradeLinks {
		if l.BrokerTradeID == "" {
			return bad("trade link needs a broker trade id")
		}
		if !l.Role.Valid() {
			return bad("trade link %q needs a role", l.BrokerTradeID)
		}
		if seen[l] {
			return bad("trade link %q is listed twice with role %s", l.BrokerTradeID, l.Role)
		}
		seen[l] = true
	}

	// A benchmark needs a stated source, whatever its basis.
	if a.Benchmark.Known() && strings.TrimSpace(a.Benchmark.Source()) == "" {
		return bad("benchmark requires a source")
	}
	if a.Slippage.Known() || a.SlippageCost.Known() {
		if !a.Benchmark.Known() {
			return bad("slippage requires a benchmark price and its source")
		}
		if !a.Price.Known() {
			return bad("slippage requires an execution price")
		}
	}
	if sl, ok := a.Slippage.Get(); ok {
		if cost, ok := a.SlippageCost.Get(); ok && cost.IsValid() && sl.Sign()*moneySign(cost) < 0 {
			return bad("slippage and slippage_cost have opposite signs")
		}
	}
	if a.Slippage.Basis() == BasisReported {
		return bad("slippage is derived by Trader and cannot be broker-reported")
	}
	if a.SlippageCost.Basis() == BasisReported {
		return bad("slippage_cost is derived by Trader and cannot be broker-reported")
	}
	return nil
}

func (a Activity) validateKindSpecific(bad func(string, ...any) error) error {
	switch a.Kind {
	case KindFee:
		if !a.Commission.Known() && !a.OtherFees.Known() {
			return bad("fee requires commission or other_fees")
		}
	case KindDeposit:
		if amount, _ := a.Cash.Get(); amount.IsValid() && !isPositive(amount) {
			return bad("deposit cash must be positive")
		}
	case KindWithdrawal:
		if amount, _ := a.Cash.Get(); amount.IsValid() && !isNegative(amount) {
			return bad("withdrawal cash must be negative")
		}
	}
	return nil
}

// moneySign returns -1, 0 or 1 for m, and 0 if m cannot be compared.
func moneySign(m num.Money) int {
	zero, err := num.ParseMoney("0", m.Currency())
	if err != nil {
		return 0
	}
	c, err := m.Cmp(zero)
	if err != nil {
		return 0
	}
	return c
}

func isPositive(m num.Money) bool { return moneySign(m) > 0 }

func isNegative(m num.Money) bool { return moneySign(m) < 0 }
