package account

import (
	"fmt"
	"math"

	"github.com/rustyeddy/trader/idgen"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
)

// Account holds the financial state for a single trading account.
// All monetary values are scaled integers (types.Money = int64 × types.MoneyScale).
// Invariants that must hold after every operation:
//   - Equity = Balance + UnrealizedPL
//   - FreeMargin = Equity − MarginUsed
type Account struct {
	ID           string
	Name         string
	Currency     string      // account denomination (e.g. "USD")
	Balance      types.Money // realised cash; updated on every close
	Equity       types.Money // Balance + sum of unrealised P/L across open lots
	MarginUsed   types.Money // sum of margin reserved by open lots
	FreeMargin   types.Money // Equity − MarginUsed
	MarginLevel  types.Money // Equity / MarginUsed × types.MoneyScale (0 when flat)
	RiskFraction types.Rate  // fraction of equity risked per trade (e.g. 0.005 = 0.5 %)

	Lots   LotBook
	Trades []*Trade // closed trades, appended by CloseLot

	// evtQ is the order-filled/position-closed notification queue — see
	// account/events.go (SubmitOpen, SubmitClose, Events, ...). Lazily
	// initialized on first use, same as Lots.
	evtQ chan *Event
}

// NewAccount creates an Account with the given name and opening deposit.
// Currency defaults to "USD"; RiskFraction defaults to 0.5 %.
func NewAccount(name string, deposit types.Money) *Account {
	acct := &Account{
		ID:           idgen.NewULID(),
		Name:         name,
		Currency:     "USD",
		Balance:      deposit,
		Equity:       deposit,
		MarginUsed:   0.0,
		RiskFraction: types.RateFromFloat(0.005),
	}
	return acct
}

// quoteToAccountRate returns the current conversion rate from an instrument's
// quote currency into the account's base currency.
//
// It is used for position sizing and risk calculations when a price move
// denominated in quote currency must be expressed in account currency.
//
// Examples for a USD account:
//   - EURUSD -> 1.0
//   - USDJPY -> 1 / USDJPY
//   - EURGBP -> GBPUSD, or 1 / USDGBP if only the inverse exists
//
// The returned types.Rate is scaled by types.RateScale.
func (acct *Account) quoteToAccountRate(inst string, price types.Price) (types.Rate, error) {
	return quoteToAccountRateFor(acct.Currency, inst, price)
}

// quoteToAccountRateFor is quoteToAccountRate's currency-parameterized core,
// usable without a full Account (see account_sizing.go's SizingInputs).
func quoteToAccountRateFor(currency string, inst string, price types.Price) (types.Rate, error) {
	meta := market.GetInstrument(inst)
	if meta == nil {
		return 0, fmt.Errorf("unknown instrument: %s", inst)
	}
	if meta.QuoteCurrency == currency {
		return types.Rate(types.RateScale), nil
	}

	if meta.BaseCurrency == currency {
		r, err := types.MulDivCeil64(int64(types.MoneyScale), int64(types.PriceScale), int64(price))
		if err != nil {
			return 0, err
		}
		return types.Rate(r), nil
	}

	// Cross pair: neither quote nor base is the account currency.
	// Use a static approximate USD rate per currency. This introduces a
	// bounded error (~±30% over long backtests) on absolute dollar P/L but
	// does not affect win/loss decisions or relative return percentages.
	if currency == "USD" {
		if r, ok := market.ApproximateUSDPerUnit(meta.QuoteCurrency); ok {
			return r, nil
		}
	}
	return 0, fmt.Errorf("unsupported quote-to-account conversion: %s -> %s", meta.QuoteCurrency, currency)
}

// AddLot registers a newly opened lot with the account and immediately
// revalues all open positions at the lot's entry price.
func (acct *Account) AddLot(lot *Lot) error {
	if acct == nil {
		return fmt.Errorf("account is nil")
	}
	if err := lot.Validate(); err != nil {
		return err
	}
	if lot.Units <= 0 {
		return fmt.Errorf("position units must be > 0")
	}
	if lot.EntryPrice <= 0 {
		return fmt.Errorf("position entry price must be > 0")
	}

	if err := acct.Lots.Add(lot); err != nil {
		return err
	}
	return acct.ResolveWithMarks(map[string]types.Price{
		lot.Instrument: lot.EntryPrice,
	})
}

