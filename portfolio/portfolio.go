package portfolio

import (
	"fmt"
	"time"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/num"
)

// ConversionStatus reports whether a Portfolio's Equity could be
// computed for every contributing account. Like order.Status, its zero
// value, ConversionUnknown, is reserved for an unconstructed Portfolio
// rather than silently meaning "complete" — NewPortfolio always sets it
// to ConversionComplete or ConversionIncomplete and never leaves it
// Unknown.
type ConversionStatus uint8

const (
	// ConversionUnknown is ConversionStatus's zero value.
	ConversionUnknown ConversionStatus = iota
	// ConversionComplete means every account's Equity was resolved into
	// the Portfolio's BaseCurrency.
	ConversionComplete
	// ConversionIncomplete means at least one account's currency had no
	// matching ConversionRate; see MissingCurrencies.
	ConversionIncomplete
)

// String returns a human-readable ConversionStatus name.
func (s ConversionStatus) String() string {
	switch s {
	case ConversionComplete:
		return "complete"
	case ConversionIncomplete:
		return "incomplete"
	default:
		return fmt.Sprintf("ConversionStatus(%d)", uint8(s))
	}
}

// Portfolio is a Trader-level view spanning one or more account.Snapshot
// values. It is immutable; construct one with NewPortfolio.
type Portfolio struct {
	baseCurrency num.Currency
	asOf         time.Time

	accounts []account.Snapshot

	conversionStatus  ConversionStatus
	conversionRates   []ConversionRate
	missingCurrencies []num.Currency
	equity            *num.Money

	exposures []Exposure
}

// PortfolioParams supplies NewPortfolio's input.
type PortfolioParams struct {
	// BaseCurrency is the reporting currency Equity is expressed in.
	BaseCurrency num.Currency
	// AsOf is when the caller assembled this view; see the package doc
	// comment for why this is distinct from any account's own AsOf.
	AsOf time.Time
	// Accounts is every account.Snapshot this Portfolio spans. Must be
	// non-empty, and no two entries may share an AccountID.
	Accounts []account.Snapshot
	// Rates supplies whatever exchange rates the caller has for
	// converting an account's currency into BaseCurrency. An account
	// already denominated in BaseCurrency needs no entry. At most one
	// entry per From currency is allowed.
	Rates []ConversionRate
}

// NewPortfolio validates params and returns an immutable Portfolio.
func NewPortfolio(params PortfolioParams) (Portfolio, error) {
	if !params.BaseCurrency.IsValid() {
		return Portfolio{}, fmt.Errorf("%w: base currency must be valid", ErrInvalidPortfolio)
	}
	if params.AsOf.IsZero() {
		return Portfolio{}, fmt.Errorf("%w: as-of time must be set", ErrInvalidPortfolio)
	}
	if len(params.Accounts) == 0 {
		return Portfolio{}, fmt.Errorf("%w: at least one account is required", ErrInvalidPortfolio)
	}

	accounts, err := checkAccounts(params.Accounts)
	if err != nil {
		return Portfolio{}, fmt.Errorf("%w: accounts: %v", ErrInvalidPortfolio, err)
	}

	rateByFrom, err := checkRates(params.Rates, params.BaseCurrency, params.AsOf)
	if err != nil {
		return Portfolio{}, fmt.Errorf("%w: rates: %v", ErrInvalidPortfolio, err)
	}

	status, usedRates, missing, equity, err := aggregateEquity(accounts, params.BaseCurrency, rateByFrom)
	if err != nil {
		return Portfolio{}, fmt.Errorf("%w: %v", ErrInvalidPortfolio, err)
	}

	return Portfolio{
		baseCurrency:      params.BaseCurrency,
		asOf:              params.AsOf,
		accounts:          accounts,
		conversionStatus:  status,
		conversionRates:   usedRates,
		missingCurrencies: missing,
		equity:            equity,
		exposures:         buildExposures(accounts),
	}, nil
}

func checkAccounts(accounts []account.Snapshot) ([]account.Snapshot, error) {
	seen := make(map[id.AccountID]struct{}, len(accounts))
	cloned := make([]account.Snapshot, len(accounts))
	for i, a := range accounts {
		if a.AccountID().IsZero() {
			return nil, fmt.Errorf("entry %d: account snapshot must be constructed with NewSnapshot", i)
		}
		if _, ok := seen[a.AccountID()]; ok {
			return nil, fmt.Errorf("entry %d: duplicate account id %s", i, a.AccountID())
		}
		seen[a.AccountID()] = struct{}{}
		cloned[i] = a
	}
	return cloned, nil
}

func checkRates(rates []ConversionRate, base num.Currency, portfolioAsOf time.Time) (map[num.Currency]ConversionRate, error) {
	byFrom := make(map[num.Currency]ConversionRate, len(rates))
	for i, r := range rates {
		if err := checkConversionRate(r, base, portfolioAsOf); err != nil {
			return nil, fmt.Errorf("entry %d: %w", i, err)
		}
		if _, ok := byFrom[r.From]; ok {
			return nil, fmt.Errorf("entry %d: duplicate rate for currency %s", i, r.From)
		}
		byFrom[r.From] = r
	}
	return byFrom, nil
}

