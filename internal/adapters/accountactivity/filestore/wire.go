package filestore

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/internal/account"
	"github.com/rustyeddy/trader/internal/accountactivity"
	"github.com/rustyeddy/trader/internal/id"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

// Explicit wire structs, never reflection over the domain type: Value's
// fields are unexported, so a bare json.Marshal would silently drop the
// provenance this ledger exists to keep (the same discipline as the journal
// adapter's wire.go).

const schemaVersion = 1

type valueWire[T any] struct {
	Value  T      `json:"value"`
	Basis  string `json:"basis"`
	Source string `json:"source,omitempty"`
}

func toValueWire[T any](v accountactivity.Value[T]) *valueWire[T] {
	got, ok := v.Get()
	if !ok {
		return nil
	}
	return &valueWire[T]{Value: got, Basis: v.Basis().String(), Source: v.Source()}
}

func fromValueWire[T any](w *valueWire[T]) (accountactivity.Value[T], error) {
	if w == nil {
		return accountactivity.Value[T]{}, nil
	}
	switch w.Basis {
	case "reported":
		if w.Source == "" {
			return accountactivity.Reported(w.Value), nil
		}
		return accountactivity.ReportedFrom(w.Value, w.Source)
	case "derived":
		return accountactivity.Derived(w.Value, w.Source)
	default:
		return accountactivity.Value[T]{}, fmt.Errorf("value basis %q is not reported or derived", w.Basis)
	}
}

type tradeLinkWire struct {
	BrokerTradeID string `json:"broker_trade_id"`
	Role          string `json:"role"`
}

type accountRefWire struct {
	AccountID string `json:"account_id"`
	Broker    string `json:"broker"`
}

type activityWire struct {
	Schema             int    `json:"schema"`
	Provider           string `json:"provider"`
	Environment        string `json:"environment"`
	ProviderAccountID  string `json:"provider_account_id"`
	ProviderActivityID string `json:"provider_activity_id"`
	ProviderType       string `json:"provider_type"`
	Kind               string `json:"kind"`

	Account *accountRefWire `json:"account,omitempty"`

	OccurredAt time.Time `json:"occurred_at"`
	ImportedAt time.Time `json:"imported_at"`

	Instrument    string          `json:"instrument,omitempty"`
	Side          string          `json:"side,omitempty"`
	BrokerOrderID string          `json:"broker_order_id,omitempty"`
	TradeLinks    []tradeLinkWire `json:"trade_links,omitempty"`

	Quantity     *valueWire[num.Quantity] `json:"quantity,omitempty"`
	Price        *valueWire[num.Price]    `json:"price,omitempty"`
	RealizedPL   *valueWire[num.Money]    `json:"realized_pl,omitempty"`
	SpreadCost   *valueWire[num.Money]    `json:"spread_cost,omitempty"`
	Commission   *valueWire[num.Money]    `json:"commission,omitempty"`
	Financing    *valueWire[num.Money]    `json:"financing,omitempty"`
	OtherFees    *valueWire[num.Money]    `json:"other_fees,omitempty"`
	Cash         *valueWire[num.Money]    `json:"cash,omitempty"`
	Benchmark    *valueWire[num.Price]    `json:"benchmark,omitempty"`
	Slippage     *valueWire[num.Rate]     `json:"slippage,omitempty"`
	SlippageCost *valueWire[num.Money]    `json:"slippage_cost,omitempty"`
}

var kindByName = map[string]accountactivity.Kind{}

func init() {
	for k := accountactivity.KindFill; k.Valid(); k++ {
		kindByName[k.String()] = k
	}
}

func toWire(a accountactivity.Activity) activityWire {
	w := activityWire{
		Schema:             schemaVersion,
		Provider:           a.Provider,
		Environment:        a.Environment.String(),
		ProviderAccountID:  a.ProviderAccountID,
		ProviderActivityID: a.ProviderActivityID,
		ProviderType:       a.ProviderType,
		Kind:               a.Kind.String(),
		OccurredAt:         a.OccurredAt.UTC(),
		ImportedAt:         a.ImportedAt.UTC(),
		Instrument:         a.Instrument.String(),
		BrokerOrderID:      a.BrokerOrderID,
		Quantity:           toValueWire(a.Quantity),
		Price:              toValueWire(a.Price),
		RealizedPL:         toValueWire(a.RealizedPL),
		SpreadCost:         toValueWire(a.SpreadCost),
		Commission:         toValueWire(a.Commission),
		Financing:          toValueWire(a.Financing),
		OtherFees:          toValueWire(a.OtherFees),
		Cash:               toValueWire(a.Cash),
		Benchmark:          toValueWire(a.Benchmark),
		Slippage:           toValueWire(a.Slippage),
		SlippageCost:       toValueWire(a.SlippageCost),
	}
	if a.Side.Valid() {
		w.Side = a.Side.String()
	}
	if a.Account != (account.Reference{}) {
		w.Account = &accountRefWire{AccountID: a.Account.AccountID.String(), Broker: a.Account.Broker}
	}
	for _, l := range a.TradeLinks {
		w.TradeLinks = append(w.TradeLinks, tradeLinkWire{BrokerTradeID: l.BrokerTradeID, Role: l.Role.String()})
	}
	return w
}

