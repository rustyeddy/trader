package trader

import (
	"fmt"

	"github.com/rustyeddy/trader/types"
)

func TradeMargin(units types.Units, price types.Price, instrument string, quoteToAccount types.Rate) (types.Money, error) {
	meta, ok := types.Instruments[instrument]
	if !ok {
		return 0, fmt.Errorf("unknown instrument %s", instrument)
	}
	if quoteToAccount <= 0 {
		return 0, fmt.Errorf("invalid quoteToAccount rate: %d", quoteToAccount)
	}
	if meta.MarginRate <= 0 {
		return 0, fmt.Errorf("invalid margin rate for %s: %d", instrument, meta.MarginRate)
	}

	u := types.Abs64(int64(units))
	p := int64(price)
	if p <= 0 {
		return 0, fmt.Errorf("invalid price: %d", p)
	}

	ms := int64(types.MoneyScale)

	up, err := types.MulDiv64(u, p, ms)
	if err != nil {
		return 0, err
	}
	notionalQuoteMicro, err := types.MulDiv64(up, ms, 1)
	if err != nil {
		return 0, err
	}

	notionalAcctMicro, err := types.MulDiv64(notionalQuoteMicro, int64(quoteToAccount), ms)
	if err != nil {
		return 0, err
	}

	marginMicro, err := types.MulDiv64(notionalAcctMicro, int64(meta.MarginRate), ms)
	if err != nil {
		return 0, err
	}

	return types.Money(marginMicro), nil
}