// aggregateEquity converts and sums each account's Equity into base,
// using rateByFrom for any account not already denominated in base.
func aggregateEquity(accounts []account.Snapshot, base num.Currency, rateByFrom map[num.Currency]ConversionRate) (ConversionStatus, []ConversionRate, []num.Currency, *num.Money, error) {
	var (
		usedRates   []ConversionRate
		usedFrom    = make(map[num.Currency]struct{})
		missing     []num.Currency
		missingSeen = make(map[num.Currency]struct{})
		total       num.Money
		haveTotal   bool
	)

	for _, a := range accounts {
		cur := a.Currency()

		var converted num.Money
		switch {
		case cur.Equal(base):
			converted = a.Equity()
		default:
			rate, ok := rateByFrom[cur]
			if !ok {
				if _, seen := missingSeen[cur]; !seen {
					missingSeen[cur] = struct{}{}
					missing = append(missing, cur)
				}
				continue
			}
			var err error
			converted, err = a.Equity().Convert(base, rate.Rate)
			if err != nil {
				return ConversionUnknown, nil, nil, nil, fmt.Errorf("account %s: converting equity: %w", a.AccountID(), err)
			}
			if _, ok := usedFrom[cur]; !ok {
				usedFrom[cur] = struct{}{}
				usedRates = append(usedRates, rate)
			}
		}

		if !haveTotal {
			total = converted
			haveTotal = true
			continue
		}
		var err error
		total, err = total.Add(converted)
		if err != nil {
			return ConversionUnknown, nil, nil, nil, fmt.Errorf("summing equity: %w", err)
		}
	}

	if len(missing) > 0 {
		// usedRates only reflects accounts processed before the loop
		// found a missing currency, not a settled answer to "what was
		// used to compute Equity" — there is no such total in the
		// incomplete case, so ConversionRates must not return a
		// partial, possibly-misleading subset alongside it.
		return ConversionIncomplete, nil, missing, nil, nil
	}
	return ConversionComplete, usedRates, nil, &total, nil
}

// BaseCurrency is the reporting currency Equity is expressed in.
func (p Portfolio) BaseCurrency() num.Currency { return p.baseCurrency }

// AsOf is when this Portfolio was assembled.
func (p Portfolio) AsOf() time.Time { return p.asOf }

// Accounts returns a copy of the account snapshots this Portfolio
// spans, preserving each account's provenance. account.Snapshot is
// itself immutable, so copying the slice is sufficient — there is no
// mutable state a caller could reach through an element.
func (p Portfolio) Accounts() []account.Snapshot {
	return append([]account.Snapshot(nil), p.accounts...)
}

// AccountAsOfRange returns the oldest and newest AsOf among this
// Portfolio's accounts.
func (p Portfolio) AccountAsOfRange() (oldest, newest time.Time) {
	for i, a := range p.accounts {
		if i == 0 || a.AsOf().Before(oldest) {
			oldest = a.AsOf()
		}
		if i == 0 || a.AsOf().After(newest) {
			newest = a.AsOf()
		}
	}
	return oldest, newest
}

// ConversionStatus reports whether Equity could be computed for every
// account.
func (p Portfolio) ConversionStatus() ConversionStatus { return p.conversionStatus }

// MissingCurrencies names the currencies that blocked a complete
// conversion, in the order first encountered. Empty when
// ConversionStatus is ConversionComplete.
func (p Portfolio) MissingCurrencies() []num.Currency {
	return append([]num.Currency(nil), p.missingCurrencies...)
}

// ConversionRates returns a copy of the ConversionRate values actually
// used to compute Equity, one per distinct currency converted. Empty if
// every account was already in BaseCurrency, or if ConversionStatus is
// ConversionIncomplete.
func (p Portfolio) ConversionRates() []ConversionRate {
	return append([]ConversionRate(nil), p.conversionRates...)
}

// Equity returns the sum of every account's Equity, converted into
// BaseCurrency, and true — unless ConversionStatus is
// ConversionIncomplete, in which case it returns the zero Money and
// false rather than a partial or misleading total.
func (p Portfolio) Equity() (num.Money, bool) {
	if p.equity == nil {
		return num.Money{}, false
	}
	return *p.equity, true
}

// Exposures returns a deep copy of this Portfolio's per-instrument
// position groupings. See the package doc comment for why this is a
// grouping, not a valuation.
func (p Portfolio) Exposures() []Exposure {
	cloned := make([]Exposure, len(p.exposures))
	for i, e := range p.exposures {
		cloned[i] = Exposure{instrumentID: e.instrumentID, contributors: e.Contributors()}
	}
	return cloned
}
