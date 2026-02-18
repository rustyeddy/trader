package market

type Price = int32

type Candle struct {
	O Price
	H Price
	L Price
	C Price
}
