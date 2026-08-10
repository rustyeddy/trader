package instrument

// Kind identifies the general category of economic instrument. See the
// package doc comment for why futures use two separate kinds rather than
// one "future" kind with a continuous/expiring flag.
type Kind uint8

const (
	// KindCurrencyPair is an FX pair such as EUR/USD.
	KindCurrencyPair Kind = iota + 1

	// KindEquity is a common stock.
	KindEquity

	// KindETF is an exchange-traded fund.
	KindETF

	// KindFuture is one specific expiring futures contract, such as the
	// December 2026 E-mini S&P 500 contract. Two contracts on the same
	// underlying root with different expirations are different
	// Instruments — see FutureID and NewFuture.
	KindFuture

	// KindContinuousSeries is a synthetic, non-orderable research series
	// derived from a futures family, such as a continuous, back-adjusted
	// ES series. It is never confused with an individual KindFuture
	// contract, even one sharing the same root.
	KindContinuousSeries

	// KindIndex is a non-orderable index.
	KindIndex
)

// String returns a human-readable name for k. Names are lowercase except
// where an initialism reads better uppercase, such as KindETF's "ETF".
func (k Kind) String() string {
	switch k {
	case KindCurrencyPair:
		return "currency pair"
	case KindEquity:
		return "equity"
	case KindETF:
		return "ETF"
	case KindFuture:
		return "future"
	case KindContinuousSeries:
		return "continuous series"
	case KindIndex:
		return "index"
	default:
		return "unknown instrument kind"
	}
}

// IsValid reports whether k is one of the defined Kind constants. The Go
// zero value of Kind is not valid.
func (k Kind) IsValid() bool {
	switch k {
	case KindCurrencyPair, KindEquity, KindETF, KindFuture, KindContinuousSeries, KindIndex:
		return true
	default:
		return false
	}
}