// CloseLot realizes P/L for the lot, appends the trade to the account's
// Trades history, removes the lot from the LotBook, and revalues remaining
// open lots at the exit price.
func (acct *Account) CloseLot(lot *Lot, trade *Trade) error {
	if acct == nil {
		return fmt.Errorf("account is nil")
	}
	if lot.Instrument == "" {
		return fmt.Errorf("position instrument must not be empty")
	}
	if trade.ExitPrice <= 0 {
		return fmt.Errorf("trade exit price must be > 0")
	}

	pnl, err := acct.realizePNL(lot, trade)
	if err != nil {
		return err
	}
	trade.PNL = pnl
	acct.Trades = append(acct.Trades, trade.Clone())
	acct.Lots.Delete(lot.ID)
	return acct.ResolveWithMarks(map[string]types.Price{lot.Instrument: trade.ExitPrice})
}

// lotUnrealizedPNL computes the open profit/loss for a single lot at the
// given mark price. qta is the quote-to-account rate (types.RateScale-scaled).
// The sign follows the lot's side: long gains when mark > entry; short gains
// when mark < entry.
func lotUnrealizedPNL(lot *Lot, mark types.Price, qta types.Rate) (types.Money, error) {
	if lot == nil {
		return 0, fmt.Errorf("nil position")
	}
	if lot.RemainingUnits <= 0 {
		return 0, fmt.Errorf("position %q has invalid units %d", lot.ID, lot.RemainingUnits)
	}
	if qta <= 0 {
		return 0, fmt.Errorf("invalid quote-to-account rate %d", qta)
	}

	priceDelta := int64(mark) - int64(lot.EntryPrice)
	if priceDelta == 0 {
		return 0, nil
	}

	absDelta, err := types.AbsInt64Checked(priceDelta)
	if err != nil {
		return 0, err
	}
	absUnits, err := types.AbsInt64Checked(int64(lot.RemainingUnits))
	if err != nil {
		return 0, err
	}

	deltaUnits, err := types.MulChecked64(absDelta, absUnits)
	if err != nil {
		return 0, err
	}

	whole := deltaUnits / int64(types.PriceScale)
	frac := deltaUnits % int64(types.PriceScale)

	base, err := types.MulChecked64(whole, int64(qta))
	if err != nil {
		return 0, err
	}

	fracNum, err := types.MulChecked64(frac, int64(qta))
	if err != nil {
		return 0, err
	}
	fracPart, err := types.RoundHalfAwayFromZero(fracNum, int64(types.PriceScale))
	if err != nil {
		return 0, err
	}

	if base > math.MaxInt64-fracPart {
		return 0, fmt.Errorf("position %q unrealized pnl overflow", lot.ID)
	}
	totalAbs := base + fracPart

	sign := int64(lot.Side)
	if sign != int64(types.Long) && sign != int64(types.Short) {
		return 0, fmt.Errorf("position %q has invalid side %d", lot.ID, lot.Side)
	}
	if priceDelta < 0 {
		sign = -sign
	}
	if sign < 0 {
		totalAbs = -totalAbs
	}

	return types.Money(totalAbs), nil
}

