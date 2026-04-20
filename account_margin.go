package trader

import (
	"fmt"
)

func TradeMargin(units Units, price Price, instrument string, quoteToAccount Rate) (Money, error) {
	meta, ok := Instruments[instrument]
	if !ok {
		return 0, fmt.Errorf("unknown instrument %s", instrument)
	}
	if quoteToAccount <= 0 {
		return 0, fmt.Errorf("invalid quoteToAccount rate: %d", quoteToAccount)
	}
	if meta.MarginRate <= 0 {
		return 0, fmt.Errorf("invalid margin rate for %s: %d", instrument, meta.MarginRate)
	}

	u := abs64(int64(units))
	p := int64(price)
	if p <= 0 {
		return 0, fmt.Errorf("invalid price: %d", p)
	}

	ms := int64(MoneyScale)

	up, err := mulDiv64(u, p, ms)
	if err != nil {
		return 0, err
	}
	notionalQuoteMicro, err := mulDiv64(up, ms, 1)
	if err != nil {
		return 0, err
	}

	notionalAcctMicro, err := mulDiv64(notionalQuoteMicro, int64(quoteToAccount), ms)
	if err != nil {
		return 0, err
	}

	marginMicro, err := mulDiv64(notionalAcctMicro, int64(meta.MarginRate), ms)
	if err != nil {
		return 0, err
	}

	return Money(marginMicro), nil
}