var roleByName = map[string]accountactivity.TradeRole{
	"opened": accountactivity.TradeRoleOpened, "reduced": accountactivity.TradeRoleReduced, "closed": accountactivity.TradeRoleClosed,
}

func fromWire(w activityWire) (accountactivity.Activity, error) {
	if w.Schema != schemaVersion {
		return accountactivity.Activity{}, fmt.Errorf("unsupported schema %d (want %d)", w.Schema, schemaVersion)
	}
	a := accountactivity.Activity{
		Provider:           w.Provider,
		ProviderAccountID:  w.ProviderAccountID,
		ProviderActivityID: w.ProviderActivityID,
		ProviderType:       w.ProviderType,
		OccurredAt:         w.OccurredAt,
		ImportedAt:         w.ImportedAt,
		BrokerOrderID:      w.BrokerOrderID,
		Instrument:         instrument.ID{},
	}
	switch w.Environment {
	case "practice":
		a.Environment = accountactivity.EnvironmentPractice
	case "live":
		a.Environment = accountactivity.EnvironmentLive
	default:
		return a, fmt.Errorf("environment %q is not practice or live", w.Environment)
	}
	kind, ok := kindByName[w.Kind]
	if !ok {
		return a, fmt.Errorf("kind %q is unknown", w.Kind)
	}
	a.Kind = kind

	if w.Instrument != "" {
		if err := json.Unmarshal([]byte(fmt.Sprintf("%q", w.Instrument)), &a.Instrument); err != nil {
			return a, fmt.Errorf("instrument: %w", err)
		}
	}
	switch w.Side {
	case "":
	case "buy":
		a.Side = order.Buy
	case "sell":
		a.Side = order.Sell
	default:
		return a, fmt.Errorf("side %q is not buy or sell", w.Side)
	}
	if w.Account != nil {
		aid, err := id.ParseAccountID(w.Account.AccountID)
		if err != nil {
			return a, fmt.Errorf("account id: %w", err)
		}
		a.Account = account.Reference{AccountID: aid, Broker: w.Account.Broker}
	}
	for _, l := range w.TradeLinks {
		role, ok := roleByName[l.Role]
		if !ok {
			return a, fmt.Errorf("trade link role %q is unknown", l.Role)
		}
		a.TradeLinks = append(a.TradeLinks, accountactivity.TradeLink{BrokerTradeID: l.BrokerTradeID, Role: role})
	}

	var err error
	if a.Quantity, err = fromValueWire(w.Quantity); err != nil {
		return a, fmt.Errorf("quantity: %w", err)
	}
	if a.Price, err = fromValueWire(w.Price); err != nil {
		return a, fmt.Errorf("price: %w", err)
	}
	for _, f := range []struct {
		name string
		src  *valueWire[num.Money]
		dst  *accountactivity.Value[num.Money]
	}{
		{"realized_pl", w.RealizedPL, &a.RealizedPL}, {"spread_cost", w.SpreadCost, &a.SpreadCost},
		{"commission", w.Commission, &a.Commission}, {"financing", w.Financing, &a.Financing},
		{"other_fees", w.OtherFees, &a.OtherFees}, {"cash", w.Cash, &a.Cash},
		{"slippage_cost", w.SlippageCost, &a.SlippageCost},
	} {
		if *f.dst, err = fromValueWire(f.src); err != nil {
			return a, fmt.Errorf("%s: %w", f.name, err)
		}
	}
	if a.Benchmark, err = fromValueWire(w.Benchmark); err != nil {
		return a, fmt.Errorf("benchmark: %w", err)
	}
	if a.Slippage, err = fromValueWire(w.Slippage); err != nil {
		return a, fmt.Errorf("slippage: %w", err)
	}
	return a, nil
}

type checkpointWire struct {
	Schema            int       `json:"schema"`
	Provider          string    `json:"provider"`
	Environment       string    `json:"environment"`
	ProviderAccountID string    `json:"provider_account_id"`
	Cursor            string    `json:"cursor"`
	UpdatedAt         time.Time `json:"updated_at"`
}

func toCheckpointWire(cp accountactivity.Checkpoint) checkpointWire {
	return checkpointWire{
		Schema:            schemaVersion,
		Provider:          cp.Sync.Provider,
		Environment:       cp.Sync.Environment.String(),
		ProviderAccountID: cp.Sync.ProviderAccountID,
		Cursor:            cp.Cursor,
		UpdatedAt:         cp.UpdatedAt.UTC(),
	}
}

func fromCheckpointWire(w checkpointWire) (accountactivity.Checkpoint, error) {
	if w.Schema != schemaVersion {
		return accountactivity.Checkpoint{}, fmt.Errorf("unsupported schema %d (want %d)", w.Schema, schemaVersion)
	}
	var env accountactivity.Environment
	switch w.Environment {
	case "practice":
		env = accountactivity.EnvironmentPractice
	case "live":
		env = accountactivity.EnvironmentLive
	default:
		return accountactivity.Checkpoint{}, fmt.Errorf("environment %q is not practice or live", w.Environment)
	}
	return accountactivity.Checkpoint{
		Sync:      accountactivity.SyncKey{Provider: w.Provider, Environment: env, ProviderAccountID: w.ProviderAccountID},
		Cursor:    w.Cursor,
		UpdatedAt: w.UpdatedAt,
	}, nil
}