// ResolveWithMarks recomputes all account-level derived fields (Equity,
// MarginUsed, FreeMargin, MarginLevel) using the provided mark prices.
// If a lot's instrument has no entry in marks, the lot's EntryPrice is used.
// Pass nil to revalue everything at entry.
func (acct *Account) ResolveWithMarks(marks map[string]types.Price) error {
	if acct == nil {
		return fmt.Errorf("account is nil")
	}

	equity := acct.Balance
	var marginUsed types.Money

	err := acct.Lots.Range(func(lot *Lot) error {
		if lot.Instrument == "" {
			return fmt.Errorf("position %q has empty instrument", lot.ID)
		}
		if lot.RemainingUnits <= 0 {
			return fmt.Errorf("position %q has invalid units %d", lot.ID, lot.RemainingUnits)
		}
		if lot.EntryPrice <= 0 {
			return fmt.Errorf("position %q has invalid entry price %d", lot.ID, lot.EntryPrice)
		}

		mark := lot.EntryPrice
		if marks != nil {
			if px, ok := marks[lot.Instrument]; ok {
				if px <= 0 {
					return fmt.Errorf("invalid mark for %s: %d", lot.Instrument, px)
				}
				mark = px
			}
		}

		quoteToAccountRate, err := acct.quoteToAccountRate(lot.Instrument, mark)
		if err != nil {
			return err
		}

		pnl, err := lotUnrealizedPNL(lot, mark, quoteToAccountRate)
		if err != nil {
			return err
		}
		equity += pnl

		m, err := acct.marginRequired(lot.RemainingUnits, mark, lot.Instrument)
		if err != nil {
			return err
		}
		marginUsed += m

		return nil
	})
	if err != nil {
		return err
	}

	acct.Equity = equity
	acct.MarginUsed = marginUsed
	acct.FreeMargin = acct.Equity - acct.MarginUsed

	if acct.MarginUsed > 0 {
		v, err := types.SignedMulDivRound(int64(acct.Equity), int64(types.MoneyScale), int64(acct.MarginUsed))
		if err != nil {
			return err
		}
		acct.MarginLevel = types.Money(v)
	} else {
		acct.MarginLevel = 0
	}

	return nil
}

// realizePNL closes out a lot's unrealised P/L into the account Balance.
// It updates Balance and resets Equity to the new Balance (caller must call
// ResolveWithMarks to account for any remaining open lots afterwards).
// Returns the realised P/L amount.
func (acct *Account) realizePNL(lot *Lot, trade *Trade) (types.Money, error) {
	if acct == nil {
		return 0, fmt.Errorf("account is nil")
	}
	if lot == nil {
		return 0, fmt.Errorf("position is nil")
	}
	if trade == nil {
		return 0, fmt.Errorf("trade is nil")
	}
	if lot.Instrument == "" {
		return 0, fmt.Errorf("position instrument must not be empty")
	}
	if lot.RemainingUnits <= 0 {
		return 0, fmt.Errorf("position remaining units must be > 0")
	}
	if trade.ExitPrice <= 0 {
		return 0, fmt.Errorf("trade exit price must be > 0")
	}

	quoteToAccountRate, err := acct.quoteToAccountRate(lot.Instrument, trade.ExitPrice)
	if err != nil {
		return 0, err
	}

	pnlMoney, err := lotUnrealizedPNL(lot, trade.ExitPrice, quoteToAccountRate)
	if err != nil {
		return 0, err
	}

	acct.Balance += pnlMoney
	acct.Equity = acct.Balance

	return pnlMoney, nil
}

// marginRequired returns the margin required to hold a position of the given
// size at the given price for the named instrument, expressed in account
// currency (types.Money-scaled). It uses the instrument's MarginRate and the
// account's quote-to-account conversion.
func (acct *Account) marginRequired(units types.Units, price types.Price, inst string) (types.Money, error) {
	meta := market.GetInstrument(inst)
	if meta == nil {
		return 0, fmt.Errorf("unknown instrument: %s", inst)
	}

	if meta.MarginRate <= 0 {
		return 0, fmt.Errorf("invalid margin rate for %s: %d", meta.Name, meta.MarginRate)
	}

	u, err := types.AbsInt64Checked(int64(units))
	if err != nil {
		return 0, err
	}
	p := int64(price)
	if p <= 0 {
		return 0, fmt.Errorf("invalid price: %d", p)
	}

	up, err := types.MulDivCeil64(u, p, int64(types.PriceScale))
	if err != nil {
		return 0, err
	}
	notionalQuoteMicro, err := types.MulDivCeil64(up, int64(types.MoneyScale), 1)
	if err != nil {
		return 0, err
	}

	quoteToAccountRate, err := acct.quoteToAccountRate(meta.Name, price)
	if err != nil {
		return 0, err
	}

	notionalAcctMicro, err := types.MulDivCeil64(notionalQuoteMicro, int64(quoteToAccountRate), int64(types.RateScale))
	if err != nil {
		return 0, err
	}

	marginMicro, err := types.MulDivCeil64(notionalAcctMicro, int64(meta.MarginRate), int64(types.RateScale))
	if err != nil {
		return 0, err
	}

	return types.Money(marginMicro), nil
}
